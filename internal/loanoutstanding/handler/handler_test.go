package handler_test

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loanoutstanding"
	"github.com/michaeltansy/billing-engine/internal/loanoutstanding/handler"
	"github.com/michaeltansy/billing-engine/internal/loanoutstanding/mockservice"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	m.Run()
}

func serve(t *testing.T, svc loanoutstanding.Service, target string) *httptest.ResponseRecorder {
	t.Helper()

	h := handler.NewHandler(handler.Dependencies{LoanOutstandingSvc: svc})
	mux := http.NewServeMux()
	mux.HandleFunc(handler.Route, h.Handle)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	return rec
}

func TestOutstandingOK(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		GetOutstanding(gomock.Any(), loanoutstanding.Request{LoanID: 100}).
		Return(loanoutstanding.Response{LoanID: 100, Outstanding: 5170000}, nil).
		Times(1)

	rec := serve(t, svc, "/loans/100/outstanding")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var got handler.Outstanding
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Outstanding != 5170000 {
		t.Errorf("outstanding = %d, want 5170000", got.Outstanding)
	}
}
func TestOutstandingZeroOnClosedLoan(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		GetOutstanding(gomock.Any(), loanoutstanding.Request{LoanID: 100}).
		Return(loanoutstanding.Response{LoanID: 100, Outstanding: 0}, nil).
		Times(1)

	rec := serve(t, svc, "/loans/100/outstanding")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var raw map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	v, ok := raw["outstanding"]
	if !ok {
		t.Fatal("response omitted the outstanding field entirely")
	}
	if v != float64(0) {
		t.Errorf("outstanding = %v, want 0", v)
	}
}

func TestOutstandingErrors(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		loanID     int64
		svcErr     error
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{
			name:       "unknown loan",
			target:     "/loans/999/outstanding",
			loanID:     999,
			svcErr:     apierr.ErrLoanNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "LOAN_NOT_FOUND",
			wantCalls:  1,
		},
		{
			name:       "non-numeric id is rejected before the service is called",
			target:     "/loans/abc/outstanding",
			wantStatus: http.StatusBadRequest,
			wantCode:   "MALFORMED_REQUEST",
			wantCalls:  0,
		},
		{
			name:       "zero id",
			target:     "/loans/0/outstanding",
			wantStatus: http.StatusBadRequest,
			wantCode:   "MALFORMED_REQUEST",
			wantCalls:  0,
		},
		{
			name:       "negative id",
			target:     "/loans/-1/outstanding",
			wantStatus: http.StatusBadRequest,
			wantCode:   "MALFORMED_REQUEST",
			wantCalls:  0,
		},
		{
			name:       "database failure surfaces as retryable 503",
			target:     "/loans/100/outstanding",
			loanID:     100,
			svcErr:     apierr.ErrUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "UNAVAILABLE",
			wantCalls:  1,
		},
		{
			name:       "unclassified failure does not leak as a 4xx",
			target:     "/loans/100/outstanding",
			loanID:     100,
			svcErr:     errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
			wantCalls:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			svc := mockservice.NewMockService(ctrl)

			if tc.wantCalls > 0 {
				svc.EXPECT().
					GetOutstanding(gomock.Any(), loanoutstanding.Request{LoanID: tc.loanID}).
					Return(loanoutstanding.Response{}, tc.svcErr).
					Times(tc.wantCalls)
			}

			rec := serve(t, svc, tc.target)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			var got struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", got.Error.Code, tc.wantCode)
			}
		})
	}
}
