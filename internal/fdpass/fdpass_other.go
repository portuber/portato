//go:build !unix

package fdpass

import (
	"errors"
	"net"
)

// Send is unsupported off unix: SCM_RIGHTS is a unix facility (Phase 17 covers
// Windows). The hand-off falls back to the Phase 5 close+rebind path here.
func Send(_ *net.UnixConn, _ []Offer) error {
	return errors.New("fdpass: SCM_RIGHTS unavailable on this platform")
}

// Recv is unsupported off unix: SCM_RIGHTS is a unix facility (Phase 17 covers
// Windows).
func Recv(_ *net.UnixConn) (map[string]net.Listener, error) {
	return nil, errors.New("fdpass: SCM_RIGHTS unavailable on this platform")
}
