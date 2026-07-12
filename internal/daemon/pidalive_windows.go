//go:build windows

package daemon

import "golang.org/x/sys/windows"

// stillActive is the exit-code value Windows reports for a running process
// (STILL_ACTIVE / STATUS_PENDING = 0x103 = 259). x/sys/windows exposes it only
// as the NTStatus-typed STATUS_PENDING, so it is pinned here as a plain uint32.
const stillActive uint32 = 259

// pidAlive reports whether the given PID is an existing, running process.
// OpenProcess fails with ERROR_INVALID_PARAMETER when the PID does not exist;
// GetExitCodeProcess distinguishes a live process (stillActive) from one that
// has exited.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
