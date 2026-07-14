# Overview
Billing service for the loan engine, owning the repayment lifecycle of a loan:
outstanding tracking, payment acceptance and deliquency determination.

# Requirements
## Functional
1. The borrower should be able to fetch information regarding the outstanding loan amount
2. The borrower should make an exact amount of payment according to the schedule
3. Every payment action is idempotent to avoid double submission
4. The borrower should be flagged as deliquency on having 2 consecutive missed weekly payments

## Non Functional
1. Idempotent handling (at-most-once payment) via client. Replays return the stored result rebuilt from the persisted `week_number`.
2. Payment should be ACID (payment insert + installment update + status transition in one transaction).
3. Pessimistic lock handling on loan row.
4. Payments are an append-only 1:1 ledger against installments.

## Out of Scope
1. Disbursement.
2. Partial / over-payments.
3. prepayment & interest rebase.
4. Penalties / late fees.
5. Payment gateway integration.

## Stack
1. Language: Go (1.26)
2. Database: PostgreSQL

# Data Model
## Schema
### loans
```sql
CREATE TABLE loans (
  id                 BIGSERIAL PRIMARY KEY,
  principal          BIGINT NOT NULL,
  interest_rate_bps  INT    NOT NULL,
  total_payable      BIGINT NOT NULL,
  installment_amount BIGINT NOT NULL,          -- uniform weekly amount
  tenor_weeks        INT    NOT NULL,
  start_date         DATE   NOT NULL,
  status             TEXT   NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','DELINQUENT','CLOSED'))
);
```

### loan_installments
```sql
CREATE TABLE loan_installments (
  loan_id     BIGINT NOT NULL REFERENCES loans(id),
  week_number INT    NOT NULL,
  due_date    DATE   NOT NULL,
  amount      BIGINT NOT NULL,                 -- source of truth for validation
  status      TEXT   NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PAID')),
  paid_at     TIMESTAMPTZ,
  payment_id  BIGINT,
  PRIMARY KEY (loan_id, week_number)
);
CREATE INDEX idx_inst_pending ON loan_installments (loan_id, status, due_date);
```

### payments
```sql
CREATE TABLE payments (
  id              BIGSERIAL PRIMARY KEY,
  loan_id         BIGINT NOT NULL REFERENCES loans(id),
  idempotency_key TEXT   NOT NULL,
  request_hash    TEXT   NOT NULL,             -- detects key reuse w/ different payload
  amount          BIGINT NOT NULL,
  week_number     INT    NOT NULL,             -- the single week this payment covers
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (loan_id, idempotency_key),           -- race-free dedup arbiter
  UNIQUE (loan_id, week_number)                -- structural 1:1 guarantee
);
```

# API
## Collection
[billing-engine-collection.yaml](https://github.com/user-attachments/files/29997287/billing-engine-collection.yaml)

## Contract
### Create Loan
```
POST /loans
  Body:   {"principal":5000000,"rate_bps":1000,"start_date":"2026-07-13"}
  200 → {"loan_id":6,"total_payable":5500000,"loan_status":"ACTIVE","schedule":[{"week":1,"due_date":"2026-07-20","amount":110000,"status":"PENDING"},{"week":2,"due_date":"2026-07-27","amount":110000,"status":"PENDING"},{"week":3,"due_date":"2026-08-03","amount":110000,"status":"PENDING"},{"week":4,"due_date":"2026-08-10","amount":110000,"status":"PENDING"},{"week":5,"due_date":"2026-08-17","amount":110000,"status":"PENDING"},{"week":6,"due_date":"2026-08-24","amount":110000,"status":"PENDING"},{"week":7,"due_date":"2026-08-31","amount":110000,"status":"PENDING"},{"week":8,"due_date":"2026-09-07","amount":110000,"status":"PENDING"},{"week":9,"due_date":"2026-09-14","amount":110000,"status":"PENDING"},{"week":10,"due_date":"2026-09-21","amount":110000,"status":"PENDING"},{"week":11,"due_date":"2026-09-28","amount":110000,"status":"PENDING"},{"week":12,"due_date":"2026-10-05","amount":110000,"status":"PENDING"},{"week":13,"due_date":"2026-10-12","amount":110000,"status":"PENDING"},{"week":14,"due_date":"2026-10-19","amount":110000,"status":"PENDING"},{"week":15,"due_date":"2026-10-26","amount":110000,"status":"PENDING"},{"week":16,"due_date":"2026-11-02","amount":110000,"status":"PENDING"},{"week":17,"due_date":"2026-11-09","amount":110000,"status":"PENDING"},{"week":18,"due_date":"2026-11-16","amount":110000,"status":"PENDING"},{"week":19,"due_date":"2026-11-23","amount":110000,"status":"PENDING"},{"week":20,"due_date":"2026-11-30","amount":110000,"status":"PENDING"},{"week":21,"due_date":"2026-12-07","amount":110000,"status":"PENDING"},{"week":22,"due_date":"2026-12-14","amount":110000,"status":"PENDING"},{"week":23,"due_date":"2026-12-21","amount":110000,"status":"PENDING"},{"week":24,"due_date":"2026-12-28","amount":110000,"status":"PENDING"},{"week":25,"due_date":"2027-01-04","amount":110000,"status":"PENDING"},{"week":26,"due_date":"2027-01-11","amount":110000,"status":"PENDING"},{"week":27,"due_date":"2027-01-18","amount":110000,"status":"PENDING"},{"week":28,"due_date":"2027-01-25","amount":110000,"status":"PENDING"},{"week":29,"due_date":"2027-02-01","amount":110000,"status":"PENDING"},{"week":30,"due_date":"2027-02-08","amount":110000,"status":"PENDING"},{"week":31,"due_date":"2027-02-15","amount":110000,"status":"PENDING"},{"week":32,"due_date":"2027-02-22","amount":110000,"status":"PENDING"},{"week":33,"due_date":"2027-03-01","amount":110000,"status":"PENDING"},{"week":34,"due_date":"2027-03-08","amount":110000,"status":"PENDING"},{"week":35,"due_date":"2027-03-15","amount":110000,"status":"PENDING"},{"week":36,"due_date":"2027-03-22","amount":110000,"status":"PENDING"},{"week":37,"due_date":"2027-03-29","amount":110000,"status":"PENDING"},{"week":38,"due_date":"2027-04-05","amount":110000,"status":"PENDING"},{"week":39,"due_date":"2027-04-12","amount":110000,"status":"PENDING"},{"week":40,"due_date":"2027-04-19","amount":110000,"status":"PENDING"},{"week":41,"due_date":"2027-04-26","amount":110000,"status":"PENDING"},{"week":42,"due_date":"2027-05-03","amount":110000,"status":"PENDING"},{"week":43,"due_date":"2027-05-10","amount":110000,"status":"PENDING"},{"week":44,"due_date":"2027-05-17","amount":110000,"status":"PENDING"},{"week":45,"due_date":"2027-05-24","amount":110000,"status":"PENDING"},{"week":46,"due_date":"2027-05-31","amount":110000,"status":"PENDING"},{"week":47,"due_date":"2027-06-07","amount":110000,"status":"PENDING"},{"week":48,"due_date":"2027-06-14","amount":110000,"status":"PENDING"},{"week":49,"due_date":"2027-06-21","amount":110000,"status":"PENDING"},{"week":50,"due_date":"2027-06-28","amount":110000,"status":"PENDING"}]}
```

### Get Loan Schedule
```
GET /loans/{loan_id}/schedule
  200 → {"loan_status":"ACTIVE","installments":[{"week":1,"due_date":"2026-06-29","amount":110000,"status":"PAID"},{"week":2,"due_date":"2026-07-06","amount":110000,"status":"PAID"},{"week":3,"due_date":"2026-07-13","amount":110000,"status":"PAID"},{"week":4,"due_date":"2026-07-20","amount":110000,"status":"PENDING"},{"week":5,"due_date":"2026-07-27","amount":110000,"status":"PENDING"},{"week":6,"due_date":"2026-08-03","amount":110000,"status":"PENDING"},{"week":7,"due_date":"2026-08-10","amount":110000,"status":"PENDING"},{"week":8,"due_date":"2026-08-17","amount":110000,"status":"PENDING"},{"week":9,"due_date":"2026-08-24","amount":110000,"status":"PENDING"},{"week":10,"due_date":"2026-08-31","amount":110000,"status":"PENDING"},{"week":11,"due_date":"2026-09-07","amount":110000,"status":"PENDING"},{"week":12,"due_date":"2026-09-14","amount":110000,"status":"PENDING"},{"week":13,"due_date":"2026-09-21","amount":110000,"status":"PENDING"},{"week":14,"due_date":"2026-09-28","amount":110000,"status":"PENDING"},{"week":15,"due_date":"2026-10-05","amount":110000,"status":"PENDING"},{"week":16,"due_date":"2026-10-12","amount":110000,"status":"PENDING"},{"week":17,"due_date":"2026-10-19","amount":110000,"status":"PENDING"},{"week":18,"due_date":"2026-10-26","amount":110000,"status":"PENDING"},{"week":19,"due_date":"2026-11-02","amount":110000,"status":"PENDING"},{"week":20,"due_date":"2026-11-09","amount":110000,"status":"PENDING"},{"week":21,"due_date":"2026-11-16","amount":110000,"status":"PENDING"},{"week":22,"due_date":"2026-11-23","amount":110000,"status":"PENDING"},{"week":23,"due_date":"2026-11-30","amount":110000,"status":"PENDING"},{"week":24,"due_date":"2026-12-07","amount":110000,"status":"PENDING"},{"week":25,"due_date":"2026-12-14","amount":110000,"status":"PENDING"},{"week":26,"due_date":"2026-12-21","amount":110000,"status":"PENDING"},{"week":27,"due_date":"2026-12-28","amount":110000,"status":"PENDING"},{"week":28,"due_date":"2027-01-04","amount":110000,"status":"PENDING"},{"week":29,"due_date":"2027-01-11","amount":110000,"status":"PENDING"},{"week":30,"due_date":"2027-01-18","amount":110000,"status":"PENDING"},{"week":31,"due_date":"2027-01-25","amount":110000,"status":"PENDING"},{"week":32,"due_date":"2027-02-01","amount":110000,"status":"PENDING"},{"week":33,"due_date":"2027-02-08","amount":110000,"status":"PENDING"},{"week":34,"due_date":"2027-02-15","amount":110000,"status":"PENDING"},{"week":35,"due_date":"2027-02-22","amount":110000,"status":"PENDING"},{"week":36,"due_date":"2027-03-01","amount":110000,"status":"PENDING"},{"week":37,"due_date":"2027-03-08","amount":110000,"status":"PENDING"},{"week":38,"due_date":"2027-03-15","amount":110000,"status":"PENDING"},{"week":39,"due_date":"2027-03-22","amount":110000,"status":"PENDING"},{"week":40,"due_date":"2027-03-29","amount":110000,"status":"PENDING"},{"week":41,"due_date":"2027-04-05","amount":110000,"status":"PENDING"},{"week":42,"due_date":"2027-04-12","amount":110000,"status":"PENDING"},{"week":43,"due_date":"2027-04-19","amount":110000,"status":"PENDING"},{"week":44,"due_date":"2027-04-26","amount":110000,"status":"PENDING"},{"week":45,"due_date":"2027-05-03","amount":110000,"status":"PENDING"},{"week":46,"due_date":"2027-05-10","amount":110000,"status":"PENDING"},{"week":47,"due_date":"2027-05-17","amount":110000,"status":"PENDING"},{"week":48,"due_date":"2027-05-24","amount":110000,"status":"PENDING"},{"week":49,"due_date":"2027-05-31","amount":110000,"status":"PENDING"},{"week":50,"due_date":"2027-06-07","amount":110000,"status":"PENDING"}]}
```

### Get Outstanding
```
GET /loans/{loan_id}/outstanding
    200 → {"outstanding": 5170000}
```

### Check Deliquency
```
GET /loans/{loan_id}/deliquency
  200 → {"is_delinquent":true, "overdue_weeks":[3,4], "loan_status":"DELINQUENT",
         "as_of":"2026-07-28T00:00:00Z"}
```

### Make Payment
```
POST /loans/{id}/payments
  Header: Idempotency-Key: <uuid>              -- one fresh key per payment action
  Body:   {"amount":110000}
  201 → {"payment_id":987, "week_paid":3, "outstanding":5170000, "loan_status":"DELINQUENT"}
  200 → idempotent replay (same key + same payload) — stored result; note the
        embedded outstanding reflects state at original execution time
```

to generate client side UUID, we can use https://www.uuidgenerator.net/version4

## Error Taxonomy
Uniform envelope for all non-2xx responses:

```json
{"error": {"code": "INVALID_AMOUNT",
           "message": "payment must equal the current installment of 110000",
           "details": {"expected": 110000, "received": 220000}}}
```

| HTTP | code | trigger | retryable |
|---|---|---|---|
| 400 | MALFORMED_REQUEST | bad JSON, missing Idempotency-Key header | no — fix request |
| 404 | LOAN_NOT_FOUND | unknown loan id | no |
| 409 | LOAN_CLOSED | payment on closed loan with a new key | no |
| 409 | IDEMPOTENCY_KEY_REUSED | same key, different request_hash | no — client bug |
| 422 | INVALID_AMOUNT | amount ≠ oldest pending installment amount; `details.expected` carries the correct value | no — fix amount; **same key may be retried** (rejection rolls back, key not burned) |
| 500 | INTERNAL | invariant violation, DB failure | **yes, same key** |
| 503 | UNAVAILABLE | DB unreachable | **yes, same key** |

Contract rule: **5xx retries MUST reuse the same Idempotency-Key** — this is
what makes the idempotency design work from the client side.

Implementation: sentinel errors in the domain layer
(`ErrLoanNotFound`, `ErrLoanClosed`, `ErrInvalidAmount`, `ErrKeyReused`),
mapped to HTTP status + code in exactly one place at the edge. The repository
layer translates driver errors (pg code `23505` on the key index → replay/
key-reused path; on the week index → invariant violation → INTERNAL) so
Postgres details never leak upward.

# How to Run the App
```sh
cp files/config.yaml.example config.yaml        
export $(grep -v '^#' .env | xargs)

docker compose up --build
```
