package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/update"
)

// updateChecker is the phase-49 background poll: when (and only when) the
// config consents (defaults.update_check: true), the daemon asks GitHub for
// the latest release at most once per TTL (24h) and records it in the shared
// check cache, so the TUI header and doctor report without network I/O of
// their own. Off-consent it is fully idle — zero requests, zero writes.
//
// The ticker re-reads the consent on every tick instead of caching it, so a
// `portato update consent off` (or any hand edit of config.yaml) takes
// effect within one tick via the same file the reload watcher observes; no
// restart, no IPC surface.
type updateChecker struct {
	log    *slog.Logger
	client *update.Client

	mu     sync.Mutex
	tick   func() time.Time
	ttl    time.Duration
	check  func(context.Context) (update.Release, error)
	onDone func(update.Release)
}

const (
	// updateTickInterval is how often consent + cache age are re-evaluated.
	// A tick performs no network I/O unless a check is actually due.
	updateTickInterval = time.Hour
	// updateCheckTTL is the minimum spacing between two successful checks.
	updateCheckTTL = 24 * time.Hour
)

func newUpdateChecker(log *slog.Logger) *updateChecker {
	return &updateChecker{
		log:  log,
		tick: time.Now,
		ttl:  updateCheckTTL,
		check: func(ctx context.Context) (update.Release, error) {
			return update.NewClient(updateUserAgent).Latest(ctx)
		},
	}
}

// updateUserAgent is set by the cmd package (which owns the embedded
// version) before the daemon starts; the fallback keeps tests honest.
var updateUserAgent = "portato/daemon"

// SetUpdateUserAgent installs the User-Agent the background check sends
// (called by cmd with "portato/<version>" before the daemon starts).
func SetUpdateUserAgent(ua string) { updateUserAgent = ua }

// run loops until ctx is done: every tick it re-reads the config; with
// consent on and the cache past its TTL it performs one check and records
// the result. Errors are logged at debug (a background poll must never
// surface as a daemon problem) and never advance the cache clock — the
// retry waits for the next tick after the TTL.
func (u *updateChecker) run(ctx context.Context, cfgPath string, cfgLoader func(string) (*config.Config, error)) {
	t := time.NewTicker(updateTickInterval)
	defer t.Stop()
	u.tryCheck(ctx, cfgPath, cfgLoader)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.tryCheck(ctx, cfgPath, cfgLoader)
		}
	}
}

// tryCheck performs one gate evaluation (consent + TTL) and, when due, one
// network check + cache write. Split from run for direct testability.
func (u *updateChecker) tryCheck(ctx context.Context, cfgPath string, cfgLoader func(string) (*config.Config, error)) {
	cfg, err := cfgLoader(cfgPath)
	if err != nil {
		u.log.Debug("update check: config unreadable", "err", err)
		return
	}
	if cfg.Defaults.UpdateCheck == nil || !*cfg.Defaults.UpdateCheck {
		return
	}
	cache, err := update.LoadCache()
	if err != nil {
		cache = update.CheckCache{}
	}
	now := u.tick()
	if !cache.NeedsCheck(now, u.ttl) {
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	rel, err := u.check(checkCtx)
	if err != nil {
		u.log.Debug("update check failed", "err", err)
		return
	}
	if err := update.SaveCache(update.CheckCache{LastCheck: now, Latest: rel.Version}); err != nil {
		u.log.Debug("update check: cache write failed", "err", err)
		return
	}
	u.log.Debug("update check done", "latest", rel.Version)
	if u.onDone != nil {
		u.onDone(rel)
	}
}

// updateCheckTimeout bounds one background network check.
const updateCheckTimeout = 10 * time.Second
