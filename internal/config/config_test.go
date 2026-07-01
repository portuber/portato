package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestLoadValidYAML(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id_ed25519
  known_hosts: ~/.ssh/known_hosts
  accept_new_hosts: false
tunnels:
  - name: db-stage
    type: local
    local: 5432
    remote: 10.0.0.5:5432
    ssh: deploy@bastion.example.com:2222
    identity: ~/.ssh/deploy_key
    enabled: true
  - name: admin
    type: local
    local: 127.0.0.1:8080
    remote: web.internal:80
    ssh: web.internal
    enabled: false
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Tunnels) != 2 {
		t.Fatalf("expected 2 tunnels, got %d", len(c.Tunnels))
	}
	db := c.Tunnels[0]
	if db.Name != "db-stage" || db.Type != "local" || db.Enabled != true {
		t.Errorf("db-stage mismatch: %+v", db)
	}
	if db.User != "deploy" || db.Host != "bastion.example.com" || db.Port != 2222 {
		t.Errorf("db-stage ssh parse mismatch: user=%q host=%q port=%d", db.User, db.Host, db.Port)
	}
	if got := db.ListenAddr(); got != "127.0.0.1:5432" {
		t.Errorf("db-stage listen addr = %q, want 127.0.0.1:5432", got)
	}
	ad := c.Tunnels[1]
	if ad.User != "alice" || ad.Host != "web.internal" || ad.Port != 22 {
		t.Errorf("admin ssh parse mismatch: user=%q host=%q port=%d", ad.User, ad.Host, ad.Port)
	}
	if got := ad.ListenAddr(); got != "127.0.0.1:8080" {
		t.Errorf("admin listen addr = %q, want 127.0.0.1:8080", got)
	}
}

func TestApplyDefaults(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/default_key
tunnels:
  - name: t1
    type: local
    local: 9090
    remote: dst:90
    ssh: host.example.com:2020
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t1 := c.Tunnels[0]
	if got := t1.ListenAddr(); got != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:9090", got)
	}
	if t1.User != "alice" || t1.Port != 2020 {
		t.Errorf("defaults: user=%q port=%d, want alice/2020", t1.User, t1.Port)
	}
	if got := t1.ResolvedIdentity(c.Defaults); got != filepath.Join(home(t), ".ssh", "default_key") {
		t.Errorf("resolved identity = %q, want %s", got, filepath.Join(home(t), ".ssh", "default_key"))
	}
	if got := c.Defaults.ResolvedKnownHosts(); got != filepath.Join(home(t), ".ssh", "known_hosts") {
		t.Errorf("resolved known_hosts = %q, want %s", got, filepath.Join(home(t), ".ssh", "known_hosts"))
	}
}

func home(t *testing.T) string {
	t.Helper()
	h, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return h
}

func TestRoundTrip(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	original := &Config{
		Defaults: Defaults{Identity: "~/.ssh/id_ed25519", KnownHosts: "~/.ssh/known_hosts"},
		Tunnels: []Tunnel{
			{Name: "db", Type: "local", Local: "5432", Remote: "10.0.0.5:5432", SSH: "user@host.example.com:22", Enabled: false},
		},
	}
	if err := original.Save(p); err != nil {
		t.Fatalf("save original: %v", err)
	}
	loaded1, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	loaded1.Tunnels[0].Enabled = true
	if err := loaded1.Save(p); err != nil {
		t.Fatalf("save toggled: %v", err)
	}
	loaded2, err := Load(p)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !loaded2.Tunnels[0].Enabled {
		t.Errorf("after round-trip Enabled = false, want true")
	}
	if !reflect.DeepEqual(loaded1, loaded2) {
		t.Errorf("round-trip state mismatch:\n loaded1=%+v\n loaded2=%+v", loaded1, loaded2)
	}
	if loaded2.Tunnels[0].Local != "5432" {
		t.Errorf("local was normalized on save: got %q, want original 5432", loaded2.Tunnels[0].Local)
	}
}

func TestValidateErrors(t *testing.T) {
	t.Setenv("USER", "alice")
	cases := []struct {
		name    string
		cfg     *Config
		wantSub string
	}{
		{
			name:    "duplicate name",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "a", Type: "local", Local: "1", Remote: "r:1", SSH: "h:22"}, {Name: "a", Type: "local", Local: "1", Remote: "r:1", SSH: "h:22"}}},
			wantSub: "duplicate name",
		},
		{
			name:    "empty name",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "", Type: "local", Local: "1", Remote: "r:1", SSH: "h:22"}}},
			wantSub: "name is empty",
		},
		{
			name:    "bad name chars",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "bad name!", Type: "local", Local: "1", Remote: "r:1", SSH: "h:22"}}},
			wantSub: "name must be",
		},
		{
			name:    "unsupported type",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "a", Type: "foo", Local: "1", Remote: "r:1", SSH: "h:22"}}},
			wantSub: "not supported",
		},
		{
			name:    "empty local",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "a", Type: "local", Local: "  ", Remote: "r:1", SSH: "h:22"}}},
			wantSub: "local is empty",
		},
		{
			name:    "empty remote",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "a", Type: "local", Local: "1", Remote: "  ", SSH: "h:22"}}},
			wantSub: "remote is empty",
		},
		{
			name:    "empty ssh host",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "a", Type: "local", Local: "1", Remote: "r:1", SSH: "  "}}},
			wantSub: "ssh host is empty",
		},
		{
			name:    "port out of range",
			cfg:     &Config{Tunnels: []Tunnel{{Name: "a", Type: "local", Local: "1", Remote: "r:1", SSH: "h:0"}}},
			wantSub: "out of range",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.prepare()
			err := tc.cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestParseSSH(t *testing.T) {
	t.Setenv("USER", "alice")
	cases := []struct {
		in       string
		wantUser string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{in: "deploy@host.example.com:2222", wantUser: "deploy", wantHost: "host.example.com", wantPort: 2222},
		{in: "host.example.com:2222", wantUser: "alice", wantHost: "host.example.com", wantPort: 2222},
		{in: "deploy@host.example.com", wantUser: "deploy", wantHost: "host.example.com", wantPort: 22},
		{in: "host.example.com", wantUser: "alice", wantHost: "host.example.com", wantPort: 22},
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			u, h, p, err := parseSSH(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSSH: %v", err)
			}
			if u != tc.wantUser || h != tc.wantHost || p != tc.wantPort {
				t.Errorf("got user=%q host=%q port=%d, want user=%q host=%q port=%d", u, h, p, tc.wantUser, tc.wantHost, tc.wantPort)
			}
		})
	}
}

func TestListenAddr(t *testing.T) {
	cases := []struct {
		local string
		want  string
	}{
		{"5432", "127.0.0.1:5432"},
		{"127.0.0.1:5432", "127.0.0.1:5432"},
		{":8080", "127.0.0.1:8080"},
		{"0.0.0.0:9000", "0.0.0.0:9000"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.local, func(t *testing.T) {
			got := Tunnel{Local: tc.local}.ListenAddr()
			if got != tc.want {
				t.Errorf("ListenAddr(%q) = %q, want %q", tc.local, got, tc.want)
			}
		})
	}
}

func TestRemoteListenAddr(t *testing.T) {
	cases := []struct {
		remote string
		want   string
	}{
		// A bare port or ":port" binds all interfaces via the "*" wildcard
		// (needs GatewayPorts on the server), not loopback.
		{"5432", "*:5432"},
		{":8080", "*:8080"},
		{"*:9000", "*:9000"},
		// Explicit hosts are preserved verbatim.
		{"127.0.0.1:5432", "127.0.0.1:5432"},
		{"0.0.0.0:9000", "0.0.0.0:9000"},
		{"[::]:9090", "[::]:9090"},
		{"185.191.126.49:9090", "185.191.126.49:9090"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.remote, func(t *testing.T) {
			got := Tunnel{Remote: tc.remote}.RemoteListenAddr()
			if got != tc.want {
				t.Errorf("RemoteListenAddr(%q) = %q, want %q", tc.remote, got, tc.want)
			}
		})
	}
}

func TestValidateAcceptsTypes(t *testing.T) {
	cases := []struct {
		name   string
		typ    string
		local  string
		remote string
		ok     bool
	}{
		{"local is valid", "local", "5432", "127.0.0.1:5432", true},
		{"remote is valid", "remote", "5432", "127.0.0.1:5432", true},
		{"empty defaults to local", "", "5432", "127.0.0.1:5432", true},
		{"dynamic is valid", "dynamic", "1080", "127.0.0.1:5432", true},
		{"dynamic without remote is valid", "dynamic", "1080", "", true},
		{"dynamic without local is rejected", "dynamic", "", "127.0.0.1:5432", false},
		{"local without local is rejected", "local", "", "127.0.0.1:5432", false},
		{"remote without local is rejected", "remote", "", "127.0.0.1:5432", false},
		{"local without remote is rejected", "local", "5432", "", false},
		{"remote without remote is rejected", "remote", "5432", "", false},
		{"bogus type", "foo", "5432", "127.0.0.1:5432", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Tunnels: []Tunnel{{Name: "a", Type: tc.typ, Local: tc.local, Remote: tc.remote, SSH: "u@h:22"}}}
			cfg.prepare()
			err := cfg.Validate()
			if tc.ok && err != nil {
				t.Fatalf("expected type %q to validate, got: %v", tc.typ, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected type %q to be rejected", tc.typ)
			}
		})
	}
}

func TestExpandTilde(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	c := &Config{
		Defaults: Defaults{Identity: "~/keys/default", KnownHosts: "~/kh"},
		Tunnels: []Tunnel{
			{Name: "a", Type: "local", Remote: "r:1", SSH: "h:22", Identity: "~/keys/tunnel"},
		},
	}
	c.prepare()
	if got := c.Tunnels[0].ResolvedIdentity(c.Defaults); got != filepath.Join(dir, "keys", "tunnel") {
		t.Errorf("tunnel identity = %q, want %s", got, filepath.Join(dir, "keys", "tunnel"))
	}
	if got := c.Defaults.ResolvedKnownHosts(); got != filepath.Join(dir, "kh") {
		t.Errorf("known_hosts = %q, want %s", got, filepath.Join(dir, "kh"))
	}
}

func TestEnsureExample(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "config.yaml")
	created, err := EnsureExample(p)
	if err != nil {
		t.Fatalf("EnsureExample (create): %v", err)
	}
	if !created {
		t.Errorf("first call: created=false, want true")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("example file not created: %v", err)
	}
	created2, err := EnsureExample(p)
	if err != nil {
		t.Fatalf("EnsureExample (exist): %v", err)
	}
	if created2 {
		t.Errorf("second call: created=true, want false")
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("Load example: %v", err)
	}
}

func TestLoadCreatesExampleWhenMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load on missing path: %v", err)
	}
	if len(c.Tunnels) != 1 {
		t.Fatalf("expected example with 1 tunnel, got %d", len(c.Tunnels))
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("config not created: %v", err)
	}
}

func TestSavePermissions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	cfg := &Config{Tunnels: []Tunnel{{Name: "a", Type: "local", Remote: "r:1", SSH: "h:22"}}}
	if err := cfg.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 600", mode)
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if p == "" {
		t.Errorf("DefaultPath empty")
	}
	if !strings.HasSuffix(p, filepath.Join("portato", "config.yaml")) {
		t.Errorf("DefaultPath = %q, want suffix portato/config.yaml", p)
	}
}

// TestLoadLogRotationDefaults parses a config with the defaults.log.* block
// (Phase 22) and asserts the fields round-trip. An absent block must leave
// the struct at its zero value so the writer falls back to its defaults.
func TestLoadLogRotationDefaults(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id_ed25519
  known_hosts: ~/.ssh/known_hosts
  log:
    max_size_mb: 2
    max_age_days: 14
    retain: 5
tunnels:
  - name: db-stage
    type: local
    local: 5432
    remote: 10.0.0.5:5432
    ssh: deploy@bastion.example.com:2222
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := LogConfig{MaxSizeMB: 2, MaxAgeDays: 14, Retain: 5}
	if !reflect.DeepEqual(c.Defaults.Log, want) {
		t.Fatalf("Defaults.Log = %+v, want %+v", c.Defaults.Log, want)
	}
}

// TestLoadLogRotationDefaultsAbsent ensures a config without a log: block does
// not break loading and leaves the knobs zero (writer defaults apply).
func TestLoadLogRotationDefaultsAbsent(t *testing.T) {
	t.Setenv("USER", "alice")
	dir := t.TempDir()
	p := writeConfigFile(t, dir, "config.yaml", `
defaults:
  identity: ~/.ssh/id_ed25519
tunnels:
  - name: db-stage
    type: local
    local: 5432
    remote: 10.0.0.5:5432
    ssh: deploy@bastion.example.com:2222
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(c.Defaults.Log, LogConfig{}) {
		t.Fatalf("Defaults.Log = %+v, want zero value", c.Defaults.Log)
	}
}
