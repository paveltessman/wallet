// Command wallet runs the wallet service.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"wallet/internal/httpapi"
	"wallet/internal/postgres"
)

const (
	pingTimeout       = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
)

var errUsage = errors.New("usage: wallet [migrate up|down|status]")

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wallet:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] != "migrate" {
		return errUsage
	}

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}

	ctx := context.Background()

	if len(args) > 0 {
		return migrate(ctx, cfg.databaseURL(), args[1:], os.Stdout)
	}
	return serve(ctx, cfg)
}

func serve(ctx context.Context, cfg config) error {
	store, err := postgres.New(ctx, postgres.Config{
		URL:            cfg.databaseURL(),
		MaxConns:       cfg.DBMaxConns,
		AcquireTimeout: cfg.DBAcquireTimeout,
	})
	if err != nil {
		return err
	}
	defer store.Close()

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := store.Ping(pingCtx); err != nil {
		return err
	}

	server := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.HTTPPort)),
		Handler:           httpapi.NewHandler(store),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return fmt.Errorf("serve HTTP: %w", server.ListenAndServe())
}
