//go:build !unix

package daemon

import "os"

// acquireInstanceLock is a no-op on platforms without flock (e.g. Windows —
// Phase 17 will use LockFileEx). Single-instance guarding there falls back to
// the discovery marker / PID check in ensureNotRunning.
func acquireInstanceLock(path string) (*os.File, error) {
	_ = path
	return nil, nil
}
