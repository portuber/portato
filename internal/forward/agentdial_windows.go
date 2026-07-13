//go:build windows

package forward

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// sshAgentPipe is the Windows OpenSSH agent's well-known named pipe. There is
// no SSH_AUTH_SOCK equivalent on Windows.
const sshAgentPipe = `\\.\pipe\openssh-ssh-agent`

// agentDialTimeout caps the named-pipe dial so an absent or stalled agent does
// not hang the SSH handshake: on failure dialAgent returns (nil, false) and the
// caller falls back to identity-key auth.
const agentDialTimeout = 2 * time.Second

// dialAgent opens a connection to the ssh-agent. On Windows that is the OpenSSH
// agent's named pipe (\\.\pipe\openssh-ssh-agent). The dial is context-capped
// at agentDialTimeout; on failure it returns (nil, false) and the caller falls
// back to identity-key auth. The returned net.Conn feeds agent.NewClient, which
// is OS-agnostic.
func dialAgent(ctx context.Context, log *slog.Logger) (net.Conn, bool) {
	dctx, cancel := context.WithTimeout(ctx, agentDialTimeout)
	defer cancel()
	conn, err := winio.DialPipeContext(dctx, sshAgentPipe)
	if err != nil {
		log.Warn("dial ssh-agent failed; falling back to identity", "err", err)
		return nil, false
	}
	return conn, true
}
