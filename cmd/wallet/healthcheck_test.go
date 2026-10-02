package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthcheck(t *testing.T) {
	t.Run("200 gives nil", func(t *testing.T) {
		var gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
		}))
		t.Cleanup(server.Close)

		if err := healthcheck(context.Background(), server.URL+"/health/ready", time.Second); err != nil {
			t.Errorf("healthcheck = %v, want nil", err)
		}
		if gotPath != "/health/ready" {
			t.Errorf("path = %q, want /health/ready", gotPath)
		}
	})

	t.Run("503 gives an error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(server.Close)

		if err := healthcheck(context.Background(), server.URL, time.Second); err == nil {
			t.Error("healthcheck = nil, want an error")
		}
	})

	t.Run("a closed port gives an error", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addr := listener.Addr().String()
		_ = listener.Close()

		if err := healthcheck(context.Background(), "http://"+addr, time.Second); err == nil {
			t.Error("healthcheck = nil, want an error")
		}
	})

	t.Run("a slow answer gives an error after the timeout", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-release
		}))
		// Close waits for the handler. t.Cleanup runs in reverse order, so the release comes first.
		t.Cleanup(server.Close)
		t.Cleanup(func() { close(release) })

		if err := healthcheck(context.Background(), server.URL, 50*time.Millisecond); err == nil {
			t.Error("healthcheck = nil, want an error")
		}
	})
}

func TestReadyURL(t *testing.T) {
	if got, want := readyURL(8080), "http://127.0.0.1:8080/health/ready"; got != want {
		t.Errorf("readyURL = %q, want %q", got, want)
	}
}
