package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/update"
)

// resetConsentSeams snapshots the four consent seams and restores them.
func resetConsentSeams(t *testing.T) {
	t.Helper()
	gate, inter, prompt, persist := consentAskGate, consentAskInteractive, consentAskPrompt, consentAskPersist
	t.Cleanup(func() {
		consentAskGate, consentAskInteractive, consentAskPrompt, consentAskPersist = gate, inter, prompt, persist
	})
}

func consentFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	update.CachePathForTest(t, filepath.Join(dir, "update.json"))
	return path
}

func TestConsentAskPendingOnly(t *testing.T) {
	resetConsentSeams(t)
	asked := 0
	consentAskInteractive = func() bool { return true }
	consentAskPrompt = func(string) bool { return true }
	consentAskPersist = func(_ string, _ bool) error { asked++; return nil }

	for _, body := range []string{
		"defaults: {}\n",                     // absent → ask
		"defaults:\n  update_check: true\n",  // answered on → no ask
		"defaults:\n  update_check: false\n", // answered off → no ask
	} {
		path := consentFixture(t, body)
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		before := asked
		_ = maybeAskUpdateConsent(path, cfg)
		want := 1
		if cfg.Defaults.UpdateCheck != nil {
			want = 0
		}
		if asked-before != want {
			t.Errorf("body %q: asks = %d, want %d", body, asked-before, want)
		}
	}
}

func TestConsentAskNonTTYDoesNotConsume(t *testing.T) {
	resetConsentSeams(t)
	asked := false
	consentAskInteractive = func() bool { return false }
	consentAskPrompt = func(string) bool { asked = true; return true }

	path := consentFixture(t, "defaults: {}\n")
	cfg, _ := config.Load(path)
	_ = maybeAskUpdateConsent(path, cfg)
	if asked {
		t.Error("non-TTY run consumed the question")
	}
	// And nothing was persisted.
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Defaults.UpdateCheck != nil {
		t.Error("non-TTY run persisted a consent answer")
	}
}

func TestConsentAskPersistsAnswer(t *testing.T) {
	resetConsentSeams(t)
	consentAskInteractive = func() bool { return true }

	for _, tc := range []struct {
		answer bool
		want   string
	}{
		{true, "update_check: true"},
		{false, "update_check: false"},
	} {
		path := consentFixture(t, "defaults: {}\n")
		consentAskPrompt = func(string) bool { return tc.answer }
		cfg, _ := config.Load(path)
		out := maybeAskUpdateConsent(path, cfg)
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), tc.want) {
			t.Errorf("answer %v: config =\n%s\nwant %s", tc.answer, data, tc.want)
		}
		// The returned config reflects the answer (reloaded from disk).
		if out.Defaults.UpdateCheck == nil || *out.Defaults.UpdateCheck != tc.answer {
			t.Errorf("answer %v: returned config UpdateCheck = %v", tc.answer, out.Defaults.UpdateCheck)
		}
	}
}

func TestConsentAskPersistFailureStaysPending(t *testing.T) {
	resetConsentSeams(t)
	consentAskInteractive = func() bool { return true }
	consentAskPrompt = func(string) bool { return true }
	consentAskPersist = func(string, bool) error { return os.ErrPermission }

	path := consentFixture(t, "defaults: {}\n")
	cfg, _ := config.Load(path)
	_ = maybeAskUpdateConsent(path, cfg)
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Defaults.UpdateCheck != nil {
		t.Error("failed persist must leave the question pending")
	}
}

func TestDoctorUpdateLine(t *testing.T) {
	prevExe := doctorExecutable
	t.Cleanup(func() { doctorExecutable = prevExe })
	cases := []struct {
		name    string
		cfg     string
		cache   update.CheckCache
		version string
		exe     string
		want    string
		notWant string
	}{
		{
			name: "off",
			cfg:  "defaults:\n  update_check: false\n",
			want: "checks off",
		},
		{
			name: "pending",
			cfg:  "defaults: {}\n",
			want: "not asked yet",
		},
		{
			name:  "on-no-result",
			cfg:   "defaults:\n  update_check: true\n",
			cache: update.CheckCache{},
			want:  "no result yet",
		},
		{
			name:    "on-newer",
			cfg:     "defaults:\n  update_check: true\n",
			cache:   update.CheckCache{Latest: "v9.9.9"},
			version: "1.6.1",
			want:    "v9.9.9 available",
		},
		{
			name:    "on-uptodate",
			cfg:     "defaults:\n  update_check: true\n",
			cache:   update.CheckCache{Latest: "v1.6.1"},
			version: "1.6.1",
			want:    "up to date",
		},
		{
			name:    "on-dev-build",
			cfg:     "defaults:\n  update_check: true\n",
			cache:   update.CheckCache{Latest: "v9.9.9"},
			version: "dev",
			want:    "latest v9.9.9",
			notWant: "available",
		},
		{
			name:    "managed-channel-hint",
			cfg:     "defaults:\n  update_check: true\n",
			cache:   update.CheckCache{Latest: "v9.9.9"},
			version: "1.6.1",
			exe:     "/opt/homebrew/bin/portato",
			want:    "homebrew install (`apply` defers to: brew upgrade --cask portuber/tap/portato)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := consentFixture(t, tc.cfg)
			if err := update.SaveCache(tc.cache); err != nil {
				t.Fatal(err)
			}
			oldVer := version
			if tc.version != "" {
				version = tc.version
			}
			t.Cleanup(func() { version = oldVer })
			doctorExecutable = func() (string, error) { return tc.exe, nil }
			if tc.exe == "" {
				doctorExecutable = prevExe
			}

			out := &strings.Builder{}
			d := newDoctor(out)
			checkUpdate(d, path)
			s := out.String()
			if !strings.Contains(s, tc.want) {
				t.Errorf("doctor line = %q, want %q", s, tc.want)
			}
			if tc.notWant != "" && strings.Contains(s, tc.notWant) {
				t.Errorf("doctor line = %q, must not contain %q", s, tc.notWant)
			}
		})
	}
}

func TestMaybeAskConsentTTYGate(t *testing.T) {
	resetConsentSeams(t)
	// Non-interactive: nothing happens, false returned.
	consentAskInteractive = func() bool { return false }
	path := consentFixture(t, "defaults: {}\n")
	if maybeAskConsentTTY(path) {
		t.Error("non-TTY maybeAskConsentTTY reports asked")
	}
	// Interactive + pending: asked, persisted, true returned.
	consentAskInteractive = func() bool { return true }
	consentAskPrompt = func(string) bool { return true }
	if !maybeAskConsentTTY(path) {
		t.Error("TTY maybeAskConsentTTY with pending question reports not-asked")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.UpdateCheck == nil || !*cfg.Defaults.UpdateCheck {
		t.Error("answer not persisted by maybeAskConsentTTY")
	}
	// Second call: already answered → false, no re-ask.
	if maybeAskConsentTTY(path) {
		t.Error("second maybeAskConsentTTY asks again")
	}
}
