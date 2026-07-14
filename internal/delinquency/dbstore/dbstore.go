package dbstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"

	database "github.com/michaeltansy/billing-engine/database/postgres"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/delinquency/service"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

type Store struct {
	db *sqlx.DB
}

var overdueSnapshotQuery = `
	SELECT
		l.status,
		i.week_number
	FROM
		loans l
	LEFT JOIN
		loan_installments i
	ON
		i.loan_id = l.id
		AND i.status = 'PENDING'
		AND i.due_date < $2
	WHERE
		l.id = $1
	ORDER BY
		i.week_number`

type overdueRow struct {
	Status     loan.Status   `db:"status"`
	WeekNumber sql.NullInt64 `db:"week_number"`
}

func (s *Store) OverdueSnapshot(ctx context.Context, loanID int64, asOfDate time.Time) (service.Snapshot, error) {
	var rows []overdueRow

	err := s.db.SelectContext(ctx, &rows, overdueSnapshotQuery, loanID, asOfDate)
	if err != nil {
		return service.Snapshot{}, database.Translate(err)
	}
	if len(rows) == 0 {
		return service.Snapshot{}, apierr.ErrLoanNotFound
	}

	snapshot := service.Snapshot{
		LoanStatus:   rows[0].Status,
		OverdueWeeks: make([]int, 0, len(rows)),
	}
	for _, row := range rows {
		if row.WeekNumber.Valid {
			snapshot.OverdueWeeks = append(snapshot.OverdueWeeks, int(row.WeekNumber.Int64))
		}
	}

	return snapshot, nil
}

func NewDBStore(db *sqlx.DB) *Store {
	return &Store{
		db: db,
	}
}
