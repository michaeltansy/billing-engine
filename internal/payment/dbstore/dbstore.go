package dbstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	database "github.com/michaeltansy/billing-engine/database/postgres"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/payment"
	"github.com/michaeltansy/billing-engine/internal/payment/service"
)

type Store struct {
	db *sqlx.DB
}

const findPaymentByKeyQuery = `
	SELECT id, loan_id, amount, week_number, request_hash
	FROM payments
	WHERE loan_id = $1 AND idempotency_key = $2`

// FindPaymentByKey is the replay fast path: no locks, no transaction.
func (s *Store) FindPaymentByKey(ctx context.Context, loanID int64, key string) (*payment.Payment, error) {
	var p payment.Payment

	err := s.db.GetContext(ctx, &p, findPaymentByKeyQuery, loanID, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, database.Translate(err)
	}

	return &p, nil
}

const replayStateQuery = `
	SELECT
		l.total_payable - COALESCE((
			SELECT SUM(p.amount)
			FROM payments p
			WHERE p.loan_id = l.id AND p.id <= $2
		), 0) AS outstanding,
		l.status AS loan_status
	FROM loans l
	WHERE l.id = $1`

func (s *Store) ReplayState(ctx context.Context, loanID, paymentID int64) (service.ReplayState, error) {
	var row struct {
		Outstanding int64       `db:"outstanding"`
		LoanStatus  loan.Status `db:"loan_status"`
	}

	err := s.db.GetContext(ctx, &row, replayStateQuery, loanID, paymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ReplayState{}, apierr.ErrLoanNotFound
	}
	if err != nil {
		return service.ReplayState{}, database.Translate(err)
	}

	return service.ReplayState{Outstanding: row.Outstanding, LoanStatus: row.LoanStatus}, nil
}

func (s *Store) BeginTx(ctx context.Context) (service.Tx, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, database.Translate(err)
	}

	return &Tx{tx: tx}, nil
}

func NewDBStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

type Tx struct {
	tx *sqlx.Tx
}

const lockLoanQuery = `
	SELECT id, status
	FROM loans
	WHERE id = $1
	FOR UPDATE`

func (t *Tx) LockLoan(ctx context.Context, loanID int64) (service.LoanRow, error) {
	var row service.LoanRow

	err := t.tx.GetContext(ctx, &row, lockLoanQuery, loanID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.LoanRow{}, apierr.ErrLoanNotFound
	}
	if err != nil {
		return service.LoanRow{}, database.Translate(err)
	}

	return row, nil
}

const oldestPendingQuery = `
	SELECT week_number, amount
	FROM loan_installments
	WHERE loan_id = $1 AND status = 'PENDING'
	ORDER BY week_number
	LIMIT 1
	FOR UPDATE`

func (t *Tx) OldestPendingInstallment(ctx context.Context, loanID int64) (service.Installment, error) {
	var inst service.Installment

	err := t.tx.GetContext(ctx, &inst, oldestPendingQuery, loanID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.Installment{}, service.ErrNoPendingInstallment
	}
	if err != nil {
		return service.Installment{}, database.Translate(err)
	}

	return inst, nil
}

const insertPaymentQuery = `
	INSERT INTO payments (loan_id, idempotency_key, request_hash, amount, week_number)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING id`

func (t *Tx) InsertPayment(ctx context.Context, p service.NewPayment) (int64, error) {
	var id int64

	err := t.tx.GetContext(ctx, &id, insertPaymentQuery,
		p.LoanID, p.IdemKey, p.RequestHash, p.Amount, p.WeekNumber)
	if err != nil {
		if database.IsUniqueViolation(err, database.ConstraintPaymentsLoanKey) {
			return 0, service.ErrDuplicateKey
		}
		if database.IsUniqueViolation(err, database.ConstraintPaymentsLoanWeek) {
			return 0, service.ErrDuplicateWeek
		}
		return 0, database.Translate(err)
	}

	return id, nil
}

const markPaidQuery = `
	UPDATE loan_installments
	SET status = 'PAID', paid_at = $4, payment_id = $3
	WHERE loan_id = $1 AND week_number = $2 AND status = 'PENDING'`

func (t *Tx) MarkInstallmentPaid(ctx context.Context, loanID int64, week int, paymentID int64, paidAt time.Time) error {
	res, err := t.tx.ExecContext(ctx, markPaidQuery, loanID, week, paymentID, paidAt)
	if err != nil {
		return database.Translate(err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return database.Translate(err)
	}

	if rows != 1 {
		return fmt.Errorf("loan %d week %d: expected 1 pending installment, updated %d", loanID, week, rows)
	}

	return nil
}

const pendingTotalQuery = `
	SELECT COALESCE(SUM(amount), 0)
	FROM loan_installments
	WHERE loan_id = $1 AND status = 'PENDING'`

func (t *Tx) PendingTotal(ctx context.Context, loanID int64) (int64, error) {
	var total int64

	if err := t.tx.GetContext(ctx, &total, pendingTotalQuery, loanID); err != nil {
		return 0, database.Translate(err)
	}

	return total, nil
}

const overdueCountQuery = `
	SELECT COUNT(*)
	FROM loan_installments
	WHERE loan_id = $1 AND status = 'PENDING' AND due_date < $2`

func (t *Tx) OverdueCount(ctx context.Context, loanID int64, asOfDate time.Time) (int, error) {
	var count int

	if err := t.tx.GetContext(ctx, &count, overdueCountQuery, loanID, asOfDate); err != nil {
		return 0, database.Translate(err)
	}

	return count, nil
}

const updateLoanStatusQuery = `
	UPDATE loans
	SET status = $2
	WHERE id = $1`

func (t *Tx) UpdateLoanStatus(ctx context.Context, loanID int64, status loan.Status) error {
	if _, err := t.tx.ExecContext(ctx, updateLoanStatusQuery, loanID, status); err != nil {
		return database.Translate(err)
	}

	return nil
}

func (t *Tx) Commit() error {
	if err := t.tx.Commit(); err != nil {
		return database.Translate(err)
	}

	return nil
}

func (t *Tx) Rollback() error {
	if err := t.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return database.Translate(err)
	}

	return nil
}
