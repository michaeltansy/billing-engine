package dbtest

import (
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/michaeltansy/billing-engine/internal/loan"
)

const DSNEnv = "TEST_DATABASE_URL"

func Connect(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping repository tests", DSNEnv)
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	Reset(t, db)

	return db
}

func Reset(t *testing.T, db *sqlx.DB) {
	t.Helper()

	_, err := db.Exec(`TRUNCATE loan_installments, payments, loans RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
}

func V1Terms(startDate time.Time) loan.Terms {
	return loan.Terms{
		Principal:  5_000_000,
		RateBps:    1000,
		TenorWeeks: loan.TenorWeeks,
		StartDate:  startDate,
	}
}

func Date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func SeedLoan(t *testing.T, db *sqlx.DB, terms loan.Terms) int64 {
	t.Helper()

	schedule := loan.GenerateSchedule(terms)

	var loanID int64

	err := db.Get(&loanID, `
		INSERT INTO loans (principal, interest_rate_bps, total_payable, installment_amount, tenor_weeks, start_date, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'ACTIVE')
		RETURNING id`,
		terms.Principal, terms.RateBps, schedule.TotalPayable,
		schedule.InstallmentAmount, terms.TenorWeeks, terms.StartDate,
	)
	if err != nil {
		t.Fatalf("seeding loan: %v", err)
	}

	for _, inst := range schedule.Installments {
		_, err := db.Exec(`
			INSERT INTO loan_installments (loan_id, week_number, due_date, amount, status)
			VALUES ($1, $2, $3, $4, 'PENDING')`,
			loanID, inst.WeekNumber, inst.DueDate, inst.Amount,
		)
		if err != nil {
			t.Fatalf("seeding installment %d: %v", inst.WeekNumber, err)
		}
	}

	return loanID
}

func PayWeek(t *testing.T, db *sqlx.DB, loanID int64, week int, amount int64, key string) int64 {
	t.Helper()

	var paymentID int64

	err := db.Get(&paymentID, `
		INSERT INTO payments (loan_id, idempotency_key, request_hash, amount, week_number)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		loanID, key, "hash-"+key, amount, week,
	)
	if err != nil {
		t.Fatalf("seeding payment for week %d: %v", week, err)
	}

	_, err = db.Exec(`
		UPDATE loan_installments
		SET status = 'PAID', paid_at = now(), payment_id = $3
		WHERE loan_id = $1 AND week_number = $2`,
		loanID, week, paymentID,
	)
	if err != nil {
		t.Fatalf("marking week %d paid: %v", week, err)
	}

	return paymentID
}

func SetLoanStatus(t *testing.T, db *sqlx.DB, loanID int64, status loan.Status) {
	t.Helper()

	if _, err := db.Exec(`UPDATE loans SET status = $2 WHERE id = $1`, loanID, status); err != nil {
		t.Fatalf("setting loan status: %v", err)
	}
}
