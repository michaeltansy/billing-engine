package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/clock"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/payment"
)

var (
	ErrDuplicateKey         = errors.New("payment: idempotency key already used for this loan")
	ErrDuplicateWeek        = errors.New("payment: week already settled")
	ErrNoPendingInstallment = errors.New("payment: open loan has no pending installment")
)

type LoanRow struct {
	ID     int64       `db:"id"`
	Status loan.Status `db:"status"`
}

type Installment struct {
	WeekNumber int   `db:"week_number"`
	Amount     int64 `db:"amount"`
}

type NewPayment struct {
	LoanID      int64
	IdemKey     string
	RequestHash string
	Amount      int64
	WeekNumber  int
}

type ReplayState struct {
	Outstanding int64
	LoanStatus  loan.Status
}

type Service struct {
	dbStore DBStore
	clock   clock.Clock
}

//go:generate mockgen -source=./service.go -destination=./mock_dbstore.go -package=service github.com/michaeltansy/billing-engine/internal/payment/service DBStore,Tx
type DBStore interface {
	// FindPaymentByKey returns the existing payment for (loanID, key), or nil when
	// there is none. It takes no locks: this is the replay fast path.
	FindPaymentByKey(ctx context.Context, loanID int64, key string) (*payment.Payment, error)

	// ReplayState rebuilds the result of an already-applied payment.
	ReplayState(ctx context.Context, loanID, paymentID int64) (ReplayState, error)

	BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
	LockLoan(ctx context.Context, loanID int64) (LoanRow, error)
	OldestPendingInstallment(ctx context.Context, loanID int64) (Installment, error)
	InsertPayment(ctx context.Context, p NewPayment) (int64, error)
	MarkInstallmentPaid(ctx context.Context, loanID int64, week int, paymentID int64, paidAt time.Time) error
	PendingTotal(ctx context.Context, loanID int64) (int64, error)
	OverdueCount(ctx context.Context, loanID int64, asOfDate time.Time) (int, error)
	UpdateLoanStatus(ctx context.Context, loanID int64, status loan.Status) error

	Commit() error
	Rollback() error
}

func (s *Service) MakePayment(ctx context.Context, r payment.Request) (payment.Response, error) {
	hash := requestHash(r.Amount)

	existing, err := s.dbStore.FindPaymentByKey(ctx, r.LoanID, r.IdemKey)
	if err != nil {
		return payment.Response{}, err
	}
	if existing != nil {
		return s.replay(ctx, existing, hash)
	}

	return s.apply(ctx, r, hash)
}

func (s *Service) apply(ctx context.Context, r payment.Request, hash string) (payment.Response, error) {
	tx, err := s.dbStore.BeginTx(ctx)
	if err != nil {
		return payment.Response{}, err
	}

	defer tx.Rollback()

	ln, err := tx.LockLoan(ctx, r.LoanID)
	if err != nil {
		return payment.Response{}, err
	}
	if ln.Status == loan.StatusClosed {
		return payment.Response{}, apierr.ErrLoanClosed
	}

	inst, err := tx.OldestPendingInstallment(ctx, r.LoanID)
	if err != nil {
		return payment.Response{}, err
	}

	if r.Amount != inst.Amount {
		return payment.Response{}, apierr.InvalidAmount(inst.Amount, r.Amount)
	}

	paymentID, err := tx.InsertPayment(ctx, NewPayment{
		LoanID:      r.LoanID,
		IdemKey:     r.IdemKey,
		RequestHash: hash,
		Amount:      r.Amount,
		WeekNumber:  inst.WeekNumber,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateKey) {
			if rbErr := tx.Rollback(); rbErr != nil {
				return payment.Response{}, rbErr
			}
			return s.replayByKey(ctx, r.LoanID, r.IdemKey, hash)
		}
		if errors.Is(err, ErrDuplicateWeek) {
			return payment.Response{}, fmt.Errorf("loan %d week %d: %w", r.LoanID, inst.WeekNumber, err)
		}
		return payment.Response{}, err
	}

	if err := tx.MarkInstallmentPaid(ctx, r.LoanID, inst.WeekNumber, paymentID, s.clock.Now().UTC()); err != nil {
		return payment.Response{}, err
	}

	pendingTotal, err := tx.PendingTotal(ctx, r.LoanID)
	if err != nil {
		return payment.Response{}, err
	}

	overdue, err := tx.OverdueCount(ctx, r.LoanID, clock.Today(s.clock))
	if err != nil {
		return payment.Response{}, err
	}

	next := NextStatus(ln.Status, pendingTotal, overdue)
	if next != ln.Status {
		if err := tx.UpdateLoanStatus(ctx, r.LoanID, next); err != nil {
			return payment.Response{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return payment.Response{}, err
	}

	return payment.Response{
		PaymentID:   paymentID,
		WeekPaid:    inst.WeekNumber,
		Outstanding: pendingTotal,
		LoanStatus:  next,
	}, nil
}

func (s *Service) replayByKey(ctx context.Context, loanID int64, key, hash string) (payment.Response, error) {
	existing, err := s.dbStore.FindPaymentByKey(ctx, loanID, key)
	if err != nil {
		return payment.Response{}, err
	}
	if existing == nil {
		return payment.Response{}, fmt.Errorf("loan %d key %q: %w", loanID, key, ErrDuplicateKey)
	}

	return s.replay(ctx, existing, hash)
}

func (s *Service) replay(ctx context.Context, existing *payment.Payment, hash string) (payment.Response, error) {
	if existing.RequestHash != hash {
		return payment.Response{}, apierr.ErrKeyReused
	}

	state, err := s.dbStore.ReplayState(ctx, existing.LoanID, existing.ID)
	if err != nil {
		return payment.Response{}, err
	}

	return payment.Response{
		PaymentID:   existing.ID,
		WeekPaid:    existing.WeekNumber,
		Outstanding: state.Outstanding,
		LoanStatus:  state.LoanStatus,
		Replayed:    true,
	}, nil
}

func NextStatus(prev loan.Status, pendingTotal int64, overdue int) loan.Status {
	switch {
	case pendingTotal == 0:
		return loan.StatusClosed
	case overdue == 0:
		return loan.StatusActive
	case overdue >= 2 || prev == loan.StatusDelinquent:
		return loan.StatusDelinquent
	default:
		return loan.StatusActive
	}
}

func requestHash(amount int64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "amount=%d", amount))
	return hex.EncodeToString(sum[:])
}

func NewService(dbStore DBStore, clk clock.Clock) *Service {
	return &Service{
		dbStore: dbStore,
		clock:   clk,
	}
}
