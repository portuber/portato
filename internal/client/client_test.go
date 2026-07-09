package client_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/ipctoken"
)

// startUnixServer serves handler on a unix socket at sock and returns a stop
// function. A small stand-in for the daemon so the client's token handling can
// be exercised in isolation.
func startUnixServer(t *testing.T, sock string, handler http.HandlerFunc) func() {
	t.Helper()
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen %s: %v", sock, err)
	}
	srv := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() { _ = srv.Serve(ln); close(done) }()
	// Wait until the socket accepts connections so the first request does not
	// race the accept loop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, derr := net.Dial("unix", sock); derr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return func() {
		_ = srv.Shutdown(context.Background())
		<-done
		_ = ln.Close()
		_ = os.Remove(sock)
	}
}

// shortSocketDir returns a temp dir under /tmp (kept short so the unix-socket
// path stays under macOS's SUN_LEN limit). t.TempDir() paths are too long.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pt-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestClient_AttachesBearerToken: when a token file is present next to the
// socket, client.New reads it and the RoundTripper adds "Authorization: Bearer
// <token>" to every request, so a server that requires the token answers 200.
func TestClient_AttachesBearerToken(t *testing.T) {
	sock := filepath.Join(shortSocketDir(t), "s.sock")
	tok := "client-test-token-123"
	if err := ipctoken.WriteToken(ipctoken.TokenPath(sock), tok); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	var got atomic.Value // string
	stop := startUnixServer(t, sock, func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer "+tok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer stop()

	c := client.New(sock)
	if err := c.Healthz(); err != nil {
		t.Fatalf("Healthz against a token-requiring server: %v", err)
	}
	if h, _ := got.Load().(string); h != "Bearer "+tok {
		t.Fatalf("server saw Authorization %q, want %q", h, "Bearer "+tok)
	}
}

// TestClient_NoTokenSendsNoHeader: without a token file (old daemon, or
// --ipc-token off) the client sends no Authorization header — backward compat.
func TestClient_NoTokenSendsNoHeader(t *testing.T) {
	sock := filepath.Join(shortSocketDir(t), "s.sock")
	// Deliberately no token file written.

	var got atomic.Value // string
	stop := startUnixServer(t, sock, func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer stop()

	c := client.New(sock)
	if err := c.Healthz(); err != nil {
		t.Fatalf("Healthz: %v", err)
	}
	if h, _ := got.Load().(string); h != "" {
		t.Fatalf("server saw Authorization %q, want empty (no token file)", h)
	}
}
