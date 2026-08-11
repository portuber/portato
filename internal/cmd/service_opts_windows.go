//go:build windows

package cmd

import (
	"path/filepath"
	"strings"
)

// stableBinaryPath rewrites a Scoop version-pinned binary path to the stable
// `current` junction so the autostart entry survives `scoop update portato`.
// Scoop lays out `%USERPROFILE%\scoop\apps\portato\<version>\portato.exe` and
// keeps a `current` directory junction pointing at the active version; the
// version directory is pruned on update, so a path captured against <version>
// silently breaks. Rewriting the version segment to `current` is path-shape
// only (no registry / scoop-state reads) and only matches the Scoop layout, so
// non-Scoop installs and explicit versioned paths elsewhere are untouched.
func stableBinaryPath(bin string) string {
	sep := string(filepath.Separator)
	parts := strings.Split(filepath.Clean(bin), sep)
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] == "scoop" && parts[i+1] == "apps" && parts[i+2] == "portato" && parts[i+3] != "current" {
			parts[i+3] = "current"
			return strings.Join(parts, sep)
		}
	}
	return bin
}
