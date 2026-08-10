//go:build !windows

package main

import "github.com/portuber/portato/internal/cmd"

// run executes the cobra tree. On non-Windows platforms there is no Service
// Control Manager to dispatch, so this is a straight passthrough.
func run() error {
	return cmd.Execute()
}
