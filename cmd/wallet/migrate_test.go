package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"wallet/internal/testdb"
)

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	dbURL := testdb.New(t)

	steps := []struct {
		action string
		want   string
	}{
		{"status", "pending  00001_wallets.sql\n"},
		{"down", "no applied migrations\n"},
		{"up", "OK    up 00001_wallets.sql"},
		{"status", "applied  00001_wallets.sql "},
		{"up", "no pending migrations\n"},
		{"down", "OK    down 00001_wallets.sql"},
		{"status", "pending  00001_wallets.sql\n"},
	}
	for _, step := range steps {
		var out bytes.Buffer
		if err := migrate(ctx, dbURL, []string{step.action}, &out); err != nil {
			t.Fatalf("migrate %s: %v", step.action, err)
		}
		if !strings.HasPrefix(out.String(), step.want) {
			t.Fatalf("migrate %s output = %q, want the prefix %q", step.action, out.String(), step.want)
		}
	}
}

func TestMigrateUsage(t *testing.T) {
	// The usage check comes before the connection, so the URL is never used.
	for _, args := range [][]string{nil, {"sideways"}, {"up", "down"}} {
		err := migrate(context.Background(), "postgres://unused", args, &bytes.Buffer{})
		if !errors.Is(err, errMigrateUsage) {
			t.Errorf("migrate %q err = %v, want errMigrateUsage", args, err)
		}
	}
}
