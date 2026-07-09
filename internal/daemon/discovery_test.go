package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/portuber/portato/internal/ipctoken"
)

func TestDiscoveryPathUnderConfigHome(t *testing.T) {
	p, err := DiscoveryPath()
	if err != nil {
		t.Fatalf("DiscoveryPath: %v", err)
	}
	if filepath.Base(p) != markerFile {
		t.Fatalf("DiscoveryPath base = %q, want %q", filepath.Base(p), markerFile)
	}
	if filepath.Base(filepath.Dir(p)) != "portato" {
		t.Fatalf("DiscoveryPath should sit under .../portato/, got %q", p)
	}
}

func TestRuntimeSocketPathUidScoped(t *testing.T) {
	p, err := RuntimeSocketPath()
	if err != nil {
		t.Fatalf("RuntimeSocketPath: %v", err)
	}
	if filepath.Base(p) == "portato.sock" {
		t.Fatalf("runtime socket name must be uid-scoped to avoid collisions, got %q", p)
	}
}

func TestWriteReadMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "nested", "daemon.socket") // dir does not exist yet
	socket := filepath.Join(dir, "portato.sock")
	if err := WriteMarker(mp, socket, 12345); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	// The parent dir is created by WriteMarker.
	if _, err := os.Stat(filepath.Dir(mp)); err != nil {
		t.Fatalf("marker dir not created: %v", err)
	}
	// Mode 0600.
	if info, err := os.Stat(mp); err != nil {
		t.Fatalf("stat marker: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("marker perm = %o, want 0600", info.Mode().Perm())
	}
	m, err := ReadMarker(mp)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if m.Socket != socket || m.PID != 12345 {
		t.Fatalf("marker = %+v, want socket=%q pid=12345", m, socket)
	}
}

func TestReadMarkerMissingIsNotExist(t *testing.T) {
	_, err := ReadMarker(filepath.Join(t.TempDir(), "nope"))
	if !os.IsNotExist(err) {
		t.Fatalf("want os.IsNotExist, got %v", err)
	}
}

func TestReadMarkerCorrupt(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := os.WriteFile(mp, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMarker(mp); err == nil {
		t.Fatal("expected error for corrupt marker")
	}
}

func TestReadMarkerInvalidFields(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := os.WriteFile(mp, []byte(`{"socket":"","pid":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMarker(mp); err == nil {
		t.Fatal("expected error for marker with empty socket / pid 0")
	}
}

func TestRemoveMarkerIdempotent(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := RemoveMarker(mp); err != nil {
		t.Fatalf("remove on missing marker: %v", err)
	}
	if err := WriteMarker(mp, "/x", 1); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMarker(mp); err != nil {
		t.Fatalf("remove existing marker: %v", err)
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Fatalf("marker still present after RemoveMarker")
	}
}

// withIsolatedDiscovery points discovery at fresh temp dirs so a test neither
// sees the user's real marker nor the host's real runtime socket (e.g. a daemon
// left running outside the test). Returns the marker path and the runtime
// socket path discovery will consult.
func withIsolatedDiscovery(t *testing.T) (markerPath, runtimePath string) {
	t.Helper()
	t.Setenv("PORTATO_SOCKET", "")
	socketOverride = ""
	// Isolate the marker path via the discoveryPathFn seam. xdg.ConfigHome is
	// cached at package init, so t.Setenv("XDG_CONFIG_HOME") would NOT redirect
	// DiscoveryPath, and the tests would read/clobber the host's real marker.
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	saved := discoveryPathFn
	discoveryPathFn = func() (string, error) { return mp, nil }
	t.Cleanup(func() { discoveryPathFn = saved })
	// Likewise isolate the single-instance lock (Phase 22): daemon.New acquires
	// a flock at lockPathFn(), so a test calling New must not touch the host's
	// real lock (and must not be blocked by a real daemon running outside it).
	lp := filepath.Join(filepath.Dir(mp), "daemon.lock")
	savedLock := lockPathFn
	lockPathFn = func() (string, error) { return lp, nil }
	t.Cleanup(func() { lockPathFn = savedLock })
	// RuntimeSocketPath uses os.TempDir() on darwin; redirect it to a short
	// dir under /tmp so (a) a host daemon's socket is not picked up by the
	// fallback probe, and (b) the runtime path stays under sockaddr_un's
	// sun_path limit (104 on macOS).
	shortTmp, err := os.MkdirTemp("/tmp", "pt-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(shortTmp) })
	t.Setenv("TMPDIR", shortTmp)
	rp, err := RuntimeSocketPath()
	if err != nil {
		t.Fatalf("RuntimeSocketPath: %v", err)
	}
	return mp, rp
}

// shortSocketPath returns a unique unix-socket path short enough to fit in a
// sockaddr_un (macOS sun_path limit is 104). t.TempDir() is too long once the
// test name is included, so the dir goes under /tmp.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pt-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// startHealthzServer serves 200 OK at GET /healthz on a unix socket at path and
// returns a stop function. A stand-in for a live daemon, for probeSocket /
// ResolveSocket / ensureNotRunning tests.
func startHealthzServer(t *testing.T, path string) func() {
	t.Helper()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen %s: %v", path, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(http.ResponseWriter, *http.Request) {})
	srv := &http.Server{Handler: mux}
	done := make(chan struct{})
	go func() { _ = srv.Serve(ln); close(done) }()
	// Wait until the server actually answers so tests don't race the accept
	// loop (a connection accepted into the kernel backlog but not yet Accept()ed
	// would time out the healthz probe).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if probeSocket(path) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return func() {
		_ = srv.Shutdown(context.Background())
		<-done
		_ = ln.Close()
		_ = os.Remove(path)
	}
}

func TestResolveSocketNoMarker(t *testing.T) {
	withIsolatedDiscovery(t)
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty when no marker and nothing listening, got %q", got)
	}
}

func TestResolveSocketLiveMarker(t *testing.T) {
	mp, _ := withIsolatedDiscovery(t)
	liveSocket := shortSocketPath(t)
	stop := startHealthzServer(t, liveSocket)
	defer stop()
	if err := WriteMarker(mp, liveSocket, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != liveSocket {
		t.Fatalf("ResolveSocket = %q, want live marker socket %q", got, liveSocket)
	}
}

func TestResolveSocketStaleMarkerCleaned(t *testing.T) {
	mp, _ := withIsolatedDiscovery(t)
	// A leftover socket file nothing is listening on, pointed at by a marker
	// whose owning PID is dead.
	deadSocket := filepath.Join(filepath.Dir(mp), "dead.sock")
	if err := os.WriteFile(deadSocket, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarker(mp, deadSocket, 999999); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty for a stale marker with nothing live, got %q", got)
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Fatalf("stale marker not removed by ResolveSocket")
	}
	if _, err := os.Stat(deadSocket); !os.IsNotExist(err) {
		t.Fatalf("stale socket pointed at by the marker not removed")
	}
}

// TestResolveSocketFallbackToRuntimePath covers the fix's target case: no
// marker at all (a misled client deleted it, or schema drift), yet a live
// daemon is still listening on the canonical runtime socket. Discovery must
// reach it via the healthz fallback instead of declaring "not running".
func TestResolveSocketFallbackToRuntimePath(t *testing.T) {
	_, rp := withIsolatedDiscovery(t)
	stop := startHealthzServer(t, rp)
	defer stop()
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != rp {
		t.Fatalf("ResolveSocket = %q, want runtime fallback %q", got, rp)
	}
}

// TestResolveSocketCorruptMarkerRemovedThenFallback: a corrupt marker is
// removed and discovery falls through to the runtime probe.
func TestResolveSocketCorruptMarkerRemovedThenFallback(t *testing.T) {
	mp, _ := withIsolatedDiscovery(t)
	if err := os.WriteFile(mp, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty (corrupt marker, nothing live), got %q", got)
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Fatalf("corrupt marker not removed")
	}
}

// TestResolveSocketWedgedDaemonMarkerKept: the marker points at a silent socket
// but its owning PID is alive (a wedged/hung daemon). Discovery must NOT delete
// the marker (the daemon may recover) and falls through to the runtime probe.
func TestResolveSocketWedgedDaemonMarkerKept(t *testing.T) {
	mp, _ := withIsolatedDiscovery(t)
	silentSocket := filepath.Join(filepath.Dir(mp), "wedged.sock")
	if err := os.WriteFile(silentSocket, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarker(mp, silentSocket, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty (no live socket), got %q", got)
	}
	if _, err := os.Stat(mp); os.IsNotExist(err) {
		t.Fatalf("marker must NOT be removed when the owning PID is still alive")
	}
}

func TestProbeSocketLiveAndSilent(t *testing.T) {
	path := shortSocketPath(t)
	if probeSocket(path) {
		t.Fatal("probeSocket on a missing path should be false")
	}
	stop := startHealthzServer(t, path)
	defer stop()
	if !probeSocket(path) {
		t.Fatal("probeSocket on a live /healthz server should be true")
	}
}

// startAuthHealthzServer serves /healthz at path but requires the bearer token,
// standing in for an authenticated daemon so probeSocket's token handling can
// be exercised without a full Server.
func startAuthHealthzServer(t *testing.T, path, token string) func() {
	t.Helper()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen %s: %v", path, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: mux}
	done := make(chan struct{})
	go func() { _ = srv.Serve(ln); close(done) }()
	// Wait until the server accepts connections (dial-only, no auth yet).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, derr := net.Dial("unix", path); derr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return func() {
		_ = srv.Shutdown(context.Background())
		<-done
		_ = ln.Close()
		_ = os.Remove(path)
	}
}

// TestProbeSocketAttachesToken: with a token file next to the socket, the probe
// reads it and sends the header, so a 200 reaches the caller; with the file
// gone, no header is sent and the authed server's 401 makes the probe false.
func TestProbeSocketAttachesToken(t *testing.T) {
	path := shortSocketPath(t)
	tok, err := ipctoken.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if err := ipctoken.WriteToken(ipctoken.TokenPath(path), tok); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	t.Cleanup(func() { _ = ipctoken.RemoveToken(ipctoken.TokenPath(path)) })

	stop := startAuthHealthzServer(t, path, tok)
	defer stop()

	if !probeSocket(path) {
		t.Fatal("probeSocket should succeed when the token file matches")
	}
	if err := ipctoken.RemoveToken(ipctoken.TokenPath(path)); err != nil {
		t.Fatalf("RemoveToken: %v", err)
	}
	if probeSocket(path) {
		t.Fatal("probeSocket should fail when the token file is missing and the server requires auth")
	}
}

// TestEnsureNotRunning_StaleMarkerButLiveSocketBlocksStart: the marker's PID is
// dead but the socket still answers (PID reused, or a kill -0 hiccup). A second
// daemon start must NOT clobber the live daemon's marker/socket.
func TestEnsureNotRunning_StaleMarkerButLiveSocketBlocksStart(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "daemon.socket")
	liveSocket := shortSocketPath(t)
	stop := startHealthzServer(t, liveSocket)
	defer stop()
	if err := WriteMarker(mp, liveSocket, 999999); err != nil {
		t.Fatal(err)
	}
	if err := ensureNotRunning(mp, liveSocket, false); err == nil {
		t.Fatal("ensureNotRunning must block when the socket answers despite a dead marker PID")
	}
	if _, err := os.Stat(mp); err != nil {
		t.Errorf("live daemon's marker must be left in place: %v", err)
	}
}

// TestEnsureNotRunning_DeadMarkerDeadSocketCleansUp: a genuinely stale marker
// (dead PID, silent socket) is removed so a fresh daemon can start.
func TestEnsureNotRunning_DeadMarkerDeadSocketCleansUp(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "daemon.socket")
	deadSocket := filepath.Join(dir, "dead.sock")
	if err := os.WriteFile(deadSocket, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarker(mp, deadSocket, 999999); err != nil {
		t.Fatal(err)
	}
	if err := ensureNotRunning(mp, filepath.Join(dir, "new.sock"), false); err != nil {
		t.Fatalf("ensureNotRunning should allow a fresh start, got %v", err)
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Errorf("stale marker not removed")
	}
	if _, err := os.Stat(deadSocket); !os.IsNotExist(err) {
		t.Errorf("stale socket not removed")
	}
}

// TestEnsureNotRunning_LivePIDBlocksStart: an alive marker PID blocks a second
// start outright (no probe needed).
func TestEnsureNotRunning_LivePIDBlocksStart(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "daemon.socket")
	sock := shortSocketPath(t)
	if err := WriteMarker(mp, sock, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := ensureNotRunning(mp, sock, false); err == nil {
		t.Fatal("ensureNotRunning must block when the marker PID is alive")
	}
}

func TestResolveSocketOverrideFlagWins(t *testing.T) {
	defer func() { socketOverride = "" }()
	SetSocketOverride("/tmp/portato-override-test.sock")
	if got := SocketOverride(); got != "/tmp/portato-override-test.sock" {
		t.Fatalf("SocketOverride flag = %q", got)
	}
	got, err := ResolveSocket()
	if err != nil {
		t.Fatalf("ResolveSocket: %v", err)
	}
	if got != "/tmp/portato-override-test.sock" {
		t.Fatalf("ResolveSocket override = %q", got)
	}
}

func TestSocketOverrideEnvFallback(t *testing.T) {
	socketOverride = ""
	t.Setenv("PORTATO_SOCKET", "/tmp/portato-env-test.sock")
	if got := SocketOverride(); got != "/tmp/portato-env-test.sock" {
		t.Fatalf("SocketOverride env = %q", got)
	}
}

func TestWriteMarkerIsJSON(t *testing.T) {
	mp := filepath.Join(t.TempDir(), "daemon.socket")
	if err := WriteMarker(mp, "/s", 42); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	var m Marker
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("marker is not valid JSON: %v", err)
	}
}
