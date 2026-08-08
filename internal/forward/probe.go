package forward

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"

	"github.com/portuber/portato/internal/config"
	"golang.org/x/crypto/ssh"
)

// ProbeOutcome is the result of a non-interactive forwarding-permission probe
// of one tuber's server (Phase 41). It classifies why a tunnel would (or would
// not) work, focusing on the server-side sshd gate.
type ProbeOutcome int

const (
	// ProbeHealthy means the SSH dial succeeded and a direct-tcpip probe was
	// accepted (AllowTcpForwarding is on).
	ProbeHealthy ProbeOutcome = iota
	// ProbeForwardingDenied means the server rejected a direct-tcpip open with
	// ssh.Prohibited — i.e. `AllowTcpForwarding no` in sshd_config.
	ProbeForwardingDenied
	// ProbeAuthUnavailable means no usable auth method (no ssh-agent, no
	// unencrypted identity). A passphrase-protected identity that is not in the
	// agent also lands here: the probe is non-interactive and cannot prompt.
	ProbeAuthUnavailable
	// ProbeAuthFailed means the dial reached the server but the credentials
	// were rejected.
	ProbeAuthFailed
	// ProbeHostKey means the server's host key is unknown or mismatched.
	ProbeHostKey
	// ProbeConnectFailed means the TCP dial to the server failed (refused,
	// timeout, unreachable).
	ProbeConnectFailed
	// ProbeOther is an uncategorized failure.
	ProbeOther
)

func (o ProbeOutcome) String() string {
	switch o {
	case ProbeHealthy:
		return "healthy"
	case ProbeForwardingDenied:
		return "forwarding-denied"
	case ProbeAuthUnavailable:
		return "auth-unavailable"
	case ProbeAuthFailed:
		return "auth-failed"
	case ProbeHostKey:
		return "host-key"
	case ProbeConnectFailed:
		return "connect-failed"
	default:
		return "other"
	}
}

// ProbeResult is the outcome of a single ProbeForwarding call.
type ProbeResult struct {
	Outcome ProbeOutcome
	// Detail is a human-readable explanation: the underlying error message, or
	// an extra note (e.g. the GatewayPorts caveat for a -R non-loopback bind).
	Detail string
}

// ProbeForwarding non-interactively dials cfg's server and probes whether SSH
// forwarding is permitted there. It uses key-only auth (ssh-agent + the
// configured identity, no passphrase/password prompts — doctor is a separate
// process and cannot show the TUI modals), so a passphrase-protected identity
// that is not in the agent surfaces as ProbeAuthUnavailable/ProbeAuthFailed.
//
// The probe opens a direct-tcpip channel to a throwaway target; if the server
// rejects it with ssh.Prohibited, AllowTcpForwarding is off. A `GatewayPorts no`
// silent-loopback downgrade on a -R non-loopback bind is NOT detectable from
// the client (RFC 4254 §7.1: the tcpip-forward reply carries only the port),
// so Detail carries an explicit caveat for that case instead.
func ProbeForwarding(ctx context.Context, cfg config.Tuber, def config.Defaults, log *slog.Logger) ProbeResult {
	auths, closeAgent := authMethods(ctx, cfg, def, log, nil, nil)
	defer closeAgent()
	if len(auths) == 0 {
		return ProbeResult{Outcome: ProbeAuthUnavailable, Detail: "no ssh-agent and no unencrypted identity; start ssh-agent/ssh-add, or cache the key passphrase via `portato add-identity`"}
	}

	// dialConn so a jump tuber is probed through its chain (intermediates +
	// target both key-only here); a no-jump tuber takes the unchanged
	// single-hop path.
	client, err := dialConn(ctx, cfg, def, log, nil, auths, auths)
	if err != nil {
		return classifyProbeDialErr(err)
	}
	defer client.Close()

	// direct-tcpip probe. A Prohibited rejection means AllowTcpForwarding no.
	// Any other outcome (target refuses, or the channel opens) means forwarding
	// is permitted — AllowTcpForwarding applies uniformly to direct-tcpip and
	// tcpip-forward, so this one probe suffices for all tunnel types.
	if conn, derr := client.Dial("tcp", "127.0.0.1:1"); derr != nil {
		if isProhibited(derr) {
			return ProbeResult{Outcome: ProbeForwardingDenied, Detail: "AllowTcpForwarding no on the server (direct-tcpip rejected)"}
		}
	} else if conn != nil {
		_ = conn.Close()
	}

	detail := "forwarding permitted"
	if cfg.Type == "remote" && isNonLoopbackBind(cfg.RemoteListenAddr()) {
		detail = "forwarding permitted; -R non-loopback bind assumed (GatewayPorts not verifiable client-side — if the public address is unreachable, set `GatewayPorts yes` on the server)"
	}
	return ProbeResult{Outcome: ProbeHealthy, Detail: detail}
}

// classifyProbeDialErr maps a dialOnce error to a ProbeOutcome.
func classifyProbeDialErr(err error) ProbeResult {
	if err == nil {
		return ProbeResult{Outcome: ProbeHealthy}
	}
	var uhe *unknownHostError
	if errors.As(err, &uhe) {
		return ProbeResult{Outcome: ProbeHostKey, Detail: uhe.Error()}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "auth failed"):
		return ProbeResult{Outcome: ProbeAuthFailed, Detail: msg}
	case strings.Contains(msg, "no ssh auth method available"):
		return ProbeResult{Outcome: ProbeAuthUnavailable, Detail: msg}
	case strings.Contains(msg, "connect refused"),
		strings.Contains(msg, "connect timeout"),
		strings.Contains(msg, "no such host"):
		return ProbeResult{Outcome: ProbeConnectFailed, Detail: msg}
	}
	return ProbeResult{Outcome: ProbeOther, Detail: msg}
}

// isProhibited reports whether err is a direct-tcpip channel-open rejection
// with ssh.Prohibited — the sshd signature of `AllowTcpForwarding no`. Shared
// by ProbeForwarding and the runtime -L/-D dial hints.
func isProhibited(err error) bool {
	var oce *ssh.OpenChannelError
	return errors.As(err, &oce) && oce.Reason == ssh.Prohibited
}

// dialHintMsg returns the log message for a -L/-D client.Dial failure, with an
// AllowTcpForwarding hint appended when the server rejected the direct-tcpip
// open (ssh.Prohibited). Used by the runtime dial paths so a -L/-D tunnel that
// fails because of `AllowTcpForwarding no` says so in the `l` log screen
// instead of a bare "dial remote failed".
func dialHintMsg(prefix string, err error) string {
	if isProhibited(err) {
		return prefix + ": direct-tcpip rejected (AllowTcpForwarding no on the server?)"
	}
	return prefix
}

// isNonLoopbackBind reports whether addr (a RemoteListenAddr such as ":16379",
// "*:16379", "0.0.0.0:16379", or "127.0.0.1:16379") asks the server to bind a
// non-loopback interface — the case where GatewayPorts matters.
func isNonLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	switch host {
	case "", "*", "0.0.0.0", "::", "[::]":
		return true
	}
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
		return true
	}
	return false
}
