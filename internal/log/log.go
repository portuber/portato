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

// LogOptions configures the rotating writer's behaviour. All fields optional: a
// zero value falls back to the writer's package default (MaxSize/Retain) or is
// disabled (MaxAgeDays). Built from the config's defaults.log.* block (Phase 22)
// by the cmd layer and passed into Setup.
type LogOptions struct {
	MaxSize    int64 // bytes per file before rotation; 0 -> defaultMaxSize
	Retain     int   // rotated archives to keep; 0 -> defaultKeep
	MaxAgeDays int   // purge archives older than N days at rotation; 0 -> disabled
}

func Setup(path string, level slog.Level, opts LogOptions) (*slog.Logger, *Ring, io.Closer, error) {
	if path == "" {
		path = DefaultPath()
	}
	// The base writer is a size-rotating file so logs persist across restarts
	// in bounded disk; it feeds the same records the ring captures, so the
	// on-disk log and the TUI's scrollback stay identical. The level gates the
	// file write threshold (Phase 20 --log-level); the ring still captures at
	// Debug independently so the TUI logs screen keeps its debug toggle.
	w, err := NewRotatingWriter(path, opts.MaxSize, opts.Retain, opts.MaxAgeDays)
	if err != nil {
		return nil, nil, nil, err
	}
	ring := NewRing()
	base := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	h := ringHandler{base: base, ring: ring}
	return slog.New(h), ring, w, nil
}
