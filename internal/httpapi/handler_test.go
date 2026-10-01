package httpapi_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
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

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID, "")

		assertStatus(t, rec, http.StatusOK, "application/json")
		if got != uuid.MustParse(walletID) {
			t.Errorf("store got ID %s, want %s", got, walletID)
		}
		assertBody(t, rec, map[string]any{"walletId": walletID, "balance": 1500.0})
	})

	t.Run("returns the ID in the canonical form", func(t *testing.T) {
		store := &fakeStore{t: t, balance: func(context.Context, uuid.UUID) (int64, error) { return 0, nil }}

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/urn:uuid:"+strings.ToUpper(walletID), "")

		assertStatus(t, rec, http.StatusOK, "application/json")
		assertBody(t, rec, map[string]any{"walletId": walletID, "balance": 0.0})
	})

	t.Run("a bad UUID gives 400", func(t *testing.T) {
		rec := serve(t, &fakeStore{t: t}, http.MethodGet, "/api/v1/wallet/not-a-uuid", "")

		assertError(t, rec, http.StatusBadRequest, "INVALID_REQUEST", false)
	})

	t.Run("a store error gives an error body", func(t *testing.T) {
		store := &fakeStore{t: t, balance: func(context.Context, uuid.UUID) (int64, error) {
			return 0, fmt.Errorf("get the balance: %w", wallet.ErrNotFound)
		}}

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID, "")

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

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID, "")

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

		rec := serve(t, store, http.MethodGet, "/api/v1/wallet/"+walletID, "")

		assertError(t, rec, http.StatusInternalServerError, "INTERNAL_ERROR", false)
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("body = %s, want no error text", rec.Body)
		}
	})
}

func TestOperation(t *testing.T) {
	t.Run("deposit returns the new balance", func(t *testing.T) {
		var gotID uuid.UUID
		var gotAmount int64
		store := &fakeStore{t: t, deposit: func(_ context.Context, id uuid.UUID, amount int64) (int64, error) {
			gotID, gotAmount = id, amount
			return 1500, nil
		}}

		rec := post(t, store, `{"walletId":"`+walletID+`","operationType":"DEPOSIT","amount":1000}`)

		assertStatus(t, rec, http.StatusOK, "application/json")
		if gotID != uuid.MustParse(walletID) || gotAmount != 1000 {
			t.Errorf("Deposit got %s %d, want %s 1000", gotID, gotAmount, walletID)
		}
		assertBody(t, rec, map[string]any{"walletId": walletID, "balance": 1500.0})
	})

	t.Run("withdraw returns the new balance", func(t *testing.T) {
		var gotID uuid.UUID
		var gotAmount int64
		store := &fakeStore{t: t, withdraw: func(_ context.Context, id uuid.UUID, amount int64) (int64, error) {
			gotID, gotAmount = id, amount
			return 500, nil
		}}

		rec := post(t, store, `{"walletId":"`+walletID+`","operationType":"WITHDRAW","amount":1000}`)

		assertStatus(t, rec, http.StatusOK, "application/json")
		if gotID != uuid.MustParse(walletID) || gotAmount != 1000 {
			t.Errorf("Withdraw got %s %d, want %s 1000", gotID, gotAmount, walletID)
		}
		assertBody(t, rec, map[string]any{"walletId": walletID, "balance": 500.0})
	})

	for _, amount := range []int64{0, math.MaxInt64} {
		t.Run(fmt.Sprintf("amount %d is valid", amount), func(t *testing.T) {
			var got int64 = -1
			store := &fakeStore{t: t, deposit: func(_ context.Context, _ uuid.UUID, amount int64) (int64, error) {
				got = amount
				return amount, nil
			}}

			rec := post(t, store, fmt.Sprintf(`{"walletId":"%s","operationType":"DEPOSIT","amount":%d}`, walletID, amount))

			assertStatus(t, rec, http.StatusOK, "application/json")
			if got != amount {
				t.Errorf("Deposit got amount %d, want %d", got, amount)
			}
		})
	}

	t.Run("a store error gives an error body", func(t *testing.T) {
		store := &fakeStore{t: t, withdraw: func(context.Context, uuid.UUID, int64) (int64, error) {
			return 0, fmt.Errorf("withdraw: %w", wallet.ErrInsufficientFunds)
		}}

		rec := post(t, store, `{"walletId":"`+walletID+`","operationType":"WITHDRAW","amount":1000}`)

		assertError(t, rec, http.StatusConflict, "INSUFFICIENT_FUNDS", false)
	})

	t.Run("an unknown outcome is not retryable", func(t *testing.T) {
		store := &fakeStore{t: t, deposit: func(context.Context, uuid.UUID, int64) (int64, error) {
			return 0, fmt.Errorf("deposit: %w", wallet.ErrOutcomeUnknown)
		}}

		rec := post(t, store, `{"walletId":"`+walletID+`","operationType":"DEPOSIT","amount":1000}`)

		assertError(t, rec, http.StatusServiceUnavailable, "OPERATION_OUTCOME_UNKNOWN", false)
	})
}

func TestOperationInvalidRequest(t *testing.T) {
	const (
		notJSON   = "The request body is not valid JSON."
		badID     = "The field walletId must be a UUID."
		badType   = "The field operationType must be DEPOSIT or WITHDRAW."
		badAmount = "The field amount must be an integer from 0 to 9223372036854775807."
	)
	// body gives a valid request body with one member replaced. An empty value removes the member.
	body := func(name, value string) string {
		members := map[string]string{
			"walletId":      `"` + walletID + `"`,
			"operationType": `"DEPOSIT"`,
			"amount":        "1000",
		}
		members[name] = value
		var parts []string
		for _, n := range []string{"walletId", "operationType", "amount"} {
			if members[n] != "" {
				parts = append(parts, `"`+n+`":`+members[n])
			}
		}
		return "{" + strings.Join(parts, ",") + "}"
	}

	tests := []struct {
		name   string
		body   string
		detail string
	}{
		{"an empty body", "", notJSON},
		{"malformed JSON", "{", notJSON},
		{"trailing data", body("", "") + "{}", notJSON},
		{"a duplicate field", `{"amount":1,"amount":2}`, notJSON},
		{"an array", "[]", "The request body must be a JSON object."},
		{"null", "null", "The field walletId is required."},
		{"the task typo valletId", strings.Replace(body("", ""), "walletId", "valletId", 1), "The field valletId is unknown."},
		{"a field in a wrong case", strings.Replace(body("", ""), "walletId", "walletID", 1), "The field walletID is unknown."},
		{"no walletId", body("walletId", ""), "The field walletId is required."},
		{"no operationType", body("operationType", ""), "The field operationType is required."},
		{"no amount", body("amount", ""), "The field amount is required."},
		{"a null amount", body("amount", "null"), "The field amount is required."},
		{"a bad UUID", body("walletId", `"not-a-uuid"`), badID},
		{"a number as walletId", body("walletId", "1"), badID},
		{"a lowercase operationType", body("operationType", `"deposit"`), badType},
		{"an unknown operationType", body("operationType", `"TRANSFER"`), badType},
		{"a number as operationType", body("operationType", "1"), badType},
		{"a negative amount", body("amount", "-1"), badAmount},
		{"a fraction", body("amount", "10.5"), badAmount},
		{"a fraction with a zero part", body("amount", "1.0"), badAmount},
		{"an exponent", body("amount", "1e3"), badAmount},
		{"a string amount", body("amount", `"1000"`), badAmount},
		{"an amount above int64", body("amount", "9223372036854775808"), badAmount},
		{"a body above the limit", body("", "") + strings.Repeat(" ", 1024), "The request body is larger than 1024 bytes."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The fake fails the test on any call, so no invalid request reaches the store.
			rec := post(t, &fakeStore{t: t}, tt.body)

			assertError(t, rec, http.StatusBadRequest, "INVALID_REQUEST", false)
			var got struct {
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode the error body %s: %v", rec.Body, err)
			}
			if got.Detail != tt.detail {
				t.Errorf("detail = %q, want %q", got.Detail, tt.detail)
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	tests := []struct {
		method string
		target string
		allow  string
	}{
		{http.MethodGet, "/api/v1/wallet", "POST"},
		{http.MethodPut, "/api/v1/wallet", "POST"},
		{http.MethodPost, "/api/v1/wallet/" + walletID, "GET, HEAD"},
		{http.MethodPut, "/api/v1/wallet/" + walletID, "GET, HEAD"},
		{http.MethodDelete, "/api/v1/wallet/" + walletID, "GET, HEAD"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			rec := serve(t, &fakeStore{t: t}, tt.method, tt.target, "")

			assertError(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", false)
			if allow := rec.Header().Get("Allow"); allow != tt.allow {
				t.Errorf("Allow = %q, want %q", allow, tt.allow)
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

func serve(t *testing.T, store httpapi.Store, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	httpapi.NewHandler(store).ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func post(t *testing.T, store httpapi.Store, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, store, http.MethodPost, "/api/v1/wallet", body)
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
