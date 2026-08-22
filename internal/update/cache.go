package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
)

// CheckCache is the daemon-written record of the last successful background
// check (Phase 49). Consent lives elsewhere — in config.yaml's
// defaults.update_check (nil = not asked, true/false = the answer) — because
// it is a user setting, not machine state. This file is pure cache: TUI /
// doctor read it to show "vX.Y.Z available" without any network I/O; losing
// it only costs one extra poll.
//
// The tri-state consent model:
//
//	update_check absent  → "ask": one-time prompt pending, no background checks
//	update_check: true   → "on":  the daemon checks daily (24h TTL)
//	update_check: false  → "off": never check, never ask again
type CheckCache struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest,omitempty"`
}

// cachePath resolves the cache file location:
// xdg.StateHome/portato/update.json — next to the log files and the Phase-48
// markers, the directory the daemon already owns. PORTATO_STATE_HOME
// overrides the base (the same test seam the markers use).
func cachePath() string {
	base := xdg.StateHome
	if dir := os.Getenv("PORTATO_STATE_HOME"); dir != "" {
		base = dir
	}
	return filepath.Join(base, "portato", "update.json")
}

// CachePathForTest lets tests in other internal packages point the cache at
// a temp dir; same hook pattern as SetBaseForTest (production callers cannot
// supply a testing-style hook).
func CachePathForTest(t interface{ Cleanup(func()) }, dir string) {
	prev := cachePathOverride
	cachePathOverride = dir
	t.Cleanup(func() { cachePathOverride = prev })
}

// cachePathOverride, when non-empty, replaces cachePath() wholesale — the
// variable behind CachePathForTest.
var cachePathOverride string

// LoadCache reads the last successful check. A missing file is not an error:
// it returns the zero cache (never checked). A corrupt file is treated the
// same way — the cache is disposable, so it is rewritten on the next
// successful check rather than surfacing an error to the user.
func LoadCache() (CheckCache, error) {
	path := cachePath()
	if cachePathOverride != "" {
		path = cachePathOverride
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckCache{}, nil
		}
		return CheckCache{}, fmt.Errorf("update: read cache: %w", err)
	}
	var c CheckCache
	if err := json.Unmarshal(data, &c); err != nil {
		return CheckCache{}, nil
	}
	return c, nil
}

// SaveCache atomically writes the check cache (tmp + rename, mode 0600).
func SaveCache(c CheckCache) error {
	path := cachePath()
	if cachePathOverride != "" {
		path = cachePathOverride
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("update: create state dir: %w", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("update: marshal cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("update: write cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("update: rename cache: %w", err)
	}
	return nil
}

// NeedsCheck reports whether a background check is due: no successful check
// has ever happened, or the last one is older than ttl. A failed check does
// not advance LastCheck, so a flaky network retries at most once per tick.
func (c CheckCache) NeedsCheck(now time.Time, ttl time.Duration) bool {
	return c.LastCheck.IsZero() || now.Sub(c.LastCheck) >= ttl
}
