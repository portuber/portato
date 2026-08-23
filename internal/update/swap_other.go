//go:build !windows

package update

import (
	"fmt"
	"os"
)

// SwapBinary replaces the binary at path with newBinary (staged next to it)
// using two atomic renames: current -> path+".old" (the one-level rollback
// copy), newBinary -> path. A previous .old is replaced — one level of
// rollback, not an archive. The file mode is taken from the current binary.
// On unix a running process keeps its old inode, so a live daemon is safe;
// the caller prints the restart hint.
func SwapBinary(path, newBinary string) error {
	mode := os.FileMode(0o755)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(newBinary, mode); err != nil {
		return fmt.Errorf("update: stage mode: %w", err)
	}
	old := path + ".old"
	if _, err := os.Stat(old); err == nil {
		if err := os.Remove(old); err != nil {
			return fmt.Errorf("update: drop previous backup: %w", err)
		}
	}
	if err := os.Rename(path, old); err != nil {
		return fmt.Errorf("update: backup current binary: %w", err)
	}
	if err := os.Rename(newBinary, path); err != nil {
		// Best-effort restore: the user is not left without a binary.
		_ = os.Rename(old, path)
		return fmt.Errorf("update: install new binary: %w", err)
	}
	return nil
}

// RollbackCommand is the documented one-liner restoring the previous
// binary from the one-level backup.
func RollbackCommand(path string) string {
	return "mv " + path + ".old " + path
}
