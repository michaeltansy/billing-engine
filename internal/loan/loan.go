package loan

import (
	"context"
	"time"
)

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

// InstallmentStatus is the settled state of a single week.
type InstallmentStatus string

const (
	InstallmentPending InstallmentStatus = "PENDING"
	InstallmentPaid    InstallmentStatus = "PAID"
)

type CreateRequest struct {
	Terms Terms
}

type CreateResponse struct {
	LoanID       int64
	TotalPayable int64
	LoanStatus   Status
	Installments []Installment
}

type ScheduleRequest struct {
	LoanID int64
}

type ScheduleResponse struct {
	LoanID       int64
	LoanStatus   Status
	Installments []ScheduleEntry
}

// ScheduleEntry is one week as read back from storage. It is deliberately not
// loan.Installment: a generated installment has no settled state yet, whereas a
// stored one always does. Keeping them separate stops a freshly generated week
// from being mistaken for a persisted PENDING one.
type ScheduleEntry struct {
	WeekNumber int
	DueDate    time.Time
	Amount     int64
	Status     InstallmentStatus
}

//go:generate mockgen -source=./loan.go -destination=mockservice/mock_service.go -package=mockservice github.com/michaeltansy/billing-engine/internal/loan Service
type Service interface {
	CreateLoan(ctx context.Context, r CreateRequest) (CreateResponse, error)
	GetSchedule(ctx context.Context, r ScheduleRequest) (ScheduleResponse, error)
}
