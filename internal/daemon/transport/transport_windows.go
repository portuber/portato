//go:build windows

package transport

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func init() { Default = pipeTransport{} }

// pipeTransport serves IPC over a Windows named pipe (\\.\pipe\portato). A pipe
// has no socket file, so there is no directory to create or chmod; access is
// guarded by the Phase 18 IPC bearer token layered on top (a stricter pipe
// security descriptor is a later refinement).
type pipeTransport struct{}

func (pipeTransport) Listen(addr string) (net.Listener, error) {
	return winio.ListenPipe(addr, nil)
}

func (pipeTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, addr)
}
