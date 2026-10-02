package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeHTTP(t *testing.T) {
	t.Run("finishes the request in progress and refuses new connections", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			_, _ = io.WriteString(w, "done")
		})
		addr, cancel, done := startServer(t, handler, 5*time.Second)

		resp := goGet(addr)
		<-started
		cancel()
		waitForRefusal(t, addr)
		close(release)

		r := <-resp
		if r.err != nil || r.status != http.StatusOK || r.body != "done" {
			t.Errorf("response = %d %q, %v, want 200 \"done\", nil", r.status, r.body, r.err)
		}
		if err := <-done; err != nil {
			t.Errorf("serveHTTP = %v, want nil", err)
		}
	})

	t.Run("ends the requests after the limit", func(t *testing.T) {
		started := make(chan struct{})
		requestEnded := make(chan struct{})
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(requestEnded)
		})
		addr, cancel, done := startServer(t, handler, 50*time.Millisecond)

		resp := goGet(addr)
		<-started
		cancel()

		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("serveHTTP = %v, want context.DeadlineExceeded", err)
		}
		select {
		case <-requestEnded:
		case <-time.After(5 * time.Second):
			t.Error("the request context did not end after the limit")
		}
		if r := <-resp; r.err == nil {
			t.Errorf("response = %d, want an error", r.status)
		}
	})

	t.Run("returns the error of Serve", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		_ = listener.Close()

		err = serveHTTP(context.Background(), &http.Server{ReadHeaderTimeout: time.Second}, listener, time.Second)
		if err == nil {
			t.Error("serveHTTP = nil, want an error")
		}
	})
}

// startServer runs serveHTTP on a free port. The returned cancel starts the shutdown.
func startServer(t *testing.T, handler http.Handler, timeout time.Duration) (addr string, cancel context.CancelFunc, done <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errs := make(chan error, 1)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { errs <- serveHTTP(ctx, server, listener, timeout) }()

	return listener.Addr().String(), cancel, errs
}

type getResult struct {
	status int
	body   string
	err    error
}

// goGet sends a GET in a goroutine.
func goGet(addr string) <-chan getResult {
	done := make(chan getResult, 1)
	go func() {
		client := &http.Client{Transport: &http.Transport{}}
		resp, err := client.Get("http://" + addr + "/")
		if err != nil {
			done <- getResult{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		done <- getResult{status: resp.StatusCode, body: string(body), err: err}
	}()
	return done
}

// waitForRefusal waits until the server closes its listener, which is the first step of the shutdown.
func waitForRefusal(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s still accepts connections after 5s", addr)
}
