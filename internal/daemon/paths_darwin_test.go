//go:build darwin

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSocketDirDarwinIgnoresRuntimeDir guards the regression that motivated
// the deterministic macOS path: adrg/xdg derives xdg.RuntimeDir from
// XDG_RUNTIME_DIR, which varies across terminal/tmux sessions, so the daemon
// and a client launched from different shells disagreed on the socket path.
// On macOS the socket dir must be a fixed Application Support subdirectory and
// must NOT move when XDG_RUNTIME_DIR changes.
func TestSocketDirDarwinIgnoresRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/var/folders/bogus/T/tmp.deadbeef")

	got, err := socketDir()
	if err != nil {
		t.Fatalf("socketDir: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "portato")
	if got != want {
		t.Fatalf("socketDir = %q, want %q (must not depend on XDG_RUNTIME_DIR)", got, want)
	}
}
