package service

import (
	"context"

	"github.com/michaeltansy/billing-engine/internal/loanoutstanding"
)

type Service struct {
	dbStore DBStore
}

//go:generate mockgen -source=./service.go -destination=./mock_dbstore.go -package=service github.com/michaeltansy/billing-engine/internal/loanoutstanding/service DBStore
type DBStore interface {
	// PendingTotal sums the loan's PENDING installments. It returns
	// apierr.ErrLoanNotFound when the loan does not exist.
	PendingTotal(ctx context.Context, loanID int64) (int64, error)
}

// GetOutstanding returns the sum of the loan's PENDING installments.
// A CLOSED loan has none, so it reports 0 without needing a status check.
func (s *Service) GetOutstanding(ctx context.Context, r loanoutstanding.Request) (loanoutstanding.Response, error) {
	outstanding, err := s.dbStore.PendingTotal(ctx, r.LoanID)
	if err != nil {
		return loanoutstanding.Response{}, err
	}

	return loanoutstanding.Response{
		LoanID:      r.LoanID,
		Outstanding: outstanding,
	}, nil
}

func NewService(dbStore DBStore) *Service {
	return &Service{dbStore: dbStore}
}
