//go:build unix

package forward

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strings"
)

// dialAgent opens a connection to the ssh-agent. On unix the agent is reached
// via the SSH_AUTH_SOCK unix-domain socket (set by the agent / the desktop
// session). It returns (nil, false) when no agent is configured or the dial
// fails, so the caller falls back to identity-key auth. Windows has its own
// named-pipe variant (agentdial_windows.go); the ctx is unused on unix but kept
// for signature parity.
func dialAgent(_ context.Context, log *slog.Logger) (net.Conn, bool) {
	sock := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK"))
	if sock == "" {
		return nil, false
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		log.Warn("dial ssh-agent failed; falling back to identity", "err", err)
		return nil, false
	}
	return conn, true
}
