package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/portuber/portato/internal/config"
)

// newNudgeFixture wires a fresh-install nudge scenario: HOME with a
// forwarded ~/.ssh/config, a bootstrapped config.yaml, and the nudge seams
// overridden (gate open, TTY assumed) with call recording. Returns the
// config path and a recorder for the done marker.
func newNudgeFixture(t *testing.T, sshConfig string) (cfgPath string, done *[]bool) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PORTATO_STATE_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ssh", "config"), []byte(sshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath = filepath.Join(dir, "config.yaml")
	if _, err := config.EnsureExample(cfgPath); err != nil {
		t.Fatal(err)
	}

	prevGate, prevTTY, prevDone, prevConfirm := importNudgeGate, importNudgeInteractive, importNudgeDone, confirmImport
	calls := []bool{}
	importNudgeGate = func() bool { return true }
	importNudgeInteractive = func() bool { return true }
	importNudgeDone = func() { calls = append(calls, true) }
	confirmImport = func(string) bool { return true }
	t.Cleanup(func() {
		importNudgeGate, importNudgeInteractive, importNudgeDone, confirmImport = prevGate, prevTTY, prevDone, prevConfirm
	})
	return cfgPath, &calls
}

func nudgeTubers(t *testing.T, cfgPath string) map[string]config.Tuber {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]config.Tuber{}
	for _, tub := range cfg.Tubers {
		out[tub.Name] = tub
	}
	return out
}

func TestNudge_AcceptImportsAllDisabled(t *testing.T) {
	cfgPath, done := newNudgeFixture(t, `Host db
  LocalForward 5432 10.0.0.5:5432
  DynamicForward 1080

Host ci
  RemoteForward 8080 127.0.0.1:80
`)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	got := maybeOfferImport(cfgPath, cfg)

	tubers := nudgeTubers(t, cfgPath)
	for _, name := range []string{"db-5432", "db-1080", "ci-8080"} {
		tub, ok := tubers[name]
		if !ok {
			t.Fatalf("%s not imported; got %v", name, tubers)
		}
		if tub.Enabled {
			t.Errorf("%s must be disabled", name)
		}
	}
	if len(*done) != 1 {
		t.Errorf("offer must be consumed exactly once; got %d", len(*done))
	}
	if got == cfg {
		// A successful import reloads the config so the TUI lists the tubers.
		found := false
		for _, tub := range got.Tubers {
			if tub.Name == "db-5432" {
				found = true
			}
		}
		if !found {
			t.Errorf("returned config does not include the imported tubers")
		}
	}
}

func TestNudge_DeclineImportsNothing(t *testing.T) {
	cfgPath, done := newNudgeFixture(t, "Host db\n  LocalForward 5432 10.0.0.5:5432\n")
	confirmImport = func(string) bool { return false }

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	maybeOfferImport(cfgPath, cfg)

	if tubers := nudgeTubers(t, cfgPath); len(tubers) != 1 {
		t.Errorf("decline must import nothing; got %v", tubers)
	}
	if len(*done) != 1 {
		t.Errorf("decline still consumes the offer; got %d calls", len(*done))
	}
}

func TestNudge_ZeroCandidatesSilentOneShot(t *testing.T) {
	cfgPath, done := newNudgeFixture(t, "Host plain\n  HostName 10.0.0.1\n")
	prompted := false
	confirmImport = func(string) bool { prompted = true; return true }

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	maybeOfferImport(cfgPath, cfg)

	if prompted {
		t.Error("zero candidates must not prompt")
	}
	if len(*done) != 1 {
		t.Errorf("zero candidates still consumes the offer; got %d calls", len(*done))
	}
}

func TestNudge_NonTTYLeavesOfferUnconsumed(t *testing.T) {
	cfgPath, done := newNudgeFixture(t, "Host db\n  LocalForward 5432 10.0.0.5:5432\n")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// go test stdin is not a terminal: the interactive guard fails and the
	// offer must survive for a later interactive run.
	importNudgeInteractive = func() bool { return false }
	maybeOfferImport(cfgPath, cfg)

	if len(*done) != 0 {
		t.Errorf("non-TTY launch must not consume the offer; got %d calls", len(*done))
	}
	if tubers := nudgeTubers(t, cfgPath); len(tubers) != 1 {
		t.Errorf("non-TTY launch must import nothing; got %v", tubers)
	}
}

func TestNudge_GateClosedNeverNudges(t *testing.T) {
	cfgPath, done := newNudgeFixture(t, "Host db\n  LocalForward 5432 10.0.0.5:5432\n")
	importNudgeGate = func() bool { return false }
	confirmImport = func(string) bool { t.Error("closed gate must not prompt"); return false }

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	maybeOfferImport(cfgPath, cfg)

	if len(*done) != 0 {
		t.Errorf("closed gate must not touch the markers; got %d calls", len(*done))
	}
}
