package handler_test

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/michaeltansy/billing-engine/internal/apierr"
	"github.com/michaeltansy/billing-engine/internal/loan"
	"github.com/michaeltansy/billing-engine/internal/payment"
	"github.com/michaeltansy/billing-engine/internal/payment/handler"
	"github.com/michaeltansy/billing-engine/internal/payment/mockservice"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	m.Run()
}

func serve(t *testing.T, svc payment.Service, target, key, body string) *httptest.ResponseRecorder {
	t.Helper()

	h := handler.NewHandler(handler.Dependencies{PaymentSvc: svc})
	mux := http.NewServeMux()
	mux.HandleFunc(handler.Route, h.Handle)

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if key != "" {
		req.Header.Set(handler.IdempotencyKeyHeader, key)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestPaymentApplied201(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		MakePayment(gomock.Any(), payment.Request{LoanID: 100, Amount: 110000, IdemKey: "key-1"}).
		Return(payment.Response{
			PaymentID: 987, WeekPaid: 3, Outstanding: 5170000,
			LoanStatus: loan.StatusDelinquent, Replayed: false,
		}, nil).
		Times(1)

	rec := serve(t, svc, "/loans/100/payments", "key-1", `{"amount":110000}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
}

func TestPaymentReplay200(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().
		MakePayment(gomock.Any(), gomock.Any()).
		Return(payment.Response{
			PaymentID: 987, WeekPaid: 3, Outstanding: 5170000,
			LoanStatus: loan.StatusDelinquent, Replayed: true,
		}, nil).
		Times(1)

	rec := serve(t, svc, "/loans/100/payments", "key-1", `{"amount":110000}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
}

func TestPaymentErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"unknown loan", apierr.ErrLoanNotFound, http.StatusNotFound},
		{"closed loan, new key", apierr.ErrLoanClosed, http.StatusConflict},
		{"key reused with different payload", apierr.ErrKeyReused, http.StatusConflict},
		{"wrong amount", apierr.InvalidAmount(110000, 220000), http.StatusUnprocessableEntity},
		{"database unreachable", apierr.ErrUnavailable, http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			svc := mockservice.NewMockService(ctrl)
			svc.EXPECT().MakePayment(gomock.Any(), gomock.Any()).
				Return(payment.Response{}, tc.err).Times(1)

			rec := serve(t, svc, "/loans/100/payments", "key-1", `{"amount":110000}`)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestPaymentMalformedRequests(t *testing.T) {
	tests := []struct {
		name   string
		target string
		key    string
		body   string
	}{
		{"missing idempotency key", "/loans/100/payments", "", `{"amount":110000}`},
		{"blank idempotency key", "/loans/100/payments", "   ", `{"amount":110000}`},
		{"non-numeric loan id", "/loans/abc/payments", "key-1", `{"amount":110000}`},
		{"zero loan id", "/loans/0/payments", "key-1", `{"amount":110000}`},
		{"malformed json", "/loans/100/payments", "key-1", `{"amount":`},
		{"missing amount", "/loans/100/payments", "key-1", `{}`},
		{"zero amount", "/loans/100/payments", "key-1", `{"amount":0}`},
		{"negative amount", "/loans/100/payments", "key-1", `{"amount":-110000}`},
		{"unknown field", "/loans/100/payments", "key-1", `{"amount":110000,"week":3}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			svc := mockservice.NewMockService(ctrl)
			svc.EXPECT().MakePayment(gomock.Any(), gomock.Any()).Times(0)

			rec := serve(t, svc, tc.target, tc.key, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestClientCannotChooseWeek(t *testing.T) {
	ctrl := gomock.NewController(t)

	svc := mockservice.NewMockService(ctrl)
	svc.EXPECT().MakePayment(gomock.Any(), gomock.Any()).Times(0)

	rec := serve(t, svc, "/loans/100/payments", "key-1", `{"amount":110000,"week_number":7}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
