package dbstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/dbtest"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/loan/dbstore"
)

var seedStart = dbtest.Date(2026, 6, 22)

func TestCreateLoanWritesLoanAndFullSchedule(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	terms := dbtest.V1Terms(seedStart)
	schedule := loan.GenerateSchedule(terms)

	loanID, err := store.CreateLoan(context.Background(), terms, schedule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loanID == 0 {
		t.Fatal("loan id = 0, want a generated id")
	}

	var got struct {
		TotalPayable      int64       `db:"total_payable"`
		InstallmentAmount int64       `db:"installment_amount"`
		TenorWeeks        int         `db:"tenor_weeks"`
		Status            loan.Status `db:"status"`
	}
	err = db.Get(&got, `
		SELECT total_payable, installment_amount, tenor_weeks, status
		FROM loans WHERE id = $1`, loanID)
	if err != nil {
		t.Fatalf("reading back loan: %v", err)
	}

	if got.TotalPayable != 5500000 || got.InstallmentAmount != 110000 || got.TenorWeeks != 50 {
		t.Errorf("loan row = %+v, want 5500000 / 110000 / 50", got)
	}

	if got.Status != loan.StatusActive {
		t.Errorf("status = %s, want ACTIVE", got.Status)
	}

	var count int
	var sum int64
	if err := db.Get(&count, `SELECT count(*) FROM loan_installments WHERE loan_id = $1`, loanID); err != nil {
		t.Fatalf("counting installments: %v", err)
	}
	if err := db.Get(&sum, `SELECT COALESCE(SUM(amount), 0) FROM loan_installments WHERE loan_id = $1`, loanID); err != nil {
		t.Fatalf("summing installments: %v", err)
	}

	if count != 50 {
		t.Errorf("persisted installments = %d, want 50", count)
	}
	if sum != got.TotalPayable {
		t.Errorf("persisted installments sum to %d, want total_payable %d", sum, got.TotalPayable)
	}

	var pending int
	if err := db.Get(&pending, `
		SELECT count(*) FROM loan_installments
		WHERE loan_id = $1 AND status = 'PENDING' AND paid_at IS NULL AND payment_id IS NULL`, loanID); err != nil {
		t.Fatalf("counting pending: %v", err)
	}
	if pending != 50 {
		t.Errorf("pending installments = %d, want 50", pending)
	}
}

func TestCreateLoanPersistsRemainderInFinalWeek(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	terms := loan.Terms{Principal: 1000000, RateBps: 1000, TenorWeeks: 7, StartDate: seedStart}
	schedule := loan.GenerateSchedule(terms)

	loanID, err := store.CreateLoan(context.Background(), terms, schedule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var final int64
	if err := db.Get(&final, `
		SELECT amount FROM loan_installments WHERE loan_id = $1 AND week_number = 7`, loanID); err != nil {
		t.Fatalf("reading final installment: %v", err)
	}
	if final != 157_148 {
		t.Errorf("final week amount = %d, want 157148", final)
	}

	var sum int64
	if err := db.Get(&sum, `SELECT SUM(amount) FROM loan_installments WHERE loan_id = $1`, loanID); err != nil {
		t.Fatalf("summing: %v", err)
	}
	if sum != 1100000 {
		t.Errorf("installments sum to %d, want 1100000", sum)
	}
}

func TestCreateLoanRollsBackLoanWhenScheduleIsInvalid(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	terms := dbtest.V1Terms(seedStart)
	schedule := loan.GenerateSchedule(terms)

	schedule.Installments[10].Amount = 0

	if _, err := store.CreateLoan(context.Background(), terms, schedule); err == nil {
		t.Fatal("expected the invalid schedule to be rejected")
	}

	var loans, installments int
	if err := db.Get(&loans, `SELECT count(*) FROM loans`); err != nil {
		t.Fatalf("counting loans: %v", err)
	}
	if err := db.Get(&installments, `SELECT count(*) FROM loan_installments`); err != nil {
		t.Fatalf("counting installments: %v", err)
	}

	if loans != 0 {
		t.Errorf("loans = %d, want 0 — the loan row must roll back with its schedule", loans)
	}
	if installments != 0 {
		t.Errorf("installments = %d, want 0", installments)
	}
}

func TestGetScheduleReportsPerWeekStatus(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	loanID := dbtest.SeedLoan(t, db, dbtest.V1Terms(seedStart))
	dbtest.PayWeek(t, db, loanID, 1, 110000, "k1")
	dbtest.PayWeek(t, db, loanID, 2, 110000, "k2")
	dbtest.SetLoanStatus(t, db, loanID, loan.StatusDelinquent)

	got, err := store.GetSchedule(context.Background(), loanID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.LoanStatus != loan.StatusDelinquent {
		t.Errorf("loan_status = %s, want DELINQUENT", got.LoanStatus)
	}
	if len(got.Installments) != 50 {
		t.Fatalf("installments = %d, want 50", len(got.Installments))
	}

	// Ordered by week, with the settled weeks reading back PAID.
	for i, inst := range got.Installments {
		if inst.WeekNumber != i+1 {
			t.Fatalf("installments are not ordered by week: index %d holds week %d", i, inst.WeekNumber)
		}

		want := loan.InstallmentPending
		if inst.WeekNumber <= 2 {
			want = loan.InstallmentPaid
		}
		if inst.Status != want {
			t.Errorf("week %d status = %s, want %s", inst.WeekNumber, inst.Status, want)
		}
	}

	// Due dates survive the round trip as UTC calendar dates.
	if !got.Installments[0].DueDate.Equal(dbtest.Date(2026, 6, 29)) {
		t.Errorf("week 1 due %s, want 2026-06-29", got.Installments[0].DueDate)
	}
}

func TestGetScheduleLoanNotFound(t *testing.T) {
	db := dbtest.Connect(t)
	store := dbstore.NewDBStore(db)

	_, err := store.GetSchedule(context.Background(), 99999)
	if !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Fatalf("err = %v, want ErrLoanNotFound", err)
	}
}
