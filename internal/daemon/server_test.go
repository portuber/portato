package daemon

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	"github.com/kipkaev55/portato/internal/ipctoken"
	routelog "github.com/kipkaev55/portato/internal/log"
)

// fakeEngine is a tunneler stand-in: it flips in-memory states instead of
// opening SSH connections, so daemon HTTP/persistence logic is deterministic.
type fakeEngine struct {
	mu     sync.Mutex
	states map[string]forward.State
	cfg    *config.Config
	subs   map[chan struct{}]struct{}
}

func newFakeEngine(cfg *config.Config) *fakeEngine {
	states := make(map[string]forward.State, len(cfg.Tunnels))
	for _, t := range cfg.Tunnels {
		states[t.Name] = forward.Off
	}
	return &fakeEngine{states: states, cfg: cfg}
}

func (f *fakeEngine) List() []forward.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]forward.Status, 0, len(f.cfg.Tunnels))
	for _, t := range f.cfg.Tunnels {
		out = append(out, forward.Status{
			Name:   t.Name,
			Type:   t.Type,
			Local:  t.ListenAddr(),
			Remote: t.Remote,
			State:  f.states[t.Name],
		})
	}
	return out
}

func (f *fakeEngine) Enable(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[name] = forward.Connecting
	return nil
}

func (f *fakeEngine) Disable(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[name] = forward.Off
	return nil
}

func (f *fakeEngine) Restart(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[name] = forward.Connecting
	return nil
}

func (f *fakeEngine) Reload(cfg *config.Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = cfg
}

func (f *fakeEngine) StartEnabled() {}
func (f *fakeEngine) StopAll()      {}

// Subscribe mimics the real Engine broker for SSE tests. broadcast() fans a
// signal out to every active subscriber (drop-old).
func (f *fakeEngine) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 16)
	f.mu.Lock()
	if f.subs == nil {
		f.subs = make(map[chan struct{}]struct{})
	}
	f.subs[ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
	}
}

func (f *fakeEngine) broadcast() {
	f.mu.Lock()
	for ch := range f.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	f.mu.Unlock()
}

func testConfig() *config.Config {
	return &config.Config{
		Tunnels: []config.Tunnel{{
			Name:   "db",
			Type:   "local",
			Local:  "5432",
			Remote: "db:5432",
			SSH:    "u@h:22",
		}},
	}
}

func tunnelEnabled(cfg *config.Config, name string) bool {
	for _, t := range cfg.Tunnels {
		if t.Name == name {
			return t.Enabled
		}
	}
	return false
}

func waitForFile(p string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(p); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %s", p)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForGone(p string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %s to disappear", p)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// shortDir returns a short temp directory for the unix socket, avoiding the
// macOS SUN_LEN (103 byte) limit that long t.TempDir() paths can exceed.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestServer_RoundTrip(t *testing.T) {
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	sock := filepath.Join(dir, "portato.sock")
	marker := filepath.Join(dir, "daemon.socket")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, marker, slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startErr := make(chan error, 1)
	go func() { startErr <- s.Start(ctx) }()
	if err := waitForFile(sock, 2*time.Second); err != nil {
		t.Fatalf("socket not created: %v", err)
	}

	// Socket must be owner-only (SPEC §6, DoD: 0600).
	if info, err := os.Stat(sock); err != nil {
		t.Fatalf("stat socket: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket perm = %o, want 0600", info.Mode().Perm())
	}

	c := client.New(sock)

	if err := c.Healthz(); err != nil {
		t.Fatalf("healthz: %v", err)
	}

	list, err := c.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "db" || list[0].State != forward.Off {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := c.Enable("db"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload cfg: %v", err)
	}
	if !tunnelEnabled(persisted, "db") {
		t.Fatalf("enable not persisted to YAML")
	}
	if list, _ := c.List(); list[0].State == forward.Off {
		t.Fatalf("tunnel not started after enable")
	}

	if err := c.Restart("db"); err != nil {
		t.Fatalf("restart: %v", err)
	}

	if err := c.Disable("db"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	persisted2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload cfg: %v", err)
	}
	if tunnelEnabled(persisted2, "db") {
		t.Fatalf("disable not persisted to YAML")
	}
	if list, _ := c.List(); list[0].State != forward.Off {
		t.Fatalf("tunnel not stopped after disable")
	}

	if err := c.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if err := c.Enable("nope"); err == nil {
		t.Fatalf("expected error for unknown tunnel")
	}

	cancel()
	if err := <-startErr; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
	if err := waitForGone(sock, 2*time.Second); err != nil {
		t.Fatalf("socket not removed on shutdown: %v", err)
	}
	if err := waitForGone(marker, 2*time.Second); err != nil {
		t.Fatalf("discovery marker not removed on shutdown: %v", err)
	}
}

func TestServer_EnableIdempotent(t *testing.T) {
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	cfg.Save(cfgPath)
	sock := filepath.Join(dir, "portato.sock")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, filepath.Join(dir, "daemon.socket"), slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)
	waitForFile(sock, 2*time.Second)

	c := client.New(sock)
	if err := c.Enable("db"); err != nil {
		t.Fatalf("first enable: %v", err)
	}
	if err := c.Enable("db"); err != nil {
		t.Fatalf("second enable: %v", err)
	}
}

// TestServer_Logs verifies the daemon serves its ring buffer over GET /logs,
// filtered by the ?name= query (Phase 11 TUI logs screen).
func TestServer_Logs(t *testing.T) {
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	ring := routelog.NewRing()
	ring.Append(routelog.Entry{Tunnel: "db", Msg: "connected", Level: slog.LevelInfo})
	ring.Append(routelog.Entry{Tunnel: "other", Msg: "noise", Level: slog.LevelDebug})

	sock := filepath.Join(dir, "portato.sock")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, filepath.Join(dir, "daemon.socket"), slog.Default(), ring)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)
	if err := waitForFile(sock, 2*time.Second); err != nil {
		t.Fatalf("socket not created: %v", err)
	}

	c := client.New(sock)

	// Filtered by tunnel.
	db, err := c.Logs("db")
	if err != nil {
		t.Fatalf("Logs(db): %v", err)
	}
	if len(db) != 1 || db[0].Msg != "connected" {
		t.Fatalf("Logs(db) = %+v, want one connected entry", db)
	}

	// Unfiltered returns everything.
	all, err := c.Logs("")
	if err != nil {
		t.Fatalf("Logs(\"\"): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("Logs(\"\") = %d entries, want 2", len(all))
	}
}

func TestEnsureNotRunning(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "daemon.socket")
	sock := filepath.Join(dir, "portato.sock")

	// No marker + stale socket → ok, stale socket removed.
	if err := os.WriteFile(sock, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureNotRunning(marker, sock, false); err != nil {
		t.Fatalf("no marker: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("stale socket not removed")
	}

	// Corrupt marker → ok, cleaned.
	if err := os.WriteFile(marker, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureNotRunning(marker, sock, false); err != nil {
		t.Fatalf("corrupt marker: %v", err)
	}

	// Dead PID marker → ok, marker + its socket removed.
	deadSock := filepath.Join(dir, "dead.sock")
	if err := WriteMarker(marker, deadSock, 999999); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deadSock, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureNotRunning(marker, sock, false); err != nil {
		t.Fatalf("dead pid: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("dead-pid marker not removed")
	}

	// Live PID (the test process itself) → already-running error.
	if err := WriteMarker(marker, sock, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := ensureNotRunning(marker, sock, false); err == nil {
		t.Fatalf("expected already-running error for live pid")
	}
}

// readSSEFrame reads one SSE frame from br: a sequence of lines terminated by
// a blank line. Returns the concatenated lines (without the terminator).
func readSSEFrame(t *testing.T, br *bufio.Reader) string {
	t.Helper()
	var sb strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		if line == "\n" {
			return strings.TrimRight(sb.String(), "\n")
		}
		sb.WriteString(line)
	}
}

func newEventServer(t *testing.T) (*Server, *fakeEngine, *client.Client, context.CancelFunc) {
	t.Helper()
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	sock := filepath.Join(dir, "portato.sock")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, filepath.Join(dir, "daemon.socket"), slog.Default(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	go s.Start(ctx)
	if err := waitForFile(sock, 2*time.Second); err != nil {
		cancel()
		t.Fatalf("socket not created: %v", err)
	}
	t.Cleanup(func() {
		cancel()
	})
	return s, fe, client.New(sock), cancel
}

func TestServer_EventsStreamMultipleClients(t *testing.T) {
	_, fe, c, _ := newEventServer(t)

	rc1, err := c.Events(context.Background())
	if err != nil {
		t.Fatalf("events1: %v", err)
	}
	defer rc1.Close()
	rc2, err := c.Events(context.Background())
	if err != nil {
		t.Fatalf("events2: %v", err)
	}
	defer rc2.Close()

	br1 := bufio.NewReader(rc1)
	br2 := bufio.NewReader(rc2)

	// Both clients must receive the initial frame immediately on connect.
	for i, br := range []*bufio.Reader{br1, br2} {
		if frame := readSSEFrame(t, br); !strings.HasPrefix(frame, "data:") {
			t.Fatalf("client %d initial frame = %q, want a data frame", i, frame)
		}
	}

	// A daemon-side state change fans out to both clients simultaneously.
	fe.broadcast()
	for i, br := range []*bufio.Reader{br1, br2} {
		if frame := readSSEFrame(t, br); !strings.HasPrefix(frame, "data:") {
			t.Fatalf("client %d post-broadcast frame = %q, want data", i, frame)
		}
	}
}

func TestServer_EventsClientDisconnectUnsubscribes(t *testing.T) {
	_, fe, c, _ := newEventServer(t)

	rc, err := c.Events(context.Background())
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	br := bufio.NewReader(rc)
	readSSEFrame(t, br) // consume the initial frame

	// There must be exactly one subscriber while connected.
	fe.mu.Lock()
	if n := len(fe.subs); n != 1 {
		t.Fatalf("subs while connected = %d, want 1", n)
	}
	fe.mu.Unlock()

	// Closing the client stream must drop the subscriber on the server side.
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
	if !waitFor(func() bool {
		fe.mu.Lock()
		defer fe.mu.Unlock()
		return len(fe.subs) == 0
	}, time.Second) {
		t.Fatalf("subscriber not removed after client disconnect")
	}
}

// waitFor polls cond until it returns true or the timeout elapses.
func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// unixHTTPDo sends a request over the unix socket at sock with an optional
// Authorization header and returns the response. Used to exercise the auth
// middleware without going through the client package (which attaches its own
// header in a later commit).
func unixHTTPDo(t *testing.T, sock, method, path, authHeader string) *http.Response {
	t.Helper()
	hc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), method, "http://portato"+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// startAuthServer boots a server with ipcToken enabled (so Start generates the
// token and installs the middleware) and returns it plus the generated token.
func startAuthServer(t *testing.T) (s *Server, sock, tok string) {
	t.Helper()
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	cfg.Save(cfgPath)
	sock = filepath.Join(dir, "portato.sock")
	fe := newFakeEngine(cfg)
	s = newServer(fe, cfg, cfgPath, sock, filepath.Join(dir, "daemon.socket"), slog.Default(), nil)
	s.ipcToken = true

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		<-ctx.Done()
	})
	go s.Start(ctx)
	if err := waitForFile(sock, 2*time.Second); err != nil {
		t.Fatalf("socket not created: %v", err)
	}
	tok, err := ipctoken.ReadToken(ipctoken.TokenPath(sock))
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	return s, sock, tok
}

func TestServer_AuthMiddleware(t *testing.T) {
	_, sock, tok := startAuthServer(t)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer deadbeef", http.StatusUnauthorized},
		{"wrong scheme", tok, http.StatusUnauthorized},
		{"correct", "Bearer " + tok, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := unixHTTPDo(t, sock, http.MethodGet, "/healthz", c.header)
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

func TestServer_AuthProtectsEveryRoute(t *testing.T) {
	_, sock, _ := startAuthServer(t)
	// A few representative routes across methods — all must 401 without a token.
	for _, p := range []string{"/tunnels", "/config", "/logs", "/reload"} {
		method := http.MethodGet
		if p == "/reload" {
			method = http.MethodPost
		}
		resp := unixHTTPDo(t, sock, method, p, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s without token = %d, want 401", method, p, resp.StatusCode)
		}
	}
}

func TestServer_AuthTokenFileRemovedOnShutdown(t *testing.T) {
	s, sock, _ := startAuthServer(t)
	tokenPath := ipctoken.TokenPath(sock)
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("token file not present while running: %v", err)
	}
	s.Shutdown()
	if err := waitForGone(tokenPath, 2*time.Second); err != nil {
		t.Fatalf("token file not removed on shutdown: %v", err)
	}
	if err := waitForGone(sock, 2*time.Second); err != nil {
		t.Fatalf("socket not removed on shutdown: %v", err)
	}
}

// TestServer_AuthDisabledByDefault ensures the test helper path (ipcToken false)
// serves unauthenticated, so existing handler tests are unaffected by the
// middleware and a daemon built with --ipc-token off behaves openly.
func TestServer_AuthDisabledByDefault(t *testing.T) {
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	cfg.Save(cfgPath)
	sock := filepath.Join(dir, "portato.sock")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, filepath.Join(dir, "daemon.socket"), slog.Default(), nil)
	// ipcToken stays false (newServer default) -> no token, no middleware.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)
	if err := waitForFile(sock, 2*time.Second); err != nil {
		t.Fatalf("socket not created: %v", err)
	}
	// No token file must exist.
	if _, err := os.Stat(ipctoken.TokenPath(sock)); !os.IsNotExist(err) {
		t.Fatalf("token file should not exist when ipcToken is disabled")
	}
	resp := unixHTTPDo(t, sock, http.MethodGet, "/healthz", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (auth disabled)", resp.StatusCode)
	}
}

// TestNew_HonorsEscapeHatch: production New enables the IPC token by default,
// and SetIpcTokenDisabled(true) (the --ipc-token off / PORTATO_NO_IPC_TOKEN=1
// escape hatch) flips it off so no token is generated or enforced.
func TestNew_HonorsEscapeHatch(t *testing.T) {
	// Isolate discovery + socket so New does not touch the host's real marker.
	withIsolatedDiscovery(t)
	dir := shortDir(t)
	sock := filepath.Join(dir, "portato.sock")
	SetSocketOverride(sock)
	t.Cleanup(func() { SetSocketOverride("") })

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	SetIpcTokenDisabled(false)
	s, err := New(cfg, cfgPath, slog.Default(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !s.ipcToken {
		t.Fatal("s.ipcToken = false, want true by default")
	}
	_ = s.Shutdown()

	SetIpcTokenDisabled(true)
	t.Cleanup(func() { SetIpcTokenDisabled(false) })
	s2, err := New(cfg, cfgPath, slog.Default(), nil)
	if err != nil {
		t.Fatalf("New under escape hatch: %v", err)
	}
	if s2.ipcToken {
		t.Fatal("s.ipcToken = true under escape hatch, want false")
	}
	_ = s2.Shutdown()
}
