//go:build unix

package daemon

import (
	"errors"
	"syscall"
)

// pidAlive reports whether the given PID is an existing process. EPERM (the
// process exists but is not ours) also counts as alive. Windows overrides this
// (pidalive_windows.go) with an OpenProcess/GetExitCodeProcess check.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
