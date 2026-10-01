package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"

	"wallet/db"
)

var errMigrateUsage = errors.New("usage: wallet migrate up|down|status")

// migrate runs one goose action on the database. down rolls back one version.
func migrate(ctx context.Context, dbURL string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errMigrateUsage
	}
	action := args[0]
	if action != "up" && action != "down" && action != "status" {
		return errMigrateUsage
	}

	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("open the database: %w", err)
	}
	provider, err := db.NewProvider(sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		return err
	}
	// Provider.Close closes sqlDB.
	defer func() { _ = provider.Close() }()

	switch action {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("apply the migrations: %w", err)
		}
		if len(results) == 0 {
			_, _ = fmt.Fprintln(out, "no pending migrations")
		}
		for _, r := range results {
			_, _ = fmt.Fprintln(out, r)
		}

	case "down":
		result, err := provider.Down(ctx)
		if errors.Is(err, goose.ErrNoNextVersion) {
			_, _ = fmt.Fprintln(out, "no applied migrations")
			return nil
		}
		if err != nil {
			return fmt.Errorf("roll back the last migration: %w", err)
		}
		_, _ = fmt.Fprintln(out, result)

	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read the migration status: %w", err)
		}
		for _, s := range statuses {
			line := fmt.Sprintf("%-8s %s", s.State, filepath.Base(s.Source.Path))
			if s.State == goose.StateApplied {
				line += " " + s.AppliedAt.UTC().Format(time.RFC3339)
			}
			_, _ = fmt.Fprintln(out, line)
		}
	}

	return nil
}
