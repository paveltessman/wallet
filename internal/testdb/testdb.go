// Package testdb gives each integration test an empty PostgreSQL database.
package testdb

import (
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// New makes an empty database on the server in TEST_DATABASE_URL and returns its URL.
// The cleanup of t drops the database.
// New fails the test if TEST_DATABASE_URL is not set.
func New(t *testing.T) string {
	t.Helper()

	serverURL := os.Getenv("TEST_DATABASE_URL")
	if serverURL == "" {
		t.Fatal("TEST_DATABASE_URL is not set. Run make up, then put the URL into .setenv.")
	}

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, serverURL)
	if err != nil {
		t.Fatalf("connect to TEST_DATABASE_URL: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	name := "wallet_test_" + strings.ToLower(rand.Text())
	ident := pgx.Identifier{name}.Sanitize()

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}

	// t.Cleanup runs the functions in reverse order, so the drop runs before conn.Close.
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	dbURL, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	dbURL.Path = "/" + name

	return dbURL.String()
}
