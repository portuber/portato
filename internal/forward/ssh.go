package forward

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const connectTimeout = 5 * time.Second

// dialSSH establishes an SSH client connection to the tunnel's server.
// The TCP dial is context-aware so it can be interrupted by tunnel shutdown.
func dialSSH(ctx context.Context, cfg config.Tunnel, def config.Defaults, log *slog.Logger) (*ssh.Client, error) {
	auths, err := authMethods(cfg, def, log)
	if err != nil {
		return nil, err
	}
	hostCb, err := hostKeyCallback(def, log)
	if err != nil {
		return nil, err
	}
	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auths,
		HostKeyCallback: hostCb,
		Timeout:         connectTimeout,
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))

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

func authMethods(cfg config.Tunnel, def config.Defaults, log *slog.Logger) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if sock := os.Getenv("SSH_AUTH_SOCK"); strings.TrimSpace(sock) != "" {
		methods = append(methods, agentAuthMethod(sock))
	}
	if idPath := cfg.ResolvedIdentity(def); idPath != "" {
		signer, err := loadIdentity(idPath)
		if err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
		} else {
			log.Warn("failed to load identity key", "path", idPath, "err", err)
		}
	}
	if len(methods) == 0 {
		return nil, errors.New("no ssh auth method available: start ssh-agent (SSH_AUTH_SOCK) or configure an identity key")
	}
	return methods, nil
}

func agentAuthMethod(sock string) ssh.AuthMethod {
	return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("dial ssh-agent: %w", err)
		}
		defer conn.Close()
		return agent.NewClient(conn).Signers()
	})
}

func loadIdentity(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	return signer, nil
}

func hostKeyCallback(def config.Defaults, log *slog.Logger) (ssh.HostKeyCallback, error) {
	hosts := def.ResolvedKnownHosts()
	if err := ensureKnownHostsFile(hosts); err != nil {
		return nil, err
	}
	base, err := knownhosts.New(hosts)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %s: %w", hosts, err)
	}
	return wrapHostKey(hosts, base, def.AcceptNewHosts, log), nil
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
	return fmt.Sprintf("unknown host %s (%s); add to known_hosts or set accept_new_hosts: true", e.host, e.fingerprint)
}

func wrapHostKey(hostsFile string, base ssh.HostKeyCallback, acceptNew bool, log *slog.Logger) ssh.HostKeyCallback {
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
					return &unknownHostError{host: hostname, fingerprint: fp}
				}
				log.Info("tofu: added new host to known_hosts", "host", hostname, "fingerprint", fp)
				return nil
			}
			return &unknownHostError{host: hostname, fingerprint: fp}
		}
		want := ke.Want[0].Key
		return fmt.Errorf("host key mismatch for %s: expected %s", hostname, ssh.FingerprintSHA256(want))
	}
}

func appendKnownHost(hostsFile, hostname string, key ssh.PublicKey) error {
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
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
