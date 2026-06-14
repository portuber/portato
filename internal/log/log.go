package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

func DefaultPath() string {
	return filepath.Join(xdg.StateHome, "portato", "portato.log")
}

func Setup(path string) (*slog.Logger, io.Closer, error) {
	if path == "" {
		path = DefaultPath()
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("create log dir: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	h := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), f, nil
}
