package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/clock"
	"github.com/michaeltansy/billing-engine/internal/delinquency"
	"github.com/michaeltansy/billing-engine/internal/delinquency/service"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

const loanID = int64(100)

func TestDelinquencyRule(t *testing.T) {
	tests := []struct {
		name         string
		status       loan.Status
		overdueWeeks []int
		want         bool
	}{
		{"current loan, nothing overdue", loan.StatusActive, nil, false},
		{"one overdue while active is not yet delinquent", loan.StatusActive, []int{3}, false},
		{"two overdue crosses into delinquency", loan.StatusActive, []int{3, 4}, true},
		{"three overdue stays delinquent", loan.StatusActive, []int{3, 4, 5}, true},

		// Hysteresis: one catch-up payment takes overdue 2 -> 1 but does not cure.
		{"hysteresis holds delinquent at one overdue", loan.StatusDelinquent, []int{4}, true},
		{"full cure returns to current", loan.StatusDelinquent, nil, false},

		// The never-payer: no payment has ever run, so the stored flag is a stale
		// ACTIVE. Only the derived clause catches this.
		{"never-payer with stale active flag", loan.StatusActive, []int{1, 2, 3, 4}, true},

		{"closed loan has no pending installments", loan.StatusClosed, nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			store := service.NewMockDBStore(ctrl)
			store.EXPECT().
				OverdueSnapshot(gomock.Any(), loanID, gomock.Any()).
				Return(service.Snapshot{LoanStatus: tc.status, OverdueWeeks: tc.overdueWeeks}, nil).
				Times(1)

			svc := service.NewService(store, clock.NewFake(time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)))

			got, err := svc.GetDelinquency(context.Background(), delinquency.Request{LoanID: loanID})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.IsDelinquent != tc.want {
				t.Errorf("is_delinquent = %v, want %v (status %s, overdue %v)",
					got.IsDelinquent, tc.want, tc.status, tc.overdueWeeks)
			}
			if got.LoanStatus != tc.status {
				t.Errorf("loan_status = %s, want %s", got.LoanStatus, tc.status)
			}
			if len(got.OverdueWeeks) != len(tc.overdueWeeks) {
				t.Errorf("overdue_weeks = %v, want %v", got.OverdueWeeks, tc.overdueWeeks)
			}
		})
	}
}

func TestOverdueBoundary(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "last payable second of the due day",
			now:  time.Date(2026, 7, 20, 23, 59, 59, 0, time.UTC),
			want: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "first overdue second, the day rolls over",
			now:  time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			store := service.NewMockDBStore(ctrl)
			store.EXPECT().
				OverdueSnapshot(gomock.Any(), loanID, tc.want).
				Return(service.Snapshot{LoanStatus: loan.StatusActive}, nil).
				Times(1)

			svc := service.NewService(store, clock.NewFake(tc.now))

			got, err := svc.GetDelinquency(context.Background(), delinquency.Request{LoanID: loanID})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !got.AsOf.Equal(tc.now) {
				t.Errorf("as_of = %s, want %s", got.AsOf, tc.now)
			}
		})
	}
}

func TestAsOfIsUTC(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*60*60)

	now := time.Date(2026, 7, 21, 6, 0, 0, 0, jakarta)
	wantDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	ctrl := gomock.NewController(t)

	store := service.NewMockDBStore(ctrl)
	store.EXPECT().
		OverdueSnapshot(gomock.Any(), loanID, wantDate).
		Return(service.Snapshot{LoanStatus: loan.StatusActive}, nil).
		Times(1)

	svc := service.NewService(store, clock.NewFake(now))

	got, err := svc.GetDelinquency(context.Background(), delinquency.Request{LoanID: loanID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AsOf.Location() != time.UTC {
		t.Errorf("as_of location = %s, want UTC", got.AsOf.Location())
	}
}

func TestGetDelinquencyPropagatesStoreError(t *testing.T) {
	ctrl := gomock.NewController(t)

	store := service.NewMockDBStore(ctrl)
	store.EXPECT().
		OverdueSnapshot(gomock.Any(), loanID, gomock.Any()).
		Return(service.Snapshot{}, apierr.ErrLoanNotFound).
		Times(1)

	svc := service.NewService(store, clock.NewFake(time.Now()))

	_, err := svc.GetDelinquency(context.Background(), delinquency.Request{LoanID: loanID})
	if !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Fatalf("err = %v, want ErrLoanNotFound", err)
	}
}
