package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runDoctor(t *testing.T, cfgPath string) (string, error) {
	t.Helper()
	saved := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = saved })
	// Keep the agent/daemon checks deterministic: an unset agent is only an
	// informational line, never a failure.
	t.Setenv("SSH_AUTH_SOCK", "")

	var buf bytes.Buffer
	cmd := doctorCmd
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := doctorRunE(cmd, nil)
	return buf.String(), err
}

func TestDoctor_PassesOnHealthyConfig(t *testing.T) {
	dir := t.TempDir()
	id := filepath.Join(dir, "id_test")
	if err := os.WriteFile(id, []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "defaults:\n  identity: " + id + "\ntunnels:\n  - name: t1\n    type: local\n    local: \"19995\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should pass on a healthy config, got err=%v\n%s", err, out)
	}
	for _, want := range []string{"✓ config", "✓ identity"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, "✗") {
		t.Errorf("no failures expected on a healthy config\ngot:\n%s", out)
	}
}

func TestDoctor_FailsOnMissingIdentity(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "defaults:\n  identity: " + filepath.Join(dir, "nope") + "\ntunnels:\n  - name: t1\n    type: local\n    local: \"19996\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runDoctor(t, cfgPath)
	if err == nil {
		t.Fatalf("doctor should fail when an identity is missing\ngot:\n%s", out)
	}
	if !strings.Contains(out, "✗ identity") {
		t.Errorf("output should flag the missing identity\ngot:\n%s", out)
	}
}

func TestDoctor_FailsOnBadConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("tunnels:\n  - name: x\n    type: bogus\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runDoctor(t, cfgPath)
	if err == nil {
		t.Fatalf("doctor should fail on an invalid config\ngot:\n%s", out)
	}
	if !strings.Contains(out, "✗ config") {
		t.Errorf("output should flag the config failure\ngot:\n%s", out)
	}
}

// TestDoctor_ReportsLogPathAndRotation verifies the Phase 13 `logs` check:
// with a daemon log file and one archive, doctor prints the path and the
// archive's mtime as the last rotation.
func TestDoctor_ReportsLogPathAndRotation(t *testing.T) {
	dir := t.TempDir()
	daemonLog := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(daemonLog, []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemonLog+".1", []byte("archive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withLogPaths(t, daemonLog)

	cfgPath := filepath.Join(dir, "config.yaml")
	body := "tunnels:\n  - name: t1\n    type: local\n    local: \"19997\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ logs") {
		t.Errorf("output missing the logs check\ngot:\n%s", out)
	}
	if !strings.Contains(out, "last rotated") {
		t.Errorf("output should report the last rotation\ngot:\n%s", out)
	}
	if !strings.Contains(out, "daemon.log") {
		t.Errorf("output should name the log path\ngot:\n%s", out)
	}
}

// TestDoctor_ReportsNoRotationYet covers the fresh-install path: the daemon
// log exists but no archive has been produced yet.
func TestDoctor_ReportsNoRotationYet(t *testing.T) {
	dir := t.TempDir()
	daemonLog := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(daemonLog, []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withLogPaths(t, daemonLog)

	cfgPath := filepath.Join(dir, "config.yaml")
	body := "tunnels:\n  - name: t1\n    type: local\n    local: \"19998\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "no rotation yet") {
		t.Errorf("output should report no rotation yet\ngot:\n%s", out)
	}
}

// withLogPaths points doctor's log check at the given temp paths for the test.
func withLogPaths(t *testing.T, paths ...string) {
	t.Helper()
	saved := logStatePaths
	logStatePaths = func() []string { return paths }
	t.Cleanup(func() { logStatePaths = saved })
}

// writeMinimalConfig writes a minimal valid config into dir and returns its path.
func writeMinimalConfig(t *testing.T, dir string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "tunnels:\n  - name: t1\n    type: local\n    local: \"20001\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func withLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	saved := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = saved })
}

func withAutostart(t *testing.T, path string) {
	t.Helper()
	saved := autostartArtefact
	autostartArtefact = func() string { return path }
	t.Cleanup(func() { autostartArtefact = saved })
}

// TestDoctor_PrintsVersion verifies the Phase 21 version header: doctor prints
// the embedded version/commit/date before the checks.
func TestDoctor_PrintsVersion(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeMinimalConfig(t, dir)
	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "portato dev (commit") {
		t.Errorf("output should print the version header\ngot:\n%s", out)
	}
}

// TestDoctor_ConfigDirWritable verifies the Phase 21 config-dir writability
// check: a temp config dir is reported writable.
func TestDoctor_ConfigDirWritable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeMinimalConfig(t, dir)
	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ config dir") {
		t.Errorf("output should report a writable config dir\ngot:\n%s", out)
	}
}

// TestDoctor_BinaryOnPath covers the binary-on-PATH check (Phase 21) when
// portato is resolvable.
func TestDoctor_BinaryOnPath(t *testing.T) {
	withLookPath(t, func(string) (string, error) { return "/usr/local/bin/portato", nil })
	dir := t.TempDir()
	cfgPath := writeMinimalConfig(t, dir)
	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ binary") {
		t.Errorf("output should report binary on PATH\ngot:\n%s", out)
	}
}

// TestDoctor_BinaryNotOnPath: a missing PATH entry is informational, not a failure.
func TestDoctor_BinaryNotOnPath(t *testing.T) {
	withLookPath(t, func(string) (string, error) { return "", os.ErrNotExist })
	dir := t.TempDir()
	cfgPath := writeMinimalConfig(t, dir)
	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should still pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "· binary") {
		t.Errorf("output should report binary as info\ngot:\n%s", out)
	}
}

// TestDoctor_AutostartInstalled: an existing autostart definition is a pass.
func TestDoctor_AutostartInstalled(t *testing.T) {
	artefact := filepath.Join(t.TempDir(), "portato.service")
	if err := os.WriteFile(artefact, []byte("unit"), 0o600); err != nil {
		t.Fatal(err)
	}
	withAutostart(t, artefact)
	dir := t.TempDir()
	cfgPath := writeMinimalConfig(t, dir)
	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ autostart") {
		t.Errorf("output should report autostart installed\ngot:\n%s", out)
	}
}

// TestDoctor_AutostartMissing: no autostart definition is informational.
func TestDoctor_AutostartMissing(t *testing.T) {
	withAutostart(t, filepath.Join(t.TempDir(), "absent"))
	dir := t.TempDir()
	cfgPath := writeMinimalConfig(t, dir)
	out, err := runDoctor(t, cfgPath)
	if err != nil {
		t.Fatalf("doctor should still pass, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "· autostart") {
		t.Errorf("output should report autostart as info\ngot:\n%s", out)
	}
}
