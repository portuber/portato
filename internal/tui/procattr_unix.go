//go:build unix

package tui

import "syscall"

// detachedSysProcAttr returns the SysProcAttr that detaches the spawned daemon
// from the standalone's session/process group: a new session (Setsid) on unix.
// Windows overrides this (procattr_windows.go).
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
