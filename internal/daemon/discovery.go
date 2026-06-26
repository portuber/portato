package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/adrg/xdg"
)

// Phase 12 — robust IPC socket discovery.
//
// The daemon's unix socket lives in a semantically correct runtime location
// that may differ across shells/sessions (macOS `$TMPDIR`, Linux
// `$XDG_RUNTIME_DIR`). Because that path is not stable, the daemon advertises
// it via a small, stable discovery marker (JSON) under the config home; every
// client reads the marker instead of guessing the path. See SPEC §6.

const (
	// markerFile is the stable pointer to the live socket. It sits next to the
	// config under the config home (NOT the socket itself, despite the name).
	markerFile = "daemon.socket"
)

// Marker is the discovery payload: where the daemon is listening and which PID
// owns it (so stale markers can be detected).
type Marker struct {
	Socket string `json:"socket"`
	PID    int    `json:"pid"`
}

// DiscoveryPath returns the path of the stable discovery marker under the
// config home. This is the pointer clients read, not the socket itself.
func DiscoveryPath() (string, error) {
	return filepath.Join(xdg.ConfigHome, "portato", markerFile), nil
}

// RuntimeSocketPath returns where the socket file itself should live: a
// runtime/temp dir. On Linux the per-user runtime dir (`/run/user/<uid>`,
// set by systemd/logind) is preferred; on macOS there is no such dir, so
// `$TMPDIR` (via os.TempDir) is used. The filename is uid-scoped to avoid
// collisions between users on shared hosts.
func RuntimeSocketPath() (string, error) {
	name := fmt.Sprintf("portato-%d.sock", os.Getuid())
	if runtime.GOOS != "darwin" {
		if dir := xdg.RuntimeDir; dir != "" {
			return filepath.Join(dir, name), nil
		}
	}
	return filepath.Join(os.TempDir(), name), nil
}

// WriteMarker atomically writes the discovery marker at markerPath: JSON
// {"socket","pid"}, mode 0600, via tmp-file + rename so a partial write never
// leaves a corrupt pointer.
func WriteMarker(markerPath, socket string, pid int) error {
	if dir := filepath.Dir(markerPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create discovery dir: %w", err)
		}
	}
	data, err := json.Marshal(Marker{Socket: socket, PID: pid})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(markerPath), ".daemon.*.tmp")
	if err != nil {
		return fmt.Errorf("create discovery tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after the rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write discovery tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, markerPath); err != nil {
		return fmt.Errorf("rename discovery: %w", err)
	}
	return nil
}

// ReadMarker reads and parses the discovery marker at markerPath. A missing
// marker is reported via an error that satisfies os.IsNotExist.
func ReadMarker(markerPath string) (Marker, error) {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return Marker{}, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return Marker{}, fmt.Errorf("parse discovery marker: %w", err)
	}
	if m.Socket == "" || m.PID <= 0 {
		return Marker{}, fmt.Errorf("invalid discovery marker: socket=%q pid=%d", m.Socket, m.PID)
	}
	return m, nil
}

// RemoveMarker removes the discovery marker (best-effort; missing is fine).
func RemoveMarker(markerPath string) error {
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// pidAlive reports whether the given PID is an existing process. EPERM (the
// process exists but is not ours) also counts as alive.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// --- socket override (--socket / PORTATO_SOCKET) ---------------------------

// socketOverride, when non-empty, bypasses discovery: the daemon binds it and
// clients dial it directly. Set by the root command from the --socket flag.
// The PORTATO_SOCKET env is read as a fallback (see SocketOverride).
var socketOverride string

// SetSocketOverride sets the process-wide socket override. Intended to be
// called once from the root command's PersistentPreRun when --socket is given.
func SetSocketOverride(p string) { socketOverride = p }

// SocketOverride returns the active override: the --socket flag value if set,
// otherwise the PORTATO_SOCKET env var, otherwise "" (use discovery).
func SocketOverride() string {
	if socketOverride != "" {
		return socketOverride
	}
	return os.Getenv("PORTATO_SOCKET")
}

// probeTimeout caps a discovery healthz probe: a live local daemon answers in
// well under this.
const probeTimeout = 300 * time.Millisecond

// ResolveSocket returns the socket path a client should dial. The override
// (--socket / PORTATO_SOCKET) wins outright. Otherwise discovery is, in order:
//
//  1. The discovery marker — if the socket it advertises answers /healthz, use
//     it. A marker whose socket is silent is stale; when its owning PID is also
//     gone, the marker and the leftover socket file are removed, while a
//     still-living PID (a wedged daemon) is left untouched.
//  2. A fallback probe of the canonical runtime socket path (RuntimeSocketPath)
//     — a daemon that lost its marker (a misled client deleted it, a schema
//     drift, a crash) is still listening there and must stay reachable.
//
// healthz is the source of truth for liveness throughout; pidAlive only
// informs cleanup, so a reused PID never causes a live daemon's socket to be
// deleted. Returns "" when no live daemon is discoverable.
func ResolveSocket() (string, error) {
	if ov := SocketOverride(); ov != "" {
		return ov, nil
	}
	markerPath, err := DiscoveryPath()
	if err != nil {
		return "", err
	}

	if m, rerr := ReadMarker(markerPath); rerr == nil {
		if probeSocket(m.Socket) {
			return m.Socket, nil
		}
		// The advertised socket is silent. If the owning PID is gone this is a
		// stale marker -> clean up marker + leftover socket and fall through to
		// the runtime-path fallback. A live PID (wedged daemon) is left as-is.
		if !pidAlive(m.PID) {
			_ = RemoveMarker(markerPath)
			_ = os.Remove(m.Socket)
		}
	} else if !os.IsNotExist(rerr) {
		// Corrupt/invalid marker — remove it so it stops misleading, then fall
		// through to the runtime-path fallback.
		_ = RemoveMarker(markerPath)
	}

	// Fallback: a live daemon may have lost its marker. Probe the canonical
	// runtime socket directly before declaring "not running".
	if rp, rerr := RuntimeSocketPath(); rerr == nil && probeSocket(rp) {
		return rp, nil
	}
	return "", nil
}

// probeSocket reports whether a portato daemon answers GET /healthz at the
// given unix socket path. It is the authoritative liveness check for discovery:
// a socket that answers is alive regardless of what any marker/PID claims, and
// a silent socket is never trusted to be alive just because a PID exists.
func probeSocket(path string) bool {
	client := &http.Client{
		Timeout: probeTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", path)
			},
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://portato/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}
