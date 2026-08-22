package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const updateCheckFixture = `# top-of-file comment
defaults:
  known_hosts: ~/.ssh/known_hosts # inline comment on another key
  ssh_password_store: false
tubers:
  - name: db
    type: local
    local: 5432
    remote: 10.0.0.5:5432
    ssh: user@bastion:22
`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetDefaultsBoolNodeSet(t *testing.T) {
	path := writeFixture(t, updateCheckFixture)
	tt := true
	if err := SetDefaultsBoolNode(path, "update_check", &tt); err != nil {
		t.Fatalf("set true: %v", err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "update_check: true") {
		t.Errorf("config after set:\n%s\nwant update_check: true", s)
	}
	// Comments and other keys survive.
	for _, want := range []string{"# top-of-file comment", "# inline comment on another key", "ssh_password_store: false", "name: db"} {
		if !strings.Contains(s, want) {
			t.Errorf("config lost %q:\n%s", want, s)
		}
	}
	// The Go struct sees it.
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Defaults.UpdateCheck == nil || !*cfg.Defaults.UpdateCheck {
		t.Errorf("Defaults.UpdateCheck = %v, want true", cfg.Defaults.UpdateCheck)
	}
}

func TestSetDefaultsBoolNodeReplaceAndRemove(t *testing.T) {
	path := writeFixture(t, updateCheckFixture)
	tt := true
	if err := SetDefaultsBoolNode(path, "update_check", &tt); err != nil {
		t.Fatal(err)
	}
	ff := false
	if err := SetDefaultsBoolNode(path, "update_check", &ff); err != nil {
		t.Fatalf("set false: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.UpdateCheck == nil || *cfg.Defaults.UpdateCheck {
		t.Errorf("UpdateCheck = %v, want explicit false", cfg.Defaults.UpdateCheck)
	}
	if err := SetDefaultsBoolNode(path, "update_check", nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.UpdateCheck != nil {
		t.Errorf("UpdateCheck = %v after removal, want nil (re-armed ask)", cfg.Defaults.UpdateCheck)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "update_check") {
		t.Errorf("config still mentions update_check after removal:\n%s", data)
	}
}

func TestSetDefaultsBoolNodeNoDefaultsMapping(t *testing.T) {
	path := writeFixture(t, "tubers: []\n")
	tt := true
	if err := SetDefaultsBoolNode(path, "update_check", &tt); err != nil {
		t.Fatalf("create defaults: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.UpdateCheck == nil || !*cfg.Defaults.UpdateCheck {
		t.Errorf("UpdateCheck = %v, want true in created defaults", cfg.Defaults.UpdateCheck)
	}
}

func TestSetDefaultsBoolNodeRemoveAbsentNoop(t *testing.T) {
	path := writeFixture(t, updateCheckFixture)
	if err := SetDefaultsBoolNode(path, "update_check", nil); err != nil {
		t.Fatalf("remove absent: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != updateCheckFixture {
		t.Errorf("remove of absent key changed the file:\n%s", data)
	}
}
