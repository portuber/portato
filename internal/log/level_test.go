package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetup_LevelGatesFileWrite verifies the Phase 20 --log-level wiring: the
// level passed to Setup is the file handler's threshold. At Debug, debug
// records reach the file; at Error, info records are suppressed while error
// records still land. The ring is unaffected (it captures Debug regardless),
// which is asserted separately by the ring-capture behaviour.
func TestSetup_LevelGatesFileWrite(t *testing.T) {
	t.Run("debug writes debug records", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "p.log")
		logger, _, closer, err := Setup(path, slog.LevelDebug, LogOptions{})
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		logger.Debug("dbg-line", "k", "v")
		if err := closer.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		got, _ := os.ReadFile(path)
		if !strings.Contains(string(got), "dbg-line") {
			t.Errorf("debug level: file = %q, want it to contain dbg-line", got)
		}
	})

	t.Run("error suppresses info keeps error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "p.log")
		logger, _, closer, err := Setup(path, slog.LevelError, LogOptions{})
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		logger.Info("info-line")
		logger.Error("err-line")
		if err := closer.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		got, _ := os.ReadFile(path)
		if strings.Contains(string(got), "info-line") {
			t.Errorf("error level: file should not contain info-line, got %s", got)
		}
		if !strings.Contains(string(got), "err-line") {
			t.Errorf("error level: file = %q, want it to contain err-line", got)
		}
	})
}

// TestSetup_RingKeepsDebugAtInfoLevel guards the deliberate asymmetry: even
// when the file threshold is Info, the ring still captures Debug records so the
// TUI logs screen's debug toggle has something to show.
func TestSetup_RingKeepsDebugAtInfoLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.log")
	logger, ring, closer, err := Setup(path, slog.LevelInfo, LogOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer closer.Close()
	logger.Debug("dbg-in-ring")
	if len(ring.Lines("")) == 0 {
		t.Error("ring should capture debug records even at Info file level")
	}
}
