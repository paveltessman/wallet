package httpapi_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"uuid"

	"wallet/internal/httpapi"
	"wallet/internal/wallet"
)

const walletID = "5f1c8a2e-4b7d-4c1a-9f3e-2d6b8a0c7e41"

func TestBalance(t *testing.T) {
	t.Run("returns the balance", func(t *testing.T) {
		var got uuid.UUID
		store := &fakeStore{t: t, balance: func(_ context.Context, id uuid.UUID) (int64, error) {
			got = id
			return 1500, nil
		}}

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID)

		assertStatus(t, rec, http.StatusOK, "application/json")
		if got != uuid.MustParse(walletID) {
			t.Errorf("store got ID %s, want %s", got, walletID)
		}
		assertBody(t, rec, map[string]any{"walletId": walletID, "balance": 1500.0})
	})

	t.Run("returns the ID in the canonical form", func(t *testing.T) {
		store := &fakeStore{t: t, balance: func(context.Context, uuid.UUID) (int64, error) { return 0, nil }}

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/urn:uuid:"+strings.ToUpper(walletID))

		assertStatus(t, rec, http.StatusOK, "application/json")
		assertBody(t, rec, map[string]any{"walletId": walletID, "balance": 0.0})
	})

	t.Run("a bad UUID gives 400", func(t *testing.T) {
		rec := serve(t, &fakeStore{t: t}, http.MethodGet, "/api/v1/wallet/not-a-uuid")

		assertError(t, rec, http.StatusBadRequest, "INVALID_REQUEST", false)
	})

	t.Run("a store error gives an error body", func(t *testing.T) {
		store := &fakeStore{t: t, balance: func(context.Context, uuid.UUID) (int64, error) {
			return 0, fmt.Errorf("get the balance: %w", wallet.ErrNotFound)
		}}

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID)

		assertError(t, rec, http.StatusNotFound, "WALLET_NOT_FOUND", false)
		assertBody(t, rec, map[string]any{
			"title":     "Not Found",
			"status":    404.0,
			"detail":    "Wallet " + walletID + " does not exist.",
			"code":      "WALLET_NOT_FOUND",
			"retryable": false,
		})
	})

	t.Run("an overload gives 429 with no Retry-After", func(t *testing.T) {
		store := &fakeStore{t: t, balance: func(context.Context, uuid.UUID) (int64, error) {
			return 0, wallet.ErrOverloaded
		}}

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID)

		assertError(t, rec, http.StatusTooManyRequests, "SERVICE_OVERLOADED", true)
		// Spec decision 10: the client chooses the retry delay.
		if v := rec.Header().Get("Retry-After"); v != "" {
			t.Errorf("Retry-After = %q, want no header", v)
		}
	})

	t.Run("an unexpected error does not show its text", func(t *testing.T) {
		store := &fakeStore{t: t, balance: func(context.Context, uuid.UUID) (int64, error) {
			return 0, errors.New("secret internal detail")
		}}

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID)

		assertError(t, rec, http.StatusInternalServerError, "INTERNAL_ERROR", false)
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("body = %s, want no error text", rec.Body)
		}
	})
}

func TestMethodNotAllowed(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := serve(t, &fakeStore{t: t}, method, "/api/v1/wallet/"+walletID)

			assertError(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", false)
			if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
				t.Errorf("Allow = %q, want %q", allow, "GET, HEAD")
			}
		})
	}
}

// fakeStore calls the function of each method. A call to a nil function fails the test.
type fakeStore struct {
	t        *testing.T
	deposit  func(ctx context.Context, id uuid.UUID, amount int64) (int64, error)
	withdraw func(ctx context.Context, id uuid.UUID, amount int64) (int64, error)
	balance  func(ctx context.Context, id uuid.UUID) (int64, error)
}

func (f *fakeStore) Deposit(ctx context.Context, id uuid.UUID, amount int64) (int64, error) {
	if f.deposit == nil {
		f.t.Error("unexpected call to Deposit")
		return 0, errors.New("unexpected call")
	}
	return f.deposit(ctx, id, amount)
}

func (f *fakeStore) Withdraw(ctx context.Context, id uuid.UUID, amount int64) (int64, error) {
	if f.withdraw == nil {
		f.t.Error("unexpected call to Withdraw")
		return 0, errors.New("unexpected call")
	}
	return f.withdraw(ctx, id, amount)
}

func (f *fakeStore) Balance(ctx context.Context, id uuid.UUID) (int64, error) {
	if f.balance == nil {
		f.t.Error("unexpected call to Balance")
		return 0, errors.New("unexpected call")
	}
	return f.balance(ctx, id)
}

func serve(t *testing.T, store httpapi.Store, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	httpapi.NewHandler(store).ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, status int, contentType string) {
	t.Helper()

	if rec.Code != status {
		t.Errorf("status = %d, want %d, body %s", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentType {
		t.Errorf("Content-Type = %q, want %q", ct, contentType)
	}
}

// assertBody compares the full body, so an extra member also fails the test.
func assertBody(t *testing.T, rec *httptest.ResponseRecorder, want map[string]any) {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode the body %s: %v", rec.Body, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string, retryable bool) {
	t.Helper()

	assertStatus(t, rec, status, "application/problem+json")

	var got struct {
		Title     string `json:"title"`
		Status    int    `json:"status"`
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode the error body %s: %v", rec.Body, err)
	}
	if got.Title != http.StatusText(status) || got.Status != status || got.Code != code || got.Retryable != retryable {
		t.Errorf("error body = %+v, want %q %d %s retryable %t", got, http.StatusText(status), status, code, retryable)
	}
}
