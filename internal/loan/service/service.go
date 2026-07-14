package service

import (
	"context"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

type Service struct {
	dbStore DBStore
}

//go:generate mockgen -source=./service.go -destination=./mock_dbstore.go -package=service github.com/michaeltansy/billing-engine/internal/loan/service DBStore
type DBStore interface {
	CreateLoan(ctx context.Context, terms loan.Terms, schedule loan.Schedule) (int64, error)

	// GetSchedule returns every week of the loan with its settled state, ordered by
	// week. It returns apierr.ErrLoanNotFound when the loan does not exist.
	GetSchedule(ctx context.Context, loanID int64) (loan.ScheduleResponse, error)
}

// GetSchedule returns the loan's full schedule with per-week status (F5).
//
// This is a plain read of the materialised schedule, not a regeneration of it:
// the installments were fixed at creation, and weeks that have been paid carry
// that fact. Regenerating here would silently discard every payment.
func (s *Service) GetSchedule(ctx context.Context, r loan.ScheduleRequest) (loan.ScheduleResponse, error) {
	return s.dbStore.GetSchedule(ctx, r.LoanID)
}

func (s *Service) CreateLoan(ctx context.Context, r loan.CreateRequest) (loan.CreateResponse, error) {
	terms := r.Terms
	terms.TenorWeeks = loan.TenorWeeks

	if err := ValidateTerms(terms); err != nil {
		return loan.CreateResponse{}, err
	}

	schedule := loan.GenerateSchedule(terms)

	loanID, err := s.dbStore.CreateLoan(ctx, terms, schedule)
	if err != nil {
		return loan.CreateResponse{}, err
	}

	return loan.CreateResponse{
		LoanID:       loanID,
		TotalPayable: schedule.TotalPayable,
		LoanStatus:   loan.StatusActive,
		Installments: schedule.Installments,
	}, nil
}

func ValidateTerms(t loan.Terms) error {
	switch {
	case t.Principal <= 0:
		return apierr.Malformed("principal must be a positive integer")
	case t.RateBps < 0:
		return apierr.Malformed("rate_bps must not be negative")
	case t.StartDate.IsZero():
		return apierr.Malformed("start_date is required")
	}

	if loan.GenerateSchedule(t).InstallmentAmount <= 0 {
		return apierr.Malformed("principal is too small to split across the tenor")
	}

	return nil
}

func NewService(dbStore DBStore) *Service {
	return &Service{dbStore: dbStore}
}
