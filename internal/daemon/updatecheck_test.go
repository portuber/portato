package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/update"
)

// newTestChecker builds an updateChecker with instrumented seams: a frozen
// clock, a counting check fn and an onDone hook.
func newTestChecker(t *testing.T) (*updateChecker, *int, *time.Time) {
	t.Helper()
	calls, checked := 0, time.Time{}
	c := newUpdateChecker(slog.Default())
	c.tick = func() time.Time { return checked }
	c.check = func(context.Context) (update.Release, error) {
		calls++
		return update.Release{Version: "v9.9.9"}, nil
	}
	c.onDone = func(update.Release) {}
	return c, &calls, &checked
}

// writeConsentConfig writes a config with the given consent body into dir
// and returns its path.
func writeConsentConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpdateCheckerConsentGate(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"on", "defaults:\n  update_check: true\n", 1},
		{"off", "defaults:\n  update_check: false\n", 0},
		{"absent", "defaults: {}\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			update.CachePathForTest(t, filepath.Join(dir, "update.json"))
			path := writeConsentConfig(t, dir, tc.body)

			c, calls, checked := newTestChecker(t)
			*checked = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
			c.tryCheck(context.Background(), path, config.Load)
			if *calls != tc.want {
				t.Errorf("consent %s: checks = %d, want %d", tc.name, *calls, tc.want)
			}
			if tc.want == 1 {
				cache, _ := update.LoadCache()
				if cache.Latest != "v9.9.9" || cache.LastCheck.IsZero() {
					t.Errorf("cache = %+v after a due check", cache)
				}
			}
		})
	}
}

func TestUpdateCheckerTTL(t *testing.T) {
	dir := t.TempDir()
	update.CachePathForTest(t, filepath.Join(dir, "update.json"))
	path := writeConsentConfig(t, dir, "defaults:\n  update_check: true\n")

	c, calls, checked := newTestChecker(t)
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	// Fresh cache: checks.
	*checked = base
	c.tryCheck(context.Background(), path, config.Load)
	if *calls != 1 {
		t.Fatalf("fresh: checks = %d, want 1", *calls)
	}
	// 23h later: TTL not elapsed, skip.
	*checked = base.Add(23 * time.Hour)
	c.tryCheck(context.Background(), path, config.Load)
	if *calls != 1 {
		t.Fatalf("23h: checks = %d, want still 1", *calls)
	}
	// 25h later: checks again.
	*checked = base.Add(25 * time.Hour)
	c.tryCheck(context.Background(), path, config.Load)
	if *calls != 2 {
		t.Fatalf("25h: checks = %d, want 2", *calls)
	}
}

func TestUpdateCheckerFailureDoesNotAdvanceClock(t *testing.T) {
	dir := t.TempDir()
	update.CachePathForTest(t, filepath.Join(dir, "update.json"))
	path := writeConsentConfig(t, dir, "defaults:\n  update_check: true\n")

	c, _, checked := newTestChecker(t)
	c.check = func(context.Context) (update.Release, error) {
		return update.Release{}, errors.New("boom")
	}
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	*checked = base
	c.tryCheck(context.Background(), path, config.Load)

	cache, _ := update.LoadCache()
	if !cache.LastCheck.IsZero() {
		t.Errorf("failed check advanced last_check to %v", cache.LastCheck)
	}
	// One tick later (within TTL of zero-cache semantics: never checked =>
	// due again) a recovered network retries — no lockout.
	c.check = func(context.Context) (update.Release, error) {
		return update.Release{Version: "v9.9.9"}, nil
	}
	c.tryCheck(context.Background(), path, config.Load)
	cache, _ = update.LoadCache()
	if cache.Latest != "v9.9.9" {
		t.Errorf("recovered check did not populate the cache: %+v", cache)
	}
}

func TestUpdateCheckerUnreadableConfig(t *testing.T) {
	dir := t.TempDir()
	update.CachePathForTest(t, filepath.Join(dir, "update.json"))

	c, calls, _ := newTestChecker(t)
	c.tryCheck(context.Background(), filepath.Join(dir, "missing.yaml"), config.Load)
	if *calls != 0 {
		t.Errorf("unreadable config: checks = %d, want 0", *calls)
	}
}
