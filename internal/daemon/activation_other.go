//go:build !linux

package daemon

import "net"

// activationListeners is a no-op off Linux: socket activation is a systemd
// mechanism (LISTEN_FDS). launchd's equivalent needs a libc call
// (launch_activate_socket_fd) which would require cgo, incompatible with the
// pure-Go single binary, so macOS stays bind-on-start. Phase 22.
func activationListeners() ([]net.Listener, error) { return nil, nil }
