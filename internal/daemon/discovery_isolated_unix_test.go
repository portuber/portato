//go:build unix

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

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
	// RuntimeSocketPath's location differs by OS (Phase 40): on non-darwin it
	// honours XDG_RUNTIME_DIR then os.TempDir(); on darwin it uses the
	// runtimeSocketDir seam (xdg.StateHome is cached at package init, so
	// t.Setenv cannot redirect it). Redirect BOTH to a short dir under /tmp so
	// (a) a host daemon's socket is not picked up by the fallback probe, and
	// (b) the runtime path stays under sockaddr_un's sun_path limit (104 on
	// macOS).
	shortTmp, err := os.MkdirTemp("/tmp", "pt-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(shortTmp) })
	t.Setenv("TMPDIR", shortTmp)
	savedDir := runtimeSocketDir
	runtimeSocketDir = func() string { return shortTmp }
	t.Cleanup(func() { runtimeSocketDir = savedDir })
	rp, err := RuntimeSocketPath()
	if err != nil {
		t.Fatalf("RuntimeSocketPath: %v", err)
	}
	return mp, rp
}
