package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

const (
	DefaultRequestTimeoutInMS = 1000

	CreateRoute = "POST /loans"

	dateLayout = "2006-01-02"

	maxBodyBytes = 1 << 12
)

type Handler struct {
	deps Dependencies
	cfg  config
}

type Dependencies struct {
	LoanSvc loan.Service
}

type config struct {
	timeoutInMS int
}

type CreateLoanRequest struct {
	Principal *int64  `json:"principal"`
	RateBps   *int    `json:"rate_bps"`
	StartDate *string `json:"start_date"`
}

// Installment is the wire shape of one week, shared by the create and schedule
// responses so both describe a week identically.
type Installment struct {
	Week    int                    `json:"week"`
	DueDate string                 `json:"due_date"`
	Amount  int64                  `json:"amount"`
	Status  loan.InstallmentStatus `json:"status"`
}

type CreateLoanResponse struct {
	LoanID       int64         `json:"loan_id"`
	TotalPayable int64         `json:"total_payable"`
	LoanStatus   loan.Status   `json:"loan_status"`
	Schedule     []Installment `json:"schedule"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.timeoutInMS)*time.Millisecond)
	defer cancel()

	req, err := parseRequest(r)
	if err != nil {
		apierr.Write(w, err)
		return
	}

	resp, err := h.deps.LoanSvc.CreateLoan(ctx, req)
	if err != nil {
		apierr.Write(w, err)
		return
	}

	schedule := make([]Installment, 0, len(resp.Installments))
	for _, inst := range resp.Installments {
		schedule = append(schedule, Installment{
			Week:    inst.WeekNumber,
			DueDate: inst.DueDate.Format(dateLayout),
			Amount:  inst.Amount,
			Status:  loan.InstallmentPending,
		})
	}

	writeJSON(w, http.StatusCreated, CreateLoanResponse{
		LoanID:       resp.LoanID,
		TotalPayable: resp.TotalPayable,
		LoanStatus:   resp.LoanStatus,
		Schedule:     schedule,
	})
}

func parseRequest(r *http.Request) (loan.CreateRequest, error) {
	var body CreateLoanRequest

	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&body); err != nil {
		return loan.CreateRequest{}, apierr.Malformed("body must be a JSON object with principal, rate_bps and start_date")
	}

	if body.Principal == nil {
		return loan.CreateRequest{}, apierr.Malformed("principal is required")
	}
	if body.RateBps == nil {
		return loan.CreateRequest{}, apierr.Malformed("rate_bps is required")
	}
	if body.StartDate == nil {
		return loan.CreateRequest{}, apierr.Malformed("start_date is required")
	}

	startDate, err := time.ParseInLocation(dateLayout, *body.StartDate, time.UTC)
	if err != nil {
		return loan.CreateRequest{}, apierr.Malformed("start_date must be a calendar date, e.g. 2026-07-13")
	}

	return loan.CreateRequest{
		Terms: loan.Terms{
			Principal: *body.Principal,
			RateBps:   *body.RateBps,
			StartDate: startDate,
		},
	}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("loan: encoding response: %v", err)
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
