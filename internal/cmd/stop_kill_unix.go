//go:build unix

package cmd

import "syscall"

// stopKill terminates the daemon process with SIGTERM — the graceful signal the
// daemon traps for `portato stop`. Windows overrides this (stop_kill_windows.go)
// since it has no SIGTERM equivalent.
var stopKill = func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
