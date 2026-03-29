package testutil

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

var (
	loopbackCheckOnce sync.Once
	loopbackCheckErr  error
)

func loopbackListenerError() error {
	loopbackCheckOnce.Do(func() {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			loopbackCheckErr = err
			return
		}
		_ = listener.Close()
	})
	return loopbackCheckErr
}

// SkipIfLoopbackUnavailable skips tests that require opening a local listener
// when the current environment forbids bind/listen operations.
func SkipIfLoopbackUnavailable(t testing.TB) {
	t.Helper()
	if err := loopbackListenerError(); err != nil {
		t.Skipf("skipping test: loopback listener unavailable in current environment: %v", err)
	}
}

// NewLoopbackServer starts an httptest server on 127.0.0.1 using a tcp4 listener.
func NewLoopbackServer(t testing.TB, handler http.Handler) *httptest.Server {
	t.Helper()
	SkipIfLoopbackUnavailable(t)

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open loopback listener: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}
