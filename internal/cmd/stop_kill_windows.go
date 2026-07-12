//go:build windows

package cmd

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// stopKill terminates the daemon process. Windows has no SIGTERM, so the
// process is terminated directly (OpenProcess + TerminateProcess). This is the
// documented Phase 17 limitation: the daemon's own graceful StopAll path is not
// triggered by `portato stop` on Windows the way a trapped SIGTERM is on unix.
var stopKill = func(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open daemon process: %w", err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("terminate daemon process: %w", err)
	}
	return nil
}
