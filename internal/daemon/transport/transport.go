// Package transport abstracts the daemon IPC transport behind a build-tagged
// seam: a unix-domain socket on darwin/linux and a named pipe on Windows
// (Phase 17). The daemon server, the IPC client and the discovery healthz probe
// all go through it, so none of them knows which transport is in use.
//
// The address passed to Listen/Dial is whatever the daemon path resolver
// returned: a socket file path on unix, a `\\.\pipe\portato` pipe name on
// Windows. Exactly one implementation is compiled in per platform
// (transport_unix.go / transport_windows.go), and each sets Default in init.
package transport

import (
	"context"
	"net"
)

// Transport is the IPC transport shared by the daemon and its clients.
type Transport interface {
	// Listen binds the daemon's IPC address and returns the listener http.Serve
	// consumes. On unix this creates the socket directory, binds the unix-domain
	// socket and chmods it 0600; on Windows it creates the named pipe.
	Listen(addr string) (net.Listener, error)
	// Dial opens a connection to the daemon's IPC address (used by clients and
	// the discovery healthz probe).
	Dial(ctx context.Context, addr string) (net.Conn, error)
}

// Default is the platform transport, set by the build-tagged implementation.
// Exactly one init (transport_unix.go or transport_windows.go) assigns it.
var Default Transport
