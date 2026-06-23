package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Rotation knobs. Sized to bound a long-running daemon's disk usage to roughly
// (keep+1) * maxSize. Constants for now; config later if needed.
const (
	defaultMaxSize int64 = 1 << 20 // 1 MiB
	defaultKeep          = 3
)

// RotatingWriter is an io.WriteCloser that writes to a file at path and, once
// the file reaches maxSize bytes, rotates it: path -> path.1 -> path.2 -> ...
// keeping at most `keep` archives (the oldest is removed on each rotation).
//
// It exists so daemon/standalone logs persist across restarts in a bounded
// amount of disk: a long-running daemon would otherwise grow one file forever.
// The write path is mutex-guarded so the slog handler's concurrent calls are
// safe. RotatingWriter also satisfies the base-writer role that ringHandler
// (ring.go) forwards formatted records to, so the on-disk log and the TUI's
// in-memory ring see identical content.
type RotatingWriter struct {
	path    string
	maxSize int64
	keep    int

	mu         sync.Mutex
	f          *os.File
	written    int64 // bytes in the current file, cached to avoid a stat per write
	lastRotate time.Time
}

// NewRotatingWriter opens path (created 0600, parent dir 0700) for appending
// and returns a writer that rotates at maxSize, keeping `keep` archives. A
// zero/negative maxSize or keep falls back to the package defaults.
func NewRotatingWriter(path string, maxSize int64, keep int) (*RotatingWriter, error) {
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}
	if keep <= 0 {
		keep = defaultKeep
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create log dir: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat log file: %w", err)
	}
	return &RotatingWriter{
		path:    path,
		maxSize: maxSize,
		keep:    keep,
		f:       f,
		written: info.Size(),
	}, nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.f.Write(p)
	if n > 0 {
		w.written += int64(n)
	}
	if err != nil {
		return n, err
	}
	if w.written >= w.maxSize {
		// Rotation is best-effort: a rotation failure must not lose the write
		// or surface to slog (which would then noise up the daemon). The
		// current file stays open on failure and the next Write retries.
		_ = w.rotateLocked()
	}
	return n, nil
}

// rotateLocked closes the current file, shifts the archive chain down by one
// (dropping the oldest beyond `keep`), renames it to .1, and opens a fresh
// file. Caller holds w.mu.
func (w *RotatingWriter) rotateLocked() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	// Free the tail of the chain first so the renames below have room: keep=3
	// means archives .1, .2, .3 may exist; drop .3 before shifting.
	if w.keep > 0 {
		_ = os.Remove(archivePath(w.path, w.keep))
	}
	for i := w.keep - 1; i >= 1; i-- {
		_ = os.Rename(archivePath(w.path, i), archivePath(w.path, i+1))
	}
	if err := os.Rename(w.path, archivePath(w.path, 1)); err != nil {
		// Rename failed (cross-device? permissions?): reopen the original so
		// subsequent writes still land somewhere instead of losing the writer.
		f, oerr := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if oerr != nil {
			return oerr
		}
		w.f = f
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w.f = f
	w.written = 0
	w.lastRotate = time.Now()
	return nil
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	return w.f.Close()
}

// LastRotate returns the time of the most recent rotation, or the zero time if
// none has happened yet. `portato doctor` is a separate process and cannot read
// this field; it reads rotation evidence from the archive files instead.
func (w *RotatingWriter) LastRotate() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastRotate
}

func archivePath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}
