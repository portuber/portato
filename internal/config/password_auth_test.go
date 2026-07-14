package config

import (
	"testing"
)

// bp is a test helper that returns a pointer to b, for building *bool values.
func bp(b bool) *bool { return &b }

// TestPasswordAuth_Load asserts the per-tuber password_auth and the
// defaults.ssh_password_store flags parse when present. password_auth is a
// *bool so absent can be distinguished from an explicit false (opt-out). No
// password field exists anywhere in the schema.
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
    password_auth: false
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Defaults.PasswordAuth == nil || !*c.Defaults.PasswordAuth {
		t.Error("Defaults.PasswordAuth should be &true when set to true")
	}
	if !c.Defaults.SSHPasswordStore {
		t.Error("Defaults.SSHPasswordStore should be true when set")
	}
	if len(c.Tubers) != 1 || c.Tubers[0].PasswordAuth == nil || *c.Tubers[0].PasswordAuth {
		t.Error("tuber PasswordAuth should be &false when set to false")
	}
}

// TestPasswordAuth_AutoOnByDefault asserts password_auth is nil (→ on-by-
// default) when the field is absent, and ssh_password_store defaults false.
func TestPasswordAuth_AutoOnByDefault(t *testing.T) {
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
	if c.Defaults.PasswordAuth != nil {
		t.Error("Defaults.PasswordAuth should be nil (auto-on) when absent")
	}
	if c.Defaults.SSHPasswordStore {
		t.Error("Defaults.SSHPasswordStore should default to false")
	}
	if c.Tubers[0].PasswordAuth != nil {
		t.Error("tuber PasswordAuth should be nil (auto-on) when absent")
	}
	if !c.Tubers[0].ResolvedPasswordAuth(c.Defaults) {
		t.Error("ResolvedPasswordAuth should be on when neither explicitly disables it")
	}
}

// TestPasswordAuth_RoundTrip asserts an explicit opt-out survives a Save→Load.
func TestPasswordAuth_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id
  password_auth: false
  ssh_password_store: true
tubers:
  - name: t1
    type: local
    local: 9090
    remote: dst:90
    ssh: host:22
    password_auth: false
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
	if c2.Defaults.PasswordAuth == nil || *c2.Defaults.PasswordAuth {
		t.Error("Defaults.PasswordAuth opt-out did not survive Save→Load")
	}
	if !c2.Defaults.SSHPasswordStore {
		t.Error("Defaults.SSHPasswordStore did not survive Save→Load")
	}
	if c2.Tubers[0].PasswordAuth == nil || *c2.Tubers[0].PasswordAuth {
		t.Error("tuber PasswordAuth opt-out did not survive Save→Load")
	}
}

// TestResolvedPasswordAuth covers the opt-out resolution rule: ON by default,
// and a single explicit password_auth: false (tuber or defaults) turns it OFF.
func TestResolvedPasswordAuth(t *testing.T) {
	cases := []struct {
		name   string
		tuber  *bool
		def    *bool
		expect bool
	}{
		{"both nil (auto-on)", nil, nil, true},
		{"tuber true", bp(true), nil, true},
		{"defaults true", nil, bp(true), true},
		{"both true", bp(true), bp(true), true},
		{"tuber false", bp(false), nil, false},
		{"defaults false", nil, bp(false), false},
		{"tuber false overrides defaults true", bp(false), bp(true), false},
		{"defaults false overrides tuber true", bp(true), bp(false), false},
		{"both false", bp(false), bp(false), false},
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

// TestDefaultsEqual_PasswordAuth asserts two parses of the same password_auth
// value compare Equal even though the *bool pointers differ — Reload must not
// spuriously restart every tuber on a no-op reload.
func TestDefaultsEqual_PasswordAuth(t *testing.T) {
	a := Defaults{PasswordAuth: bp(false)}
	b := Defaults{PasswordAuth: bp(false)}
	if a == b {
		t.Fatal("pointers should differ for two bp(false) calls")
	}
	if !a.Equal(b) {
		t.Error("Equal should treat two &false as equal despite different pointers")
	}
	c := Defaults{PasswordAuth: nil}
	if !a.Equal(a) || !c.Equal(Defaults{}) {
		t.Error("Equal should be reflexive for nil/absent")
	}
	if a.Equal(c) {
		t.Error("&false must not equal nil")
	}
}
