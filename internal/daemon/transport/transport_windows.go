//go:build windows

package transport

import (
	"context"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func init() { Default = pipeTransport{} }

// pipeTransport serves IPC over a Windows named pipe (\\.\pipe\portato). A pipe
// has no socket file, so there is no directory to create or chmod; access is
// guarded by the pipe security descriptor below plus the Phase 18 IPC bearer
// token layered on top.
type pipeTransport struct{}

func (pipeTransport) Listen(addr string) (net.Listener, error) {
	sddl, err := pipeSDDL()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(addr, &winio.PipeConfig{SecurityDescriptor: sddl})
}

func (pipeTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, addr)
}

// pipeSDDL builds the pipe's security descriptor: full access for SYSTEM and
// the Administrators group, and for the process token's user (the pipe owner).
//
// Why an explicit DACL: a nil SecurityDescriptor makes CreateNamedPipe build
// the default DACL from the *creating token*. Under the Phase-47 SCM service
// that token belongs to the service logon session (session 0): the resulting
// DACL grants Administrators/SYSTEM but NOT the same user's interactive logon
// session, so an unelevated `portato list` from the desktop gets access
// denied. Granting the user SID explicitly makes the pipe reachable from both
// the service and the interactive session of the same account (matching the
// unix socket's 0600-owner semantics); the Phase 18 bearer token still gates
// the protocol itself.
func pipeSDDL() (string, error) {
	tok := windows.GetCurrentProcessToken()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("resolve process user for pipe security: %w", err)
	}
	return "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + tu.User.Sid.String() + ")", nil
}
