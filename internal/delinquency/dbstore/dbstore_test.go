package dbstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/dbtest"
	"github.com/michaeltansy/billing-engine/internal/delinquency/dbstore"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

var seedStart = dbtest.Date(2026, 6, 22)

func TestOverdueSnapshotCountsOnlyOverduePending(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	loanID := dbtest.SeedLoan(t, db, dbtest.V1Terms(seedStart))

	got, err := store.OverdueSnapshot(context.Background(), loanID, dbtest.Date(2026, 7, 14))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.LoanStatus != loan.StatusActive {
		t.Errorf("loan_status = %s, want ACTIVE", got.LoanStatus)
	}
	if len(got.OverdueWeeks) != 3 {
		t.Fatalf("overdue weeks = %v, want [1 2 3]", got.OverdueWeeks)
	}
	for i, want := range []int{1, 2, 3} {
		if got.OverdueWeeks[i] != want {
			t.Errorf("overdue[%d] = %d, want %d", i, got.OverdueWeeks[i], want)
		}
	}

	dbtest.PayWeek(t, db, loanID, 1, 110_000, "k1")

	got, err = store.OverdueSnapshot(context.Background(), loanID, dbtest.Date(2026, 7, 14))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.OverdueWeeks) != 2 || got.OverdueWeeks[0] != 2 {
		t.Errorf("after paying week 1, overdue = %v, want [2 3]", got.OverdueWeeks)
	}
}

func TestOverdueSnapshotDayBoundary(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	loanID := dbtest.SeedLoan(t, db, dbtest.V1Terms(seedStart))

	tests := []struct {
		name        string
		asOf        time.Time
		wantOverdue int
	}{
		{"the day before it is due", dbtest.Date(2026, 6, 28), 0},
		{"on the due day itself, still payable", dbtest.Date(2026, 6, 29), 0},
		{"the day after, now overdue", dbtest.Date(2026, 6, 30), 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.OverdueSnapshot(context.Background(), loanID, tc.asOf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got.OverdueWeeks) != tc.wantOverdue {
				t.Errorf("as of %s overdue = %v, want %d week(s)",
					tc.asOf.Format("2006-01-02"), got.OverdueWeeks, tc.wantOverdue)
			}
		})
	}
}

func TestOverdueSnapshotCurrentLoanIsNotNotFound(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	loanID := dbtest.SeedLoan(t, db, dbtest.V1Terms(seedStart))
	dbtest.SetLoanStatus(t, db, loanID, loan.StatusDelinquent)

	got, err := store.OverdueSnapshot(context.Background(), loanID, dbtest.Date(2026, 6, 1))
	if err != nil {
		t.Fatalf("a current loan must not error, got: %v", err)
	}

	if got.LoanStatus != loan.StatusDelinquent {
		t.Errorf("loan_status = %s, want DELINQUENT", got.LoanStatus)
	}
	if len(got.OverdueWeeks) != 0 {
		t.Errorf("overdue = %v, want none", got.OverdueWeeks)
	}
}

func TestOverdueSnapshotLoanNotFound(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	_, err := store.OverdueSnapshot(context.Background(), 99999, dbtest.Date(2026, 7, 14))
	if !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Fatalf("err = %v, want ErrLoanNotFound", err)
	}
}
