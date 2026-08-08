package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	ssh_config "github.com/kevinburke/ssh_config"
)

// decodeSSH builds an in-memory ssh config (Phase 44) so the resolution tests
// are deterministic — they never touch the developer's real ~/.ssh/config.
func decodeSSH(t *testing.T, text string) *ssh_config.Config {
	t.Helper()
	cfg, err := ssh_config.Decode(strings.NewReader(text))
	if err != nil {
		t.Fatalf("decode ssh config: %v", err)
	}
	return cfg
}

// nilSSH is a loader that reports "no ssh config", exercising the unchanged /
// literal path.
func nilSSH() func() (*ssh_config.Config, error) {
	return func() (*ssh_config.Config, error) { return nil, nil }
}

// withSSH returns a loader that yields the given in-memory ssh config.
func withSSH(cfg *ssh_config.Config) func() (*ssh_config.Config, error) {
	return func() (*ssh_config.Config, error) { return cfg, nil }
}

func TestParseSSHExplicit(t *testing.T) {
	cases := []struct {
		in               string
		user, host       string
		port             int
		userExp, portExp bool
	}{
		{"alias", "current", "alias", 22, false, false},
		{"me@alias", "me", "alias", 22, true, false},
		{"alias:2222", "current", "alias", 2222, false, true},
		{"me@alias:2222", "me", "alias", 2222, true, true},
	}
	for _, tc := range cases {
		u, h, p, ue, pe, err := parseSSHExplicit(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if tc.user != "current" && u != tc.user {
			t.Errorf("%q: user = %q, want %q", tc.in, u, tc.user)
		}
		if h != tc.host {
			t.Errorf("%q: host = %q, want %q", tc.in, h, tc.host)
		}
		if p != tc.port {
			t.Errorf("%q: port = %d, want %d", tc.in, p, tc.port)
		}
		if ue != tc.userExp {
			t.Errorf("%q: userExplicit = %v, want %v", tc.in, ue, tc.userExp)
		}
		if pe != tc.portExp {
			t.Errorf("%q: portExplicit = %v, want %v", tc.in, pe, tc.portExp)
		}
	}
}

func TestSSHAliasResolution(t *testing.T) {
	cfg := decodeSSH(t, `
Host myalias
  HostName 10.0.0.5
  User deploy
  Port 2200
  IdentityFile ~/.ssh/deploy_key
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "8080", Remote: "127.0.0.1:80", SSH: "myalias"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tr := c.Tubers[0]
	if tr.Host != "10.0.0.5" {
		t.Errorf("Host = %q, want 10.0.0.5", tr.Host)
	}
	if tr.User != "deploy" {
		t.Errorf("User = %q, want deploy", tr.User)
	}
	if tr.Port != 2200 {
		t.Errorf("Port = %d, want 2200", tr.Port)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".ssh", "deploy_key")
	if tr.SSHIdentity != want {
		t.Errorf("SSHIdentity = %q, want %q", tr.SSHIdentity, want)
	}
	if got := tr.ResolvedIdentity(c.Defaults); got != want {
		t.Errorf("ResolvedIdentity = %q, want %q", got, want)
	}
}

func TestSSHConfigProxyJump(t *testing.T) {
	cfg := decodeSSH(t, `
Host target
  HostName target.example.com
  ProxyJump bastion

Host bastion
  HostName bastion.example.com
  User buser
  Port 2222
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "8080", Remote: "127.0.0.1:80", SSH: "target"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tr := c.Tubers[0]
	// The derived chain populates Jumps only; Jump stays empty so Save never
	// writes it back as user content (the round-trip pitfall).
	if tr.Jump != "" {
		t.Errorf("Jump = %q, want empty (derived chain must not leak to the persisted field)", tr.Jump)
	}
	if len(tr.Jumps) != 1 {
		t.Fatalf("Jumps = %v, want 1 hop", tr.Jumps)
	}
	j := tr.Jumps[0]
	if j.User != "buser" || j.Host != "bastion.example.com" || j.Port != 2222 {
		t.Errorf("resolved hop = %+v, want buser@bastion.example.com:2222", j)
	}
}

func TestSSHConfigPrecedenceUserPort(t *testing.T) {
	cfg := decodeSSH(t, `
Host alias
  HostName alias.example.com
  User cfguser
  Port 2222
`)
	// Explicit ssh: user/port override the alias's User/Port; HostName applies.
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "me@alias:9999"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tr := c.Tubers[0]
	if tr.User != "me" || tr.Port != 9999 || tr.Host != "alias.example.com" {
		t.Errorf("resolved = %s@%s:%d, want me@alias.example.com:9999", tr.User, tr.Host, tr.Port)
	}
}

func TestSSHConfigPrecedenceIdentity(t *testing.T) {
	cfg := decodeSSH(t, `
Host alias
  HostName alias.example.com
  IdentityFile ~/.ssh/from_config
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "alias", Identity: "~/keys/explicit"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tr := c.Tubers[0]
	if tr.SSHIdentity != "" {
		t.Errorf("SSHIdentity = %q, want empty (explicit identity must suppress ssh-config IdentityFile)", tr.SSHIdentity)
	}
	home, _ := os.UserHomeDir()
	if got := tr.ResolvedIdentity(c.Defaults); got != filepath.Join(home, "keys", "explicit") {
		t.Errorf("ResolvedIdentity = %q, want the explicit tuber identity", got)
	}
}

func TestSSHConfigPrecedenceJump(t *testing.T) {
	cfg := decodeSSH(t, `
Host alias
  HostName alias.example.com
  ProxyJump from_config
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "alias", Jump: "user@explicit:2022"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tr := c.Tubers[0]
	if len(tr.Jumps) != 1 || tr.Jumps[0].Host != "explicit" || tr.Jumps[0].Port != 2022 {
		t.Errorf("Jumps = %+v, want the explicit jump user@explicit:2022", tr.Jumps)
	}
}

func TestSSHConfigNoMatchIsLiteral(t *testing.T) {
	cfg := decodeSSH(t, `
Host other
  HostName other.example.com
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "literal-host:2022"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tr := c.Tubers[0]
	if tr.Host != "literal-host" || tr.Port != 2022 {
		t.Errorf("resolved = %s:%d, want literal literal-host:2022", tr.Host, tr.Port)
	}
	if tr.SSHIdentity != "" {
		t.Errorf("SSHIdentity = %q, want empty for a no-match literal", tr.SSHIdentity)
	}
}

func TestSSHConfigAbsentGraceful(t *testing.T) {
	// No ssh config at all ⇒ behaviour unchanged (literal user@host:port).
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "u@h:2222"}}}
	if err := c.prepareWith(nilSSH()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tr := c.Tubers[0]
	if tr.User != "u" || tr.Host != "h" || tr.Port != 2222 {
		t.Errorf("resolved = %s@%s:%d, want u@h:2222", tr.User, tr.Host, tr.Port)
	}
}

func TestSSHConfigIdentityTokenExpansion(t *testing.T) {
	cfg := decodeSSH(t, `
Host alias
  HostName alias.example.com
  User deploy
  IdentityFile %d/.ssh/id_%h
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "alias"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".ssh", "id_alias.example.com")
	if got := c.Tubers[0].SSHIdentity; got != want {
		t.Errorf("SSHIdentity = %q, want %q", got, want)
	}
}

func TestSSHConfigProxyJumpCycle(t *testing.T) {
	// The same alias hop twice ⇒ the visited-set flags a cycle.
	cfg := decodeSSH(t, `
Host bastion
  HostName bastion.example.com
Host target
  HostName target.example.com
  ProxyJump bastion,bastion
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "target"}}}
	if err := c.prepareWith(withSSH(cfg)); err == nil {
		t.Fatalf("prepare: expected a cycle error, got nil")
	}
}

func TestSSHConfigDerivedChainNotPersisted(t *testing.T) {
	cfg := decodeSSH(t, `
Host target
  HostName target.example.com
  ProxyJump bastion

Host bastion
  HostName bastion.example.com
`)
	c := &Config{Tubers: []Tuber{{Name: "t", Type: "local", Local: "1", Remote: "127.0.0.1:1", SSH: "target"}}}
	if err := c.prepareWith(withSSH(cfg)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "jump:") {
		t.Errorf("derived ProxyJump leaked into the saved config:\n%s", string(data))
	}
}

func TestSSHConfigUnreadableIsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file perms are not enforced the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file perms")
	}
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sshDir, "config")
	if err := os.WriteFile(p, []byte("Host x\n  User y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })

	prev := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	t.Cleanup(func() { os.Setenv("HOME", prev) })

	if _, err := loadUserSSHConfig(); err == nil {
		t.Fatalf("expected an unreadable-config error, got nil")
	}
}
