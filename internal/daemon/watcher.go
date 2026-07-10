package daemon

import (
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// watchPollInterval is how often the watcher stats the config file looking for
// changes. Default 500ms: an edit applies within ~1s (poll + settle), the
// phase-28 DoD budget. Overridable in tests.
var watchPollInterval = 500 * time.Millisecond

// watchSettle is the quiet window the file must remain unchanged for before a
// detected edit triggers a reload. It doubles as the debounce/coalesce window
// (a save burst keeps pushing the reload out until writes stop) and as the
// guard against reading a half-written file. Overridable in tests.
var watchSettle = 300 * time.Millisecond

// watcher polls a config file for changes and triggers a reload. It is
// deliberately polling-only (no fsnotify dependency): robust to the atomic
// temp+rename saves almost every editor uses — an inode-bound inotify watch
// would detach from the path on rename, while a path stat stays anchored — and
// CGO-free; a ~1s apply latency is acceptable for a config file. Reload
// failures never crash the daemon: the injected reload func (Server's
// reloadFromWatch) keeps the last-good config — applyReload returns before
// swapping on a parse error — so a syntactically bad edit is logged and
// skipped. A vanished file is likewise skipped (not reloaded), since
// config.Load on a missing path recreates an example and would clear the live
// tubers.
type watcher struct {
	path    string
	log     *slog.Logger
	reload  func() error
	stopCh  chan struct{}
	done    chan struct{}
	started atomic.Bool
}

func newWatcher(path string, reload func() error, log *slog.Logger) *watcher {
	if log == nil {
		log = slog.Default()
	}
	return &watcher{
		path:   path,
		log:    log,
		reload: reload,
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// start captures the current file as the reload baseline (synchronously, so a
// change made after start returns is guaranteed to be detected — no race with
// the loop goroutine's first stat) and launches the poll loop. Call once; stop
// waits for it to exit. The started flag lets stop() no-op when the watcher
// was never started (a Server built via New but never Start()ed, e.g. the
// second-instance rejection path in tests).
func (w *watcher) start() {
	stable, stableOK := w.stat()
	w.started.Store(true)
	go w.loop(stable, stableOK)
}

// loop is the poll-debounce-reload engine. State:
//   - stable: the stat signature at the time of the last successful reload
//     (initial value = the file at startup, so a boot-time config does not
//     trigger a redundant reload).
//   - last: the stat observed on the previous poll (used to detect churn
//     within a save burst).
//   - armed + deadline: a change is pending; reload once the file has been
//     quiet for watchSettle.
//
// A change detected re-arms the deadline on every subsequent churn, so a burst
// of writes collapses into a single reload that fires watchSettle after the
// last write — which also guarantees the file is settled (never read mid-write).
func (w *watcher) loop(stable os.FileInfo, stableOK bool) {
	defer close(w.done)

	ticker := time.NewTicker(watchPollInterval)
	defer ticker.Stop()

	last := stable
	var (
		armed    bool
		deadline time.Time
		vanished bool // warned about absence since the file last existed
	)
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			cur, exists := w.stat()
			if !exists {
				// Skip: a reload would hit config.Load's EnsureExample path
				// and wipe the live tubers. Warn once, then stay quiet until
				// the file reappears.
				if stableOK && !vanished {
					w.log.Warn("config vanished, keeping last-good config", "path", w.path)
					vanished = true
				}
				continue
			}
			vanished = false
			if !sameStat(cur, stable) {
				// Changed since the last reload.
				switch {
				case !armed:
					armed = true
					deadline = time.Now().Add(watchSettle)
				case !sameStat(cur, last):
					// Still churning: push the reload out.
					deadline = time.Now().Add(watchSettle)
				case !time.Now().Before(deadline):
					// Quiet for >= watchSettle: reload the settled file.
					if err := w.reload(); err != nil {
						w.log.Warn("config reload skipped", "err", err)
					} else {
						w.log.Info("config reloaded (file changed)", "path", w.path)
					}
					stable, stableOK = cur, true
					armed = false
				}
			} else {
				// Unchanged since last reload (or reverted to it): disarm.
				armed = false
			}
			last = cur
		}
	}
}

// stat returns the file's signature and whether it exists.
func (w *watcher) stat() (os.FileInfo, bool) {
	fi, err := os.Stat(w.path)
	if err != nil {
		return nil, false
	}
	return fi, true
}

// sameStat reports whether two stat results describe the same content: equal
// size and equal modification time. mtime carries nanosecond precision on
// APFS/ext4, so even a quick double-save is detected while coalescing a burst.
func sameStat(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// stop signals the loop to exit and blocks until it has. Idempotent and safe
// to call on a watcher that was never started (a no-op in that case).
func (w *watcher) stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
	if w.started.Load() {
		<-w.done
	}
}
