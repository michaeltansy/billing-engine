package delinquency

import (
	"context"
	"time"

	"github.com/michaeltansy/billing-engine/internal/loan"
)

type Request struct {
	LoanID int64
}

type Response struct {
	LoanID       int64
	IsDelinquent bool
	OverdueWeeks []int
	LoanStatus   loan.Status
	AsOf         time.Time
}

//go:generate mockgen -source=./delinquency.go -destination=mockservice/mock_service.go -package=mockservice github.com/michaeltansy/billing-engine/internal/delinquency Service
type Service interface {
	GetDelinquency(ctx context.Context, r Request) (Response, error)
}
