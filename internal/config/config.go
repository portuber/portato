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
	ssh_config "github.com/kevinburke/ssh_config"
	"gopkg.in/yaml.v3"
)

const (
	defaultSSHPort = 22
	defaultHost    = "127.0.0.1"
	// remoteWildcard is the host a type=remote tuber requests on the SSH
	// server when `remote` has no explicit host (a bare port or ":port"). It
	// binds all interfaces (so a reverse forward is publicly reachable by
	// default, the common "expose my local service through the server" case)
	// rather than loopback. An explicit host still wins: `127.0.0.1:port` for
	// loopback-only, `0.0.0.0:port`/`[::]:port` for other wildcards. A non-
	// loopback bind requires `GatewayPorts yes|clientspecified` in sshd_config.
	remoteWildcard = "*"
	configDir      = "portato"
	configFile     = "config.yaml"
	// maxJumpDepth bounds the recursion of a ssh-config ProxyJump chain
	// (Phase 44). OpenSSH's -J is flat, so this is purely defensive against a
	// pathological self-referential Host block.
	maxJumpDepth = 8
)

type Config struct {
	Defaults Defaults `yaml:"defaults" json:"defaults"`
	Tubers   []Tuber  `yaml:"tubers" json:"tubers"`
}

type Defaults struct {
	Identity       string `yaml:"identity" json:"identity"`
	KnownHosts     string `yaml:"known_hosts" json:"known_hosts"`
	AcceptNewHosts bool   `yaml:"accept_new_hosts" json:"accept_new_hosts"`

	// Socks5User/Socks5Password are the default SOCKS5 user/pass authentication
	// for type=dynamic tubers (Phase 20). A tuber may override per-name. When
	// both are empty the SOCKS proxy stays open (NoAuth) — preserving the
	// pre-Phase-20 behaviour. Only honoured by type=dynamic tubers.
	Socks5User     string `yaml:"socks5_user" json:"socks5_user"`
	Socks5Password string `yaml:"socks5_password" json:"socks5_password"`

	// IdentityPassphraseStore (Phase 19) opts in to persisting SSH identity
	// passphrases in the OS keyring so they survive a daemon restart. Default
	// false: nothing is stored without explicit consent. A passphrase is still
	// cached in memory for the process either way (so reconnects don't
	// re-prompt); this flag only gates cross-restart persistence.
	IdentityPassphraseStore bool `yaml:"identity_passphrase_store" json:"identity_passphrase_store"`

	// PasswordAuth (Phase 35) controls the SSH password-auth fallback. A tunnel
	// whose keys (agent → identity) don't authenticate falls back to an
	// interactive password prompt (OpenSSH-style) — ON BY DEFAULT. Set
	// password_auth: false to opt a tunnel (or, here in defaults, every tunnel)
	// OUT — e.g. a deployment that only ever uses keys and never wants a prompt
	// (including avoiding a premature prompt while an agent finishes starting at
	// boot). nil (absent) and true both mean "on". The password itself is NEVER
	// stored in config — it is supplied interactively and, when SSHPasswordStore
	// is set, held in the OS keyring. A *bool distinguishes absent (on) from an
	// explicit false (off).
	PasswordAuth *bool `yaml:"password_auth,omitempty" json:"password_auth,omitempty"`

	// SSHPasswordStore (Phase 35) opts in to persisting SSH passwords in the OS
	// keyring (per account, "password:<user>@<host>:<port>") so they survive a
	// daemon restart. Default false: nothing is stored without explicit consent.
	// A password is still cached in memory for the process either way (so
	// reconnects don't re-prompt); this flag only gates cross-restart
	// persistence, mirroring IdentityPassphraseStore.
	SSHPasswordStore bool `yaml:"ssh_password_store" json:"ssh_password_store"`

	// Log configures the persistent file's size-capped rotation (Phase 22). The
	// fields are hints: a zero/negative value falls back to the rotating
	// writer's package defaults at setup time, so an absent `log:` block keeps
	// the pre-Phase-22 behaviour. MaxAgeDays bounds retention by age (archives
	// older than N days are purged at rotation); it is NOT a time-based rotation
	// trigger — rotation stays size-driven (see internal/log).
	Log LogConfig `yaml:"log" json:"log"`
}

// LogConfig holds the config-driven log-rotation knobs (Phase 22). All fields
// optional; zero means "use the writer default".
type LogConfig struct {
	// MaxSizeMB is the per-file size cap in MiB at which the log rotates.
	MaxSizeMB int `yaml:"max_size_mb" json:"max_size_mb"`
	// MaxAgeDays purges rotated archives whose age exceeds this many days
	// (evaluated at each rotation). 0 disables age-based purging. It does not
	// trigger rotation on its own; rotation is size-driven (Retain/MaxSizeMB).
	MaxAgeDays int `yaml:"max_age_days" json:"max_age_days"`
	// Retain is how many rotated archives to keep (.1 .. .Retain). The oldest
	// beyond Retain (and any older than MaxAgeDays) is dropped on rotation.
	Retain int `yaml:"retain" json:"retain"`
}

type Tuber struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Local    string `yaml:"local" json:"local"`
	Remote   string `yaml:"remote" json:"remote"`
	SSH      string `yaml:"ssh" json:"ssh"`
	Identity string `yaml:"identity" json:"identity"`
	Enabled  bool   `yaml:"enabled" json:"enabled"`

	// Tags (Phase 46) is the tuber's tag list for grouping: `enable|disable|restart
	// --tag X`, the TUI `#tag` filter, and `a` / `x` over a filtered view. Each tag
	// reuses the validName alphabet; validated and deduped in Validate().
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`

	// Jump (Phase 43) is a ProxyJump / OpenSSH `-J` value: a single
	// `user@host[:port]` hop or a comma-separated chain. The tuber dials its
	// `ssh:` target through these intermediates in order. Empty keeps the
	// single-hop path. Parsed into Jumps by prepare().
	Jump string `yaml:"jump,omitempty" json:"jump,omitempty"`

	// PasswordAuth (Phase 35) controls this tuber's SSH password-auth fallback
	// (see Defaults.PasswordAuth). nil (absent) → inherit the on-by-default
	// behaviour; password_auth: false → opt this tunnel out of the prompt and
	// keep it key-only (with reconnect retries); true is redundant with absent.
	// The password is never stored here (only this flag); see
	// Defaults.SSHPasswordStore for keyring persistence.
	PasswordAuth *bool `yaml:"password_auth,omitempty" json:"password_auth,omitempty"`

	// Socks5User/Socks5Password override Defaults for type=dynamic tubers
	// (Phase 20). A tuber-level pair wins over the defaults pair; an empty
	// tuber-level value falls back to defaults. NoAuth when both resolve empty.
	Socks5User     string `yaml:"socks5_user" json:"socks5_user"`
	Socks5Password string `yaml:"socks5_password" json:"socks5_password"`

	// User/Host/Port are derived from SSH via prepare() and never persisted.
	// Excluded from JSON so they are not echoed over IPC.
	User string `yaml:"-" json:"-"`
	Host string `yaml:"-" json:"-"`
	Port int    `yaml:"-" json:"-"`

	// Jumps is the parsed ProxyJump chain (derived from Jump by prepare(),
	// never persisted). Empty -> the single-hop dial path.
	Jumps []Hop `yaml:"-" json:"-"`

	// SSHIdentity (Phase 44) is an IdentityFile resolved from ~/.ssh/config
	// (when the tuber's `ssh:` is an alias with an IdentityFile and no
	// explicit `identity:`). Derived by prepare(), never persisted — it only
	// fills the gap between an explicit tuber identity and the default one
	// (see ResolvedIdentity). Already token-expanded (~ %h %u %d).
	SSHIdentity string `yaml:"-" json:"-"`
}

// Hop is one address in a ProxyJump chain (Phase 43): a user/host/port triple
// parsed from a single `user@host[:port]` token by parseSSH. The final hop of
// a dial is the tuber's own SSH target; Jumps holds only the intermediates.
type Hop struct {
	User string
	Host string
	Port int
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
	if err := c.prepare(); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Validate() error {
	seen := make(map[string]struct{})
	for i := range c.Tubers {
		t := &c.Tubers[i]
		if err := validateTuber(t, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateTuber(t *Tuber, seen map[string]struct{}) error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("tuber %q: name is empty", t.Name)
	}
	if !validName(t.Name) {
		return fmt.Errorf("tuber %q: name must be alphanumeric, dashes or underscores", t.Name)
	}
	if _, ok := seen[t.Name]; ok {
		return fmt.Errorf("tuber %q: duplicate name", t.Name)
	}
	seen[t.Name] = struct{}{}
	switch t.Type {
	case "local", "remote", "dynamic":
	default:
		return fmt.Errorf("tuber %q: type %q not supported (supported: local, remote, dynamic)", t.Name, t.Type)
	}
	// local is required for every type: for local/dynamic it is the listen
	// address (a bare port expands to 127.0.0.1:port); for remote it is the
	// address server-side connections are forwarded to here.
	if strings.TrimSpace(t.Local) == "" {
		return fmt.Errorf("tuber %q: local is empty", t.Name)
	}
	// remote is the destination dialed on the host (local) or the address
	// listened on the host (remote). A dynamic (-D) tuber has no remote.
	if t.Type != "dynamic" && strings.TrimSpace(t.Remote) == "" {
		return fmt.Errorf("tuber %q: remote is empty", t.Name)
	}
	// For type: local, remote is the dial destination on the host, so it
	// must be a complete host:port — a bare port is ambiguous and would
	// otherwise fail later at dial time with an opaque net error.
	if t.Type == "local" {
		if err := validateHostPort(t.Remote); err != nil {
			return fmt.Errorf("tuber %q: remote %q is not a valid host:port for type: local (e.g. 127.0.0.1:1234)", t.Name, t.Remote)
		}
	}
	if strings.TrimSpace(t.Host) == "" {
		return fmt.Errorf("tuber %q: ssh host is empty", t.Name)
	}
	if t.Port < 1 || t.Port > 65535 {
		return fmt.Errorf("tuber %q: ssh port %d out of range (1-65535)", t.Name, t.Port)
	}
	if err := validateJump(t.Name, t.Jump); err != nil {
		return err
	}
	if err := validateTags(t.Name, t.Tags); err != nil {
		return err
	}
	return nil
}

const (
	maxTagsPerTuber = 16
	maxTagLen       = 32
)

// validateTags enforces the `tags:` field: each tag is non-empty, validName
// (shell-safe), ≤maxTagLen chars; ≤maxTagsPerTuber per tuber; case-sensitive
// dedup. Empty list is always valid.
func validateTags(name string, tags []string) error {
	if len(tags) > maxTagsPerTuber {
		return fmt.Errorf("tuber %q: too many tags (%d > %d)", name, len(tags), maxTagsPerTuber)
	}
	seen := make(map[string]struct{}, len(tags))
	for i, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return fmt.Errorf("tuber %q: tag #%d is empty", name, i+1)
		}
		if !validName(tag) {
			return fmt.Errorf("tuber %q: tag %q must be alphanumeric, dashes or underscores", name, tag)
		}
		if len(tag) > maxTagLen {
			return fmt.Errorf("tuber %q: tag %q too long (%d > %d)", name, tag, len(tag), maxTagLen)
		}
		if _, ok := seen[tag]; ok {
			return fmt.Errorf("tuber %q: duplicate tag %q", name, tag)
		}
		seen[tag] = struct{}{}
	}
	return nil
}

// validateJump enforces the `jump:` field's shape: each hop is a
// `user@host[:port]` that parses via parseSSH (like `ssh:`) with a non-empty
// host and in-range port. Empty tokens (a stray comma like "a,,b") are rejected
// — parseJumpChain skips them leniently for the dial, but load must fail
// loudly. An empty jump is always valid.
func validateJump(name, jump string) error {
	jump = strings.TrimSpace(jump)
	if jump == "" {
		return nil
	}
	for n, tok := range strings.Split(jump, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return fmt.Errorf("tuber %q: jump hop #%d is empty (check for a stray comma)", name, n+1)
		}
		_, host, port, err := parseSSH(tok)
		if err != nil {
			return fmt.Errorf("tuber %q: jump hop #%d %q: %w", name, n+1, tok, err)
		}
		if host == "" {
			return fmt.Errorf("tuber %q: jump hop #%d %q: host is empty", name, n+1, tok)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("tuber %q: jump hop #%d %q: port %d out of range (1-65535)", name, n+1, tok, port)
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
		Tubers: []Tuber{
			{
				Name:    "db-stage",
				Type:    "local",
				Local:   "5432",
				Remote:  "10.0.0.5:5432",
				SSH:     "user@bastion.example.com:22",
				Enabled: false,
				Tags:    []string{"staging", "db"},
			},
		},
	}
}

// prepare derives the non-persisted fields (Type default, User/Host/Port from
// `ssh:`, Jumps from `jump:`) and, since Phase 44, resolves `ssh:` against the
// user's ~/.ssh/config (HostName/User/Port/IdentityFile/ProxyJump). The error
// path is the ssh-config read: a missing file is fine (resolution is skipped,
// behaviour unchanged), but an existing unreadable file is a hard load error
// (see loadUserSSHConfig). prepareWith exists so tests inject an in-memory
// config instead of touching the real ~/.ssh/config.
func (c *Config) prepare() error {
	return c.prepareWith(loadUserSSHConfig)
}

func (c *Config) prepareWith(load func() (*ssh_config.Config, error)) error {
	sshCfg, err := load()
	if err != nil {
		return err
	}
	for i := range c.Tubers {
		t := &c.Tubers[i]
		if strings.TrimSpace(t.Type) == "" {
			t.Type = "local"
		}
		if err := resolveTuber(t, sshCfg); err != nil {
			return err
		}
	}
	return nil
}

// aliasValues holds the ssh-config keys Portato resolves for one alias.
type aliasValues struct {
	hostName, user, port, identity, proxyJump string
}

// matched reports whether a Host block contributed any resolvable key. A block
// that sets none (e.g. only keepalive knobs) is treated as no-match — also
// OpenSSH's effective result — so the host stays literal.
func (v aliasValues) matched() bool {
	return v.hostName != "" || v.user != "" || v.port != "" || v.identity != "" || v.proxyJump != ""
}

func lookupAlias(cfg *ssh_config.Config, alias string) aliasValues {
	var v aliasValues
	if cfg == nil {
		return v
	}
	get := func(key string) string {
		x, _ := cfg.Get(alias, key)
		return strings.TrimSpace(x)
	}
	v.hostName = get("HostName")
	v.user = get("User")
	v.port = get("Port")
	v.identity = get("IdentityFile")
	v.proxyJump = get("ProxyJump")
	return v
}

// applyAlias fills a tuber's gaps from ssh-config with openssh-faithful
// precedence: explicit tuber values (the `me@x:2222` parts of `ssh:`, or a
// tuber `identity:`) win; ssh-config only supplies what was left defaulted.
func applyAlias(t *Tuber, v aliasValues, userExplicit, portExplicit bool) {
	if v.hostName != "" {
		t.Host = v.hostName
	}
	if !userExplicit && v.user != "" {
		t.User = v.user
	}
	if !portExplicit && v.port != "" {
		if n, err := strconv.Atoi(v.port); err == nil {
			t.Port = n
		}
	}
	if strings.TrimSpace(t.Identity) == "" && v.identity != "" {
		t.SSHIdentity = expandIdentityTokens(v.identity, t.User, v.hostName)
	}
}

// resolveTuber fills a tuber's derived fields. `ssh:` is parsed first (so a
// literal user@host:port works with no ssh-config). When the parsed host matches
// a Host block, ssh-config fills the gaps (applyAlias). No match ⇒ literal,
// silently (OpenSSH). A ProxyJump from ssh-config populates Jumps only — Jump
// stays empty so the derived chain is never written back to config.yaml by Save
// (which marshals Tuber verbatim).
func resolveTuber(t *Tuber, sshCfg *ssh_config.Config) error {
	u, host, port, userExplicit, portExplicit, err := parseSSHExplicit(t.SSH)
	if err == nil {
		t.User, t.Host, t.Port = u, host, port
	}
	v := lookupAlias(sshCfg, t.Host)
	matched := v.matched()
	if matched {
		applyAlias(t, v, userExplicit, portExplicit)
	}
	if strings.TrimSpace(t.Jump) != "" {
		t.Jumps = parseJumpChain(t.Jump)
		return nil
	}
	proxyJump := ""
	if matched {
		proxyJump = v.proxyJump
	}
	if proxyJump == "" {
		return nil
	}
	resolved, err := resolveJumpChain(proxyJump, sshCfg, map[string]bool{}, 0)
	if err != nil {
		return fmt.Errorf("tuber %q: proxyjump from ssh config: %w", t.Name, err)
	}
	if err := validateJump(t.Name, resolved); err != nil {
		return err
	}
	t.Jumps = parseJumpChain(resolved)
	return nil
}

// expandJumpHop resolves one comma-separated hop of a ssh-config ProxyJump into
// `user@host:port`, expanding the hop's own HostName/User/Port when it is an
// alias (single-pass, OpenSSH `-J` semantics — a jump host's own ProxyJump is
// NOT applied). visited guards against a self-referential Host block.
func expandJumpHop(tok string, cfg *ssh_config.Config, visited map[string]bool) (string, error) {
	u, host, port, userExplicit, portExplicit, err := parseSSHExplicit(tok)
	if err != nil {
		return "", fmt.Errorf("hop %q: %w", tok, err)
	}
	v := lookupAlias(cfg, host)
	if !v.matched() {
		return fmt.Sprintf("%s@%s:%d", u, host, port), nil
	}
	if visited[host] {
		return "", fmt.Errorf("cycle through %q", host)
	}
	visited[host] = true
	if v.hostName != "" {
		host = v.hostName
	}
	if !userExplicit && v.user != "" {
		u = v.user
	}
	if !portExplicit && v.port != "" {
		if n, e := strconv.Atoi(v.port); e == nil {
			port = n
		}
	}
	return fmt.Sprintf("%s@%s:%d", u, host, port), nil
}

// resolveJumpChain expands a ProxyJump value (from ssh-config) into a resolved
// comma-chain. The depth cap is purely defensive (OpenSSH's -J is flat).
func resolveJumpChain(pj string, sshCfg *ssh_config.Config, visited map[string]bool, depth int) (string, error) {
	if depth > maxJumpDepth {
		return "", fmt.Errorf("chain too deep (>%d hops)", maxJumpDepth)
	}
	out := make([]string, 0, len(strings.Split(pj, ",")))
	for _, tok := range strings.Split(pj, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return "", fmt.Errorf("empty hop (stray comma)")
		}
		hop, err := expandJumpHop(tok, sshCfg, visited)
		if err != nil {
			return "", err
		}
		out = append(out, hop)
	}
	return strings.Join(out, ","), nil
}

// expandIdentityTokens expands an IdentityFile from ssh-config the way ssh does:
// %d → home dir, %h → the resolved target host, %u → the user, then a leading
// ~ → home. kevinburke/ssh_config returns these tokens literally.
func expandIdentityTokens(idf, sshUser, host string) string {
	home, _ := os.UserHomeDir()
	r := strings.NewReplacer("%d", home, "%h", host, "%u", sshUser)
	return expandTilde(r.Replace(idf))
}

// loadUserSSHConfig reads ~/.ssh/config for ssh-config resolution. A missing
// file is not an error (resolution is simply skipped → unchanged behaviour);
// an existing but unreadable file is, so a permission/IO problem surfaces as a
// clear load error rather than a silent dial failure. The lenient parser does
// not reject odd but salvageable input (mirroring OpenSSH), so a "parse error"
// in the strict sense is not produced; Include directives are followed.
func loadUserSSHConfig() (*ssh_config.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	p := filepath.Join(home, ".ssh", "config")
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ssh config %s: %w", p, err)
	}
	defer f.Close()
	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("parse ssh config %s: %w", p, err)
	}
	return cfg, nil
}

// parseJumpChain splits a ProxyJump `jump:` value into its parsed hops. An empty
// (or whitespace-only) value yields nil so the dial takes its single-hop path.
// The empty check is load-critical: strings.Split("", ",") returns [""] (one
// empty token), parseSSH("") errors, and without this guard every tuber would
// fail to load. Empty tokens in a non-empty chain (e.g. "a,,b") are skipped
// here; Validate rejects them. See validateJump for the strict checks.
func parseJumpChain(jump string) []Hop {
	if strings.TrimSpace(jump) == "" {
		return nil
	}
	var hops []Hop
	for _, tok := range strings.Split(jump, ",") {
		if strings.TrimSpace(tok) == "" {
			continue
		}
		u, h, p, err := parseSSH(tok)
		if err != nil {
			continue
		}
		hops = append(hops, Hop{User: u, Host: h, Port: p})
	}
	return hops
}

func (t Tuber) ListenAddr() string {
	return normalizeAddrPort(t.Local, defaultHost)
}

// RemoteListenAddr is the address a type=remote tuber listens on, on the SSH
// server side. A bare port or ":port" binds all interfaces via remoteWildcard
// ("*:port"); an explicit host (127.0.0.1, 0.0.0.0, [::], a public IP, …) is
// used as written. A non-loopback bind requires GatewayPorts
// yes|clientspecified in sshd_config.
func (t Tuber) RemoteListenAddr() string {
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

// validateHostPort reports whether s parses as a non-empty host and port,
// the form required for a type: local remote (the dial destination).
func validateHostPort(s string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	if host == "" {
		return fmt.Errorf("missing host")
	}
	if port == "" {
		return fmt.Errorf("missing port")
	}
	return nil
}

func (t Tuber) ResolvedIdentity(d Defaults) string {
	if strings.TrimSpace(t.Identity) != "" {
		return expandTilde(t.Identity)
	}
	// Phase 44: an IdentityFile resolved from ~/.ssh/config fills the gap
	// between an explicit tuber identity and the default. SSHIdentity is
	// already token-expanded by prepare(), so no expandTilde here.
	if strings.TrimSpace(t.SSHIdentity) != "" {
		return t.SSHIdentity
	}
	if strings.TrimSpace(d.Identity) != "" {
		return expandTilde(d.Identity)
	}
	return ""
}

// ResolvedPasswordAuth reports whether SSH password authentication is enabled
// for this tuber (Phase 35). It is ON BY DEFAULT (OpenSSH-style): when keys
// (agent → identity) don't authenticate, the dial falls back to an interactive
// password prompt. A single explicit password_auth: false — on the tuber or in
// the defaults — opts OUT (key-only, with reconnect retries). nil/true both
// mean "on". Public-key auth is always tried first; password is only a last
// resort. The password itself is never in config.
func (t Tuber) ResolvedPasswordAuth(d Defaults) bool {
	if t.PasswordAuth != nil && !*t.PasswordAuth {
		return false
	}
	if d.PasswordAuth != nil && !*d.PasswordAuth {
		return false
	}
	return true
}

// Equal reports whether two Defaults are value-equal, dereferencing PasswordAuth
// (a *bool) so two parses of the same file compare equal even though the
// pointers differ. Engine.Reload restarts tubers only when defaults actually
// change, so this must not report spurious differences.
func (d Defaults) Equal(o Defaults) bool {
	return d.Identity == o.Identity &&
		d.KnownHosts == o.KnownHosts &&
		d.AcceptNewHosts == o.AcceptNewHosts &&
		d.Socks5User == o.Socks5User &&
		d.Socks5Password == o.Socks5Password &&
		d.IdentityPassphraseStore == o.IdentityPassphraseStore &&
		d.SSHPasswordStore == o.SSHPasswordStore &&
		d.Log == o.Log &&
		boolPtrEqual(d.PasswordAuth, o.PasswordAuth)
}

// boolPtrEqual compares two *bool by value (nil == nil; nil != &x; &x == &y by
// pointed value).
func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// PasswordAccountKey is the shared provider key under which a tuber's SSH
// password is cached/stored (Phase 35): "password:<user>@<host>:<port>". It is
// namespaced ("password:") so it never collides with an identity-passphrase key
// (which is the identity file path). Both the forward dial (Get) and the
// controller/daemon handlers (Set) build it from this one method so the two
// sides can never disagree. User/Host/Port are populated by prepare() from SSH.
func (t Tuber) PasswordAccountKey() string {
	return fmt.Sprintf("password:%s@%s:%d", t.User, t.Host, t.Port)
}

// ResolvedSocks5User returns the SOCKS5 username a type=dynamic tuber should
// authenticate with: the tuber-level value wins, otherwise the defaults value
// (Phase 20). Empty means no auth (a password alone is meaningless — see
// ResolvedSocks5Creds).
func (t Tuber) ResolvedSocks5User(d Defaults) string {
	if strings.TrimSpace(t.Socks5User) != "" {
		return t.Socks5User
	}
	return d.Socks5User
}

// ResolvedSocks5Password mirrors ResolvedSocks5User for the password half.
func (t Tuber) ResolvedSocks5Password(d Defaults) string {
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

// parseSSH parses a `user@host[:port]` value, defaulting an absent user to the
// current OS user and an absent port to 22. It is a thin wrapper over
// parseSSHExplicit that drops the explicit-vs-defaulted flags (those matter
// only for ssh-config precedence in prepare, see Phase 44).
func parseSSH(s string) (usr, host string, port int, err error) {
	usr, host, port, _, _, err = parseSSHExplicit(s)
	return usr, host, port, err
}

// parseSSHExplicit is the core parser: it reports whether the user and port
// were written explicitly (an `@` ⇒ user explicit; a numeric `:port` ⇒ port
// explicit) so the ssh-config resolver can honour precedence — an explicit
// `ssh: me@alias:2222` overrides the alias's User/Port, a defaulted one
// inherits them. Absent values still default (user → current OS user, port →
// 22) so the returned triple is always dial-ready.
func parseSSHExplicit(s string) (usr, host string, port int, userExplicit, portExplicit bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", 0, false, false, fmt.Errorf("ssh is empty")
	}
	hostPart := s
	if i := strings.LastIndex(s, "@"); i >= 0 {
		usr = s[:i]
		hostPart = s[i+1:]
		userExplicit = true
	}
	host = hostPart
	port = defaultSSHPort
	if i := strings.LastIndex(hostPart, ":"); i >= 0 {
		if n, perr := strconv.Atoi(hostPart[i+1:]); perr == nil {
			host = hostPart[:i]
			port = n
			portExplicit = true
		}
	}
	if !userExplicit {
		usr = currentUser()
	}
	return usr, host, port, userExplicit, portExplicit, nil
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

// ValidName is the exported form of validName: the shared tuber/tag name
// alphabet (letters, digits, `-`, `_`). The Phase-48 importer uses it to
// derive tuber names from ssh_config host patterns.
func ValidName(s string) bool { return validName(s) }

// ResolveSSHHost resolves a bare `ssh:` target (e.g. a ssh_config host
// pattern) against cfg with the Phase-44 precedence (explicit parts win,
// ssh-config fills the gaps) and reports the effective host and port. A
// pattern with no matching block resolves to itself, port 22. The empty
// host means resolution failed (a ProxyJump cycle in ssh config) — the
// caller treats that as an error. Exported for the Phase-48 importer's
// dedup key, which compares resolved endpoints, not raw spellings.
func ResolveSSHHost(target string, cfg *ssh_config.Config) (host string, port int) {
	t := Tuber{SSH: target}
	if err := resolveTuber(&t, cfg); err != nil {
		return "", 0
	}
	return t.Host, t.Port
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

// ExpandTilde is the exported form of expandTilde: it resolves a leading ~ to
// the user's home directory (like ResolvedIdentity does) and leaves other paths
// untouched. `portato add-identity` / `forget-identity` use it to key the
// keyring exactly the way the dial does, so the two never disagree.
func ExpandTilde(p string) string { return expandTilde(p) }

type tuberRaw struct {
	Name           string   `yaml:"name"`
	Type           string   `yaml:"type"`
	Local          any      `yaml:"local"`
	Remote         string   `yaml:"remote"`
	SSH            string   `yaml:"ssh"`
	Identity       string   `yaml:"identity"`
	Enabled        bool     `yaml:"enabled"`
	PasswordAuth   *bool    `yaml:"password_auth"`
	Socks5User     string   `yaml:"socks5_user"`
	Socks5Password string   `yaml:"socks5_password"`
	Jump           string   `yaml:"jump"`
	Tags           []string `yaml:"tags"`
}

func (t *Tuber) UnmarshalYAML(value *yaml.Node) error {
	var raw tuberRaw
	if err := value.Decode(&raw); err != nil {
		return err
	}
	t.Name = raw.Name
	t.Type = raw.Type
	t.Remote = raw.Remote
	t.SSH = raw.SSH
	t.Identity = raw.Identity
	t.Enabled = raw.Enabled
	t.PasswordAuth = raw.PasswordAuth
	t.Socks5User = raw.Socks5User
	t.Socks5Password = raw.Socks5Password
	t.Jump = raw.Jump
	t.Tags = raw.Tags
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
		return fmt.Errorf("tuber %q: local must be a port number or host:port", raw.Name)
	}
	return nil
}
