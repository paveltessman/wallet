// Package httpapi serves the wallet API over HTTP.
package httpapi

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"uuid"
)

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
	mux.HandleFunc("GET /api/v1/wallet/{walletId}", h.balance)
	// A pattern without a method gets the other methods. The pattern with a method is more specific, so it wins.
	mux.Handle("/api/v1/wallet/{walletId}", methodNotAllowed("GET, HEAD"))

	return mux
}

type walletResponse struct {
	WalletID uuid.UUID `json:"walletId"`
	Balance  int64     `json:"balance"`
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
