package config

import (
	"path/filepath"
	"testing"
)

// TestLoadSocks5Creds verifies Phase 20 SOCKS5 user/pass fields parse from YAML
// at both the tunnel and defaults level (via the custom tunnelRaw unmarshal).
func TestLoadSocks5Creds(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id_ed25519
  known_hosts: ~/.ssh/known_hosts
  socks5_user: defaultuser
  socks5_password: defaultpass
tunnels:
  - name: override
    type: dynamic
    local: 1080
    ssh: host.example.com
    socks5_user: tunneluser
    socks5_password: tunnelpass
  - name: inherit
    type: dynamic
    local: 1081
    ssh: host.example.com
  - name: local-tunnel
    type: local
    local: 5432
    remote: db:5432
    ssh: host.example.com
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Defaults.Socks5User != "defaultuser" || c.Defaults.Socks5Password != "defaultpass" {
		t.Errorf("defaults socks5 = %q/%q, want defaultuser/defaultpass",
			c.Defaults.Socks5User, c.Defaults.Socks5Password)
	}
	if len(c.Tunnels) != 3 {
		t.Fatalf("expected 3 tunnels, got %d", len(c.Tunnels))
	}
	ov := c.Tunnels[0]
	if ov.Socks5User != "tunneluser" || ov.Socks5Password != "tunnelpass" {
		t.Errorf("override tunnel socks5 = %q/%q, want tunneluser/tunnelpass",
			ov.Socks5User, ov.Socks5Password)
	}
	in := c.Tunnels[1]
	if in.Socks5User != "" || in.Socks5Password != "" {
		t.Errorf("inherit tunnel socks5 = %q/%q, want empty (inherits at resolve time)",
			in.Socks5User, in.Socks5Password)
	}
}

// TestResolvedSocks5Creds covers the tunnel-wins-over-defaults resolution,
// including the empty-falls-back-to-default and both-empty (NoAuth) cases.
func TestResolvedSocks5Creds(t *testing.T) {
	def := Defaults{Socks5User: "du", Socks5Password: "dp"}
	cases := []struct {
		name        string
		tunUser     string
		tunPass     string
		wantUser    string
		wantPass    string
		wantNoCreds bool
	}{
		{"tunnel wins", "tu", "tp", "tu", "tp", false},
		{"defaults fallback", "", "", "du", "dp", false},
		{"user only falls back to defaults pass", "tu", "", "tu", "dp", false},
		{"both empty = no auth", "", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tun := Tunnel{Socks5User: tc.tunUser, Socks5Password: tc.tunPass}
			if tc.name == "both empty = no auth" {
				def = Defaults{} // exercise the truly-empty path
			} else {
				def = Defaults{Socks5User: "du", Socks5Password: "dp"}
			}
			gotUser := tun.ResolvedSocks5User(def)
			gotPass := tun.ResolvedSocks5Password(def)
			if gotUser != tc.wantUser {
				t.Errorf("user = %q, want %q", gotUser, tc.wantUser)
			}
			if gotPass != tc.wantPass {
				t.Errorf("pass = %q, want %q", gotPass, tc.wantPass)
			}
			if tc.wantNoCreds && (gotUser != "" || gotPass != "") {
				t.Errorf("expected no creds, got %q/%q", gotUser, gotPass)
			}
		})
	}
}

// TestSocks5CredsRoundTripThroughFile ensures Save→Load preserves the socks5
// fields (the custom UnmarshalYAML must keep them).
func TestSocks5CredsRoundTripThroughFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	orig := &Config{
		Defaults: Defaults{Socks5User: "u", Socks5Password: "p"},
		Tunnels: []Tunnel{{
			Name: "d", Type: "dynamic", Local: "1080", SSH: "h.example.com",
			Socks5User: "tu", Socks5Password: "tp",
		}},
	}
	if err := orig.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Defaults.Socks5User != "u" || c.Defaults.Socks5Password != "p" {
		t.Errorf("defaults round-trip: %q/%q", c.Defaults.Socks5User, c.Defaults.Socks5Password)
	}
	if c.Tunnels[0].Socks5User != "tu" || c.Tunnels[0].Socks5Password != "tp" {
		t.Errorf("tunnel round-trip: %q/%q", c.Tunnels[0].Socks5User, c.Tunnels[0].Socks5Password)
	}
}
