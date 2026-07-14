package dbstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"

	database "github.com/michaeltansy/billing-engine/database/postgres"
	"github.com/michaeltansy/billing-engine/internal/apierr"
)

type Store struct {
	db *sqlx.DB
}

var pendingTotalQuery = `
	SELECT
		COALESCE(SUM(i.amount) FILTER (WHERE i.status = 'PENDING'), 0) AS pending_total
	FROM
		loans l
	LEFT JOIN
		loan_installments i
	ON
		i.loan_id = l.id
	WHERE
		l.id = $1
	GROUP BY
		l.id`

func (s *Store) PendingTotal(ctx context.Context, loanID int64) (int64, error) {
	var pendingTotal int64

	err := s.db.GetContext(ctx, &pendingTotal, pendingTotalQuery, loanID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apierr.ErrLoanNotFound
	}
	if err != nil {
		return 0, database.Translate(err)
	}

	return pendingTotal, nil
}

func NewDBStore(db *sqlx.DB) *Store {
	return &Store{
		db: db,
	}
}
