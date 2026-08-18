//go:build windows

package daemon

import (
	"errors"

	"golang.org/x/sys/windows"
)

// stillActive is the exit-code value Windows reports for a running process
// (STILL_ACTIVE / STATUS_PENDING = 0x103 = 259). x/sys/windows exposes it only
// as the NTStatus-typed STATUS_PENDING, so it is pinned here as a plain uint32.
const stillActive uint32 = 259

// pidAlive reports whether the given PID is an existing, running process.
// OpenProcess fails with ERROR_INVALID_PARAMETER when the PID does not exist.
// An ERROR_ACCESS_DENIED failure means the process EXISTS but the caller may
// not open it (e.g. the session-0 SCM service process seen from an
// unelevated interactive session) — that is reported as alive: "cannot
// interrogate" must not be misread as "dead", or discovery-marker cleanup
// would delete a live daemon's marker (the Phase-48-verification finding:
// daemon.socket vanished after boot while the service was serving fine).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
