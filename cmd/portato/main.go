package main

import "os"

// main dispatches to run, which is platform-specific: on Windows it detects a
// Service Control Manager launch before cobra runs (run_windows.go); elsewhere
// it runs the cobra tree directly (run_other.go).
func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}
