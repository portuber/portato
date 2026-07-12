//go:build unix

package transport

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func init() { Default = unixTransport{} }

// unixTransport serves IPC over a unix-domain socket file (darwin/linux).
type unixTransport struct{}

func (unixTransport) Listen(addr string) (net.Listener, error) {
	if dir := filepath.Dir(addr); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create socket dir: %w", err)
		}
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(addr)
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return ln, nil
}

func (unixTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", addr)
}
