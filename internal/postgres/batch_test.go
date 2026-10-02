package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"wallet/internal/postgres"
	"wallet/internal/wallet"
)

func TestStoreBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("the writes that wait for one commit go into one transaction", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)
		countUpdates(t, conn)

		// The first write takes the row lock queue. The next writes collect in the queue of the Store.
		unlock := lockWallet(t, conn, id)
		first := goWrite(store.Deposit, id, 10)
		waitForLock(t, conn)

		// The test starts the writes one at a time, so that the queue order is known.
		writes := []struct {
			call    func(context.Context, uuid.UUID, int64) (int64, error)
			amount  int64
			balance int64
			err     error
		}{
			{store.Withdraw, 150, 0, wallet.ErrInsufficientFunds},
			{store.Deposit, 40, 150, nil},
			{store.Withdraw, 150, 0, nil},
		}
		dones := make([]<-chan writeResult, len(writes))
		for i, w := range writes {
			dones[i] = goWrite(w.call, id, w.amount)
			waitForQueueLen(t, store, id, i+1)
		}

		unlock()
		if r := <-first; r.err != nil || r.balance != 110 {
			t.Errorf("first Deposit = %d, %v, want 110, nil", r.balance, r.err)
		}
		for i, w := range writes {
			r := <-dones[i]
			if r.balance != w.balance || !errors.Is(r.err, w.err) {
				t.Errorf("write %d = %d, %v, want %d, %v", i, r.balance, r.err, w.balance, w.err)
			}
		}

		assertBalance(t, conn, id, 0)
		// One UPDATE for the first write, and one for the batch of the three next writes.
		assertUpdates(t, conn, 2)
	})

	t.Run("a cancelled write in the queue does not apply", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)

		unlock := lockWallet(t, conn, id)
		first := goWrite(store.Deposit, id, 10)
		waitForLock(t, conn)

		cancelled, cancel := context.WithCancel(ctx)
		done := make(chan error)
		go func() {
			_, err := store.Deposit(cancelled, id, 5)
			done <- err
		}()
		waitForQueueLen(t, store, id, 1)
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}

		unlock()
		if r := <-first; r.err != nil || r.balance != 110 {
			t.Errorf("first Deposit = %d, %v, want 110, nil", r.balance, r.err)
		}
		assertBalance(t, conn, id, 110)
	})

	t.Run("an error of the batch goes to every write in it", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)

		unlock := lockWallet(t, conn, id)
		first := goWrite(store.Deposit, id, 10)
		pid := waitForLock(t, conn)

		second := goWrite(store.Deposit, id, 20)
		third := goWrite(store.Withdraw, id, 30)
		waitForQueueLen(t, store, id, 2)

		// The server sends 57P01 to the batch of the first write. The batch of the two next writes then waits for the lock.
		terminate(t, conn, pid)
		if r := <-first; !errors.Is(r.err, wallet.ErrUnavailable) {
			t.Errorf("first Deposit err = %v, want ErrUnavailable", r.err)
		}
		waitForExit(t, conn, pid)

		terminate(t, conn, waitForLock(t, conn))
		for _, done := range []<-chan writeResult{second, third} {
			if r := <-done; !errors.Is(r.err, wallet.ErrUnavailable) {
				t.Errorf("err = %v, want ErrUnavailable", r.err)
			}
		}

		unlock()
		assertBalance(t, conn, id, 100)
	})

	t.Run("the queue of an idle wallet goes away", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)

		if _, err := store.Deposit(ctx, id, 10); err != nil {
			t.Fatalf("Deposit: %v", err)
		}

		// The worker removes the queue after it delivers the results, so the test waits for it.
		deadline := time.Now().Add(5 * time.Second)
		for postgres.QueueCount(store) != 0 {
			if time.Now().After(deadline) {
				t.Fatalf("%d queues remain after 5s, want 0", postgres.QueueCount(store))
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	t.Run("Close waits for the batch and the queued writes", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)

		unlock := lockWallet(t, conn, id)
		first := goWrite(store.Deposit, id, 10)
		waitForLock(t, conn)
		second := goWrite(store.Deposit, id, 5)
		waitForQueueLen(t, store, id, 1)

		closed := make(chan struct{})
		go func() {
			store.Close()
			close(closed)
		}()

		// The row lock holds the batch, so Close must still wait after this delay.
		select {
		case <-closed:
			t.Fatal("Close returned before the batch committed")
		case <-time.After(100 * time.Millisecond):
		}

		unlock()
		if r := <-first; r.err != nil || r.balance != 110 {
			t.Errorf("first Deposit = %d, %v, want 110, nil", r.balance, r.err)
		}
		if r := <-second; r.err != nil || r.balance != 115 {
			t.Errorf("second Deposit = %d, %v, want 115, nil", r.balance, r.err)
		}
		<-closed
		assertBalance(t, conn, id, 115)
	})

	t.Run("a call after Close gets ErrUnavailable", func(t *testing.T) {
		store, conn := newStore(t)
		id := insertWallet(t, conn, 100)

		store.Close()

		if _, err := store.Deposit(ctx, id, 10); !errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("Deposit err = %v, want ErrUnavailable", err)
		}
		if _, err := store.Balance(ctx, id); !errors.Is(err, wallet.ErrUnavailable) {
			t.Errorf("Balance err = %v, want ErrUnavailable", err)
		}
		if n := postgres.QueueCount(store); n != 0 {
			t.Errorf("queues = %d after Close, want 0", n)
		}
		assertBalance(t, conn, id, 100)
	})
}

type writeResult struct {
	balance int64
	err     error
}

// goWrite runs a Deposit or a Withdraw in a goroutine and returns the channel of its result.
func goWrite(call func(context.Context, uuid.UUID, int64) (int64, error), id uuid.UUID, amount int64) <-chan writeResult {
	done := make(chan writeResult, 1)
	go func() {
		balance, err := call(context.Background(), id, amount)
		done <- writeResult{balance, err}
	}()
	return done
}

func terminate(t *testing.T, conn *pgx.Conn, pid int32) {
	t.Helper()

	if _, err := conn.Exec(context.Background(), "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("terminate the backend: %v", err)
	}
}

// waitForQueueLen waits until want writes wait in the queue of the wallet.
func waitForQueueLen(t *testing.T, store *postgres.Store, id uuid.UUID, want int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if postgres.QueueLen(store, id) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queue length = %d after 5s, want %d", postgres.QueueLen(store, id), want)
}

// countUpdates makes a trigger that writes one row into balance_updates for each UPDATE statement on wallets.
func countUpdates(t *testing.T, conn *pgx.Conn) {
	t.Helper()

	if _, err := conn.Exec(context.Background(), `
		CREATE TABLE balance_updates (n int);
		CREATE FUNCTION count_update() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			INSERT INTO balance_updates VALUES (1);
			RETURN NULL;
		END $$;
		CREATE TRIGGER count_update AFTER UPDATE ON wallets FOR EACH STATEMENT EXECUTE FUNCTION count_update();
	`); err != nil {
		t.Fatalf("make the update counter: %v", err)
	}
}

func assertUpdates(t *testing.T, conn *pgx.Conn, want int) {
	t.Helper()

	var n int
	if err := conn.QueryRow(context.Background(), "SELECT count(*) FROM balance_updates").Scan(&n); err != nil {
		t.Fatalf("count the updates: %v", err)
	}
	if n != want {
		t.Errorf("UPDATE statements = %d, want %d", n, want)
	}
}
