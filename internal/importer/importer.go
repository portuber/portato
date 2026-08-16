// Package importer maps ~/.ssh/config forward directives to would-be tubers
// (Phase 48): a one-time copy into config.yaml. ssh_config is read-only
// here — it is never written, and the imported tubers keep the raw host
// pattern in `ssh:` so load-time Phase-44 resolution applies unchanged.
package importer

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"
)

// Candidate is one forward directive from ssh_config mapped to a would-be
// tuber's source fields. SSHHost is the block's first concrete pattern,
// verbatim ("db", not the resolved host); Patterns lists every concrete
// pattern of the source block so the CLI can address blocks by name.
type Candidate struct {
	SSHHost  string
	Patterns []string
	Type     string // local | remote | dynamic
	Local    string
	Remote   string
}

// maxIncludeDepth bounds Include recursion. The library enforces the same
// cap while decoding, so this is defense in depth for the scanner's own
// re-decode of included files.
const maxIncludeDepth = 5

// DefaultPath is the ssh_config location the Phase-44 reader uses.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// Load decodes an ssh config file ("" = the default ~/.ssh/config). A missing
// file is not an error (nil, nil) — mirroring the Phase-44 reader, so a
// machine without ~/.ssh/config simply has nothing to import; an existing
// unreadable or unparseable file is a clear error. A leading ~ is expanded.
func Load(path string) (*ssh_config.Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	path = expandTilde(path)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ssh config %s: %w", path, err)
	}
	defer f.Close()
	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("parse ssh config %s: %w", path, err)
	}
	return cfg, nil
}

func expandTilde(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return filepath.Join(home, p[2:])
	}
	return p
}

// Scan walks the decoded ssh config block by block and maps every
// LocalForward / RemoteForward / DynamicForward directive to a candidate, in
// file order.
//
// The walk is per-block, not alias-resolution: a literal cfg.GetAll(alias, …)
// also collects values from every other block whose patterns match the alias
// (notably Host *), leaking global forwards into each host's candidates. All
// occurrences within a block are kept (the multi-forward intent of "GetAll,
// not Get" — Get would return only the first). Skipped: Match blocks (the
// library does not support Match, Phase 44's documented limitation),
// wildcard-only blocks (Host * / ?-patterns — their forwards apply to every
// host, so they are nobody's candidate), negated patterns, and a file's
// global preamble (same reasoning as Host *). Blocks reached through Include
// files are enumerated with the same rules; an included file's own preamble
// attributes to the including block, mirroring OpenSSH's textual insertion.
func Scan(cfg *ssh_config.Config) []Candidate {
	if cfg == nil {
		return nil
	}
	s := &scanner{seen: make(map[string]struct{})}
	s.walkConfig(cfg, nil, 0)
	return s.cands
}

type scanner struct {
	cands []Candidate
	seen  map[string]struct{}
}

// block is one enumerated Host block: its concrete (non-wildcard,
// non-negated) patterns — the first becomes the candidates' sshHost.
type block struct {
	sshHost  string
	patterns []string
}

func (s *scanner) walkConfig(cfg *ssh_config.Config, owner *block, depth int) {
	for _, h := range cfg.Hosts {
		switch firstWord(h) {
		case "Match":
			s.walkIncludes(h, nil, depth)
		case "Host":
			b := newBlock(h)
			if b == nil {
				s.walkIncludes(h, nil, depth)
				continue
			}
			s.takeForwards(b, h.Nodes)
			s.walkIncludes(h, b, depth)
		default:
			if owner != nil {
				s.takeForwards(owner, h.Nodes)
			}
			s.walkIncludes(h, owner, depth)
		}
	}
}

// walkIncludes enumerates the Host blocks inside a block's Include nodes with
// the enclosing block as preamble owner. Paths mirror the library's Include
// resolution: absolute and ~/ as-is, everything else relative to ~/.ssh,
// wildcards globbed. An unreadable include is skipped leniently — the
// library already surfaced hard parse errors at Decode time.
func (s *scanner) walkIncludes(h *ssh_config.Host, owner *block, depth int) {
	if depth >= maxIncludeDepth {
		return
	}
	for _, n := range h.Nodes {
		inc, ok := n.(*ssh_config.Include)
		if !ok {
			continue
		}
		for _, p := range includePaths(inc) {
			sub, err := Load(p)
			if err != nil || sub == nil {
				continue
			}
			s.walkConfig(sub, owner, depth+1)
		}
	}
}

// includePaths extracts and resolves the file list of an Include directive.
// The library keeps the paths unexported, so they are read back from the
// rendered directive line.
func includePaths(inc *ssh_config.Include) []string {
	line := strings.TrimSpace(inc.String())
	if i := strings.Index(line, "#"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimPrefix(line, "Include")
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "="))
	home, _ := os.UserHomeDir()
	var out []string
	for _, dir := range strings.Fields(line) {
		p := dir
		switch {
		case filepath.IsAbs(p):
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(home, p[2:])
		default:
			p = filepath.Join(home, ".ssh", p)
		}
		matches, err := filepath.Glob(p)
		if err != nil || len(matches) == 0 {
			continue
		}
		out = append(out, matches...)
	}
	return out
}

// newBlock extracts a block's concrete patterns. nil means the block carries
// no usable pattern (wildcard-only, negated-only, or the implicit preamble).
func newBlock(h *ssh_config.Host) *block {
	var b block
	for _, p := range h.Patterns {
		s := p.String()
		if s == "" || strings.ContainsAny(s, "*?!") {
			continue
		}
		b.patterns = append(b.patterns, s)
	}
	if len(b.patterns) == 0 {
		return nil
	}
	b.sshHost = b.patterns[0]
	return &b
}

// firstWord classifies a block by the first word of its rendered header:
// "Host" (explicit block), "Match" (unsupported conditional), anything else —
// the implicit preamble every decoded file starts with. The renderer writes
// the keywords literally ("Host db", "Match host x"); a KV line can only
// false-positive on a first word like "HostName", which is not "Host".
func firstWord(h *ssh_config.Host) string {
	s := strings.TrimSpace(h.String())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func (s *scanner) takeForwards(b *block, nodes []ssh_config.Node) {
	for _, n := range nodes {
		kv, ok := n.(*ssh_config.KV)
		if !ok {
			continue
		}
		var typ string
		switch strings.ToLower(kv.Key) {
		case "localforward":
			typ = "local"
		case "remoteforward":
			typ = "remote"
		case "dynamicforward":
			typ = "dynamic"
		default:
			continue
		}
		local, remote, ok := parseForward(typ, kv.Value)
		if !ok {
			continue
		}
		c := Candidate{SSHHost: b.sshHost, Patterns: b.patterns, Type: typ, Local: local, Remote: remote}
		key := c.SSHHost + "|" + c.Type + "|" + c.Local + "|" + c.Remote
		if _, dup := s.seen[key]; dup {
			continue
		}
		s.seen[key] = struct{}{}
		s.cands = append(s.cands, c)
	}
}

// parseForward maps one directive value to tuber source fields per the
// Phase-48 semantics:
//
//	LocalForward   [bind:]port host:port → local
//	RemoteForward  [bind:]port host:port → remote, sides swapped (the
//	  declared listen port becomes the tuber's `remote`, the destination its
//	  `local`); a bare listen port expands to 127.0.0.1:port — OpenSSH binds
//	  the remote side to loopback by default while Portato's bare port
//	  normalises to *:port (the Phase-13 caveat)
//	DynamicForward [bind:]port            → dynamic
//
// Undocumented forms (unix-socket forwards, stray fields) are skipped, not
// errors — lenient like the parser, and visible by their absence in the
// preview list the user confirms.
func parseForward(typ, val string) (local, remote string, ok bool) {
	fields := strings.Fields(val)
	switch typ {
	case "dynamic":
		if len(fields) != 1 || !validBindPort(fields[0]) {
			return "", "", false
		}
		return fields[0], "", true
	case "local", "remote":
		if len(fields) != 2 || !validBindPort(fields[0]) || !validHostPort(fields[1]) {
			return "", "", false
		}
		if typ == "local" {
			return fields[0], fields[1], true
		}
		bind := fields[0]
		if host, _, _ := splitBind(bind); host == "" {
			bind = "127.0.0.1:" + bind
		}
		return fields[1], bind, true
	}
	return "", "", false
}

// splitBind splits a "[bind:]port" listen address (the declared side of a
// forward): "5432" → bare port; "127.0.0.1:5432", "[::1]:5432", ":5432" →
// host and port. ok is false when no valid 1-65535 port is present.
func splitBind(s string) (host, port string, ok bool) {
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p, validPort(p)
	}
	if validPort(s) {
		return "", s, true
	}
	return "", "", false
}

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

func validBindPort(s string) bool {
	_, _, ok := splitBind(s)
	return ok
}

// validHostPort checks the destination side of a forward: a non-empty host
// and a valid port ("10.0.0.5:5432", "[::1]:80", "db.internal:5432"). A bare
// value — a unix-socket path or a stray port — is not the documented form.
func validHostPort(s string) bool {
	h, p, err := net.SplitHostPort(s)
	return err == nil && h != "" && validPort(p)
}

// Filter keeps candidates whose source block declares one of the patterns
// exactly (case-insensitive — ssh patterns are). Matching is by declared
// pattern, not ssh-style alias resolution, because the import addresses
// blocks; --all covers the bulk case.
func Filter(cands []Candidate, patterns []string) []Candidate {
	want := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		want[strings.ToLower(p)] = struct{}{}
	}
	var out []Candidate
	for _, c := range cands {
		for _, p := range c.Patterns {
			if _, ok := want[strings.ToLower(p)]; ok {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// Blocks returns the distinct source-block sshHosts of cands in scan order —
// the importable blocks a bare `portato import` suggests in its hint.
func Blocks(cands []Candidate) []string {
	seen := make(map[string]struct{}, len(cands))
	var out []string
	for _, c := range cands {
		if _, ok := seen[c.SSHHost]; ok {
			continue
		}
		seen[c.SSHHost] = struct{}{}
		out = append(out, c.SSHHost)
	}
	return out
}
