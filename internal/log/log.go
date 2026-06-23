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

func Setup(path string) (*slog.Logger, *Ring, io.Closer, error) {
	if path == "" {
		path = DefaultPath()
	}
	// The base writer is a size-rotating file so logs persist across restarts
	// in bounded disk; it feeds the same records the ring captures, so the
	// on-disk log and the TUI's scrollback stay identical.
	w, err := NewRotatingWriter(path, defaultMaxSize, defaultKeep)
	if err != nil {
		return nil, nil, nil, err
	}
	ring := NewRing()
	base := slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := ringHandler{base: base, ring: ring}
	return slog.New(h), ring, w, nil
}
