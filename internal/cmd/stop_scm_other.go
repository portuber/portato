//go:build !windows

package cmd

// stopViaSCM is a no-op on non-Windows (no Service Control Manager); stopRunE
// falls straight through to the SIGTERM-by-PID path.
var stopViaSCM = func() (bool, error) { return false, nil }
