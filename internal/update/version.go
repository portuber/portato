package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed strict vX.Y.Z triple (VERSIONING.md: portato tags are
// always stable vX.Y.Z, never pre-releases, so three integers are the whole
// grammar). The zero Version is not a meaningful version; use ParseVersion,
// whose ok=false covers dev/snapshot builds ("dev", "unknown", "*-next")
// that cannot be compared to a release.
type Version struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion parses "vX.Y.Z" or "X.Y.Z" (the embedded binary version comes
// from goreleaser without the "v"; GitHub tag_name carries it). Exactly
// three numeric components — anything else (a pre-release suffix, fewer or
// more components, non-digits, overflow) is rejected.
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var v Version
	for i, p := range parts {
		if !isDigits(p) {
			return Version{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, false
		}
		switch i {
		case 0:
			v.Major = n
		case 1:
			v.Minor = n
		case 2:
			v.Patch = n
		}
	}
	return v, true
}

// String renders the canonical v-prefixed form.
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns -1, 0 or +1 as a is older than, equal to, or newer than b.
func (a Version) Compare(b Version) int {
	if a.Major != b.Major {
		return sign(a.Major - b.Major)
	}
	if a.Minor != b.Minor {
		return sign(a.Minor - b.Minor)
	}
	return sign(a.Patch - b.Patch)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// isDigits reports whether s is non-empty and all ASCII digits (rejects a
// sign, so "v1.7.-1" is not a version).
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
