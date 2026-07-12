//go:build windows

package tui

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachedSysProcAttr returns the SysProcAttr that detaches the spawned daemon
// from the standalone on Windows: a new process group with no inherited
// console (DETACHED_PROCESS).
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
