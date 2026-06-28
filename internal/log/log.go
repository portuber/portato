package log

import (
	"io"
	"log/slog"
	"path/filepath"

	"github.com/adrg/xdg"
)

func DefaultPath() string {
	return filepath.Join(xdg.StateHome, "portato", "portato.log")
}

// DaemonPath is the log file used by `portato daemon`.
func DaemonPath() string {
	return filepath.Join(xdg.StateHome, "portato", "daemon.log")
}

func Setup(path string, level slog.Level) (*slog.Logger, *Ring, io.Closer, error) {
	if path == "" {
		path = DefaultPath()
	}
	// The base writer is a size-rotating file so logs persist across restarts
	// in bounded disk; it feeds the same records the ring captures, so the
	// on-disk log and the TUI's scrollback stay identical. The level gates the
	// file write threshold (Phase 20 --log-level); the ring still captures at
	// Debug independently so the TUI logs screen keeps its debug toggle.
	w, err := NewRotatingWriter(path, defaultMaxSize, defaultKeep)
	if err != nil {
		return nil, nil, nil, err
	}
	ring := NewRing()
	base := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	h := ringHandler{base: base, ring: ring}
	return slog.New(h), ring, w, nil
}
