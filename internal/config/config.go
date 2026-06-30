package config

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

const (
	defaultSSHPort = 22
	defaultHost    = "127.0.0.1"
	// remoteWildcard is the host a type=remote tunnel requests on the SSH
	// server when `remote` has no explicit host (a bare port or ":port"). It
	// binds all interfaces (so a reverse forward is publicly reachable by
	// default, the common "expose my local service through the server" case)
	// rather than loopback. An explicit host still wins: `127.0.0.1:port` for
	// loopback-only, `0.0.0.0:port`/`[::]:port` for other wildcards. A non-
	// loopback bind requires `GatewayPorts yes|clientspecified` in sshd_config.
	remoteWildcard = "*"
	configDir      = "portato"
	configFile     = "config.yaml"
)

type Config struct {
	Defaults Defaults `yaml:"defaults" json:"defaults"`
	Tunnels  []Tunnel `yaml:"tunnels" json:"tunnels"`
}

type Defaults struct {
	Identity       string `yaml:"identity" json:"identity"`
	KnownHosts     string `yaml:"known_hosts" json:"known_hosts"`
	AcceptNewHosts bool   `yaml:"accept_new_hosts" json:"accept_new_hosts"`

	// Socks5User/Socks5Password are the default SOCKS5 user/pass authentication
	// for type=dynamic tunnels (Phase 20). A tunnel may override per-name. When
	// both are empty the SOCKS proxy stays open (NoAuth) — preserving the
	// pre-Phase-20 behaviour. Only honoured by type=dynamic tunnels.
	Socks5User     string `yaml:"socks5_user" json:"socks5_user"`
	Socks5Password string `yaml:"socks5_password" json:"socks5_password"`

	// IdentityPassphraseStore (Phase 19) opts in to persisting SSH identity
	// passphrases in the OS keyring so they survive a daemon restart. Default
	// false: nothing is stored without explicit consent. A passphrase is still
	// cached in memory for the process either way (so reconnects don't
	// re-prompt); this flag only gates cross-restart persistence.
	IdentityPassphraseStore bool `yaml:"identity_passphrase_store" json:"identity_passphrase_store"`
}

type Tunnel struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Local    string `yaml:"local" json:"local"`
	Remote   string `yaml:"remote" json:"remote"`
	SSH      string `yaml:"ssh" json:"ssh"`
	Identity string `yaml:"identity" json:"identity"`
	Enabled  bool   `yaml:"enabled" json:"enabled"`

	// Socks5User/Socks5Password override Defaults for type=dynamic tunnels
	// (Phase 20). A tunnel-level pair wins over the defaults pair; an empty
	// tunnel-level value falls back to defaults. NoAuth when both resolve empty.
	Socks5User     string `yaml:"socks5_user" json:"socks5_user"`
	Socks5Password string `yaml:"socks5_password" json:"socks5_password"`

	// User/Host/Port are derived from SSH via prepare() and never persisted.
	// Excluded from JSON so they are not echoed over IPC.
	User string `yaml:"-" json:"-"`
	Host string `yaml:"-" json:"-"`
	Port int    `yaml:"-" json:"-"`
}

func DefaultPath() string {
	return filepath.Join(xdg.ConfigHome, configDir, configFile)
}

func Load(path string) (*Config, error) {
	path = expandTilde(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
		if _, cerr := EnsureExample(path); cerr != nil {
			return nil, fmt.Errorf("create example config: %w", cerr)
		}
		if data, err = os.ReadFile(path); err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	c.prepare()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Validate() error {
	seen := make(map[string]struct{})
	for i := range c.Tunnels {
		t := &c.Tunnels[i]
		if strings.TrimSpace(t.Name) == "" {
			return fmt.Errorf("tunnel #%d: name is empty", i+1)
		}
		if !validName(t.Name) {
			return fmt.Errorf("tunnel %q: name must be alphanumeric, dashes or underscores", t.Name)
		}
		if _, ok := seen[t.Name]; ok {
			return fmt.Errorf("tunnel %q: duplicate name", t.Name)
		}
		seen[t.Name] = struct{}{}
		switch t.Type {
		case "local", "remote", "dynamic":
		default:
			return fmt.Errorf("tunnel %q: type %q not supported (supported: local, remote, dynamic)", t.Name, t.Type)
		}
		// local is required for every type: for local/dynamic it is the listen
		// address (a bare port expands to 127.0.0.1:port); for remote it is the
		// address server-side connections are forwarded to here.
		if strings.TrimSpace(t.Local) == "" {
			return fmt.Errorf("tunnel %q: local is empty", t.Name)
		}
		// remote is the destination dialed on the host (local) or the address
		// listened on the host (remote). A dynamic (-D) tunnel has no remote.
		if t.Type != "dynamic" && strings.TrimSpace(t.Remote) == "" {
			return fmt.Errorf("tunnel %q: remote is empty", t.Name)
		}
		if strings.TrimSpace(t.Host) == "" {
			return fmt.Errorf("tunnel %q: ssh host is empty", t.Name)
		}
		if t.Port < 1 || t.Port > 65535 {
			return fmt.Errorf("tunnel %q: ssh port %d out of range (1-65535)", t.Name, t.Port)
		}
	}
	return nil
}

func (c *Config) Save(p string) error {
	p = expandTilde(p)
	if dir := filepath.Dir(p); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func EnsureExample(p string) (bool, error) {
	p = expandTilde(p)
	if _, err := os.Stat(p); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := exampleConfig().Save(p); err != nil {
		return false, err
	}
	return true, nil
}

func exampleConfig() *Config {
	return &Config{
		Defaults: Defaults{
			Identity:       "~/.ssh/id_ed25519",
			KnownHosts:     "~/.ssh/known_hosts",
			AcceptNewHosts: false,
		},
		Tunnels: []Tunnel{
			{
				Name:    "db-stage",
				Type:    "local",
				Local:   "5432",
				Remote:  "10.0.0.5:5432",
				SSH:     "user@bastion.example.com:22",
				Enabled: false,
			},
		},
	}
}

func (c *Config) prepare() {
	for i := range c.Tunnels {
		t := &c.Tunnels[i]
		if strings.TrimSpace(t.Type) == "" {
			t.Type = "local"
		}
		if u, h, p, err := parseSSH(t.SSH); err == nil {
			t.User, t.Host, t.Port = u, h, p
		}
	}
}

func (t Tunnel) ListenAddr() string {
	return normalizeAddrPort(t.Local, defaultHost)
}

// RemoteListenAddr is the address a type=remote tunnel listens on, on the SSH
// server side. A bare port or ":port" binds all interfaces via remoteWildcard
// ("*:port"); an explicit host (127.0.0.1, 0.0.0.0, [::], a public IP, …) is
// used as written. A non-loopback bind requires GatewayPorts
// yes|clientspecified in sshd_config.
func (t Tunnel) RemoteListenAddr() string {
	return normalizeRemoteAddr(t.Remote)
}

// normalizeRemoteAddr expands a remote bind address. Unlike the local listen
// address, a missing host (bare port or ":port") defaults to remoteWildcard
// rather than loopback, so a reverse forward exposes on all interfaces by
// default. An explicit host is preserved verbatim.
func normalizeRemoteAddr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Bare port, or ":port" (no host) → wildcard.
	if !strings.Contains(s, ":") || strings.HasPrefix(s, ":") {
		return net.JoinHostPort(remoteWildcard, strings.TrimPrefix(s, ":"))
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return s
	}
	if host == "" || host == "*" {
		host = remoteWildcard
	}
	return net.JoinHostPort(host, port)
}

// normalizeAddrPort expands a bare port (or host:port) into a dial/listen
// address, defaulting an empty host to defaultHost.
func normalizeAddrPort(s, defaultH string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, ":") {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			return s
		}
		if host == "" {
			host = defaultH
		}
		return net.JoinHostPort(host, port)
	}
	return net.JoinHostPort(defaultH, s)
}

func (t Tunnel) ResolvedIdentity(d Defaults) string {
	if strings.TrimSpace(t.Identity) != "" {
		return expandTilde(t.Identity)
	}
	if strings.TrimSpace(d.Identity) != "" {
		return expandTilde(d.Identity)
	}
	return ""
}

// ResolvedSocks5User returns the SOCKS5 username a type=dynamic tunnel should
// authenticate with: the tunnel-level value wins, otherwise the defaults value
// (Phase 20). Empty means no auth (a password alone is meaningless — see
// ResolvedSocks5Creds).
func (t Tunnel) ResolvedSocks5User(d Defaults) string {
	if strings.TrimSpace(t.Socks5User) != "" {
		return t.Socks5User
	}
	return d.Socks5User
}

// ResolvedSocks5Password mirrors ResolvedSocks5User for the password half.
func (t Tunnel) ResolvedSocks5Password(d Defaults) string {
	if strings.TrimSpace(t.Socks5Password) != "" {
		return t.Socks5Password
	}
	return d.Socks5Password
}

func (d Defaults) ResolvedKnownHosts() string {
	if strings.TrimSpace(d.KnownHosts) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ".ssh/known_hosts"
		}
		return filepath.Join(home, ".ssh", "known_hosts")
	}
	return expandTilde(d.KnownHosts)
}

func parseSSH(s string) (usr, host string, port int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", 0, fmt.Errorf("ssh is empty")
	}
	hostPart := s
	usr = ""
	if i := strings.LastIndex(s, "@"); i >= 0 {
		usr = s[:i]
		hostPart = s[i+1:]
	}
	host = hostPart
	port = defaultSSHPort
	if i := strings.LastIndex(hostPart, ":"); i >= 0 {
		candidate := hostPart[i+1:]
		if n, perr := strconv.Atoi(candidate); perr == nil {
			host = hostPart[:i]
			port = n
		}
	}
	if usr == "" {
		usr = currentUser()
	}
	return usr, host, port, nil
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r == '-' || r == '_':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func currentUser() string {
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	if v := os.Getenv("LOGNAME"); v != "" {
		return v
	}
	if cur, err := user.Current(); err == nil {
		return cur.Username
	}
	return ""
}

func expandTilde(p string) string {
	if p == "" || !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	rest := p[1:]
	if len(rest) > 0 && (rest[0] == '/' || rest[0] == '\\') {
		return filepath.Join(home, rest[1:])
	}
	return p
}

type tunnelRaw struct {
	Name           string `yaml:"name"`
	Type           string `yaml:"type"`
	Local          any    `yaml:"local"`
	Remote         string `yaml:"remote"`
	SSH            string `yaml:"ssh"`
	Identity       string `yaml:"identity"`
	Enabled        bool   `yaml:"enabled"`
	Socks5User     string `yaml:"socks5_user"`
	Socks5Password string `yaml:"socks5_password"`
}

func (t *Tunnel) UnmarshalYAML(value *yaml.Node) error {
	var raw tunnelRaw
	if err := value.Decode(&raw); err != nil {
		return err
	}
	t.Name = raw.Name
	t.Type = raw.Type
	t.Remote = raw.Remote
	t.SSH = raw.SSH
	t.Identity = raw.Identity
	t.Enabled = raw.Enabled
	t.Socks5User = raw.Socks5User
	t.Socks5Password = raw.Socks5Password
	switch v := raw.Local.(type) {
	case nil:
		t.Local = ""
	case int:
		t.Local = strconv.Itoa(v)
	case int64:
		t.Local = strconv.FormatInt(v, 10)
	case float64:
		t.Local = strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		t.Local = v
	default:
		return fmt.Errorf("tunnel %q: local must be a port number or host:port", raw.Name)
	}
	return nil
}
