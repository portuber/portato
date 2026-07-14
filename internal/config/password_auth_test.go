package config

import (
	"testing"
)

// TestPasswordAuth_Load asserts the per-tuber password_auth and the
// defaults.ssh_password_store flags parse when present and default to false
// when absent (both opt-in). No password field exists anywhere in the schema.
func TestPasswordAuth_Load(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id
  password_auth: true
  ssh_password_store: true
tubers:
  - name: t1
    type: local
    local: 9090
    remote: dst:90
    ssh: host:22
    password_auth: true
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Defaults.PasswordAuth {
		t.Error("Defaults.PasswordAuth should be true when set")
	}
	if !c.Defaults.SSHPasswordStore {
		t.Error("Defaults.SSHPasswordStore should be true when set")
	}
	if len(c.Tubers) != 1 || !c.Tubers[0].PasswordAuth {
		t.Error("tuber PasswordAuth should be true when set")
	}
}

// TestPasswordAuth_DefaultsFalse asserts both flags default to false when the
// block is absent (the invariant: never an unexpected password prompt).
func TestPasswordAuth_DefaultsFalse(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id
tubers:
  - name: t1
    type: local
    local: 9090
    remote: dst:90
    ssh: host:22
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Defaults.PasswordAuth {
		t.Error("Defaults.PasswordAuth should default to false")
	}
	if c.Defaults.SSHPasswordStore {
		t.Error("Defaults.SSHPasswordStore should default to false")
	}
	if c.Tubers[0].PasswordAuth {
		t.Error("tuber PasswordAuth should default to false")
	}
	if c.Tubers[0].ResolvedPasswordAuth(c.Defaults) {
		t.Error("ResolvedPasswordAuth should be false when neither opts in")
	}
}

// TestPasswordAuth_RoundTrip asserts the flags survive a Save→Load cycle (so
// toggling them in the TUI editor persists).
func TestPasswordAuth_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id
  password_auth: true
  ssh_password_store: true
tubers:
  - name: t1
    type: local
    local: 9090
    remote: dst:90
    ssh: host:22
    password_auth: true
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := c.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c2, err := Load(p)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	if !c2.Defaults.PasswordAuth || !c2.Defaults.SSHPasswordStore {
		t.Error("defaults flags did not survive Save→Load")
	}
	if !c2.Tubers[0].PasswordAuth {
		t.Error("tuber PasswordAuth did not survive Save→Load")
	}
}

// TestResolvedPasswordAuth covers the resolution rule: enabled if either the
// tuber or the defaults opt in (global or per-tuber opt-in).
func TestResolvedPasswordAuth(t *testing.T) {
	cases := []struct {
		name   string
		tuber  bool
		def    bool
		expect bool
	}{
		{"both false", false, false, false},
		{"tuber only", true, false, true},
		{"defaults only", false, true, true},
		{"both true", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tt := Tuber{PasswordAuth: tc.tuber}
			d := Defaults{PasswordAuth: tc.def}
			if got := tt.ResolvedPasswordAuth(d); got != tc.expect {
				t.Fatalf("ResolvedPasswordAuth = %v, want %v", got, tc.expect)
			}
		})
	}
}
