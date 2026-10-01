package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"wallet/db"
	"wallet/internal/testdb"
)

func TestMigrations(t *testing.T) {
	ctx := context.Background()

	sqlDB, err := sql.Open("pgx", testdb.New(t))
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}

	provider, err := db.NewProvider(sqlDB)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	assertWalletsTable(t, sqlDB, true)

	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("DownTo 0: %v", err)
	}
	assertWalletsTable(t, sqlDB, false)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up after DownTo 0: %v", err)
	}
	assertWalletsTable(t, sqlDB, true)

	t.Run("a new wallet has a zero balance", func(t *testing.T) {
		var balance int64
		err := sqlDB.QueryRowContext(ctx,
			"INSERT INTO wallets (id) VALUES ('5f1c8a2e-4b7d-4c1a-9f3e-2d6b8a0c7e41') RETURNING balance",
		).Scan(&balance)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		if balance != 0 {
			t.Errorf("balance = %d, want 0", balance)
		}
	})

	t.Run("the balance cannot go below zero", func(t *testing.T) {
		_, err := sqlDB.ExecContext(ctx,
			"INSERT INTO wallets (id, balance) VALUES ('0b6e3a52-9d1f-4e8a-b7c4-3f2a1d5e6c70', -1)",
		)

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("err = %v, want check_violation 23514", err)
		}
	})
}

func assertWalletsTable(t *testing.T, sqlDB *sql.DB, want bool) {
	t.Helper()

	var exists bool
	if err := sqlDB.QueryRow("SELECT to_regclass('wallets') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatalf("look up the wallets table: %v", err)
	}
	if exists != want {
		t.Fatalf("wallets table exists = %t, want %t", exists, want)
	}
}
