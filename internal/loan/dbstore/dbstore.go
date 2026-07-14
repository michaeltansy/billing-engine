package dbstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	database "github.com/michaeltansy/billing-engine/database/postgres"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

type Store struct {
	db *sqlx.DB
}

const insertLoanQuery = `
	INSERT INTO loans (principal, interest_rate_bps, total_payable, installment_amount, tenor_weeks, start_date, status)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING id`

const insertInstallmentsQuery = `
	INSERT INTO loan_installments (loan_id, week_number, due_date, amount, status)
	VALUES (:loan_id, :week_number, :due_date, :amount, 'PENDING')`

type installmentRow struct {
	LoanID     int64     `db:"loan_id"`
	WeekNumber int       `db:"week_number"`
	DueDate    time.Time `db:"due_date"`
	Amount     int64     `db:"amount"`
}

func (s *Store) CreateLoan(ctx context.Context, terms loan.Terms, schedule loan.Schedule) (int64, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, database.Translate(err)
	}

	defer func() { _ = tx.Rollback() }()

	var loanID int64

	err = tx.GetContext(ctx, &loanID, insertLoanQuery,
		terms.Principal,
		terms.RateBps,
		schedule.TotalPayable,
		schedule.InstallmentAmount,
		terms.TenorWeeks,
		terms.StartDate,
		loan.StatusActive,
	)
	if err != nil {
		return 0, database.Translate(err)
	}

	rows := make([]installmentRow, 0, len(schedule.Installments))
	for _, inst := range schedule.Installments {
		rows = append(rows, installmentRow{
			LoanID:     loanID,
			WeekNumber: inst.WeekNumber,
			DueDate:    inst.DueDate,
			Amount:     inst.Amount,
		})
	}

	res, err := tx.NamedExecContext(ctx, insertInstallmentsQuery, rows)
	if err != nil {
		return 0, database.Translate(err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return 0, database.Translate(err)
	}
	if affected != int64(len(rows)) {
		return 0, fmt.Errorf("loan %d: inserted %d installments, want %d", loanID, affected, len(rows))
	}

	if err := tx.Commit(); err != nil {
		return 0, database.Translate(err)
	}

	return loanID, nil
}

// The join predicate carries no filter, so a loan always yields at least one row.
// That is what distinguishes "loan exists" from "loan does not exist" — a WHERE on
// the installments would make an unknown loan and a schedule-less one look alike.
const getScheduleQuery = `
	SELECT
		l.status AS loan_status,
		i.week_number,
		i.due_date,
		i.amount,
		i.status AS installment_status
	FROM
		loans l
	LEFT JOIN
		loan_installments i
	ON
		i.loan_id = l.id
	WHERE
		l.id = $1
	ORDER BY
		i.week_number`

type scheduleRow struct {
	LoanStatus        loan.Status    `db:"loan_status"`
	WeekNumber        sql.NullInt64  `db:"week_number"`
	DueDate           sql.NullTime   `db:"due_date"`
	Amount            sql.NullInt64  `db:"amount"`
	InstallmentStatus sql.NullString `db:"installment_status"`
}

func (s *Store) GetSchedule(ctx context.Context, loanID int64) (loan.ScheduleResponse, error) {
	var rows []scheduleRow

	if err := s.db.SelectContext(ctx, &rows, getScheduleQuery, loanID); err != nil {
		return loan.ScheduleResponse{}, database.Translate(err)
	}
	if len(rows) == 0 {
		return loan.ScheduleResponse{}, apierr.ErrLoanNotFound
	}

	resp := loan.ScheduleResponse{
		LoanID:       loanID,
		LoanStatus:   rows[0].LoanStatus,
		Installments: make([]loan.ScheduleEntry, 0, len(rows)),
	}

	for _, row := range rows {
		// A loan with no installments at all is an invariant violation, not an empty
		// schedule — but it still reads as one NULL-bearing row, so skip it rather
		// than emit a zero-valued week.
		if !row.WeekNumber.Valid {
			continue
		}

		resp.Installments = append(resp.Installments, loan.ScheduleEntry{
			WeekNumber: int(row.WeekNumber.Int64),
			DueDate:    row.DueDate.Time,
			Amount:     row.Amount.Int64,
			Status:     loan.InstallmentStatus(row.InstallmentStatus.String),
		})
	}

	return resp, nil
}

func NewDBStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}
