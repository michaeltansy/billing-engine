package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loanoutstanding"
	"github.com/michaeltansy/billing-engine/internal/loanoutstanding/service"
)

func TestGetOutstanding(t *testing.T) {
	tests := []struct {
		name  string
		total int64
		want  int64
	}{
		{"fresh 50-week loan", 5500000, 5500000},
		{"three weeks paid", 5170000, 5170000},
		{"fully repaid loan reports zero", 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			store := service.NewMockDBStore(ctrl)
			store.EXPECT().
				PendingTotal(gomock.Any(), int64(100)).
				Return(tc.total, nil).
				Times(1)

			svc := service.NewService(store)

			got, err := svc.GetOutstanding(context.Background(), loanoutstanding.Request{LoanID: 100})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Outstanding != tc.want {
				t.Errorf("outstanding = %d, want %d", got.Outstanding, tc.want)
			}
			if got.LoanID != 100 {
				t.Errorf("loan id = %d, want 100", got.LoanID)
			}
		})
	}
}

func TestGetOutstandingPropagatesNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	store := service.NewMockDBStore(ctrl)
	store.EXPECT().
		PendingTotal(gomock.Any(), int64(999)).
		Return(int64(0), apierr.ErrLoanNotFound).
		Times(1)

	svc := service.NewService(store)

	_, err := svc.GetOutstanding(context.Background(), loanoutstanding.Request{LoanID: 999})

	if !errors.Is(err, apierr.ErrLoanNotFound) {
		t.Errorf("err = %v, want ErrLoanNotFound", err)
	}
}
