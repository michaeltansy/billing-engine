package handler_test

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/delinquency"
	"github.com/michaeltansy/billing-engine/internal/delinquency/handler"
	"github.com/michaeltansy/billing-engine/internal/delinquency/mockservice"
	"github.com/michaeltansy/billing-engine/internal/loan"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	m.Run()
}

func serve(t *testing.T, svc delinquency.Service, target string) *httptest.ResponseRecorder {
	t.Helper()

	h := handler.NewHandler(handler.Dependencies{DelinquencySvc: svc})
	mux := http.NewServeMux()
	mux.HandleFunc(handler.Route, h.Handle)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	return rec
}

func TestDelinquencyOK(t *testing.T) {
	ctrl := gomock.NewController(t)

	asOf := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		GetDelinquency(gomock.Any(), delinquency.Request{LoanID: 100}).
		Return(delinquency.Response{
			LoanID:       100,
			IsDelinquent: true,
			OverdueWeeks: []int{3, 4},
			LoanStatus:   loan.StatusDelinquent,
			AsOf:         asOf,
		}, nil).
		Times(1)

	rec := serve(t, svc, "/loans/100/delinquency")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	var got handler.Delinquency
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if !got.IsDelinquent {
		t.Error("is_delinquent = false, want true")
	}
	if got.LoanStatus != loan.StatusDelinquent {
		t.Errorf("loan_status = %s, want DELINQUENT", got.LoanStatus)
	}
	if len(got.OverdueWeeks) != 2 || got.OverdueWeeks[0] != 3 || got.OverdueWeeks[1] != 4 {
		t.Errorf("overdue_weeks = %v, want [3 4]", got.OverdueWeeks)
	}
	if !got.AsOf.Equal(asOf) {
		t.Errorf("as_of = %s, want %s", got.AsOf, asOf)
	}
}

func TestOverdueWeeksSerialisesAsEmptyArray(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		GetDelinquency(gomock.Any(), delinquency.Request{LoanID: 100}).
		Return(delinquency.Response{
			LoanID:     100,
			LoanStatus: loan.StatusActive,
			AsOf:       time.Now().UTC(),
		}, nil).
		Times(1)

	rec := serve(t, svc, "/loans/100/delinquency")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if string(raw["overdue_weeks"]) != "[]" {
		t.Errorf("overdue_weeks = %s, want []", raw["overdue_weeks"])
	}
}

func TestDelinquencyLoanNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		GetDelinquency(gomock.Any(), delinquency.Request{LoanID: 404}).
		Return(delinquency.Response{}, apierr.ErrLoanNotFound).
		Times(1)

	rec := serve(t, svc, "/loans/404/delinquency")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
}

func TestDelinquencyMalformedLoanID(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().GetDelinquency(gomock.Any(), gomock.Any()).Times(0)

	for _, target := range []string{"/loans/abc/delinquency", "/loans/0/delinquency", "/loans/-1/delinquency"} {
		rec := serve(t, svc, target)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}
