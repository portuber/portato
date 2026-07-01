package forward

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/kipkaev55/portato/internal/config"
	routelog "github.com/kipkaev55/portato/internal/log"
)

// TestSetStateErrLogsToRing guards the visibility fix: a tunnel failure set via
// setStateErr must reach the log ring (and thus the TUI `l` screen and the
// rotated file) — not just the truncated Status.Error field in the list row.
// The motivating case was a remote-listen error ("listen 0.0.0.0:9090 on
// server: …") that was unreachable from the TUI.
func TestSetStateErrLogsToRing(t *testing.T) {
	logger, ring, closer, err := routelog.Setup(filepath.Join(t.TempDir(), "t.log"), slog.LevelInfo, routelog.LogOptions{})
	if err != nil {
		t.Fatalf("setup logger: %v", err)
	}
	defer closer.Close()

	tn := NewTunnel(context.Background(), tunnelCfg("db"), config.Defaults{}, logger, nil)
	const msg = "listen 0.0.0.0:9090 on server: boom"
	tn.setStateErr(Error, msg)

	entries := ring.Lines("db")
	if len(entries) != 1 {
		t.Fatalf("ring captured %d entries for db, want 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Tunnel != "db" {
		t.Errorf("entry tunnel = %q, want db", e.Tunnel)
	}
	if e.Msg != msg {
		t.Errorf("entry msg = %q, want %q", e.Msg, msg)
	}
	if e.Level != slog.LevelError {
		t.Errorf("entry level = %v, want Error", e.Level)
	}

	// Logging must not change the state-machine semantics.
	st := tn.Status()
	if st.State != Error {
		t.Errorf("state = %v, want Error", st.State)
	}
	if st.Error != msg {
		t.Errorf("status error = %q, want %q", st.Error, msg)
	}
}

// TestSetStateErrNilLogSafe ensures a Tunnel built without a logger (as some
// unit tests do) does not panic when setStateErr logs.
func TestSetStateErrNilLogSafe(t *testing.T) {
	tn := &Tunnel{baseCtx: context.Background(), cfg: tunnelCfg("x")}
	tn.setStateErr(Error, "boom") // must not panic
	if got := tn.Status().State; got != Error {
		t.Errorf("state = %v, want Error", got)
	}
}
