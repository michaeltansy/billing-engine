package database

import (
	"context"
	"errors"

	"github.com/lib/pq"

	"github.com/michaeltansy/billing-engine/internal/apierr"
)

const (
	ConstraintPaymentsLoanKey  = "uq_payments_loan_key"
	ConstraintPaymentsLoanWeek = "uq_payments_loan_week"
)

const pgUniqueViolation = "23505"

func IsUniqueViolation(err error, constraint string) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	return pqErr.Code == pgUniqueViolation && pqErr.Constraint == constraint
}

func Translate(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if pqErr.Code.Class() == "08" {
			return errors.Join(apierr.ErrUnavailable, err)
		}
		return err
	}

	return errors.Join(apierr.ErrUnavailable, err)
}
