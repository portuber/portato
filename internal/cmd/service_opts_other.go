//go:build !windows

package cmd

// stableBinaryPath is a passthrough on non-Windows (the Scoop drift fix only
// applies to Scoop's Windows layout).
func stableBinaryPath(bin string) string { return bin }
