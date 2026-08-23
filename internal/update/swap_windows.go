//go:build windows

package update

import (
	"fmt"
	"os"
	"path/filepath"
)

// SwapBinary on Windows cannot replace a running .exe. It stages the new
// binary as path+".new" and returns a "restart to finish" instruction —
// the next launch (applyStagedSwap in internal/cmd, the Phase-47 pre-cobra
// precedent) completes the rename dance when the old file is not held.
func SwapBinary(path, newBinary string) error {
	mode := os.FileMode(0o755)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(newBinary, mode); err != nil {
		return fmt.Errorf("update: stage mode: %w", err)
	}
	staged := path + ".new"
	if err := os.Rename(newBinary, staged); err != nil {
		return fmt.Errorf("update: stage new binary: %w", err)
	}
	return nil
}

// CompleteStagedSwap finishes a staged swap at launch time: with no live
// binary holding path anymore (this early, nothing has started), move the
// current binary to .old and the staged .new into place. A no-op when
// nothing is staged.
func CompleteStagedSwap(path string) {
	staged := path + ".new"
	if _, err := os.Stat(staged); err != nil {
		return
	}
	old := path + ".old"
	_ = os.Remove(old)
	if err := os.Rename(path, old); err != nil {
		return
	}
	if err := os.Rename(staged, path); err != nil {
		_ = os.Rename(old, path)
		_ = os.Remove(staged)
	}
}

// StagedSwapPending reports whether a staged swap waits for this launch.
func StagedSwapPending(path string) bool {
	_, err := os.Stat(filepath.Clean(path) + ".new")
	return err == nil
}

// RollbackCommand is the documented one-liner restoring the previous
// binary from the one-level backup.
func RollbackCommand(path string) string {
	return "move /Y " + path + ".old " + path
}
