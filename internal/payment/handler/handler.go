package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/payment"
)

const (
	DefaultRequestTimeoutInMS = 1000

	Route = "POST /loans/{id}/payments"

	IdempotencyKeyHeader = "Idempotency-Key"

	// A payment body is a single integer. Anything larger is a client error, not
	// something to stream into memory.
	maxBodyBytes = 1 << 12
)

type Handler struct {
	deps Dependencies
	cfg  config
}

type Dependencies struct {
	PaymentSvc payment.Service
}

type config struct {
	timeoutInMS int
}

type PaymentRequest struct {
	Amount *int64 `json:"amount"`
}

type PaymentResponse struct {
	PaymentID   int64       `json:"payment_id"`
	WeekPaid    int         `json:"week_paid"`
	Outstanding int64       `json:"outstanding"`
	LoanStatus  loan.Status `json:"loan_status"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.timeoutInMS)*time.Millisecond)
	defer cancel()

	req, err := parseRequest(r)
	if err != nil {
		apierr.Write(w, err)
		return
	}

	resp, err := h.deps.PaymentSvc.MakePayment(ctx, req)
	if err != nil {
		apierr.Write(w, err)
		return
	}

	// 201 for a payment actually applied, 200 for a replay of one already applied.
	status := http.StatusCreated
	if resp.Replayed {
		status = http.StatusOK
	}

	writeJSON(w, status, PaymentResponse{
		PaymentID:   resp.PaymentID,
		WeekPaid:    resp.WeekPaid,
		Outstanding: resp.Outstanding,
		LoanStatus:  resp.LoanStatus,
	})
}

func parseRequest(r *http.Request) (payment.Request, error) {
	loanID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || loanID <= 0 {
		return payment.Request{}, apierr.Malformed("loan id must be a positive integer")
	}

	key := strings.TrimSpace(r.Header.Get(IdempotencyKeyHeader))
	if key == "" {
		return payment.Request{}, apierr.Malformed("Idempotency-Key header is required")
	}

	var body PaymentRequest

	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&body); err != nil {
		return payment.Request{}, apierr.Malformed("body must be a JSON object with an integer amount")
	}

	if body.Amount == nil {
		return payment.Request{}, apierr.Malformed("amount is required")
	}
	if *body.Amount <= 0 {
		return payment.Request{}, apierr.Malformed("amount must be a positive integer")
	}

	return payment.Request{
		LoanID:  loanID,
		Amount:  *body.Amount,
		IdemKey: key,
	}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("payment: encoding response: %v", err)
	}
}

type Option func(*Handler)

func WithTimeoutOptions(timeoutInMS int) Option {
	return Option(func(h *Handler) {
		if timeoutInMS <= 0 {
			timeoutInMS = DefaultRequestTimeoutInMS
		}

		h.cfg.timeoutInMS = timeoutInMS
	})
}

func NewHandler(deps Dependencies, options ...Option) *Handler {
	h := Handler{
		deps: deps,
		cfg: config{
			timeoutInMS: DefaultRequestTimeoutInMS,
		},
	}
	for _, opt := range options {
		opt(&h)
	}

	return &h
}
