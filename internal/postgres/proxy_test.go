package postgres_test

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
)

// proxy forwards TCP connections to upstream. cut closes all of them, as a network fault does.
type proxy struct {
	listener net.Listener
	upstream string

	mu    sync.Mutex
	conns []net.Conn
}

func newProxy(t *testing.T, upstream string) *proxy {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := &proxy{listener: listener, upstream: upstream}
	t.Cleanup(func() {
		_ = listener.Close()
		p.cut()
	})

	go p.serve(t)
	return p
}

func (p *proxy) addr() string {
	return p.listener.Addr().String()
}

func (p *proxy) serve(t *testing.T) {
	for {
		client, err := p.listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}

		server, err := net.Dial("tcp", p.upstream)
		if err != nil {
			t.Errorf("dial the upstream: %v", err)
			_ = client.Close()
			continue
		}

		p.mu.Lock()
		p.conns = append(p.conns, client, server)
		p.mu.Unlock()

		go pipe(server, client)
		go pipe(client, server)
	}
}

// pipe copies src to dst. When src ends, it closes both, so the other side sees the close.
func pipe(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

func (p *proxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = nil
}
