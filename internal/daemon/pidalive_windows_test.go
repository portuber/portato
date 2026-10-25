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
// still works. PIDs on Windows are a multiple of 4; the runner may have live
// processes in any single slot, so scan candidates until one reports dead
// instead of asserting on the first.
func TestPidAlive_NonexistentPID(t *testing.T) {
	self := uint32(os.Getpid())
	for pid := self + 60; pid < self+6000; pid += 4 {
		if pidAlive(int(pid)) {
			continue
		}
		return
	}
	t.Fatalf("every pid in [%d,%d) reported alive; pidAlive never hit the nonexistent-pid path", self+60, self+6000)
}
