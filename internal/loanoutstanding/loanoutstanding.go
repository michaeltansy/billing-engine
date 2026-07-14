package loanoutstanding

import (
	"context"
)

type Request struct {
	LoanID int64
}

type Response struct {
	LoanID      int64
	Outstanding int64
}

//go:generate mockgen -source=./loanoutstanding.go -destination=mockservice/mock_service.go -package=mockservice github.com/michaeltansy/billing-engine/internal/loanoutstanding Service
type Service interface {
	GetOutstanding(ctx context.Context, r Request) (Response, error)
}
