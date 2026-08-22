package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/portuber/portato/internal/update"
)

func hintSetup(t *testing.T, latest string, checked time.Time) {
	t.Helper()
	dir := t.TempDir()
	update.CachePathForTest(t, filepath.Join(dir, "update.json"))
	if latest != "" {
		if err := update.SaveCache(update.CheckCache{LastCheck: checked, Latest: latest}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpdateHintShownWhenNewer(t *testing.T) {
	hintSetup(t, "v9.9.9", time.Now())
	m := New(newFake(), Options{Mode: "standalone", Version: "1.6.1"})
	if m.updateHint != "update: v9.9.9" {
		t.Errorf("updateHint = %q, want update: v9.9.9", m.updateHint)
	}
	h := m.header()
	if !containsPlain(h, "update: v9.9.9") {
		t.Errorf("header missing the hint segment:\n%s", h)
	}
}

func TestUpdateHintHiddenStates(t *testing.T) {
	cases := []struct {
		name    string
		latest  string
		version string
	}{
		{"up to date", "v1.6.1", "1.6.1"},
		{"cache older", "v1.5.0", "1.6.1"},
		{"no cache", "", "1.6.1"},
		{"dev build", "v9.9.9", "dev"},
		{"garbage cache", "weird", "1.6.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hintSetup(t, tc.latest, time.Now())
			m := New(newFake(), Options{Mode: "attach", Version: tc.version})
			if m.updateHint != "" {
				t.Errorf("%s: updateHint = %q, want empty", tc.name, m.updateHint)
			}
			if containsPlain(m.header(), "update:") {
				t.Errorf("%s: header leaked an update segment", tc.name)
			}
		})
	}
}

// containsPlain checks for a plain-text substring after stripping ANSI SGR
// sequences, so the assertion survives styling. Uses strings.Contains on the
// cleaned string.
func containsPlain(s, want string) bool {
	var clean []byte
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b { // ESC — skip through the closing 'm'
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		clean = append(clean, s[i])
	}
	return strings.Contains(string(clean), want)
}
