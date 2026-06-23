package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

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

// ResolveSocket returns the socket path a client should dial: the override if
// set, otherwise the path advertised in the discovery marker (when the owning
// PID is still alive). Returns "" when no live daemon is discoverable; a stale
// marker is cleaned up so the next caller is not misled.
func ResolveSocket() (string, error) {
	if ov := SocketOverride(); ov != "" {
		return ov, nil
	}
	markerPath, err := DiscoveryPath()
	if err != nil {
		return "", err
	}
	return resolveFromMarker(markerPath)
}

func resolveFromMarker(markerPath string) (string, error) {
	m, err := ReadMarker(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		// A corrupt marker is stale — clean it and behave as "not running".
		_ = RemoveMarker(markerPath)
		return "", nil
	}
	if !pidAlive(m.PID) {
		_ = RemoveMarker(markerPath)
		_ = os.Remove(m.Socket)
		return "", nil
	}
	return m.Socket, nil
}
