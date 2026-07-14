package forward

import (
	"context"
	"log/slog"
	"net"
	"testing"

	"github.com/portuber/portato/internal/config"
)

func mustTuber(t *testing.T, cfg config.Tuber) *Tuber {
	t.Helper()
	return NewTuber(context.Background(), cfg, config.Defaults{}, slog.Default(), nil, nil)
}

// TestStartBindFailureSetsErrorNotRunning reproduces the wedge root cause: a
// busy local port makes Start fail to bind, so the tuber lands in state=Error
// but running stays false (no listener, no run loop) — the half-state the old
// Stop/Reconfigure/toggle code did not handle.
func TestStartBindFailureSetsErrorNotRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy listen: %v", err)
	}
	defer ln.Close()
	busy := ln.Addr().String()

	tn := mustTuber(t, config.Tuber{Name: "mysql57", Type: "local", Local: busy, Remote: "127.0.0.1:33057"})
	if err := tn.Start(context.Background()); err == nil {
		t.Fatalf("Start on busy %s: expected error, got nil", busy)
	}
	st := tn.Status()
	if st.State != Error {
		t.Fatalf("state = %s, want Error", st.State)
	}
	if st.Error == "" {
		t.Fatalf("expected listen error message, got empty")
	}
	if tn.running {
		t.Fatalf("running = true after bind failure, want false")
	}
	if tn.listener != nil {
		t.Fatalf("listener should be nil after bind failure")
	}
}

// TestStopClearsErrorWhenNotRunning verifies the fix: a tuber wedged in Error
// from a failed bind is folded back to Off by Stop, so Disable (and the TUI
// toggle) clears the error and a later Enable rebinds. Previously Stop was a
// no-op when not running, leaving Error unrecoverable via the toggle.
func TestStopClearsErrorWhenNotRunning(t *testing.T) {
	notifies := 0
	tn := mustTuber(t, config.Tuber{Name: "mysql57", Type: "local", Local: "127.0.0.1:13306", Remote: "127.0.0.1:33057"})
	tn.onChange = func() { notifies++ }

	tn.mu.Lock()
	tn.state = Error
	tn.errMsg = "listen 127.0.0.1:13306: address already in use"
	tn.mu.Unlock()

	if err := tn.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st := tn.Status()
	if st.State != Off {
		t.Fatalf("state = %s, want Off", st.State)
	}
	if st.Error != "" {
		t.Fatalf("error = %q, want empty", st.Error)
	}
	if notifies != 1 {
		t.Fatalf("expected 1 notify (Error→Off), got %d", notifies)
	}
}

// TestStopNoNotifyWhenAlreadyOff: Stop on a clean Off tuber must not fire
// change notifications (avoids a storm during Reload/StopAll teardown).
func TestStopNoNotifyWhenAlreadyOff(t *testing.T) {
	notifies := 0
	tn := mustTuber(t, config.Tuber{Name: "x", Type: "local", Local: "127.0.0.1:1", Remote: "127.0.0.1:9"})
	tn.onChange = func() { notifies++ }

	if err := tn.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if tn.Status().State != Off {
		t.Fatalf("state = %s, want Off", tn.Status().State)
	}
	if notifies != 0 {
		t.Fatalf("expected 0 notifies for Off→Off, got %d", notifies)
	}
}

// TestReconfigureResetsErrorWhenNotRunning verifies the fix for the stale
// display: editing a tuber (e.g. a new Local port) while it sits in a failed-
// bind Error must clear the stale error and drop back to Off, instead of
// showing the new address with the old port's error string.
func TestReconfigureResetsErrorWhenNotRunning(t *testing.T) {
	notifies := 0
	tn := mustTuber(t, config.Tuber{Name: "mysql57", Type: "local", Local: "127.0.0.1:13306", Remote: "127.0.0.1:33057"})
	tn.onChange = func() { notifies++ }

	tn.mu.Lock()
	tn.state = Error
	tn.errMsg = "listen 127.0.0.1:13306: address already in use"
	tn.mu.Unlock()

	newCfg := config.Tuber{Name: "mysql57", Type: "local", Local: "127.0.0.1:33058", Remote: "127.0.0.1:33057"}
	if err := tn.Reconfigure(newCfg, config.Defaults{}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}

	st := tn.Status()
	if st.State != Off {
		t.Fatalf("state = %s, want Off", st.State)
	}
	if st.Error != "" {
		t.Fatalf("error = %q, want empty (stale error not cleared)", st.Error)
	}
	if st.Local != "127.0.0.1:33058" {
		t.Fatalf("Local = %q, want 127.0.0.1:33058", st.Local)
	}
	if notifies != 1 {
		t.Fatalf("expected 1 notify (Error→Off), got %d", notifies)
	}
}

// TestReconfigureNoNotifyWhenAlreadyOff: reconfiguring a non-running Off tuber
// (e.g. editing an off forward) must update fields without firing a change.
func TestReconfigureNoNotifyWhenAlreadyOff(t *testing.T) {
	notifies := 0
	tn := mustTuber(t, config.Tuber{Name: "mysql57", Type: "local", Local: "127.0.0.1:13306", Remote: "127.0.0.1:33057"})
	tn.onChange = func() { notifies++ }

	newCfg := config.Tuber{Name: "mysql57", Type: "local", Local: "127.0.0.1:33058", Remote: "127.0.0.1:33057"}
	if err := tn.Reconfigure(newCfg, config.Defaults{}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	if tn.Status().Local != "127.0.0.1:33058" {
		t.Fatalf("Local = %q, want 127.0.0.1:33058", tn.Status().Local)
	}
	if notifies != 0 {
		t.Fatalf("expected 0 notifies for Off reconfigure, got %d", notifies)
	}
}
