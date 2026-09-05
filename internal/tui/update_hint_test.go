package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

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

func TestHeaderVersionSegment(t *testing.T) {
	hintSetup(t, "v9.9.9", time.Now())
	m := New(newFake(), Options{Mode: "attach", Version: "1.8.1"})
	m.width = 100
	h := m.header()
	for _, want := range []string{"mode: attach", "v1.8.1", "update: v9.9.9"} {
		if !containsPlain(h, want) {
			t.Errorf("header missing %q:\n%s", want, h)
		}
	}
	m2 := New(newFake(), Options{Mode: "attach", Version: "v1.8.1"})
	if !containsPlain(m2.header(), "v1.8.1") {
		t.Errorf("v-prefixed version missing: %q", m2.header())
	}
}

func TestHeaderVersionDevRaw(t *testing.T) {
	m := New(newFake(), Options{Mode: "standalone", Version: "dev"})
	h := m.header()
	if !containsPlain(h, "dev") {
		t.Errorf("dev build version missing: %q", h)
	}
	if containsPlain(h, "vdev") {
		t.Errorf("dev got a bogus v-prefix: %q", h)
	}
}

func TestHeaderNoVersionOldShape(t *testing.T) {
	m := New(newFake(), Options{Mode: "attach"})
	h := m.header()
	if !containsPlain(h, "mode: attach") {
		t.Errorf("header: %q", h)
	}
	if s := stripANSI(h); strings.HasSuffix(strings.TrimRight(s, " "), "  attach  ") {
		t.Errorf("empty version leaked a separator: %q", s)
	}
}

func TestHeaderNarrowShortHint(t *testing.T) {
	hintSetup(t, "v9.9.9", time.Now())
	m := New(newFake(), Options{Mode: "attach", Version: "1.6.1"})
	m.width = 60
	h := m.header()
	if !containsPlain(h, "→ v9.9.9") {
		t.Errorf("narrow header missing the short hint:\n%s", h)
	}
	if containsPlain(h, "update: v9.9.9") {
		t.Errorf("narrow header kept the full hint:\n%s", h)
	}
	if strings.Contains(h, "\n") {
		t.Errorf("narrow header wrapped:\n%q", h)
	}
	if w := lipgloss.Width(h); w > 60 {
		t.Errorf("narrow header width = %d, want <= 60", w)
	}
}

func TestHeaderWideFullHint(t *testing.T) {
	hintSetup(t, "v9.9.9", time.Now())
	m := New(newFake(), Options{Mode: "standalone", Version: "1.6.1"})
	m.width = 100
	h := m.header()
	if !containsPlain(h, "update: v9.9.9") {
		t.Errorf("wide header missing the full hint:\n%s", h)
	}
	if containsPlain(h, "→ ") {
		t.Errorf("wide header needlessly shortened:\n%s", h)
	}
}

func stripANSI(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b = append(b, s[i])
	}
	return string(b)
}
