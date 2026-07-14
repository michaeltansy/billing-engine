package payment

import (
	"context"

	"github.com/michaeltansy/billing-engine/internal/loan"
)

type Request struct {
	LoanID  int64
	Amount  int64
	IdemKey string
}

type Response struct {
	PaymentID   int64
	WeekPaid    int
	Outstanding int64
	LoanStatus  loan.Status

	// Replayed marks a request served from an existing payment row rather than
	// applied afresh. The edge turns it into 200 instead of 201.
	Replayed bool
}

type Payment struct {
	ID          int64  `db:"id"`
	LoanID      int64  `db:"loan_id"`
	Amount      int64  `db:"amount"`
	WeekNumber  int    `db:"week_number"`
	RequestHash string `db:"request_hash"`
}

//go:generate mockgen -source=./payment.go -destination=mockservice/mock_service.go -package=mockservice github.com/michaeltansy/billing-engine/internal/payment Service
type Service interface {
	MakePayment(ctx context.Context, r Request) (Response, error)
}
