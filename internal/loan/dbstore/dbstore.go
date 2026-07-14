package dbstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	database "github.com/michaeltansy/billing-engine/database/postgres"
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

func NewDBStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}
