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

	balance, err := sqlc.New(s.pool).Deposit(ctx, sqlc.DepositParams{ID: id, Amount: amount})
	if err != nil {
		// On a deposit, no row can only mean an unknown wallet.
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, wallet.ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == sqlstateNumericOutOfRange {
			return 0, wallet.ErrBalanceLimitExceeded
		}
		return 0, fmt.Errorf("deposit: %w", err)
	}

	return balance, nil
}

// Withdraw subtracts amount from the balance and returns the new balance.
func (s *Store) Withdraw(ctx context.Context, id uuid.UUID, amount int64) (int64, error) {
	if amount < 0 {
		return 0, fmt.Errorf("withdraw a negative amount %d", amount)
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire a connection: %w", err)
	}
	defer conn.Release()

	// The EXISTS read uses the same connection, so it does not wait in the pool after the UPDATE.
	queries := sqlc.New(conn)

	balance, err := queries.Withdraw(ctx, sqlc.WithdrawParams{ID: id, Amount: amount})
	if err == nil {
		return balance, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("withdraw: %w", err)
	}

	exists, err := queries.WalletExists(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("look up the wallet: %w", err)
	}
	if !exists {
		return 0, wallet.ErrNotFound
	}
	return 0, wallet.ErrInsufficientFunds
}

// Balance returns the balance of the wallet.
func (s *Store) Balance(ctx context.Context, id uuid.UUID) (int64, error) {
	balance, err := sqlc.New(s.pool).GetBalance(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, wallet.ErrNotFound
		}
		return 0, fmt.Errorf("get the balance: %w", err)
	}

	return balance, nil
}
