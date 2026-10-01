package httpapi

import (
	"errors"
	"net/http"
	"uuid"

	"wallet/internal/wallet"
)

// errorBody is the Problem Details body of RFC 9457.
type errorBody struct {
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func invalidRequest(detail string) errorBody {
	return errorBody{Status: http.StatusBadRequest, Detail: detail, Code: "INVALID_REQUEST"}
}

// errorFor maps an error of the Store to an error body.
func errorFor(method string, id uuid.UUID, err error) errorBody {
	switch {
	case errors.Is(err, wallet.ErrNotFound):
		return errorBody{
			Status: http.StatusNotFound,
			Detail: "Wallet " + id.String() + " does not exist.",
			Code:   "WALLET_NOT_FOUND",
		}
	case errors.Is(err, wallet.ErrInsufficientFunds):
		return errorBody{
			Status: http.StatusConflict,
			Detail: "The balance of wallet " + id.String() + " is less than the amount.",
			Code:   "INSUFFICIENT_FUNDS",
		}
	case errors.Is(err, wallet.ErrBalanceLimitExceeded):
		return errorBody{
			Status: http.StatusConflict,
			Detail: "The deposit takes the balance of wallet " + id.String() + " above the limit.",
			Code:   "BALANCE_LIMIT_EXCEEDED",
		}
	case errors.Is(err, wallet.ErrOverloaded):
		return errorBody{
			Status:    http.StatusTooManyRequests,
			Detail:    "No database connection is free. Try again later.",
			Code:      "SERVICE_OVERLOADED",
			Retryable: true,
		}
	case errors.Is(err, wallet.ErrUnavailable):
		return errorBody{
			Status:    http.StatusServiceUnavailable,
			Detail:    "The database is not available. Try again later.",
			Code:      "DATABASE_UNAVAILABLE",
			Retryable: true,
		}
	case errors.Is(err, wallet.ErrOutcomeUnknown):
		return errorBody{
			Status: http.StatusServiceUnavailable,
			Detail: "The database did not confirm the result of the operation.",
			Code:   "OPERATION_OUTCOME_UNKNOWN",
			// A read changes nothing, so a retry for read is safe.
			Retryable: method == http.MethodGet || method == http.MethodHead,
		}
	default:
		// The error text can hold internal details, so the body does not include it.
		return errorBody{
			Status: http.StatusInternalServerError,
			Detail: "An unexpected error occurred.",
			Code:   "INTERNAL_ERROR",
		}
	}
}

func writeError(w http.ResponseWriter, body errorBody) {
	body.Title = http.StatusText(body.Status)
	writeJSON(w, body.Status, "application/problem+json", body)
}
