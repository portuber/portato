package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	CachePathForTest(t, filepath.Join(dir, "update.json"))

	// Missing file: zero cache, no error (never checked yet).
	c, err := LoadCache()
	if err != nil || !c.LastCheck.IsZero() || c.Latest != "" {
		t.Fatalf("LoadCache on missing file = %+v, %v; want zero, nil", c, err)
	}

	want := CheckCache{
		LastCheck: time.Date(2026, 8, 22, 11, 0, 0, 0, time.UTC),
		Latest:    "v1.7.0",
	}
	if err := SaveCache(want); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	got, err := LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if !got.LastCheck.Equal(want.LastCheck) || got.Latest != want.Latest {
		t.Fatalf("round-trip = %+v; want %+v", got, want)
	}
}

func TestCacheFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.json")
	CachePathForTest(t, path)
	if err := SaveCache(CheckCache{LastCheck: time.Now(), Latest: "v1"}); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %o, want 0600", perm)
	}
}

func TestCacheCorruptIsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.json")
	CachePathForTest(t, path)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCache()
	if err != nil || !c.LastCheck.IsZero() {
		t.Fatalf("corrupt cache = %+v, %v; want zero, nil (cache is disposable)", c, err)
	}
}

func TestNeedsCheck(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour
	cases := []struct {
		last  time.Time
		want  bool
		label string
	}{
		{time.Time{}, true, "never checked"},
		{now.Add(-23 * time.Hour), false, "23h old: skip"},
		{now.Add(-25 * time.Hour), true, "25h old: check"},
		{now.Add(-24 * time.Hour), true, "exactly ttl: check"},
	}
	for _, tc := range cases {
		if got := (CheckCache{LastCheck: tc.last}).NeedsCheck(now, ttl); got != tc.want {
			t.Errorf("NeedsCheck(%s) = %v, want %v", tc.label, got, tc.want)
		}
	}
}
