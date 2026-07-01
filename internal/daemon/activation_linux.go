//go:build linux

package daemon

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// activationListeners returns socket-activated listeners handed to the process
// by systemd via LISTEN_FDS (the fds start at 3, the SD_LISTEN_FDS_START
// convention). It is the systemd socket-activation entry point (Phase 22): when
// present, Start serves on the first one instead of binding its own socket.
// Returns nil (no activation) when LISTEN_PID is unset or does not name this
// process, so the daemon falls back to self-binding outside systemd.
func activationListeners() ([]net.Listener, error) {
	pidStr := os.Getenv("LISTEN_PID")
	if pidStr == "" {
		return nil, nil
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid != os.Getpid() {
		return nil, nil
	}
	nfdsStr := os.Getenv("LISTEN_FDS")
	if nfdsStr == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(nfdsStr)
	if err != nil || n <= 0 {
		return nil, nil
	}
	// Consume the activation env so it is not inherited by children (and so a
	// re-entrant check in the same process does not double-wrap the fds).
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")

	listeners := make([]net.Listener, 0, n)
	for i := 0; i < n; i++ {
		fd := 3 + i
		// systemd passes the fds without FD_CLOEXEC; set it so a fork/exec (the
		// hand-off path) does not leak them into children. CloseOnExec ignores
		// an fcntl error (a bad fd would already have failed FileListener).
		unix.CloseOnExec(fd)
		f := os.NewFile(uintptr(fd), fmt.Sprintf("portato-activation-%d", fd))
		ln, err := net.FileListener(f)
		if err != nil {
			return nil, fmt.Errorf("wrap activation fd %d: %w", fd, err)
		}
		listeners = append(listeners, ln)
	}
	return listeners, nil
}
