package loan_test

import (
	"testing"
	"time"

	"github.com/michaeltansy/billing-engine/internal/loan"
)

var start = time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

func TestGenerateScheduleV1Terms(t *testing.T) {
	got := loan.GenerateSchedule(loan.Terms{
		Principal: 5000000, RateBps: 1000, TenorWeeks: 50, StartDate: start,
	})

	if got.TotalPayable != 5500000 {
		t.Errorf("total_payable = %d, want 5500000", got.TotalPayable)
	}
	if got.InstallmentAmount != 110000 {
		t.Errorf("installment_amount = %d, want 110000", got.InstallmentAmount)
	}
	if len(got.Installments) != 50 {
		t.Fatalf("installments = %d, want 50", len(got.Installments))
	}

	// Week 1 is due one week after the start date, not on it.
	first := got.Installments[0]
	wantFirst := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	if first.WeekNumber != 1 || !first.DueDate.Equal(wantFirst) {
		t.Errorf("week 1 due %s, want %s", first.DueDate, wantFirst)
	}

	last := got.Installments[49]
	if last.WeekNumber != 50 || last.Amount != 110000 {
		t.Errorf("week 50 = %+v, want week 50 at 110000", last)
	}
}

func TestScheduleSumsToTotalPayable(t *testing.T) {
	tests := []struct {
		name  string
		terms loan.Terms
	}{
		{"v1 terms, divides evenly", loan.Terms{Principal: 5000000, RateBps: 1000, TenorWeeks: 50, StartDate: start}},
		{"remainder of 1", loan.Terms{Principal: 1000001, RateBps: 0, TenorWeeks: 2, StartDate: start}},
		{"large remainder", loan.Terms{Principal: 1000000, RateBps: 1000, TenorWeeks: 7, StartDate: start}},
		{"prime tenor", loan.Terms{Principal: 3333333, RateBps: 1234, TenorWeeks: 13, StartDate: start}},
		{"zero interest", loan.Terms{Principal: 5000000, RateBps: 0, TenorWeeks: 50, StartDate: start}},
		{"single installment", loan.Terms{Principal: 999999, RateBps: 777, TenorWeeks: 1, StartDate: start}},
		{"worst-case remainder, tenor-1", loan.Terms{Principal: 99, RateBps: 0, TenorWeeks: 50, StartDate: start}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := loan.GenerateSchedule(tc.terms)

			var sum int64
			for _, inst := range got.Installments {
				sum += inst.Amount
			}

			if sum != got.TotalPayable {
				t.Errorf("sum(installments) = %d, want total_payable %d (drift %d)",
					sum, got.TotalPayable, got.TotalPayable-sum)
			}
			if len(got.Installments) != tc.terms.TenorWeeks {
				t.Errorf("installments = %d, want %d", len(got.Installments), tc.terms.TenorWeeks)
			}
		})
	}
}

func TestLastInstallmentAbsorbsRemainder(t *testing.T) {
	got := loan.GenerateSchedule(loan.Terms{
		Principal: 1000000, RateBps: 1000, TenorWeeks: 7, StartDate: start,
	})

	for i, inst := range got.Installments[:6] {
		if inst.Amount != 157142 {
			t.Errorf("week %d = %d, want the uniform 157142", i+1, inst.Amount)
		}
	}

	if last := got.Installments[6]; last.Amount != 157148 {
		t.Errorf("final week = %d, want 157148 (absorbing the remainder)", last.Amount)
	}
}

func TestDueDatesAreWeeklyUTCDates(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*60*60)
	got := loan.GenerateSchedule(loan.Terms{
		Principal: 5000000, RateBps: 1000, TenorWeeks: 5,
		StartDate: time.Date(2026, 7, 13, 18, 30, 0, 0, jakarta),
	})

	want := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	for _, inst := range got.Installments {
		if !inst.DueDate.Equal(want) {
			t.Errorf("week %d due %s, want %s", inst.WeekNumber, inst.DueDate, want)
		}
		if inst.DueDate.Location() != time.UTC {
			t.Errorf("week %d due date is in %s, want UTC", inst.WeekNumber, inst.DueDate.Location())
		}

		want = want.AddDate(0, 0, 7)
	}
}
