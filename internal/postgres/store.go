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

// sqlstateNumericOutOfRange is the SQLSTATE of a BIGINT overflow.
const sqlstateNumericOutOfRange = "22003"

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

// Deposit adds amount to the balance and returns the new balance.
func (s *Store) Deposit(ctx context.Context, id uuid.UUID, amount int64) (int64, error) {
	if amount < 0 {
		return 0, fmt.Errorf("deposit a negative amount %d", amount)
	}

	conn, err := s.acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()

	balance, err := sqlc.New(conn).Deposit(ctx, sqlc.DepositParams{ID: id, Amount: amount})
	if err != nil {
		// On a deposit, no row can only mean an unknown wallet.
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, wallet.ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == sqlstateNumericOutOfRange {
			return 0, wallet.ErrBalanceLimitExceeded
		}
		return 0, fmt.Errorf("deposit: %w", classify(ctx, err))
	}

	return balance, nil
}

// Withdraw subtracts amount from the balance and returns the new balance.
func (s *Store) Withdraw(ctx context.Context, id uuid.UUID, amount int64) (int64, error) {
	if amount < 0 {
		return 0, fmt.Errorf("withdraw a negative amount %d", amount)
	}

	conn, err := s.acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()

	// The EXISTS read uses the same connection, so it does not wait in the pool after the UPDATE.
	queries := sqlc.New(conn)

	balance, err := queries.Withdraw(ctx, sqlc.WithdrawParams{ID: id, Amount: amount})
	if err == nil {
		return balance, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("withdraw: %w", classify(ctx, err))
	}

	exists, err := queries.WalletExists(ctx, id)
	if err != nil {
		classified := classify(ctx, err)
		// The UPDATE changed no row, so the operation did not apply even if the result of this read is lost.
		if errors.Is(classified, wallet.ErrOutcomeUnknown) {
			classified = fmt.Errorf("%w: %w", wallet.ErrUnavailable, err)
		}
		return 0, fmt.Errorf("look up the wallet: %w", classified)
	}
	if !exists {
		return 0, wallet.ErrNotFound
	}
	return 0, wallet.ErrInsufficientFunds
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
