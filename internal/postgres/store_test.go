package postgres_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"wallet/internal/postgres"
	"wallet/internal/testdb"
	"wallet/internal/wallet"
)

func TestStore(t *testing.T) {
	ctx := context.Background()
	store, conn := newStore(t)

	t.Run("deposit adds the amount", func(t *testing.T) {
		id := insertWallet(t, conn, 100)

		balance, err := store.Deposit(ctx, id, 50)
		if err != nil {
			t.Fatalf("Deposit: %v", err)
		}
		if balance != 150 {
			t.Errorf("balance = %d, want 150", balance)
		}
		assertBalance(t, conn, id, 150)
	})

	t.Run("withdraw subtracts the amount", func(t *testing.T) {
		id := insertWallet(t, conn, 100)

		balance, err := store.Withdraw(ctx, id, 30)
		if err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		if balance != 70 {
			t.Errorf("balance = %d, want 70", balance)
		}
		assertBalance(t, conn, id, 70)
	})

	t.Run("withdraw of the full balance leaves zero", func(t *testing.T) {
		id := insertWallet(t, conn, 100)

		balance, err := store.Withdraw(ctx, id, 100)
		if err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		if balance != 0 {
			t.Errorf("balance = %d, want 0", balance)
		}
	})

	t.Run("a zero amount changes nothing", func(t *testing.T) {
		id := insertWallet(t, conn, 100)

		if balance, err := store.Deposit(ctx, id, 0); err != nil || balance != 100 {
			t.Errorf("Deposit 0 = %d, %v, want 100, nil", balance, err)
		}
		if balance, err := store.Withdraw(ctx, id, 0); err != nil || balance != 100 {
			t.Errorf("Withdraw 0 = %d, %v, want 100, nil", balance, err)
		}
	})

	t.Run("balance returns the stored balance", func(t *testing.T) {
		id := insertWallet(t, conn, 42)

		balance, err := store.Balance(ctx, id)
		if err != nil {
			t.Fatalf("Balance: %v", err)
		}
		if balance != 42 {
			t.Errorf("balance = %d, want 42", balance)
		}
	})

	t.Run("an unknown wallet gives ErrNotFound", func(t *testing.T) {
		id := uuid.New()

		if _, err := store.Deposit(ctx, id, 10); !errors.Is(err, wallet.ErrNotFound) {
			t.Errorf("Deposit err = %v, want ErrNotFound", err)
		}
		if _, err := store.Withdraw(ctx, id, 10); !errors.Is(err, wallet.ErrNotFound) {
			t.Errorf("Withdraw err = %v, want ErrNotFound", err)
		}
		if _, err := store.Balance(ctx, id); !errors.Is(err, wallet.ErrNotFound) {
			t.Errorf("Balance err = %v, want ErrNotFound", err)
		}
	})

	t.Run("an overdraw gives ErrInsufficientFunds", func(t *testing.T) {
		id := insertWallet(t, conn, 100)

		if _, err := store.Withdraw(ctx, id, 101); !errors.Is(err, wallet.ErrInsufficientFunds) {
			t.Errorf("err = %v, want ErrInsufficientFunds", err)
		}
		assertBalance(t, conn, id, 100)
	})

	t.Run("an overflow gives ErrBalanceLimitExceeded", func(t *testing.T) {
		id := insertWallet(t, conn, math.MaxInt64-10)

		if _, err := store.Deposit(ctx, id, 11); !errors.Is(err, wallet.ErrBalanceLimitExceeded) {
			t.Errorf("err = %v, want ErrBalanceLimitExceeded", err)
		}
		assertBalance(t, conn, id, math.MaxInt64-10)
	})

	t.Run("a negative amount gives an error", func(t *testing.T) {
		id := insertWallet(t, conn, 100)

		if _, err := store.Deposit(ctx, id, -1); err == nil {
			t.Error("Deposit -1: err = nil, want an error")
		}
		if _, err := store.Withdraw(ctx, id, -1); err == nil {
			t.Error("Withdraw -1: err = nil, want an error")
		}
		assertBalance(t, conn, id, 100)
	})
}

func TestStoreConcurrency(t *testing.T) {
	const (
		start    = 500
		calls    = 100
		deposit  = 10
		withdraw = 15
	)

	ctx := context.Background()
	store, conn := newStore(t)
	id := insertWallet(t, conn, start)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		withdrawn int
	)
	for range calls {
		wg.Go(func() {
			if _, err := store.Deposit(ctx, id, deposit); err != nil {
				t.Errorf("Deposit: %v", err)
			}
		})
		wg.Go(func() {
			balance, err := store.Withdraw(ctx, id, withdraw)
			switch {
			case err == nil:
				if balance < 0 {
					t.Errorf("balance after Withdraw = %d, want 0 or more", balance)
				}
				mu.Lock()
				withdrawn++
				mu.Unlock()
			case errors.Is(err, wallet.ErrInsufficientFunds):
			default:
				t.Errorf("Withdraw: %v", err)
			}
		})
	}
	wg.Wait()

	// The test cannot predict how many withdraws succeed. It checks that the sum agrees with the count.
	assertBalance(t, conn, id, start+calls*deposit-int64(withdrawn)*withdraw)
}

// newStore makes a Store on a migrated test database.
// It also returns a direct connection for the test setup and the checks.
func newStore(t *testing.T) (*postgres.Store, *pgx.Conn) {
	t.Helper()

	dbURL := testdb.Migrated(t)
	ctx := context.Background()

	store, err := postgres.New(ctx, postgres.Config{URL: dbURL, MaxConns: 10, AcquireTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(store.Close)

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to the test database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	return store, conn
}

// insertWallet inserts a wallet with the balance and returns its ID.
func insertWallet(t *testing.T, conn *pgx.Conn, balance int64) uuid.UUID {
	t.Helper()

	id := uuid.New()
	if _, err := conn.Exec(context.Background(), "INSERT INTO wallets (id, balance) VALUES ($1, $2)", id, balance); err != nil {
		t.Fatalf("insert the wallet: %v", err)
	}
	return id
}

// assertBalance reads the balance directly from the table, not through the Store.
func assertBalance(t *testing.T, conn *pgx.Conn, id uuid.UUID, want int64) {
	t.Helper()

	var balance int64
	if err := conn.QueryRow(context.Background(), "SELECT balance FROM wallets WHERE id = $1", id).Scan(&balance); err != nil {
		t.Fatalf("read the balance: %v", err)
	}
	if balance != want {
		t.Errorf("balance = %d, want %d", balance, want)
	}
}
