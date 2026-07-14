package dbstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/dbtest"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/payment/dbstore"
	"github.com/michaeltansy/billing-engine/internal/payment/service"
)

var (
	seedStart = dbtest.Date(2026, 6, 22)
	asOf      = dbtest.Date(2026, 7, 14)
)

func setup(t *testing.T) (*sqlx.DB, *dbstore.Store, int64) {
	t.Helper()

	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)
	loanID := dbtest.SeedLoan(t, db, dbtest.V1Terms(seedStart))

	return db, store, loanID
}

func TestFindPaymentByKey(t *testing.T) {
	db, store, loanID := setup(t)

	got, err := store.FindPaymentByKey(context.Background(), loanID, "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil for an unknown key", got)
	}

	paymentID := dbtest.PayWeek(t, db, loanID, 1, 110000, "k1")

	got, err = store.FindPaymentByKey(context.Background(), loanID, "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want the stored payment")
	}
	if got.ID != paymentID || got.WeekNumber != 1 || got.Amount != 110000 {
		t.Errorf("payment = %+v, want id %d week 1 amount 110000", got, paymentID)
	}
	if got.RequestHash != "hash-k1" {
		t.Errorf("request_hash = %q, want hash-k1", got.RequestHash)
	}
}

func TestFindPaymentByKeyIsScopedToLoan(t *testing.T) {
	db, store, loanID := setup(t)

	other := dbtest.SeedLoan(t, db, dbtest.V1Terms(seedStart))
	dbtest.PayWeek(t, db, other, 1, 110000, "shared-key")

	got, err := store.FindPaymentByKey(context.Background(), loanID, "shared-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil — that key belongs to another loan", got)
	}
}

func TestReplayStateRebuildsHistoricalBalance(t *testing.T) {
	db, store, loanID := setup(t)

	first := dbtest.PayWeek(t, db, loanID, 1, 110000, "k1")
	second := dbtest.PayWeek(t, db, loanID, 2, 110000, "k2")
	dbtest.PayWeek(t, db, loanID, 3, 110000, "k3")

	// Replaying the FIRST payment must report the balance after payment 1 only —
	// 5,390,000 — not the current balance, even though two more have landed since.
	got, err := store.ReplayState(context.Background(), loanID, first)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Outstanding != 5390000 {
		t.Errorf("replay of payment 1 outstanding = %d, want 5390000", got.Outstanding)
	}

	got, err = store.ReplayState(context.Background(), loanID, second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Outstanding != 5280000 {
		t.Errorf("replay of payment 2 outstanding = %d, want 5280000", got.Outstanding)
	}
	if got.LoanStatus != loan.StatusActive {
		t.Errorf("loan_status = %s, want ACTIVE", got.LoanStatus)
	}
}

func TestLockLoan(t *testing.T) {
	_, store, loanID := setup(t)

	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	got, err := tx.LockLoan(context.Background(), loanID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != loanID || got.Status != loan.StatusActive {
		t.Errorf("loan = %+v, want id %d ACTIVE", got, loanID)
	}
}

func TestLockLoanNotFound(t *testing.T) {
	_, store, _ := setup(t)

	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.LockLoan(context.Background(), 99999); !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Fatalf("err = %v, want ErrLoanNotFound", err)
	}
}

func TestOldestPendingInstallmentAdvances(t *testing.T) {
	db, store, loanID := setup(t)

	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	inst, err := tx.OldestPendingInstallment(context.Background(), loanID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.WeekNumber != 1 || inst.Amount != 110000 {
		t.Errorf("oldest pending = %+v, want week 1 at 110000", inst)
	}
	_ = tx.Rollback()

	dbtest.PayWeek(t, db, loanID, 1, 110000, "k1")

	tx2, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx2.Rollback() }()

	inst, err = tx2.OldestPendingInstallment(context.Background(), loanID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.WeekNumber != 2 {
		t.Errorf("oldest pending = week %d, want week 2", inst.WeekNumber)
	}
}

// A fully repaid loan has no pending week. That is an invariant violation for an
// open loan, so the store reports it distinctly rather than returning a zero row.
func TestOldestPendingInstallmentNoneLeft(t *testing.T) {
	db, store, loanID := setup(t)

	for week := 1; week <= 50; week++ {
		dbtest.PayWeek(t, db, loanID, week, 110000, fmt.Sprintf("k%d", week))
	}

	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.OldestPendingInstallment(context.Background(), loanID)
	if !errors.Is(err, service.ErrNoPendingInstallment) {
		t.Fatalf("err = %v, want ErrNoPendingInstallment", err)
	}
}

func TestInsertPaymentTranslatesUniqueViolations(t *testing.T) {
	db, store, loanID := setup(t)

	dbtest.PayWeek(t, db, loanID, 1, 110000, "used-key")

	tests := []struct {
		name string
		p    service.NewPayment
		want error
	}{
		{
			name: "same idempotency key",
			p:    service.NewPayment{LoanID: loanID, IdemKey: "used-key", RequestHash: "h", Amount: 110000, WeekNumber: 2},
			want: service.ErrDuplicateKey,
		},
		{
			name: "week already settled",
			p:    service.NewPayment{LoanID: loanID, IdemKey: "fresh-key", RequestHash: "h", Amount: 110000, WeekNumber: 1},
			want: service.ErrDuplicateWeek,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := store.BeginTx(context.Background())
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback() }()

			_, err = tx.InsertPayment(context.Background(), tc.p)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTxAppliesPaymentAndRecomputes(t *testing.T) {
	db, store, loanID := setup(t)

	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	inst, err := tx.OldestPendingInstallment(ctx, loanID)
	if err != nil {
		t.Fatalf("oldest pending: %v", err)
	}

	paymentID, err := tx.InsertPayment(ctx, service.NewPayment{
		LoanID: loanID, IdemKey: "k1", RequestHash: "h1",
		Amount: inst.Amount, WeekNumber: inst.WeekNumber,
	})
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	if err := tx.MarkInstallmentPaid(ctx, loanID, inst.WeekNumber, paymentID, time.Now().UTC()); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	// Reads inside the transaction see this payment's effect.
	pending, err := tx.PendingTotal(ctx, loanID)
	if err != nil {
		t.Fatalf("pending total: %v", err)
	}
	if pending != 5_390_000 {
		t.Errorf("pending total = %d, want 5390000", pending)
	}

	overdue, err := tx.OverdueCount(ctx, loanID, asOf)
	if err != nil {
		t.Fatalf("overdue count: %v", err)
	}
	// Weeks 1..3 were overdue; week 1 is now paid, leaving 2 and 3.
	if overdue != 2 {
		t.Errorf("overdue = %d, want 2", overdue)
	}

	if err := tx.UpdateLoanStatus(ctx, loanID, loan.StatusDelinquent); err != nil {
		t.Fatalf("update status: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var status loan.Status
	if err := db.Get(&status, `SELECT status FROM loans WHERE id = $1`, loanID); err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if status != loan.StatusDelinquent {
		t.Errorf("committed status = %s, want DELINQUENT", status)
	}
}

func TestTxRollbackLeavesNoTrace(t *testing.T) {
	db, store, loanID := setup(t)

	ctx := context.Background()

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	paymentID, err := tx.InsertPayment(ctx, service.NewPayment{
		LoanID: loanID, IdemKey: "k1", RequestHash: "h1", Amount: 110000, WeekNumber: 1,
	})
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}
	if err := tx.MarkInstallmentPaid(ctx, loanID, 1, paymentID, time.Now().UTC()); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var payments, paid int
	if err := db.Get(&payments, `SELECT count(*) FROM payments WHERE loan_id = $1`, loanID); err != nil {
		t.Fatalf("counting payments: %v", err)
	}
	if err := db.Get(&paid, `SELECT count(*) FROM loan_installments WHERE loan_id = $1 AND status = 'PAID'`, loanID); err != nil {
		t.Fatalf("counting paid: %v", err)
	}

	if payments != 0 {
		t.Errorf("payments = %d, want 0 after rollback", payments)
	}
	if paid != 0 {
		t.Errorf("paid installments = %d, want 0 after rollback", paid)
	}

	got, err := store.FindPaymentByKey(ctx, loanID, "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("the rolled-back key still exists; it was burned")
	}
}

func TestRollbackAfterCommitIsSafe(t *testing.T) {
	_, store, loanID := setup(t)

	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.LockLoan(context.Background(), loanID); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Errorf("rollback after commit returned %v, want nil", err)
	}
}

func TestConcurrentPaymentsTakeDistinctWeeks(t *testing.T) {
	db, store, loanID := setup(t)

	const payers = 10

	var (
		start sync.WaitGroup
		done  sync.WaitGroup
	)
	start.Add(1)
	done.Add(payers)

	weeks := make([]int, payers)
	errs := make([]error, payers)

	for i := 0; i < payers; i++ {
		go func(i int) {
			defer done.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			start.Wait()

			tx, err := store.BeginTx(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer func() { _ = tx.Rollback() }()

			if _, err := tx.LockLoan(ctx, loanID); err != nil {
				errs[i] = err
				return
			}

			inst, err := tx.OldestPendingInstallment(ctx, loanID)
			if err != nil {
				errs[i] = err
				return
			}

			paymentID, err := tx.InsertPayment(ctx, service.NewPayment{
				LoanID: loanID, IdemKey: fmt.Sprintf("key-%d", i), RequestHash: "h",
				Amount: inst.Amount, WeekNumber: inst.WeekNumber,
			})
			if err != nil {
				errs[i] = err
				return
			}

			if err := tx.MarkInstallmentPaid(ctx, loanID, inst.WeekNumber, paymentID, time.Now().UTC()); err != nil {
				errs[i] = err
				return
			}

			if err := tx.Commit(); err != nil {
				errs[i] = err
				return
			}

			weeks[i] = inst.WeekNumber
		}(i)
	}

	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("payer %d failed: %v", i, err)
		}
	}

	seen := make(map[int]bool, payers)
	for i, w := range weeks {
		if w == 0 {
			t.Fatalf("payer %d settled no week", i)
		}
		if seen[w] {
			t.Fatalf("week %d was settled twice — the loan lock did not serialise payers", w)
		}
		seen[w] = true
	}

	for w := 1; w <= payers; w++ {
		if !seen[w] {
			t.Errorf("week %d was never settled; weeks taken: %v", w, weeks)
		}
	}

	var rows, paid int
	if err := db.Get(&rows, `SELECT count(*) FROM payments WHERE loan_id = $1`, loanID); err != nil {
		t.Fatalf("counting payments: %v", err)
	}
	if err := db.Get(&paid, `SELECT count(*) FROM loan_installments WHERE loan_id = $1 AND status = 'PAID'`, loanID); err != nil {
		t.Fatalf("counting paid: %v", err)
	}

	if rows != payers || paid != payers {
		t.Errorf("payments = %d, paid installments = %d, want %d each", rows, paid, payers)
	}
}

func TestConcurrentDuplicateKeyYieldsOneRow(t *testing.T) {
	db, store, loanID := setup(t)

	const attempts = 10

	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
	)
	start.Add(1)
	done.Add(attempts)

	var wins, dupes int

	for i := 0; i < attempts; i++ {
		go func() {
			defer done.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			start.Wait()

			tx, err := store.BeginTx(ctx)
			if err != nil {
				return
			}
			defer func() { _ = tx.Rollback() }()

			if _, err := tx.LockLoan(ctx, loanID); err != nil {
				return
			}

			inst, err := tx.OldestPendingInstallment(ctx, loanID)
			if err != nil {
				return
			}

			paymentID, err := tx.InsertPayment(ctx, service.NewPayment{
				LoanID: loanID, IdemKey: "same-key", RequestHash: "h",
				Amount: inst.Amount, WeekNumber: inst.WeekNumber,
			})
			if errors.Is(err, service.ErrDuplicateKey) {
				mu.Lock()
				dupes++
				mu.Unlock()
				return
			}
			if err != nil {
				return
			}

			if err := tx.MarkInstallmentPaid(ctx, loanID, inst.WeekNumber, paymentID, time.Now().UTC()); err != nil {
				return
			}
			if err := tx.Commit(); err != nil {
				return
			}

			mu.Lock()
			wins++
			mu.Unlock()
		}()
	}

	start.Done()
	done.Wait()

	if wins != 1 {
		t.Errorf("winners = %d, want exactly 1", wins)
	}
	if dupes != attempts-1 {
		t.Errorf("duplicate-key rejections = %d, want %d", dupes, attempts-1)
	}

	var rows, paid int
	if err := db.Get(&rows, `SELECT count(*) FROM payments WHERE loan_id = $1`, loanID); err != nil {
		t.Fatalf("counting payments: %v", err)
	}
	if err := db.Get(&paid, `SELECT count(*) FROM loan_installments WHERE loan_id = $1 AND status = 'PAID'`, loanID); err != nil {
		t.Fatalf("counting paid: %v", err)
	}

	if rows != 1 {
		t.Errorf("payment rows = %d, want exactly 1 — the borrower was charged %d times", rows, rows)
	}
	if paid != 1 {
		t.Errorf("paid installments = %d, want exactly 1", paid)
	}
}
