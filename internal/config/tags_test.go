package config

import (
	"strings"
	"testing"
)

// TestValidateTags covers the Phase 46 load-time checks: each tag is non-empty,
// validName, ≤maxTagLen, ≤maxTagsPerTuber, and dedup is case-sensitive (so
// "Db" and "db" are distinct, matching shell completion / exact #tag filter).
func TestValidateTags(t *testing.T) {
	cases := []struct {
		name    string
		tags    []string
		wantErr string
	}{
		{name: "nil ok", tags: nil},
		{name: "empty ok", tags: []string{}},
		{name: "single ok", tags: []string{"prod"}},
		{name: "many ok", tags: []string{"prod", "db", "staging"}},
		{name: "dashes underscores ok", tags: []string{"us-east-1", "team_a"}},
		{name: "case-sensitive dedup ok", tags: []string{"Db", "db"}},
		{name: "empty tag rejected", tags: []string{"prod", ""}, wantErr: "empty"},
		{name: "whitespace tag rejected", tags: []string{"  "}, wantErr: "empty"},
		{name: "bad char rejected", tags: []string{"prod", "db host"}, wantErr: "alphanumeric"},
		{name: "dot rejected", tags: []string{"v1.2"}, wantErr: "alphanumeric"},
		{name: "too long rejected", tags: []string{strings.Repeat("a", maxTagLen+1)}, wantErr: "too long"},
		{name: "max len ok", tags: []string{strings.Repeat("a", maxTagLen)}},
		{name: "duplicate rejected", tags: []string{"prod", "prod"}, wantErr: "duplicate"},
		{name: "too many rejected", tags: makeN("t", maxTagsPerTuber+1), wantErr: "too many"},
		{name: "max count ok", tags: makeN("t", maxTagsPerTuber)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTags("tuber", tc.tags)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("validateTags(%v) unexpected error: %v", tc.tags, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateTags(%v) expected error containing %q, got nil", tc.tags, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("validateTags(%v) error = %q, want substring %q", tc.tags, err.Error(), tc.wantErr)
			}
		})
	}
}

func makeN(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + string(rune('a'+(i%26))) + string(rune('0'+(i%10)))
	}
	return out
}

// TestLoadWithTags confirms the tags round-trip through YAML load. The Tuber
// has a custom UnmarshalYAML that decodes into tuberRaw and copies fields
// explicitly — a field added to Tuber but NOT to tuberRaw (or not copied)
// would be silently dropped on load (the Phase 43 jump pitfall).
func TestLoadWithTags(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  known_hosts: ~/.ssh/known_hosts
tubers:
  - name: db-prod
    type: local
    local: 5432
    remote: 10.0.0.5:5432
    ssh: user@host:22
    tags: [prod, db]
  - name: web
    type: local
    local: 8080
    remote: web:80
    ssh: user@host:22
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Tubers) != 2 {
		t.Fatalf("expected 2 tubers, got %d", len(c.Tubers))
	}
	if got := c.Tubers[0].Tags; len(got) != 2 || got[0] != "prod" || got[1] != "db" {
		t.Errorf("db-prod Tags = %v, want [prod db]", got)
	}
	if got := c.Tubers[1].Tags; len(got) != 0 {
		t.Errorf("web Tags = %v, want empty", got)
	}
}

// TestLoadWithBadTags confirms an invalid tag fails load loudly (Validate runs
// after the UnmarshalYAML copy).
func TestLoadWithBadTags(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  known_hosts: ~/.ssh/known_hosts
tubers:
  - name: bad
    type: local
    local: 5432
    remote: 10.0.0.5:5432
    ssh: user@host:22
    tags: ["prod db"]
`)
	if _, err := Load(p); err == nil {
		t.Fatal("Load expected error for invalid tag, got nil")
	}
}
