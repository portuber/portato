package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestParseJumpChain covers the Phase 43 prepare() path, whose load-critical
// guard is the empty-string case: strings.Split("", ",") returns [""] (one
// empty element), so without the TrimSpace guard every tuber — even one with
// no jump — would hit parseSSH("") and fail to load. The guard must yield nil
// for an empty/blank jump and never regress the single-hop tuber.
func TestParseJumpChain(t *testing.T) {
	t.Setenv("USER", "alice") // a hop with no user@ inherits the current user, like ssh:
	cases := []struct {
		name string
		jump string
		want []Hop
	}{
		{name: "empty", jump: "", want: nil},
		{name: "whitespace only", jump: "   ", want: nil},
		{name: "single hop defaults", jump: "bastion.example.com", want: []Hop{{User: "alice", Host: "bastion.example.com", Port: 22}}},
		{name: "single hop user port", jump: "u@bastion:2222", want: []Hop{{User: "u", Host: "bastion", Port: 2222}}},
		{name: "chain", jump: "u@edge,bastion:2200,deploy@core", want: []Hop{
			{User: "u", Host: "edge", Port: 22},
			{User: "alice", Host: "bastion", Port: 2200},
			{User: "deploy", Host: "core", Port: 22},
		}},
		{name: "whitespace around tokens trimmed", jump: " u@edge , bastion ", want: []Hop{
			{User: "u", Host: "edge", Port: 22},
			{User: "alice", Host: "bastion", Port: 22},
		}},
		{name: "double comma tokens skipped leniently", jump: "a,,b", want: []Hop{
			{User: "alice", Host: "a", Port: 22},
			{User: "alice", Host: "b", Port: 22},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseJumpChain(tc.jump)
			if !equalHops(got, tc.want) {
				t.Errorf("parseJumpChain(%q) = %+v, want %+v", tc.jump, got, tc.want)
			}
		})
	}
}

func equalHops(a, b []Hop) bool {
	if len(a) != len(b) {
		return false
	}
	// nil and an empty slice must compare unequal here only by length is
	// ambiguous, but the cases above distinguish nil ("") from populated, so a
	// length+element check suffices.
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestValidateJump covers the strict load-time checks (Phase 43). prepare()
// skips empty tokens leniently for the dial; Validate must reject them loudly
// so a typo like "a,,b" fails config load instead of silently dropping a hop.
func TestValidateJump(t *testing.T) {
	cases := []struct {
		name    string
		jump    string
		wantErr string
	}{
		{name: "empty ok", jump: ""},
		{name: "whitespace ok", jump: "  "},
		{name: "single ok", jump: "bastion.example.com"},
		{name: "user port ok", jump: "u@bastion:2222"},
		{name: "chain ok", jump: "u@edge,bastion:2200,deploy@core"},
		{name: "double comma rejected", jump: "a,,b", wantErr: "empty"},
		{name: "missing host rejected", jump: "u@", wantErr: "host is empty"},
		{name: "port out of range rejected", jump: "h:99999", wantErr: "out of range"},
		{name: "trailing comma rejected", jump: "a,b,", wantErr: "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJump("t", tc.jump)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("validateJump(%q) unexpected error: %v", tc.jump, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateJump(%q) expected error containing %q, got nil", tc.jump, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("validateJump(%q) error = %q, want substring %q", tc.jump, err.Error(), tc.wantErr)
			}
		})
	}
}

// TestLoadWithJump confirms a jump chain round-trips through YAML load: the
// field is parsed into Jumps and the tuber still loads (the UnmarshalYAML copy
// and prepare() are wired).
func TestLoadWithJump(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  known_hosts: ~/.ssh/known_hosts
tubers:
  - name: db-vpn
    type: local
    local: 5433
    remote: 10.0.0.5:5432
    ssh: deploy@10.0.0.5:22
    jump: user@edge,bastion:2200
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Tubers) != 1 {
		t.Fatalf("expected 1 tuber, got %d", len(c.Tubers))
	}
	tub := c.Tubers[0]
	if tub.Jump != "user@edge,bastion:2200" {
		t.Errorf("Jump = %q", tub.Jump)
	}
	want := []Hop{{User: "user", Host: "edge", Port: 22}, {User: "alice", Host: "bastion", Port: 2200}}
	if !equalHops(tub.Jumps, want) {
		t.Errorf("Jumps = %+v, want %+v", tub.Jumps, want)
	}
}

// TestLoadRejectsMalformedJump confirms a malformed jump fails config load
// (Validate runs at load time), so a bad value never reaches the dial.
func TestLoadRejectsMalformedJump(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults: {}
tubers:
  - name: bad
    type: local
    local: 5433
    remote: 10.0.0.5:5432
    ssh: deploy@10.0.0.5:22
    jump: edge,,core
`)
	if _, err := Load(p); err == nil {
		t.Fatal("Load expected to reject a jump with an empty hop, got nil")
	}
}

// TestNoJumpDoesNotBreakLoad is the direct regression guard for the empty-Split
// trap: a tuber with no jump (and a defaults with no jump) must load exactly as
// before, with Jumps == nil.
func TestNoJumpDoesNotBreakLoad(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  known_hosts: `+filepath.Join(t.TempDir(), "kh")+`
tubers:
  - name: plain
    type: local
    local: 5432
    remote: 10.0.0.5:5432
    ssh: user@host.example.com:22
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Tubers[0].Jumps != nil {
		t.Errorf("Jumps = %+v, want nil for a no-jump tuber", c.Tubers[0].Jumps)
	}
	if c.Tubers[0].Jump != "" {
		t.Errorf("Jump = %q, want empty", c.Tubers[0].Jump)
	}
}
