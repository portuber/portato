package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/portuber/portato/internal/daemon"
)

// restoreStopSeams saves the package-level stop seams and restores them on test
// cleanup.
func restoreStopSeams(t *testing.T) {
	t.Helper()
	rs, rmp, rk, rp, rpa, rpp :=
		stopResolveSocket, stopMarkerPath, stopKill, stopProbe, stopPidAlive, stopProcessIsPortato
	t.Cleanup(func() {
		stopResolveSocket, stopMarkerPath, stopKill, stopProbe, stopPidAlive, stopProcessIsPortato =
			rs, rmp, rk, rp, rpa, rpp
	})
}

// writeStopMarker writes a discovery marker with the given PID at a temp path
// and wires stopMarkerPath to return it.
func writeStopMarker(t *testing.T, pid int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.socket")
	if err := daemon.WriteMarker(path, "/tmp/portato-test.sock", pid); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	stopMarkerPath = func() (string, error) { return path, nil }
	return path
}

func TestStop_NoDaemon(t *testing.T) {
	restoreStopSeams(t)
	mark := writeStopMarker(t, 99999) // a PID, but the probe will say "silent"
	stopResolveSocket = func() (string, error) { return "/tmp/portato-test.sock", nil }
	stopProbe = func(string) bool { return false }
	var killed int32
	stopKill = func(int) error { atomic.AddInt32(&killed, 1); return nil }

	if err := stopRunE(nil, nil); err != nil {
		t.Fatalf("stopRunE: %v", err)
	}
	if atomic.LoadInt32(&killed) != 0 {
		t.Errorf("no daemon: should not signal, killed %d", killed)
	}
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Errorf("stale marker should be removed; stat err=%v", err)
	}
}

func TestStop_StopsRunningDaemon(t *testing.T) {
	restoreStopSeams(t)
	writeStopMarker(t, 4242)
	stopResolveSocket = func() (string, error) { return "/tmp/portato-test.sock", nil }
	up := atomic.Bool{}
	up.Store(true)
	stopProbe = func(string) bool { return up.Load() }
	var signaled int32
	stopKill = func(int) error { atomic.AddInt32(&signaled, 1); up.Store(false); return nil }

	if err := stopRunE(nil, nil); err != nil {
		t.Fatalf("stopRunE: %v", err)
	}
	if atomic.LoadInt32(&signaled) != 1 {
		t.Errorf("want exactly one SIGTERM, got %d", signaled)
	}
}

func TestStop_Timeout(t *testing.T) {
	restoreStopSeams(t)
	writeStopMarker(t, 4243)
	stopResolveSocket = func() (string, error) { return "/tmp/portato-test.sock", nil }
	stopPollInterval = 5 * time.Millisecond
	stopTimeout = 20 * time.Millisecond
	stopProbe = func(string) bool { return true } // never goes silent
	stopKill = func(int) error { return nil }

	err := stopRunE(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "did not stop") {
		t.Errorf("expected timeout error, got %v", err)
	}
}

// TestStop_WedgedDaemonRecoversByPID covers the Phase 40 recovery path: no
// socket answers (ResolveSocket returns ""), but the marker records an alive
// portato PID — a daemon wedged by its reaped socket file. stop must SIGTERM
// that PID (not delete the marker and report idle) and poll process liveness.
func TestStop_WedgedDaemonRecoversByPID(t *testing.T) {
	restoreStopSeams(t)
	writeStopMarker(t, 7777)
	stopResolveSocket = func() (string, error) { return "", nil }
	stopProbe = func(string) bool { return false }
	alive := atomic.Bool{}
	alive.Store(true)
	stopPidAlive = func(int) bool { return alive.Load() }
	stopProcessIsPortato = func(int) bool { return true }
	var signaled int32
	stopKill = func(int) error { atomic.AddInt32(&signaled, 1); alive.Store(false); return nil }
	stopPollInterval = 5 * time.Millisecond
	stopTimeout = time.Second

	if err := stopRunE(nil, nil); err != nil {
		t.Fatalf("stopRunE: %v", err)
	}
	if atomic.LoadInt32(&signaled) != 1 {
		t.Errorf("want exactly one SIGTERM by PID for a wedged daemon, got %d", signaled)
	}
}

// TestStop_WedgedWrongProcessNoSignal guards against PID reuse: the marker's
// PID is alive but is no longer portato, so stop must not signal it and falls
// through to "no daemon running".
func TestStop_WedgedWrongProcessNoSignal(t *testing.T) {
	restoreStopSeams(t)
	writeStopMarker(t, 8888)
	stopResolveSocket = func() (string, error) { return "", nil }
	stopProbe = func(string) bool { return false }
	stopPidAlive = func(int) bool { return true }          // alive ...
	stopProcessIsPortato = func(int) bool { return false } // ... but not portato (reused PID)
	stopKill = func(int) error { t.Error("should not signal a non-portato PID"); return nil }

	if err := stopRunE(nil, nil); err != nil {
		t.Fatalf("stopRunE: %v", err)
	}
}

func TestStop_DaemonAliveButNoMarker(t *testing.T) {
	restoreStopSeams(t)
	stopResolveSocket = func() (string, error) { return "/tmp/portato-test.sock", nil }
	stopMarkerPath = func() (string, error) { return "", nil } // no marker path
	stopProbe = func(string) bool { return true }              // something answers
	stopKill = func(int) error { t.Error("should not signal without a PID"); return nil }

	err := stopRunE(nil, nil)
	if err == nil {
		t.Fatal("expected an error when a daemon answers but no marker is available")
	}
}
