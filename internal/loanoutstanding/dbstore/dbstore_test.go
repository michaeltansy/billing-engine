package dbstore_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/dbtest"
	"github.com/michaeltansy/billing-engine/internal/loanoutstanding/dbstore"
)

func TestPendingTotal(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	loanID := dbtest.SeedLoan(t, db, dbtest.V1Terms(dbtest.Date(2026, 6, 22)))

	got, err := store.PendingTotal(context.Background(), loanID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 5_500_000 {
		t.Errorf("fresh loan outstanding = %d, want 5500000", got)
	}

	dbtest.PayWeek(t, db, loanID, 1, 110000, "k1")
	dbtest.PayWeek(t, db, loanID, 2, 110000, "k2")
	dbtest.PayWeek(t, db, loanID, 3, 110000, "k3")

	got, err = store.PendingTotal(context.Background(), loanID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 5_170_000 {
		t.Errorf("after 3 payments outstanding = %d, want 5170000", got)
	}
}

func TestPendingTotalFullyRepaidIsZeroNotNull(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	loanID := dbtest.SeedLoan(t, db, dbtest.V1Terms(dbtest.Date(2026, 6, 22)))

	for week := 1; week <= 50; week++ {
		dbtest.PayWeek(t, db, loanID, week, 110000, fmt.Sprintf("key-%d", week))
	}

	got, err := store.PendingTotal(context.Background(), loanID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("fully repaid outstanding = %d, want 0", got)
	}
}

func TestPendingTotalLoanNotFound(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	_, err := store.PendingTotal(context.Background(), 99999)
	if !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Fatalf("err = %v, want ErrLoanNotFound", err)
	}
}
