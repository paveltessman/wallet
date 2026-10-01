package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"uuid"

	"wallet/internal/wallet"
)

func TestErrorFor(t *testing.T) {
	id := uuid.MustParse("5f1c8a2e-4b7d-4c1a-9f3e-2d6b8a0c7e41")

	tests := []struct {
		method    string
		err       error
		status    int
		code      string
		retryable bool
	}{
		{http.MethodGet, wallet.ErrNotFound, http.StatusNotFound, "WALLET_NOT_FOUND", false},
		{http.MethodPost, wallet.ErrInsufficientFunds, http.StatusConflict, "INSUFFICIENT_FUNDS", false},
		{http.MethodPost, wallet.ErrBalanceLimitExceeded, http.StatusConflict, "BALANCE_LIMIT_EXCEEDED", false},
		{http.MethodGet, wallet.ErrOverloaded, http.StatusTooManyRequests, "SERVICE_OVERLOADED", true},
		{http.MethodGet, wallet.ErrUnavailable, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", true},
		{http.MethodPost, wallet.ErrOutcomeUnknown, http.StatusServiceUnavailable, "OPERATION_OUTCOME_UNKNOWN", false},
		{http.MethodGet, wallet.ErrOutcomeUnknown, http.StatusServiceUnavailable, "OPERATION_OUTCOME_UNKNOWN", true},
		{http.MethodHead, wallet.ErrOutcomeUnknown, http.StatusServiceUnavailable, "OPERATION_OUTCOME_UNKNOWN", true},
		{http.MethodGet, errors.New("a bug"), http.StatusInternalServerError, "INTERNAL_ERROR", false},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.code, func(t *testing.T) {
			// The Store wraps the domain errors, so the mapping must look through the wrap.
			body := errorFor(tt.method, id, fmt.Errorf("wrap: %w", tt.err))

			if body.Status != tt.status || body.Code != tt.code || body.Retryable != tt.retryable {
				t.Errorf("error body = %d %s retryable %t, want %d %s retryable %t",
					body.Status, body.Code, body.Retryable, tt.status, tt.code, tt.retryable)
			}
			if body.Detail == "" {
				t.Error("detail is empty")
			}
		})
	}
}
