// Command wallet runs the wallet service.
package main

import (
	"context"
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wallet:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}

	ctx := context.Background()

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
