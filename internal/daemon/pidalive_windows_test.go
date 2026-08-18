//go:build windows

package daemon

import (
	"os"
	"testing"
)

func TestPidAlive_SelfAndBogus(t *testing.T) {
	if !pidAlive(os.Getppid()) {
		t.Errorf("pidAlive(parent %d) = false, want true", os.Getppid())
	}
	// PID 0 is never a process; negative likewise invalid.
	if pidAlive(0) || pidAlive(-1) {
		t.Errorf("pidAlive of an invalid pid should be false")
	}
}

// TestPidAlive_NonexistentPID covers the ERROR_INVALID_PARAMETER path: a PID
// that exists in neither table must report dead, so stale-marker cleanup
// still works. PIDs on Windows are a multiple of 4; scanning a few recent
// multiples that are not the test's own process finds a free one.
func TestPidAlive_NonexistentPID(t *testing.T) {
	self := uint32(os.Getpid())
	for pid := self + 60; pid < self+6000; pid += 4 {
		if pid == self {
			continue
		}
		if pidAlive(int(pid)) {
			t.Errorf("pidAlive(%d) = true for a nonexistent pid, want false", pid)
		}
		break
	}
}
