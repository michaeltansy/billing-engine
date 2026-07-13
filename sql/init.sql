BEGIN;

CREATE TABLE IF NOT EXISTS loans (
    id                 BIGSERIAL PRIMARY KEY,
    principal          BIGINT NOT NULL CHECK (principal > 0),
    interest_rate_bps  INT    NOT NULL CHECK (interest_rate_bps >= 0),  -- 1000 = 10.00% flat p.a.
    total_payable      BIGINT NOT NULL CHECK (total_payable > 0),
    installment_amount BIGINT NOT NULL CHECK (installment_amount > 0),  -- uniform weekly amount
    tenor_weeks        INT    NOT NULL CHECK (tenor_weeks > 0),
    start_date         DATE   NOT NULL,
    status             TEXT   NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'DELINQUENT', 'CLOSED')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =====================
-- payments
-- Append-only ledger. One row == one settled week. Never UPDATEd, never DELETEd.
-- =====================
CREATE TABLE IF NOT EXISTS payments (
    id              BIGSERIAL PRIMARY KEY,
    loan_id         BIGINT NOT NULL REFERENCES loans (id),
    idempotency_key TEXT   NOT NULL,
    request_hash    TEXT   NOT NULL,                     -- detects key reuse with a different payload
    amount          BIGINT NOT NULL CHECK (amount > 0),
    week_number     INT    NOT NULL CHECK (week_number > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_payments_loan_key  UNIQUE (loan_id, idempotency_key),
    CONSTRAINT uq_payments_loan_week UNIQUE (loan_id, week_number)
);

CREATE TABLE IF NOT EXISTS loan_installments (
    loan_id     BIGINT NOT NULL REFERENCES loans (id),
    week_number INT    NOT NULL CHECK (week_number > 0),
    due_date    DATE   NOT NULL,                          -- start_date + 7 * week_number days
    amount      BIGINT NOT NULL CHECK (amount > 0),       -- source of truth for payment validation
    status      TEXT   NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PAID')),
    paid_at     TIMESTAMPTZ,
    payment_id  BIGINT REFERENCES payments (id),

    PRIMARY KEY (loan_id, week_number),

    CONSTRAINT ck_installment_paid_consistent CHECK (
        (status = 'PAID'    AND paid_at IS NOT NULL AND payment_id IS NOT NULL) OR
        (status = 'PENDING' AND paid_at IS NULL     AND payment_id IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_inst_pending ON loan_installments (loan_id, status, due_date);

COMMIT;