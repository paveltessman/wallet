// Package postgres stores the wallets in PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet/internal/postgres/internal/sqlc"
	"wallet/internal/wallet"
)

// Config holds the settings of the connection pool.
type Config struct {
	URL            string
	MaxConns       int32
	AcquireTimeout time.Duration
}

type Store struct {
	pool           *pgxpool.Pool
	acquireTimeout time.Duration
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse the database URL: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("make the connection pool: %w", err)
	}

	return &Store{pool: pool, acquireTimeout: cfg.AcquireTimeout}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

// Ping makes sure that the server accepts a connection with the configured credentials.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping the database: %w", err)
	}
	return nil
}

// Deposit adds amount to the balance and returns the new balance.
func (s *Store) Deposit(ctx context.Context, id uuid.UUID, amount int64) (int64, error) {
	if amount < 0 {
		return 0, fmt.Errorf("deposit a negative amount %d", amount)
	}

	balance, err := s.write(ctx, id, operation{apply: wallet.Deposit, amount: amount})
	if err != nil {
		return 0, fmt.Errorf("deposit: %w", err)
	}
	return balance, nil
}

// Withdraw subtracts amount from the balance and returns the new balance.
func (s *Store) Withdraw(ctx context.Context, id uuid.UUID, amount int64) (int64, error) {
	if amount < 0 {
		return 0, fmt.Errorf("withdraw a negative amount %d", amount)
	}

	balance, err := s.write(ctx, id, operation{apply: wallet.Withdraw, amount: amount})
	if err != nil {
		return 0, fmt.Errorf("withdraw: %w", err)
	}
	return balance, nil
}

// operation is one DEPOSIT or WITHDRAW. apply is wallet.Deposit or wallet.Withdraw.
type operation struct {
	apply  func(balance, amount int64) (int64, error)
	amount int64
}

type result struct {
	balance int64
	err     error
}

func (s *Store) write(ctx context.Context, id uuid.UUID, op operation) (int64, error) {
	results, err := s.applyBatch(ctx, id, []operation{op})
	if err != nil {
		return 0, err
	}
	return results[0].balance, results[0].err
}

// applyBatch applies ops to the wallet in order, in one transaction.
// The error of one op goes into its result and does not change the balance for the next ops.
// A returned error applies to all the ops.
func (s *Store) applyBatch(ctx context.Context, id uuid.UUID, ops []operation) ([]result, error) {
	conn, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", classifyBeforeCommit(ctx, err))
	}
	// After a commit, the rollback does nothing.
	defer func() { _ = tx.Rollback(ctx) }()

	queries := sqlc.New(tx)

	balance, err := queries.LockBalance(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wallet.ErrNotFound
		}
		return nil, fmt.Errorf("lock the wallet: %w", classifyBeforeCommit(ctx, err))
	}

	results := make([]result, len(ops))
	for i, op := range ops {
		next, err := op.apply(balance, op.amount)
		if err == nil {
			balance = next
		}
		results[i] = result{balance: next, err: err}
	}

	if err := queries.SetBalance(ctx, sqlc.SetBalanceParams{ID: id, Balance: balance}); err != nil {
		return nil, fmt.Errorf("set the balance: %w", classifyBeforeCommit(ctx, err))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", classifyCommit(ctx, err))
	}

	return results, nil
}

// Balance returns the balance of the wallet.
func (s *Store) Balance(ctx context.Context, id uuid.UUID) (int64, error) {
	conn, err := s.acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()

	balance, err := sqlc.New(conn).GetBalance(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, wallet.ErrNotFound
		}
		return 0, fmt.Errorf("get the balance: %w", classify(ctx, err))
	}

	return balance, nil
}

// acquire waits for a free connection up to the acquire timeout
func (s *Store) acquire(ctx context.Context) (*pgxpool.Conn, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, s.acquireTimeout)
	defer cancel()

	conn, err := s.pool.Acquire(acquireCtx)
	if err == nil {
		return conn, nil
	}

	var connectErr *pgconn.ConnectError
	switch {
	case ctx.Err() != nil:
		// The caller went away, not a database error.
	case errors.As(err, &connectErr):
		err = fmt.Errorf("%w: %w", wallet.ErrUnavailable, err)
	case errors.Is(err, context.DeadlineExceeded):
		// A slow dial also ends here, because the pool then returns only the context error.
		err = fmt.Errorf("%w: %w", wallet.ErrOverloaded, err)
	}
	return nil, fmt.Errorf("acquire a connection: %w", err)
}

// classify maps the error of a statement to a domain error.
// A server error of another class stays as it is.
func classify(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// The server rejected the statement, so it did not apply. Only these classes mean that the database is not available:
		// 08 connection exception, 53 insufficient resources, 57 operator intervention.
		switch pgErr.Code[:2] {
		case "08", "53", "57":
			return fmt.Errorf("%w: %w", wallet.ErrUnavailable, err)
		}
		return err
	}

	if pgconn.SafeToRetry(err) {
		return fmt.Errorf("%w: %w", wallet.ErrUnavailable, err)
	}
	// pgx sent the statement, but the result did not come back. The server can still apply it.
	return fmt.Errorf("%w: %w", wallet.ErrOutcomeUnknown, err)
}

// classifyBeforeCommit classifies the error of a statement before the COMMIT.
// The server rolls back a transaction that does not commit, so a lost result also means that nothing applied.
func classifyBeforeCommit(ctx context.Context, err error) error {
	classified := classify(ctx, err)
	if errors.Is(classified, wallet.ErrOutcomeUnknown) {
		return fmt.Errorf("%w: %w", wallet.ErrUnavailable, err)
	}
	return classified
}

// classifyCommit classifies the error of the COMMIT.
// pgx sends the COMMIT with the simple protocol. That path reports a lost result as "conn closed",
// and pgconn.SafeToRetry calls this error safe. Thus only an error from the server proves that the COMMIT did not apply.
func classifyCommit(ctx context.Context, err error) error {
	var pgErr *pgconn.PgError
	if ctx.Err() != nil || errors.As(err, &pgErr) {
		return classify(ctx, err)
	}
	return fmt.Errorf("%w: %w", wallet.ErrOutcomeUnknown, err)
}
