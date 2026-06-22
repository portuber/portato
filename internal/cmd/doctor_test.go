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
