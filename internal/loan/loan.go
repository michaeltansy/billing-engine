package loan

import "context"

// Status is the stored lifecycle state of a loan.
//
// The stored DELINQUENT flag is only ever written inside a payment transaction,
// so it can be stale: a borrower who never pays at all keeps an ACTIVE row while
// their installments go overdue. Delinquency is therefore always re-derived at
// read time and never read straight off this field.
type Status string

const (
	StatusActive     Status = "ACTIVE"
	StatusDelinquent Status = "DELINQUENT"
	StatusClosed     Status = "CLOSED"
)

type Request struct {
	Terms Terms
}

type Response struct {
	LoanID       int64
	TotalPayable int64
	LoanStatus   Status
	Installments []Installment
}

//go:generate mockgen -source=./loan.go -destination=mockservice/mock_service.go -package=mockservice github.com/michaeltansy/billing-engine/internal/loan Service
type Service interface {
	CreateLoan(ctx context.Context, r Request) (Response, error)
}
