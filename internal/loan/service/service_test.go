package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/loan/service"
)

var start = time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

func v1Terms() loan.Terms {
	return loan.Terms{Principal: 5000000, RateBps: 1000, TenorWeeks: loan.TenorWeeks, StartDate: start}
}

func clientTerms() loan.Terms {
	return loan.Terms{Principal: 5000000, RateBps: 1000, StartDate: start}
}

func TestCreateLoanForcesProductTenor(t *testing.T) {
	tests := []struct {
		name  string
		terms loan.Terms
	}{
		{"no tenor supplied", clientTerms()},
		{"caller tries a huge tenor", loan.Terms{Principal: 5000000, RateBps: 1000, TenorWeeks: 10000000, StartDate: start}},
		{"caller tries a negative tenor", loan.Terms{Principal: 5000000, RateBps: 1000, TenorWeeks: -1, StartDate: start}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			store := service.NewMockDBStore(ctrl)
			store.EXPECT().
				CreateLoan(gomock.Any(), v1Terms(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ loan.Terms, s loan.Schedule) (int64, error) {
					if len(s.Installments) != loan.TenorWeeks {
						t.Errorf("schedule has %d installments, want %d", len(s.Installments), loan.TenorWeeks)
					}
					return 100, nil
				}).
				Times(1)

			svc := service.NewService(store)

			got, err := svc.CreateLoan(context.Background(), loan.CreateRequest{Terms: tc.terms})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got.Installments) != loan.TenorWeeks {
				t.Errorf("installments = %d, want %d", len(got.Installments), loan.TenorWeeks)
			}
		})
	}
}

func TestCreateLoanGeneratesFullSchedule(t *testing.T) {
	ctrl := gomock.NewController(t)

	store := service.NewMockDBStore(ctrl)
	store.EXPECT().
		CreateLoan(gomock.Any(), v1Terms(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ loan.Terms, s loan.Schedule) (int64, error) {
			if len(s.Installments) != 50 {
				t.Errorf("store received %d installments, want 50", len(s.Installments))
			}

			var sum int64
			for _, inst := range s.Installments {
				sum += inst.Amount
			}
			if sum != s.TotalPayable {
				t.Errorf("store received a schedule summing to %d, want %d", sum, s.TotalPayable)
			}

			return 100, nil
		}).
		Times(1)

	svc := service.NewService(store)

	got, err := svc.CreateLoan(context.Background(), loan.CreateRequest{Terms: v1Terms()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.LoanID != 100 {
		t.Errorf("loan_id = %d, want 100", got.LoanID)
	}
	if got.TotalPayable != 5500000 {
		t.Errorf("total_payable = %d, want 5500000", got.TotalPayable)
	}
	if got.LoanStatus != loan.StatusActive {
		t.Errorf("loan_status = %s, want ACTIVE", got.LoanStatus)
	}
	if len(got.Installments) != 50 {
		t.Errorf("installments = %d, want 50", len(got.Installments))
	}
}

func TestCreateLoanRejectsInvalidTerms(t *testing.T) {
	tests := []struct {
		name  string
		terms loan.Terms
	}{
		{"zero principal", loan.Terms{Principal: 0, RateBps: 1000, StartDate: start}},
		{"negative principal", loan.Terms{Principal: -5000000, RateBps: 1000, StartDate: start}},
		{"negative rate", loan.Terms{Principal: 5000000, RateBps: -1, StartDate: start}},
		{"missing start date", loan.Terms{Principal: 5000000, RateBps: 1000}},
		{"principal too small to split", loan.Terms{Principal: 49, RateBps: 0, StartDate: start}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			store := service.NewMockDBStore(ctrl)
			store.EXPECT().CreateLoan(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

			svc := service.NewService(store)

			_, err := svc.CreateLoan(context.Background(), loan.CreateRequest{Terms: tc.terms})
			if !errors.Is(err, apierr.ErrMalformedRequest) {
				t.Fatalf("err = %v, want ErrMalformedRequest", err)
			}
		})
	}
}

func TestCreateLoanAcceptsZeroInterest(t *testing.T) {
	ctrl := gomock.NewController(t)

	fromClient := loan.Terms{Principal: 5000000, RateBps: 0, StartDate: start}
	stamped := fromClient
	stamped.TenorWeeks = loan.TenorWeeks

	store := service.NewMockDBStore(ctrl)
	store.EXPECT().CreateLoan(gomock.Any(), stamped, gomock.Any()).Return(int64(1), nil).Times(1)

	svc := service.NewService(store)

	got, err := svc.CreateLoan(context.Background(), loan.CreateRequest{Terms: fromClient})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.TotalPayable != 5000000 {
		t.Errorf("total_payable = %d, want 5000000 (no interest)", got.TotalPayable)
	}
}

// GetSchedule reads back the materialised schedule, including weeks already paid.
// It must never regenerate: regeneration would reset every PAID week to PENDING.
func TestGetSchedule(t *testing.T) {
	ctrl := gomock.NewController(t)

	want := loan.ScheduleResponse{
		LoanID:     100,
		LoanStatus: loan.StatusDelinquent,
		Installments: []loan.ScheduleEntry{
			{WeekNumber: 1, DueDate: start, Amount: 110000, Status: loan.InstallmentPaid},
			{WeekNumber: 2, DueDate: start.AddDate(0, 0, 7), Amount: 110000, Status: loan.InstallmentPending},
		},
	}

	store := service.NewMockDBStore(ctrl)
	store.EXPECT().GetSchedule(gomock.Any(), int64(100)).Return(want, nil).Times(1)

	svc := service.NewService(store)

	got, err := svc.GetSchedule(context.Background(), loan.ScheduleRequest{LoanID: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.LoanStatus != loan.StatusDelinquent {
		t.Errorf("loan_status = %s, want DELINQUENT", got.LoanStatus)
	}
	if len(got.Installments) != 2 {
		t.Fatalf("installments = %d, want 2", len(got.Installments))
	}
	if got.Installments[0].Status != loan.InstallmentPaid {
		t.Errorf("week 1 status = %s, want PAID", got.Installments[0].Status)
	}
	if got.Installments[1].Status != loan.InstallmentPending {
		t.Errorf("week 2 status = %s, want PENDING", got.Installments[1].Status)
	}
}

func TestGetScheduleLoanNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	store := service.NewMockDBStore(ctrl)
	store.EXPECT().GetSchedule(gomock.Any(), int64(404)).
		Return(loan.ScheduleResponse{}, apierr.ErrLoanNotFound).Times(1)

	svc := service.NewService(store)

	_, err := svc.GetSchedule(context.Background(), loan.ScheduleRequest{LoanID: 404})
	if !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Fatalf("err = %v, want ErrLoanNotFound", err)
	}
}
