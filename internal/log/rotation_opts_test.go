package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSetup_HonorsLogOptions proves the LogOptions passed to Setup reach the
// rotating writer: with MaxSize=16 and Retain=2, writing enough records forces
// several rotations and the archive count never exceeds Retain.
func TestSetup_HonorsLogOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.log")
	logger, _, closer, err := Setup(path, slog.LevelInfo, LogOptions{MaxSize: 16, Retain: 2})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	// Each slog text record is well over the 16-byte cap, so every record
	// rotates; 12 records overflow Retain=2 many times over.
	for i := 0; i < 12; i++ {
		logger.Info("rotation-test-line", "n", i)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !exists(archivePath(path, 1)) {
		t.Errorf("expected at least one rotated archive .1 (MaxSize=16 should force rotation)")
	}
	if exists(archivePath(path, 3)) {
		t.Errorf("archive .3 must not exist (Retain=2 should cap the chain at .2)")
	}
}

// TestRotatingWriter_AgePurgesOldArchives: with maxAgeDays set, archives older
// than the cutoff are dropped at the next rotation (retention bounded by age as
// well as count). The just-rotated current archive is always fresh, so it
// survives; only the backdated ones are purged.
func TestRotatingWriter_AgePurgesOldArchives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.log")
	w, err := NewRotatingWriter(path, 8, 3, 10) // keep 3, purge older than 10 days
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	// Build a full chain of 3 archives (.1 newest .. .3 oldest).
	for i := 1; i <= 3; i++ {
		if _, err := w.Write([]byte(rec(i))); err != nil { // 9 bytes > 8-byte cap -> rotates
			t.Fatalf("write %d: %v", i, err)
		}
	}
	for n := 1; n <= 3; n++ {
		if !exists(archivePath(path, n)) {
			t.Fatalf("setup: archive .%d should exist", n)
		}
		backDate(t, archivePath(path, n), 20*24*time.Hour) // 20 days old -> past the 10-day cutoff
	}

	// One more write triggers a rotation; purgeAgedLocked runs and drops the
	// backdated archives. The fresh .1 (the just-rotated current file) survives.
	if _, err := w.Write([]byte(rec(4))); err != nil {
		t.Fatalf("write 4: %v", err)
	}
	if !exists(archivePath(path, 1)) {
		t.Errorf("fresh archive .1 should survive (it is not past the age cutoff)")
	}
	if got := read(t, archivePath(path, 1)); got != rec(4) {
		t.Errorf("fresh .1 = %q, want %q", got, rec(4))
	}
	for n := 2; n <= 3; n++ {
		if exists(archivePath(path, n)) {
			t.Errorf("backdated archive .%d should have been purged by maxAgeDays", n)
		}
	}
}

// TestRotatingWriter_AgeDisabledKeepsOldArchives is the control: with
// maxAgeDays=0 the age purge is a no-op, so backdated archives survive (only the
// count cap applies).
func TestRotatingWriter_AgeDisabledKeepsOldArchives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.log")
	w, err := NewRotatingWriter(path, 8, 3, 0) // age purge disabled
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	for i := 1; i <= 3; i++ {
		if _, err := w.Write([]byte(rec(i))); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		backDate(t, archivePath(path, i), 20*24*time.Hour)
	}
	if _, err := w.Write([]byte(rec(4))); err != nil { // rotates; keep=3 caps at .3
		t.Fatalf("write 4: %v", err)
	}
	// With age disabled, the chain stays full (.1..3); only rec1 (oldest,
	// pushed past .3) is dropped by the count cap.
	for n := 1; n <= 3; n++ {
		if !exists(archivePath(path, n)) {
			t.Errorf("archive .%d should survive when maxAgeDays is disabled", n)
		}
	}
}

func backDate(t *testing.T, p string, d time.Duration) {
	t.Helper()
	past := time.Now().Add(-d)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatalf("Chtimes %s: %v", p, err)
	}
}
