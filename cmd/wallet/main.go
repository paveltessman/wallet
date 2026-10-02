// Command wallet runs the wallet service.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"wallet/internal/httpapi"
	"wallet/internal/postgres"
)

const (
	pingTimeout       = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	// Docker sends SIGKILL 10s after SIGTERM.
	shutdownTimeout = 8 * time.Second
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

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

	listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(cfg.HTTPPort)))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	server := &http.Server{
		Handler:           httpapi.NewHandler(store),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return serveHTTP(ctx, server, listener, shutdownTimeout)
}

// serveHTTP serves until ctx ends.
func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, timeout time.Duration) error {
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()

	select {
	case err := <-errs:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shut down HTTP: %w", err)
	}
	return nil
}
