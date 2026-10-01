package postgres_test

import (
	"context"
	"errors"
	"math"
	"net"
	"net/url"
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

func TestStoreErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("a full pool gives ErrOverloaded", func(t *testing.T) {
		store, conn := newStoreWith(t, 1, 100*time.Millisecond)
		id := insertWallet(t, conn, 100)

		// The Withdraw holds the only connection of the pool while it waits for the row lock.
		unlock := lockWallet(t, conn, id)
		done := make(chan error)
		go func() {
			_, err := store.Withdraw(ctx, id, 10)
			done <- err
		}()
		waitForLock(t, conn)

		if _, err := store.Balance(ctx, id); !errors.Is(err, wallet.ErrOverloaded) {
			t.Errorf("err = %v, want ErrOverloaded", err)
		}

		unlock()
		if err := <-done; err != nil {
			t.Errorf("Withdraw after the unlock: %v", err)
		}
	})

	t.Run("ping succeeds on a live database", func(t *testing.T) {
		store, _ := newStore(t)

		if err := store.Ping(ctx); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})

	t.Run("ping fails on a refused connection", func(t *testing.T) {
		store, err := postgres.New(ctx, postgres.Config{URL: closedPortURL(t), MaxConns: 1, AcquireTimeout: 5 * time.Second})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(store.Close)

		if err := store.Ping(ctx); err == nil {
			t.Error("Ping err = nil, want an error")
		}
	})

	t.Run("a refused connection gives ErrUnavailable", func(t *testing.T) {
		store, err := postgres.New(ctx, postgres.Config{URL: closedPortURL(t), MaxConns: 1, AcquireTimeout: 5 * time.Second})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(store.Close)
		id := uuid.New()

		if _, err := store.Deposit(ctx, id, 10); !errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("Deposit err = %v, want ErrUnavailable", err)
		}
		if _, err := store.Withdraw(ctx, id, 10); !errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("Withdraw err = %v, want ErrUnavailable", err)
		}
		if _, err := store.Balance(ctx, id); !errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("Balance err = %v, want ErrUnavailable", err)
		}
	})

	t.Run("a terminated backend gives ErrUnavailable", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)

		unlock := lockWallet(t, conn, id)
		done := make(chan error)
		go func() {
			_, err := store.Deposit(ctx, id, 10)
			done <- err
		}()
		pid := waitForLock(t, conn)

		// The server sends 57P01 admin_shutdown before it closes the connection.
		if _, err := conn.Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
			t.Fatalf("terminate the backend: %v", err)
		}
		if err := <-done; !errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("err = %v, want ErrUnavailable", err)
		}

		unlock()
		assertBalance(t, conn, id, 100)
	})

	t.Run("a lost connection before the commit gives ErrUnavailable", func(t *testing.T) {
		store, conn, proxy := newProxyStore(t)
		id := insertWallet(t, conn, 100)

		// The Deposit waits for the row lock in its transaction. Then the network fails.
		unlock := lockWallet(t, conn, id)
		done := make(chan error)
		go func() {
			_, err := store.Deposit(ctx, id, 10)
			done <- err
		}()
		pid := waitForLock(t, conn)
		proxy.cut()

		if err := <-done; !errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("err = %v, want ErrUnavailable", err)
		}

		// After the unlock, the backend finds the lost client and rolls back.
		unlock()
		waitForExit(t, conn, pid)
		assertBalance(t, conn, id, 100)
	})

	t.Run("a lost commit result gives ErrOutcomeUnknown", func(t *testing.T) {
		store, conn, proxy := newProxyStore(t)
		id := insertWallet(t, conn, 100)

		// The deferred trigger runs in the COMMIT and waits for the advisory lock of the test.
		if _, err := conn.Exec(ctx, `
			CREATE FUNCTION wait_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
				PERFORM pg_advisory_xact_lock(1);
				RETURN NULL;
			END $$;
			CREATE CONSTRAINT TRIGGER wait_at_commit AFTER UPDATE ON wallets
			DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION wait_for_test();
			SELECT pg_advisory_lock(1);
		`); err != nil {
			t.Fatalf("make the commit wait: %v", err)
		}

		done := make(chan error)
		go func() {
			_, err := store.Deposit(ctx, id, 10)
			done <- err
		}()
		waitForLock(t, conn)
		proxy.cut()

		if err := <-done; !errors.Is(err, wallet.ErrOutcomeUnknown) {
			t.Errorf("err = %v, want ErrOutcomeUnknown", err)
		}

		// The server does not see the lost client while it waits for the lock. The commit completes after the unlock.
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock(1)"); err != nil {
			t.Fatalf("unlock: %v", err)
		}
		waitForBalance(t, conn, id, 110)
	})

	t.Run("a cancelled caller gives no domain error", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)

		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		_, err := store.Balance(cancelled, id)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if errors.Is(err, wallet.ErrOverloaded) || errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("err = %v, want no domain error", err)
		}
	})
}

// newStore makes a Store on a migrated test database.
// It also returns a direct connection for the test setup and the checks.
func newStore(t *testing.T) (*postgres.Store, *pgx.Conn) {
	t.Helper()
	return newStoreWith(t, 10, 5*time.Second)
}

func newStoreWith(t *testing.T, maxConns int32, acquireTimeout time.Duration) (*postgres.Store, *pgx.Conn) {
	t.Helper()

	dbURL := testdb.Migrated(t)
	ctx := context.Background()

	store, err := postgres.New(ctx, postgres.Config{URL: dbURL, MaxConns: maxConns, AcquireTimeout: acquireTimeout})
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

// newProxyStore makes a Store that reaches a migrated test database through a proxy, so that the test can cut the network.
// It also returns a direct connection for the test setup and the checks.
func newProxyStore(t *testing.T) (*postgres.Store, *pgx.Conn, *proxy) {
	t.Helper()

	dbURL := testdb.Migrated(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to the test database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	proxyURL, err := url.Parse(dbURL)
	if err != nil {
		t.Fatalf("parse the database URL: %v", err)
	}
	p := newProxy(t, proxyURL.Host)
	proxyURL.Host = p.addr()

	store, err := postgres.New(ctx, postgres.Config{URL: proxyURL.String(), MaxConns: 1, AcquireTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(store.Close)

	return store, conn, p
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

// lockWallet locks the row of the wallet in a transaction on a new connection. The returned function ends the transaction.
// The lock does not use conn, because pg_stat_activity keeps one snapshot for a transaction and waitForLock reads it on conn.
func lockWallet(t *testing.T, conn *pgx.Conn, id uuid.UUID) (unlock func()) {
	t.Helper()

	ctx := context.Background()
	lockConn, err := pgx.ConnectConfig(ctx, conn.Config())
	if err != nil {
		t.Fatalf("connect for the lock: %v", err)
	}
	if _, err := lockConn.Exec(ctx, "BEGIN"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := lockConn.Exec(ctx, "SELECT 1 FROM wallets WHERE id = $1 FOR UPDATE", id); err != nil {
		t.Fatalf("lock the wallet: %v", err)
	}

	var once sync.Once
	unlock = func() {
		once.Do(func() {
			// Close ends the transaction with a rollback.
			if err := lockConn.Close(ctx); err != nil {
				t.Errorf("close the lock connection: %v", err)
			}
		})
	}
	t.Cleanup(unlock)
	return unlock
}

// waitForLock waits until one backend of the test database waits for a row lock, and returns its PID.
func waitForLock(t *testing.T, conn *pgx.Conn) int32 {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var pid int32
		err := conn.QueryRow(context.Background(),
			"SELECT pid FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'",
		).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no backend waits for a row lock after 5s")
	return 0
}

// waitForExit waits until the backend with the PID ends.
func waitForExit(t *testing.T, conn *pgx.Conn, pid int32) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var exists bool
		if err := conn.QueryRow(context.Background(),
			"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = $1)", pid,
		).Scan(&exists); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if !exists {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d still runs after 5s", pid)
}

// closedPortURL returns a database URL with a local port that refuses connections.
func closedPortURL(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close the listener: %v", err)
	}
	return "postgres://wallet:wallet@" + addr + "/wallet?sslmode=disable"
}

// waitForBalance waits until the balance of the wallet is want.
func waitForBalance(t *testing.T, conn *pgx.Conn, id uuid.UUID, want int64) {
	t.Helper()

	var balance int64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := conn.QueryRow(context.Background(), "SELECT balance FROM wallets WHERE id = $1", id).Scan(&balance); err != nil {
			t.Fatalf("read the balance: %v", err)
		}
		if balance == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("balance = %d after 5s, want %d", balance, want)
}
