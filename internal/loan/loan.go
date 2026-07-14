package loan

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
