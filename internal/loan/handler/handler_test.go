package handler_test

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/loan/handler"
	"github.com/michaeltansy/billing-engine/internal/loan/mockservice"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	m.Run()
}

func serve(t *testing.T, svc loan.Service, body string) *httptest.ResponseRecorder {
	t.Helper()

	h := handler.NewHandler(handler.Dependencies{LoanSvc: svc})
	mux := http.NewServeMux()
	mux.HandleFunc(handler.CreateRoute, h.Handle)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/loans", strings.NewReader(body)))

	return rec
}

func TestCreateLoan201(t *testing.T) {
	ctrl := gomock.NewController(t)

	wantTerms := loan.Terms{
		Principal: 5000000,
		RateBps:   1000,
		StartDate: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
	}

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		CreateLoan(gomock.Any(), loan.CreateRequest{Terms: wantTerms}).
		Return(loan.CreateResponse{
			LoanID:       100,
			TotalPayable: 5500000,
			LoanStatus:   loan.StatusActive,
			Installments: []loan.Installment{
				{WeekNumber: 1, DueDate: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), Amount: 110000},
				{WeekNumber: 2, DueDate: time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC), Amount: 110000},
			},
		}, nil).
		Times(1)

	rec := serve(t, svc, `{"principal":5000000,"rate_bps":1000,"start_date":"2026-07-13"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}

	var got handler.CreateLoanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if got.LoanID != 100 || got.TotalPayable != 5500000 {
		t.Errorf("got loan %d totalling %d, want 100 / 5500000", got.LoanID, got.TotalPayable)
	}

	if len(got.Schedule) != 2 {
		t.Fatalf("schedule = %d entries, want 2", len(got.Schedule))
	}
	if got.Schedule[0].DueDate != "2026-07-20" {
		t.Errorf("week 1 due_date = %q, want \"2026-07-20\"", got.Schedule[0].DueDate)
	}
	if got.Schedule[0].Status != "PENDING" {
		t.Errorf("week 1 status = %q, want PENDING", got.Schedule[0].Status)
	}
}

func TestCreateLoanMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"principal":`},
		{"empty object", `{}`},
		{"missing principal", `{"rate_bps":1000,"start_date":"2026-07-13"}`},
		{"missing rate_bps", `{"principal":5000000,"start_date":"2026-07-13"}`},
		{"missing start_date", `{"principal":5000000,"rate_bps":1000}`},
		{"client tries to set tenor_weeks", `{"principal":5000000,"rate_bps":1000,"tenor_weeks":10,"start_date":"2026-07-13"}`},
		{"start_date is a timestamp", `{"principal":5000000,"rate_bps":1000,"start_date":"2026-07-13T00:00:00Z"}`},
		{"start_date is nonsense", `{"principal":5000000,"rate_bps":1000,"start_date":"13-07-2026"}`},
		{"unknown field", `{"principal":5000000,"rate_bps":1000,"start_date":"2026-07-13","status":"CLOSED"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			svc := mockservice.NewMockService(ctrl)
			svc.EXPECT().CreateLoan(gomock.Any(), gomock.Any()).Times(0)

			rec := serve(t, svc, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestZeroRateIsNotTreatedAsMissing(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		CreateLoan(gomock.Any(), gomock.Any()).
		Return(loan.CreateResponse{LoanID: 1, TotalPayable: 5000000, LoanStatus: loan.StatusActive}, nil).
		Times(1)

	rec := serve(t, svc, `{"principal":5000000,"rate_bps":0,"start_date":"2026-07-13"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
}

func serveSchedule(t *testing.T, svc loan.Service, target string) *httptest.ResponseRecorder {
	t.Helper()

	h := handler.NewHandler(handler.Dependencies{LoanSvc: svc})
	mux := http.NewServeMux()
	mux.HandleFunc(handler.ScheduleRoute, h.HandleSchedule)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	return rec
}

func TestGetSchedule200(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		GetSchedule(gomock.Any(), loan.ScheduleRequest{LoanID: 100}).
		Return(loan.ScheduleResponse{
			LoanID:     100,
			LoanStatus: loan.StatusDelinquent,
			Installments: []loan.ScheduleEntry{
				{WeekNumber: 1, DueDate: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), Amount: 110000, Status: loan.InstallmentPaid},
				{WeekNumber: 2, DueDate: time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC), Amount: 110000, Status: loan.InstallmentPending},
			},
		}, nil).
		Times(1)

	rec := serveSchedule(t, svc, "/loans/100/schedule")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	var got handler.ScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if got.LoanStatus != loan.StatusDelinquent {
		t.Errorf("loan_status = %s, want DELINQUENT", got.LoanStatus)
	}
	if len(got.Installments) != 2 {
		t.Fatalf("installments = %d, want 2", len(got.Installments))
	}
	// Per-week status is the whole point of F5: a paid week must read back PAID.
	if got.Installments[0].Status != loan.InstallmentPaid {
		t.Errorf("week 1 status = %s, want PAID", got.Installments[0].Status)
	}
	if got.Installments[0].DueDate != "2026-07-20" {
		t.Errorf("week 1 due_date = %q, want 2026-07-20", got.Installments[0].DueDate)
	}
}

func TestGetScheduleLoanNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().GetSchedule(gomock.Any(), gomock.Any()).
		Return(loan.ScheduleResponse{}, apierr.ErrLoanNotFound).Times(1)

	rec := serveSchedule(t, svc, "/loans/404/schedule")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
}

func TestGetScheduleMalformedLoanID(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().GetSchedule(gomock.Any(), gomock.Any()).Times(0)

	for _, target := range []string{"/loans/abc/schedule", "/loans/0/schedule", "/loans/-1/schedule"} {
		if rec := serveSchedule(t, svc, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}
