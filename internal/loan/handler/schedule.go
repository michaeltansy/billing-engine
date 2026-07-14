package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

const ScheduleRoute = "GET /loans/{id}/schedule"

type ScheduleResponse struct {
	LoanStatus   loan.Status   `json:"loan_status"`
	Installments []Installment `json:"installments"`
}

func (h *Handler) HandleSchedule(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.timeoutInMS)*time.Millisecond)
	defer cancel()

	loanID, err := parseLoanID(r)
	if err != nil {
		apierr.Write(w, err)
		return
	}

	resp, err := h.deps.LoanSvc.GetSchedule(ctx, loan.ScheduleRequest{LoanID: loanID})
	if err != nil {
		apierr.Write(w, err)
		return
	}

	installments := make([]Installment, 0, len(resp.Installments))
	for _, inst := range resp.Installments {
		installments = append(installments, Installment{
			Week:    inst.WeekNumber,
			DueDate: inst.DueDate.Format(dateLayout),
			Amount:  inst.Amount,
			Status:  inst.Status,
		})
	}

	writeJSON(w, http.StatusOK, ScheduleResponse{
		LoanStatus:   resp.LoanStatus,
		Installments: installments,
	})
}

func parseLoanID(r *http.Request) (int64, error) {
	loanID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || loanID <= 0 {
		return 0, apierr.Malformed("loan id must be a positive integer")
	}

	return loanID, nil
}
