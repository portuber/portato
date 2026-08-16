package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
)

// newImportFixture writes the Phase-48 verification ssh_config fixture, an
// empty HOME (so config resolution never sees the developer's real
// ~/.ssh/config) and resets the import command's globals. Flag state is set
// by the caller on the package vars (the package convention: RunE is invoked
// directly with positional args).
func newImportFixture(t *testing.T) (sshPath, cfgPath string) {
	t.Helper()
	dir := t.TempDir()
	sshPath = filepath.Join(dir, "ssh_config")
	fixture := `Host db
  HostName 10.0.0.5
  LocalForward 5432 10.0.0.5:5432
  DynamicForward 1080

Host ci
  RemoteForward 8080 127.0.0.1:80

Host *
  LocalForward 9999 global:9999
`
	if err := os.WriteFile(sshPath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("PORTATO_STATE_HOME", dir)
	cfgPath = filepath.Join(dir, "config.yaml")

	prevCfg, prevAll, prevFrom, prevDry, prevYes := cfgFile, importAll, importFrom, importDryRun, importYes
	cfgFile, importAll, importFrom, importDryRun, importYes = cfgPath, false, sshPath, false, false
	t.Cleanup(func() {
		cfgFile, importAll, importFrom, importDryRun, importYes = prevCfg, prevAll, prevFrom, prevDry, prevYes
	})
	return sshPath, cfgPath
}

func runImport(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	c, out, errOut := captureCmd()
	err := importRunE(c, args)
	return out.String(), errOut.String(), err
}

func snapshot(t *testing.T, path string) ([]byte, time.Time) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return b, fi.ModTime()
}

func assertUnchanged(t *testing.T, path string, before []byte, beforeMod time.Time, what string) {
	t.Helper()
	after, mod := snapshot(t, path)
	if !bytes.Equal(before, after) {
		t.Errorf("%s content changed by import", what)
	}
	if !mod.Equal(beforeMod) {
		t.Errorf("%s mtime changed by import", what)
	}
}

func TestImport_DryRunListsCandidates(t *testing.T) {
	sshPath, _ := newImportFixture(t)
	before, mod := snapshot(t, sshPath)
	importDryRun, importAll = true, true

	out, errOut, err := runImport(t, nil)
	if err != nil {
		t.Fatalf("dry run: %v (stderr %q)", err, errOut)
	}
	for _, want := range []string{"db-5432", "db-1080", "ci-8080", "dry run"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "9999") {
		t.Errorf("Host * forward leaked into candidates; got:\n%s", out)
	}
	assertUnchanged(t, sshPath, before, mod, "ssh config")
}

func TestImport_DryRunDoesNotCreateConfig(t *testing.T) {
	_, cfgPath := newImportFixture(t)
	importDryRun, importAll = true, true
	if _, _, err := runImport(t, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Errorf("dry run must not create config.yaml")
	}
}

func TestImport_YesCreatesDisabledTubers(t *testing.T) {
	sshPath, cfgPath := newImportFixture(t)
	before, mod := snapshot(t, sshPath)
	importYes, importAll = true, true

	out, _, err := runImport(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "imported db-5432") || !strings.Contains(out, "imported ci-8080") {
		t.Errorf("missing per-tuber lines; got:\n%s", out)
	}
	assertUnchanged(t, sshPath, before, mod, "ssh config")
	assertImportedRoundTrip(t, cfgPath)
}

// assertImportedRoundTrip reloads the config from disk and checks the
// Phase-48 fixture's three imported tubers: field mapping, the loopback-
// preserving bare-port RemoteForward, and enabled: false everywhere.
func assertImportedRoundTrip(t *testing.T, cfgPath string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]config.Tuber{}
	for _, tub := range cfg.Tubers {
		byName[tub.Name] = tub
	}
	db, ok := byName["db-5432"]
	if !ok {
		t.Fatalf("db-5432 missing after import; got %v", byName)
	}
	if db.Enabled || db.Type != "local" || db.Local != "5432" || db.Remote != "10.0.0.5:5432" || db.SSH != "db" {
		t.Errorf("db-5432 round-trip: %+v", db)
	}
	ci, ok := byName["ci-8080"]
	if !ok {
		t.Fatalf("ci-8080 missing after import")
	}
	// Bare-port RemoteForward imports loopback-preserving, not *:port.
	if ci.Type != "remote" || ci.Remote != "127.0.0.1:8080" || ci.Local != "127.0.0.1:80" || ci.Enabled {
		t.Errorf("ci-8080 round-trip: %+v", ci)
	}
	dyn, ok := byName["db-1080"]
	if !ok || dyn.Type != "dynamic" || dyn.Local != "1080" || dyn.Enabled {
		t.Errorf("db-1080 round-trip: %+v (ok %v)", dyn, ok)
	}
}

func TestImport_NonTTYWithoutYesFails(t *testing.T) {
	newImportFixture(t)
	importAll = true
	// go test runs with stdin that is not a terminal, so the guard fires.
	_, errOut, err := runImport(t, nil)
	if err == nil {
		t.Fatal("non-TTY import without --yes must fail")
	}
	if !strings.Contains(errOut, "--yes") {
		t.Errorf("stderr should hint at --yes; got %q", errOut)
	}
}

func TestImport_NoPatternsNoAllIsErrorWithHint(t *testing.T) {
	newImportFixture(t)
	_, errOut, err := runImport(t, nil)
	if err == nil {
		t.Fatal("bare import without --all must fail")
	}
	for _, want := range []string{"--all", "db", "ci"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("hint missing %q; got %q", want, errOut)
		}
	}
}

func TestImport_PatternFilter(t *testing.T) {
	_, cfgPath := newImportFixture(t)
	importYes = true

	out, _, err := runImport(t, []string{"db"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ci-8080") {
		t.Errorf("db filter must not import ci; got:\n%s", out)
	}
	if !strings.Contains(out, "imported db-5432") {
		t.Errorf("db forward missing; got:\n%s", out)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tub := range cfg.Tubers {
		if tub.Name == "ci-8080" {
			t.Errorf("ci-8080 must not be in config")
		}
	}
}

func TestImport_UnknownPatternIsErrorWithBlocksHint(t *testing.T) {
	newImportFixture(t)
	_, errOut, err := runImport(t, []string{"nope"})
	if err == nil {
		t.Fatal("unknown pattern must fail")
	}
	if !strings.Contains(errOut, "no forwards found") || !strings.Contains(errOut, "db") {
		t.Errorf("stderr should list candidate blocks; got %q", errOut)
	}
}

func TestImport_AlreadyConfiguredSkipped(t *testing.T) {
	_, cfgPath := newImportFixture(t)
	importYes, importAll = true, true

	if _, _, err := runImport(t, nil); err != nil {
		t.Fatal(err)
	}
	out, _, err := runImport(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nothing new to import") {
		t.Errorf("second import should be a no-op; got:\n%s", out)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, tub := range cfg.Tubers {
		seen[tub.Name]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("tuber %q imported %d times", name, n)
		}
	}
}

func TestImport_FromFlag(t *testing.T) {
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "custom-ssh-config")
	if err := os.WriteFile(sshPath, []byte("Host solo\n  LocalForward 1234 far:1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("HOME", dir)
	prevCfg, prevFrom, prevYes := cfgFile, importFrom, importYes
	cfgFile, importFrom, importYes = cfgPath, sshPath, true
	t.Cleanup(func() { cfgFile, importFrom, importYes = prevCfg, prevFrom, prevYes })

	out, _, err := runImport(t, []string{"solo"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "imported solo-1234") {
		t.Errorf("got:\n%s", out)
	}
}
