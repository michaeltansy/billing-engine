package service

import (
	"context"
	"time"

	"github.com/michaeltansy/billing-engine/internal/clock"
	"github.com/michaeltansy/billing-engine/internal/delinquency"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

type Snapshot struct {
	LoanStatus   loan.Status
	OverdueWeeks []int
}

type Service struct {
	dbStore DBStore
	clock   clock.Clock
}

//go:generate mockgen -source=./service.go -destination=./mock_dbstore.go -package=service github.com/michaeltansy/billing-engine/internal/delinquency/service DBStore
type DBStore interface {
	OverdueSnapshot(ctx context.Context, loanID int64, asOfDate time.Time) (Snapshot, error)
}

func (s *Service) GetDelinquency(ctx context.Context, r delinquency.Request) (delinquency.Response, error) {
	asOf := s.clock.Now().UTC()

	snapshot, err := s.dbStore.OverdueSnapshot(ctx, r.LoanID, clock.Today(s.clock))
	if err != nil {
		return delinquency.Response{}, err
	}

	return delinquency.Response{
		LoanID:       r.LoanID,
		IsDelinquent: isDelinquent(snapshot.LoanStatus, len(snapshot.OverdueWeeks)),
		OverdueWeeks: snapshot.OverdueWeeks,
		LoanStatus:   snapshot.LoanStatus,
		AsOf:         asOf,
	}, nil
}

func isDelinquent(status loan.Status, overdue int) bool {
	if overdue >= 2 {
		return true
	}

	return status == loan.StatusDelinquent && overdue >= 1
}

func NewService(dbStore DBStore, clk clock.Clock) *Service {
	return &Service{
		dbStore: dbStore,
		clock:   clk,
	}
}
