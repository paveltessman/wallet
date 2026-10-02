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
)

const healthcheckTimeout = 2 * time.Second

func runHealthcheck(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: wallet healthcheck")
	}
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	return healthcheck(ctx, readyURL(cfg.HTTPPort), healthcheckTimeout)
}

func readyURL(port int) string {
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/health/ready"
}

func healthcheck(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("make the health request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send the health request: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the service is not ready: status %d", resp.StatusCode)
	}
	return nil
}
