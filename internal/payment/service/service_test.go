package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/clock"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/payment"
	"github.com/michaeltansy/billing-engine/internal/payment/service"
)

const (
	loanID      = int64(100)
	installment = int64(110000)
)

var now = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

func newService(t *testing.T) (*service.Service, *service.MockDBStore, *service.MockTx) {
	t.Helper()

	ctrl := gomock.NewController(t)
	store := service.NewMockDBStore(ctrl)
	tx := service.NewMockTx(ctrl)

	return service.NewService(store, clock.NewFake(now)), store, tx
}

func TestNextStatus(t *testing.T) {
	tests := []struct {
		name         string
		prev         loan.Status
		pendingTotal int64
		overdue      int
		want         loan.Status
	}{
		{"final payment closes the loan", loan.StatusActive, 0, 0, loan.StatusClosed},
		{"final payment closes even while overdue", loan.StatusDelinquent, 0, 3, loan.StatusClosed},
		{"current loan stays active", loan.StatusActive, 5000000, 0, loan.StatusActive},
		{"one overdue does not enter delinquency", loan.StatusActive, 5000000, 1, loan.StatusActive},
		{"two overdue enters delinquency", loan.StatusActive, 5000000, 2, loan.StatusDelinquent},

		// Hysteresis: the catch-up payment that takes overdue 2 -> 1 does not cure.
		{"hysteresis holds at one overdue", loan.StatusDelinquent, 5000000, 1, loan.StatusDelinquent},
		{"full cure returns to active", loan.StatusDelinquent, 5000000, 0, loan.StatusActive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := service.NextStatus(tc.prev, tc.pendingTotal, tc.overdue)
			if got != tc.want {
				t.Errorf("NextStatus(%s, %d, %d) = %s, want %s",
					tc.prev, tc.pendingTotal, tc.overdue, got, tc.want)
			}
		})
	}
}

func applyExpectations(store *service.MockDBStore, tx *service.MockTx, prev loan.Status, week int, pendingAfter int64, overdueAfter int) {
	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-1").Return(nil, nil).Times(1)
	store.EXPECT().BeginTx(gomock.Any()).Return(tx, nil).Times(1)

	tx.EXPECT().LockLoan(gomock.Any(), loanID).Return(service.LoanRow{ID: loanID, Status: prev}, nil).Times(1)
	tx.EXPECT().OldestPendingInstallment(gomock.Any(), loanID).
		Return(service.Installment{WeekNumber: week, Amount: installment}, nil).Times(1)
	tx.EXPECT().InsertPayment(gomock.Any(), gomock.Any()).Return(int64(987), nil).Times(1)
	tx.EXPECT().MarkInstallmentPaid(gomock.Any(), loanID, week, int64(987), gomock.Any()).Return(nil).Times(1)
	tx.EXPECT().PendingTotal(gomock.Any(), loanID).Return(pendingAfter, nil).Times(1)
	tx.EXPECT().OverdueCount(gomock.Any(), loanID, gomock.Any()).Return(overdueAfter, nil).Times(1)
	tx.EXPECT().Commit().Return(nil).Times(1)
	tx.EXPECT().Rollback().Return(nil).AnyTimes()
}

func TestMakePaymentApplies(t *testing.T) {
	svc, store, tx := newService(t)

	applyExpectations(store, tx, loan.StatusActive, 3, 5170000, 0)
	// Status is unchanged (ACTIVE -> ACTIVE), so no write should occur.
	tx.EXPECT().UpdateLoanStatus(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	got, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Replayed {
		t.Error("replayed = true, want false for a fresh payment")
	}
	if got.WeekPaid != 3 {
		t.Errorf("week_paid = %d, want 3", got.WeekPaid)
	}
	if got.Outstanding != 5170000 {
		t.Errorf("outstanding = %d, want 5170000", got.Outstanding)
	}
	if got.LoanStatus != loan.StatusActive {
		t.Errorf("loan_status = %s, want ACTIVE", got.LoanStatus)
	}
}

func TestMakePaymentWritesStatusTransition(t *testing.T) {
	svc, store, tx := newService(t)

	applyExpectations(store, tx, loan.StatusActive, 3, 5170000, 2)
	tx.EXPECT().UpdateLoanStatus(gomock.Any(), loanID, loan.StatusDelinquent).Return(nil).Times(1)

	got, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.LoanStatus != loan.StatusDelinquent {
		t.Errorf("loan_status = %s, want DELINQUENT", got.LoanStatus)
	}
}

func TestMakePaymentClosesLoanOnFinalInstallment(t *testing.T) {
	svc, store, tx := newService(t)

	applyExpectations(store, tx, loan.StatusActive, 50, 0, 0)
	tx.EXPECT().UpdateLoanStatus(gomock.Any(), loanID, loan.StatusClosed).Return(nil).Times(1)

	got, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.LoanStatus != loan.StatusClosed {
		t.Errorf("loan_status = %s, want CLOSED", got.LoanStatus)
	}
	if got.Outstanding != 0 {
		t.Errorf("outstanding = %d, want 0", got.Outstanding)
	}
}

func TestMakePaymentRejectsWrongAmount(t *testing.T) {
	svc, store, tx := newService(t)

	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-1").Return(nil, nil).Times(1)
	store.EXPECT().BeginTx(gomock.Any()).Return(tx, nil).Times(1)
	tx.EXPECT().LockLoan(gomock.Any(), loanID).
		Return(service.LoanRow{ID: loanID, Status: loan.StatusActive}, nil).Times(1)
	tx.EXPECT().OldestPendingInstallment(gomock.Any(), loanID).
		Return(service.Installment{WeekNumber: 3, Amount: installment}, nil).Times(1)

	tx.EXPECT().InsertPayment(gomock.Any(), gomock.Any()).Times(0)
	tx.EXPECT().Commit().Times(0)
	tx.EXPECT().Rollback().Return(nil).Times(1)

	_, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: 220000, IdemKey: "key-1",
	})

	if !errors.Is(err, apierr.ErrInvalidAmount) {
		t.Fatalf("err = %v, want ErrInvalidAmount", err)
	}

	var domainErr *apierr.Error
	if !errors.As(err, &domainErr) {
		t.Fatal("error does not carry the expected/received details")
	}
}

func TestMakePaymentReplaysOnSameKeyAndPayload(t *testing.T) {
	svc, store, _ := newService(t)

	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-1").
		Return(&payment.Payment{
			ID: 987, LoanID: loanID, Amount: installment, WeekNumber: 3,
			RequestHash: hashOf(t, installment),
		}, nil).Times(1)
	store.EXPECT().ReplayState(gomock.Any(), loanID, int64(987)).
		Return(service.ReplayState{Outstanding: 5170000, LoanStatus: loan.StatusDelinquent}, nil).Times(1)

	// No transaction may be opened for a replay.
	store.EXPECT().BeginTx(gomock.Any()).Times(0)

	got, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Replayed {
		t.Error("replayed = false, want true")
	}
	if got.PaymentID != 987 || got.WeekPaid != 3 {
		t.Errorf("payment_id/week = %d/%d, want 987/3", got.PaymentID, got.WeekPaid)
	}
}

func TestMakePaymentRejectsKeyReuseWithDifferentPayload(t *testing.T) {
	svc, store, _ := newService(t)

	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-1").
		Return(&payment.Payment{
			ID: 987, LoanID: loanID, Amount: installment, WeekNumber: 3,
			RequestHash: hashOf(t, installment),
		}, nil).Times(1)
	store.EXPECT().BeginTx(gomock.Any()).Times(0)

	_, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: 999999, IdemKey: "key-1",
	})

	if !errors.Is(err, apierr.ErrKeyReused) {
		t.Fatalf("err = %v, want ErrKeyReused", err)
	}
}

func TestFinalPaymentRetryAfterCloseReplays(t *testing.T) {
	svc, store, _ := newService(t)

	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-final").
		Return(&payment.Payment{
			ID: 999, LoanID: loanID, Amount: installment, WeekNumber: 50,
			RequestHash: hashOf(t, installment),
		}, nil).Times(1)
	store.EXPECT().ReplayState(gomock.Any(), loanID, int64(999)).
		Return(service.ReplayState{Outstanding: 0, LoanStatus: loan.StatusClosed}, nil).Times(1)
	store.EXPECT().BeginTx(gomock.Any()).Times(0)

	got, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-final",
	})
	if err != nil {
		t.Fatalf("retried final payment must replay, got error: %v", err)
	}

	if !got.Replayed {
		t.Error("replayed = false, want true")
	}
	if got.LoanStatus != loan.StatusClosed {
		t.Errorf("loan_status = %s, want CLOSED", got.LoanStatus)
	}
}

func TestMakePaymentOnClosedLoanWithNewKey(t *testing.T) {
	svc, store, tx := newService(t)

	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-new").Return(nil, nil).Times(1)
	store.EXPECT().BeginTx(gomock.Any()).Return(tx, nil).Times(1)
	tx.EXPECT().LockLoan(gomock.Any(), loanID).
		Return(service.LoanRow{ID: loanID, Status: loan.StatusClosed}, nil).Times(1)
	tx.EXPECT().InsertPayment(gomock.Any(), gomock.Any()).Times(0)
	tx.EXPECT().Commit().Times(0)
	tx.EXPECT().Rollback().Return(nil).Times(1)

	_, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-new",
	})

	if !errors.Is(err, apierr.ErrLoanClosed) {
		t.Fatalf("err = %v, want ErrLoanClosed", err)
	}
}

func TestConcurrentDuplicateRollsBackAndReplays(t *testing.T) {
	svc, store, tx := newService(t)

	gomock.InOrder(
		store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-1").Return(nil, nil).Times(1),
		store.EXPECT().BeginTx(gomock.Any()).Return(tx, nil).Times(1),
		// Second lookup, after the constraint fires: now the winner's row is there.
		store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-1").
			Return(&payment.Payment{
				ID: 987, LoanID: loanID, Amount: installment, WeekNumber: 3,
				RequestHash: hashOf(t, installment),
			}, nil).Times(1),
	)

	tx.EXPECT().LockLoan(gomock.Any(), loanID).
		Return(service.LoanRow{ID: loanID, Status: loan.StatusActive}, nil).Times(1)
	tx.EXPECT().OldestPendingInstallment(gomock.Any(), loanID).
		Return(service.Installment{WeekNumber: 3, Amount: installment}, nil).Times(1)
	tx.EXPECT().InsertPayment(gomock.Any(), gomock.Any()).
		Return(int64(0), service.ErrDuplicateKey).Times(1)
	tx.EXPECT().Rollback().Return(nil).MinTimes(1)
	tx.EXPECT().Commit().Times(0)
	tx.EXPECT().MarkInstallmentPaid(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	store.EXPECT().ReplayState(gomock.Any(), loanID, int64(987)).
		Return(service.ReplayState{Outstanding: 5170000, LoanStatus: loan.StatusActive}, nil).Times(1)

	got, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Replayed || got.PaymentID != 987 {
		t.Errorf("got %+v, want replay of payment 987", got)
	}
}

func TestMakePaymentLoanNotFound(t *testing.T) {
	svc, store, tx := newService(t)

	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "key-1").Return(nil, nil).Times(1)
	store.EXPECT().BeginTx(gomock.Any()).Return(tx, nil).Times(1)
	tx.EXPECT().LockLoan(gomock.Any(), loanID).
		Return(service.LoanRow{}, apierr.ErrLoanNotFound).Times(1)
	tx.EXPECT().Rollback().Return(nil).Times(1)
	tx.EXPECT().Commit().Times(0)

	_, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: installment, IdemKey: "key-1",
	})

	if !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Fatalf("err = %v, want ErrLoanNotFound", err)
	}
}

func hashOf(t *testing.T, amount int64) string {
	t.Helper()

	ctrl := gomock.NewController(t)
	store := service.NewMockDBStore(ctrl)
	tx := service.NewMockTx(ctrl)

	var captured string

	store.EXPECT().FindPaymentByKey(gomock.Any(), loanID, "probe").Return(nil, nil).Times(1)
	store.EXPECT().BeginTx(gomock.Any()).Return(tx, nil).Times(1)
	tx.EXPECT().LockLoan(gomock.Any(), loanID).
		Return(service.LoanRow{ID: loanID, Status: loan.StatusActive}, nil).Times(1)
	tx.EXPECT().OldestPendingInstallment(gomock.Any(), loanID).
		Return(service.Installment{WeekNumber: 1, Amount: amount}, nil).Times(1)
	tx.EXPECT().InsertPayment(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p service.NewPayment) (int64, error) {
			captured = p.RequestHash
			return 1, nil
		}).Times(1)
	tx.EXPECT().MarkInstallmentPaid(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	tx.EXPECT().PendingTotal(gomock.Any(), loanID).Return(int64(1), nil).Times(1)
	tx.EXPECT().OverdueCount(gomock.Any(), loanID, gomock.Any()).Return(0, nil).Times(1)
	tx.EXPECT().UpdateLoanStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	tx.EXPECT().Commit().Return(nil).Times(1)
	tx.EXPECT().Rollback().Return(nil).AnyTimes()

	svc := service.NewService(store, clock.NewFake(now))
	if _, err := svc.MakePayment(context.Background(), payment.Request{
		LoanID: loanID, Amount: amount, IdemKey: "probe",
	}); err != nil {
		t.Fatalf("probing request hash: %v", err)
	}

	return captured
}
