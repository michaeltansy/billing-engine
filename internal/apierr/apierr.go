package apierr

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

type Code string

const (
	CodeMalformedRequest Code = "MALFORMED_REQUEST"
	CodeLoanNotFound     Code = "LOAN_NOT_FOUND"
	CodeLoanClosed       Code = "LOAN_CLOSED"
	CodeKeyReused        Code = "IDEMPOTENCY_KEY_REUSED"
	CodeInvalidAmount    Code = "INVALID_AMOUNT"
	CodeInternal         Code = "INTERNAL"
	CodeUnavailable      Code = "UNAVAILABLE"
)

var (
	ErrMalformedRequest = errors.New("malformed request")
	ErrLoanNotFound     = errors.New("loan not found")
	ErrLoanClosed       = errors.New("loan is closed")
	ErrKeyReused        = errors.New("idempotency key reused with a different payload")
	ErrInvalidAmount    = errors.New("payment amount does not match the installment")
	ErrUnavailable      = errors.New("database unavailable")
)

type envelope struct {
	Error body `json:"error"`
}

type body struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Error struct {
	err     error
	message string
	details map[string]any
}

func (e *Error) Error() string { return e.message }

func (e *Error) Unwrap() error { return e.err }

func InvalidAmount(expected, received int64) *Error {
	return &Error{
		err:     ErrInvalidAmount,
		message: "payment must equal the current installment",
		details: map[string]any{"expected": expected, "received": received},
	}
}

func Malformed(message string) *Error {
	return &Error{err: ErrMalformedRequest, message: message}
}

func Write(w http.ResponseWriter, err error) {
	status, code := classify(err)

	if status >= http.StatusInternalServerError {
		log.Printf("apierr: %s: %v", code, err)
	}

	message := err.Error()
	var details map[string]any

	var domainErr *Error
	if errors.As(err, &domainErr) {
		message = domainErr.message
		details = domainErr.details
	}

	if status >= http.StatusInternalServerError {
		message = "internal error"
		details = nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := envelope{Error: body{Code: code, Message: message, Details: details}}
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		log.Printf("apierr: encoding response: %v", encErr)
	}
}

func classify(err error) (int, Code) {
	switch {
	case errors.Is(err, ErrMalformedRequest):
		return http.StatusBadRequest, CodeMalformedRequest
	case errors.Is(err, ErrLoanNotFound):
		return http.StatusNotFound, CodeLoanNotFound
	case errors.Is(err, ErrLoanClosed):
		return http.StatusConflict, CodeLoanClosed
	case errors.Is(err, ErrKeyReused):
		return http.StatusConflict, CodeKeyReused
	case errors.Is(err, ErrInvalidAmount):
		return http.StatusUnprocessableEntity, CodeInvalidAmount
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable, CodeUnavailable
	default:
		return http.StatusInternalServerError, CodeInternal
	}
}
