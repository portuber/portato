package forward

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/portuber/portato/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const connectTimeout = 5 * time.Second

// hostKeySink receives a rejected unknown host key so the caller (the Tunnel,
// and through Status the TUI) can offer to accept it. The arguments are the
// hostname as dialed, the key fingerprint, and a ready-to-append known_hosts
// line. Nil-safe: a nil sink records nothing.
type hostKeySink func(host, fingerprint, line string)

// dialSSH establishes an SSH client connection to the tunnel's server.
// The TCP dial is context-aware so it can be interrupted by tunnel shutdown.
// sink, when non-nil, receives any rejected unknown host key (TOFU prompt).
// provider, when non-nil, lets a passphrase-protected identity load by
// obtaining its passphrase (blocking until one is provided); passSink surfaces
// the identity path that needs a passphrase via Status.PendingPassphrase.
func dialSSH(ctx context.Context, cfg config.Tunnel, def config.Defaults, log *slog.Logger, sink hostKeySink, provider PassphraseProvider, passSink passphraseSink) (*ssh.Client, error) {
	auths, closeAgent, err := authMethods(ctx, cfg, def, log, provider, passSink)
	if err != nil {
		return nil, err
	}
	defer closeAgent()
	hostCb, err := hostKeyCallback(def, log, sink)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auths,
		HostKeyCallback: hostCb,
		Timeout:         connectTimeout,
	}
	if algos := hostKeyAlgos(def.ResolvedKnownHosts(), addr); len(algos) > 0 {
		sshCfg.HostKeyAlgorithms = algos
	}

	d := &net.Dialer{Timeout: connectTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, mapDialError(err)
	}
	if err := conn.SetDeadline(time.Now().Add(connectTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set handshake deadline: %w", err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, mapDialError(err)
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(sc, chans, reqs), nil
}

// authMethods builds the SSH auth-method chain. The returned closer must be
// invoked once the SSH handshake is done; it keeps the ssh-agent connection
// open for the lifetime of the agent-backed signers (which sign lazily during
// the handshake) and is a no-op when no agent is used. provider/passSink enable
// passphrase-protected identity loading (Phase 19); both may be nil.
func authMethods(ctx context.Context, cfg config.Tunnel, def config.Defaults, log *slog.Logger, provider PassphraseProvider, passSink passphraseSink) ([]ssh.AuthMethod, func() error, error) {
	var (
		methods []ssh.AuthMethod
		closers []io.Closer
	)
	if sock := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK")); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			log.Warn("dial ssh-agent failed; falling back to identity", "err", err)
		} else {
			ag := agent.NewClient(conn)
			methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
			closers = append(closers, conn)
		}
	}
	if idPath := cfg.ResolvedIdentity(def); idPath != "" {
		signer, err := loadIdentityWithPassphrase(ctx, idPath, provider, passSink)
		if err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
		} else {
			log.Warn("failed to load identity key", "path", idPath, "err", err)
		}
	}
	if len(methods) == 0 {
		return nil, nil, errors.New("no ssh auth method available: start ssh-agent (SSH_AUTH_SOCK) or configure an identity key")
	}
	closeAgent := func() error {
		for _, c := range closers {
			_ = c.Close()
		}
		return nil
	}
	return methods, closeAgent, nil
}

func hostKeyCallback(def config.Defaults, log *slog.Logger, sink hostKeySink) (ssh.HostKeyCallback, error) {
	hosts := def.ResolvedKnownHosts()
	if err := ensureKnownHostsFile(hosts); err != nil {
		return nil, err
	}
	base, err := knownhosts.New(hosts)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %s: %w", hosts, err)
	}
	return wrapHostKey(hosts, base, def.AcceptNewHosts, log, sink), nil
}

func ensureKnownHostsFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat known_hosts %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create known_hosts dir: %w", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("create known_hosts %s: %w", path, err)
	}
	return nil
}

type unknownHostError struct {
	host        string
	fingerprint string
}

func (e *unknownHostError) Error() string {
	return fmt.Sprintf("%s: host key not in known_hosts (%s); add it to known_hosts or set accept_new_hosts: true", e.host, e.fingerprint)
}

func wrapHostKey(hostsFile string, base ssh.HostKeyCallback, acceptNew bool, log *slog.Logger, sink hostKeySink) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		fp := ssh.FingerprintSHA256(key)
		if len(ke.Want) == 0 {
			if acceptNew {
				if werr := appendKnownHost(hostsFile, hostname, key); werr != nil {
					log.Warn("tofu: failed to append known_hosts", "host", hostname, "err", werr)
					recordUnknownHost(sink, hostname, fp, key)
					return &unknownHostError{host: hostname, fingerprint: fp}
				}
				log.Info("tofu: added new host to known_hosts", "host", hostname, "fingerprint", fp)
				return nil
			}
			recordUnknownHost(sink, hostname, fp, key)
			return &unknownHostError{host: hostname, fingerprint: fp}
		}
		want := ke.Want[0].Key
		return fmt.Errorf("host key mismatch for %s: expected %s", hostname, ssh.FingerprintSHA256(want))
	}
}

// recordUnknownHost reports the rejected key to the sink (nil-safe) so the
// caller can surface an accept prompt. It builds the known_hosts line up
// front — the exact bytes that AcceptHost will append later.
func recordUnknownHost(sink hostKeySink, hostname, fingerprint string, key ssh.PublicKey) {
	if sink == nil {
		return
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	sink(hostname, fingerprint, line)
}

func appendKnownHost(hostsFile, hostname string, key ssh.PublicKey) error {
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	return AppendKnownHostLine(hostsFile, line)
}

// AppendKnownHostLine writes a single pre-built known_hosts line to hostsFile
// (creating it owner-only if absent). It is the accept path for the TUI TOFU
// prompt: the line was captured at rejection time and is appended verbatim.
func AppendKnownHostLine(hostsFile, line string) error {
	if err := os.MkdirAll(filepath.Dir(hostsFile), 0o700); err != nil {
		return fmt.Errorf("create known_hosts dir: %w", err)
	}
	f, err := os.OpenFile(hostsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}

func mapDialError(err error) error {
	if err == nil {
		return nil
	}
	var uhe *unknownHostError
	if errors.As(err, &uhe) {
		return uhe
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain"):
		return fmt.Errorf("auth failed: %w", err)
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("connect refused: %w", err)
	case strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("connect timeout: %w", err)
	}
	return fmt.Errorf("ssh dial: %w", err)
}

// hostKeyAlgos derives the host-key algorithms to offer during the SSH
// handshake from the key types recorded for addr in the known_hosts file.
// This makes x/crypto/ssh negotiate a key type we already trust, matching
// OpenSSH behaviour and avoiding spurious "host key mismatch" when the
// server offers several key types but known_hosts only has one (the default
// preference order in x/crypto puts ECDSA ahead of ED25519). See
// golang/go#36126. Returns nil when addr is unknown (first connect), so the
// caller leaves HostKeyAlgorithms at its default.
func hostKeyAlgos(hostsFile, addr string) []string {
	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		port = "22"
	}
	normalizedTarget := knownhosts.Normalize(net.JoinHostPort(host, port))

	data, err := os.ReadFile(hostsFile)
	if err != nil {
		return nil
	}

	var algos []string
	seen := make(map[string]bool)
	add := func(in []string) {
		for _, a := range in {
			if !seen[a] {
				seen[a] = true
				algos = append(algos, a)
			}
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.HasPrefix(fields[0], "@") {
			continue
		}
		if !hostMatchesAny(fields[0], host, port, normalizedTarget) {
			continue
		}
		add(algosForKeyType(fields[1]))
	}
	return algos
}

// hostMatchesAny reports whether any comma-separated pattern in `patterns`
// matches the target, honouring negation ("!") which excludes the whole line.
func hostMatchesAny(patterns, host, port, normalizedTarget string) bool {
	matched := false
	for _, p := range strings.Split(patterns, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "!") {
			if hostPatternMatches(p[1:], host, port, normalizedTarget) {
				return false
			}
			continue
		}
		if hostPatternMatches(p, host, port, normalizedTarget) {
			matched = true
		}
	}
	return matched
}

func hostPatternMatches(pattern, host, port, normalizedTarget string) bool {
	switch {
	case strings.HasPrefix(pattern, "|1|"):
		return hashedHostMatches(pattern, normalizedTarget)
	case strings.HasPrefix(pattern, "["):
		end := strings.Index(pattern, "]:")
		if end < 0 {
			return false
		}
		return pattern[1:end] == host && pattern[end+2:] == port
	default:
		// A plain hostname (possibly with wildcards) matches the default
		// port only; non-default ports must be recorded as [host]:port.
		if !wildcardMatch(pattern, host) {
			return false
		}
		return port == "22"
	}
}

// hashedHostMatches verifies a hashed known_hosts entry of the form
// "|1|<base64 salt>|<base64 hmac-sha1>" against the normalized target.
func hashedHostMatches(pattern, normalizedTarget string) bool {
	parts := strings.Split(pattern, "|")
	if len(parts) != 4 || parts[1] != "1" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(normalizedTarget))
	return hmac.Equal(mac.Sum(nil), want)
}

func wildcardMatch(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	return err == nil && ok
}

// algosForKeyType maps a known_hosts key-type field to the negotiable host-key
// algorithms for that key. RSA keys are recorded as "ssh-rsa" but servers
// negotiate the SHA-2 variants, so all three are offered.
func algosForKeyType(keytype string) []string {
	switch keytype {
	case ssh.KeyAlgoED25519:
		return []string{ssh.KeyAlgoED25519}
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	case ssh.KeyAlgoECDSA256:
		return []string{ssh.KeyAlgoECDSA256}
	case ssh.KeyAlgoECDSA384:
		return []string{ssh.KeyAlgoECDSA384}
	case ssh.KeyAlgoECDSA521:
		return []string{ssh.KeyAlgoECDSA521}
	case ssh.InsecureKeyAlgoDSA:
		return []string{ssh.InsecureKeyAlgoDSA}
	default:
		return []string{keytype}
	}
}
