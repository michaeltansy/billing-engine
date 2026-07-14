package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/delinquency"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

const (
	DefaultRequestTimeoutInMS = 1000

	Route = "GET /loans/{id}/delinquency"
)

type Handler struct {
	deps Dependencies
	cfg  config
}

type Dependencies struct {
	DelinquencySvc delinquency.Service
}

type config struct {
	timeoutInMS int
}

type Delinquency struct {
	IsDelinquent bool        `json:"is_delinquent"`
	OverdueWeeks []int       `json:"overdue_weeks"`
	LoanStatus   loan.Status `json:"loan_status"`
	AsOf         time.Time   `json:"as_of"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.timeoutInMS)*time.Millisecond)
	defer cancel()

	loanID, err := parseLoanID(r)
	if err != nil {
		apierr.Write(w, err)
		return
	}

	resp, err := h.deps.DelinquencySvc.GetDelinquency(ctx, delinquency.Request{
		LoanID: loanID,
	})
	if err != nil {
		apierr.Write(w, err)
		return
	}

	overdueWeeks := resp.OverdueWeeks
	if overdueWeeks == nil {
		overdueWeeks = []int{}
	}

	writeJSON(w, Delinquency{
		IsDelinquent: resp.IsDelinquent,
		OverdueWeeks: overdueWeeks,
		LoanStatus:   resp.LoanStatus,
		AsOf:         resp.AsOf,
	})
}

func parseLoanID(r *http.Request) (int64, error) {
	loanID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || loanID <= 0 {
		return 0, apierr.Malformed("loan id must be a positive integer")
	}

	return loanID, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("delinquency: encoding response: %v", err)
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
