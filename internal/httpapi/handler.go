// Package httpapi serves the wallet API over HTTP.
package httpapi

import (
	"context"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"uuid"
)

// maxBodyBytes limits the request body.
const maxBodyBytes = 1024

// Store changes and reads the wallet balances. *postgres.Store satisfies it.
type Store interface {
	Deposit(ctx context.Context, id uuid.UUID, amount int64) (int64, error)
	Withdraw(ctx context.Context, id uuid.UUID, amount int64) (int64, error)
	Balance(ctx context.Context, id uuid.UUID) (int64, error)
}

type handler struct {
	store Store
}

func NewHandler(store Store) http.Handler {
	h := &handler{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/wallet", h.operation)
	mux.HandleFunc("GET /api/v1/wallet/{walletId}", h.balance)
	// A pattern without a method gets the other methods. The pattern with a method is more specific, so it wins.
	mux.Handle("/api/v1/wallet", methodNotAllowed("POST"))
	mux.Handle("/api/v1/wallet/{walletId}", methodNotAllowed("GET, HEAD"))

	return mux
}

type operationType string

const (
	operationDeposit  operationType = "DEPOSIT"
	operationWithdraw operationType = "WITHDRAW"
)

type operationRequest struct {
	WalletID      *uuid.UUID     `json:"walletId"`
	OperationType *operationType `json:"operationType"`
	Amount        *int64         `json:"amount"`
}

// The detail of a bad value for each field. The key is the JSON pointer of the field.
var fieldDetails = map[string]string{
	"/walletId":      "The field walletId must be a UUID.",
	"/operationType": "The field operationType must be DEPOSIT or WITHDRAW.",
	"/amount":        "The field amount must be an integer from 0 to " + strconv.FormatInt(math.MaxInt64, 10) + ".",
}

type walletResponse struct {
	WalletID uuid.UUID `json:"walletId"`
	Balance  int64     `json:"balance"`
}

func (h *handler) operation(w http.ResponseWriter, r *http.Request) {
	var req operationRequest
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.UnmarshalRead(body, &req, json.RejectUnknownMembers(true)); err != nil {
		writeError(w, invalidRequest(decodeDetail(err)))
		return
	}

	switch {
	case req.WalletID == nil:
		writeError(w, invalidRequest("The field walletId is required."))
		return
	case req.OperationType == nil:
		writeError(w, invalidRequest("The field operationType is required."))
		return
	case req.Amount == nil:
		writeError(w, invalidRequest("The field amount is required."))
		return
	case *req.Amount < 0:
		writeError(w, invalidRequest(fieldDetails["/amount"]))
		return
	}

	id, amount := *req.WalletID, *req.Amount
	var (
		balance int64
		err     error
	)
	switch *req.OperationType {
	case operationDeposit:
		balance, err = h.store.Deposit(r.Context(), id, amount)
	case operationWithdraw:
		balance, err = h.store.Withdraw(r.Context(), id, amount)
	default:
		writeError(w, invalidRequest(fieldDetails["/operationType"]))
		return
	}
	if err != nil {
		writeError(w, errorFor(r.Method, id, err))
		return
	}

	writeJSON(w, http.StatusOK, "application/json", walletResponse{WalletID: id, Balance: balance})
}

// decodeDetail gives the detail for an error of the decoder.
func decodeDetail(err error) string {

	var maxBytesErr *http.MaxBytesError
	var semanticErr *json.SemanticError

	switch {
	case errors.As(err, &maxBytesErr):
		return "The request body is larger than " + strconv.Itoa(maxBodyBytes) + " bytes."

	case errors.Is(err, json.ErrUnknownName) && errors.As(err, &semanticErr):
		return "The field " + strings.TrimPrefix(string(semanticErr.JSONPointer), "/") + " is unknown."

	case errors.As(err, &semanticErr):
		if detail, ok := fieldDetails[string(semanticErr.JSONPointer)]; ok {
			return detail
		}
		// An empty pointer means that the top-level value is not an object.
		return "The request body must be a JSON object."

	default:
		return "The request body is not valid JSON."
	}
}

func (h *handler) balance(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("walletId"))
	if err != nil {
		writeError(w, invalidRequest("The wallet ID is not a valid UUID."))
		return
	}

	balance, err := h.store.Balance(r.Context(), id)
	if err != nil {
		writeError(w, errorFor(r.Method, id, err))
		return
	}

	writeJSON(w, http.StatusOK, "application/json", walletResponse{WalletID: id, Balance: balance})
}

func methodNotAllowed(allow string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeError(w, errorBody{
			Status: http.StatusMethodNotAllowed,
			Detail: "The method " + r.Method + " is not allowed. Use " + allow + ".",
			Code:   "METHOD_NOT_ALLOWED",
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, contentType string, body any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	// The body is a plain struct, so only the write can fail. Then the client is gone, and nothing remains to do.
	_ = json.MarshalWrite(w, body)
}
