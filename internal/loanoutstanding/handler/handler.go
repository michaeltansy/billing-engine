package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loanoutstanding"
)

const (
	DefaultRequestTimeoutInMS = 1000

	Route = "GET /loans/{id}/outstanding"
)

type Handler struct {
	deps Dependencies
	cfg  config
}

type Dependencies struct {
	LoanOutstandingSvc loanoutstanding.Service
}

type config struct {
	timeoutInMS int
}

type Outstanding struct {
	Outstanding int64 `json:"outstanding"`
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.timeoutInMS)*time.Millisecond)
	defer cancel()

	loanID, err := parseLoanID(r)
	if err != nil {
		apierr.Write(w, err)
		return
	}

	resp, err := h.deps.LoanOutstandingSvc.GetOutstanding(ctx, loanoutstanding.Request{
		LoanID: loanID,
	})
	if err != nil {
		apierr.Write(w, err)
		return
	}

	writeJSON(w, Outstanding{Outstanding: resp.Outstanding})
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
		log.Printf("loanoutstanding: encoding response: %v", err)
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
