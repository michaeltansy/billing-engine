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
## Contract
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