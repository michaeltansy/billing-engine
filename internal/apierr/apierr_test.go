package apierr_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/michaeltansy/billing-engine/internal/apierr"
)

type response struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	m.Run()
}

func TestWriteMapsTheTaxonomy(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"malformed", apierr.Malformed("missing Idempotency-Key header"), http.StatusBadRequest, "MALFORMED_REQUEST"},
		{"not found", apierr.ErrLoanNotFound, http.StatusNotFound, "LOAN_NOT_FOUND"},
		{"closed", apierr.ErrLoanClosed, http.StatusConflict, "LOAN_CLOSED"},
		{"key reused", apierr.ErrKeyReused, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED"},
		{"invalid amount", apierr.InvalidAmount(110000, 220000), http.StatusUnprocessableEntity, "INVALID_AMOUNT"},
		{"unavailable", apierr.ErrUnavailable, http.StatusServiceUnavailable, "UNAVAILABLE"},
		{"unclassified", errors.New("boom"), http.StatusInternalServerError, "INTERNAL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			apierr.Write(rec, tc.err)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var got response
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", got.Error.Code, tc.wantCode)
			}
		})
	}
}

func TestInvalidAmountCarriesExpected(t *testing.T) {
	rec := httptest.NewRecorder()
	apierr.Write(rec, apierr.InvalidAmount(110000, 220000))

	var got response
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Error.Details["expected"] != float64(110000) {
		t.Errorf("details.expected = %v, want 110000", got.Error.Details["expected"])
	}
	if got.Error.Details["received"] != float64(220000) {
		t.Errorf("details.received = %v, want 220000", got.Error.Details["received"])
	}
}

func TestWrappedSentinelsStillClassify(t *testing.T) {
	wrapped := fmt.Errorf("dbstore: fetching loan 42: %w", apierr.ErrLoanNotFound)

	rec := httptest.NewRecorder()
	apierr.Write(rec, wrapped)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestInternalErrorDoesNotLeakDetail(t *testing.T) {
	leaky := errors.New(`pq: relation "loan_installments" does not exist (host=db password=toor)`)

	rec := httptest.NewRecorder()
	apierr.Write(rec, leaky)

	var got response
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Error.Message != "internal error" {
		t.Errorf("message = %q, want %q", got.Error.Message, "internal error")
	}
	if got.Error.Details != nil {
		t.Errorf("details = %v, want nil on a 5xx", got.Error.Details)
	}
}
