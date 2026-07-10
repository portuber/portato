package config

import (
	"testing"
)

// TestIdentityPassphraseStore_Load asserts the identity_passphrase_store flag
// parses when present and defaults to false when absent (opt-in).
func TestIdentityPassphraseStore_Load(t *testing.T) {
	t.Run("explicit true", func(t *testing.T) {
		dir := t.TempDir()
		p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id
  identity_passphrase_store: true
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
		if !c.Defaults.IdentityPassphraseStore {
			t.Error("IdentityPassphraseStore should be true when set")
		}
	})

	t.Run("absent defaults false", func(t *testing.T) {
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
		if c.Defaults.IdentityPassphraseStore {
			t.Error("IdentityPassphraseStore should default to false when absent")
		}
	})
}

// TestIdentityPassphraseStore_RoundTrip asserts the flag survives a Save→Load
// cycle (so toggling it in the TUI editor persists).
func TestIdentityPassphraseStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id
  identity_passphrase_store: true
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
	if err := c.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c2, err := Load(p)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	if !c2.Defaults.IdentityPassphraseStore {
		t.Error("IdentityPassphraseStore did not survive Save→Load")
	}
}
