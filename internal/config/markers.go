package config

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// The Phase-48 first-run import offer is gated by two empty marker files in
// the daemon state dir (xdg.StateHome/portato/, next to the log files):
//
//	fresh_install  — written when EnsureExample creates config.yaml, i.e.
//	                 the config was bootstrapped by portato, not written by
//	                 the user (who may be upgrading from a pre-import era).
//	import_offered — written when the offer is consumed: shown and answered,
//	                 or skipped because there was nothing to import. After
//	                 it exists the offer never repeats.
//
// The pair exists so a non-interactive first run (the daemon creating the
// config) does not consume the one-time offer — the next interactive launch
// still gets it.

// markersStateDir resolves the marker directory. PORTATO_STATE_HOME overrides
// the base (the test seam — xdg paths are initialized at package init, so
// t.Setenv("XDG_STATE_HOME") alone cannot redirect; tests point this at a
// temp dir so marker writes never touch the real state directory).
var markersStateDir = func() string {
	base := xdg.StateHome
	if dir := os.Getenv("PORTATO_STATE_HOME"); dir != "" {
		base = dir
	}
	return filepath.Join(base, "portato")
}

const (
	freshInstallMarker  = "fresh_install"
	importOfferedMarker = "import_offered"
)

// MarkFreshInstall records that this install's config.yaml was created by
// portato itself (EnsureExample) — the signal the first-run import offer
// keys on. Best-effort: a failure to write never blocks config creation.
func MarkFreshInstall() { touchMarker(freshInstallMarker) }

// FreshInstall reports whether the config was bootstrapped by portato (the
// fresh_install marker is present).
func FreshInstall() bool { return markerExists(freshInstallMarker) }

// MarkImportOffered consumes the one-time import offer: it already happened
// (shown and answered, or nothing to import) and must never repeat.
// Best-effort.
func MarkImportOffered() { touchMarker(importOfferedMarker) }

// ImportOffered reports whether the import offer already happened.
func ImportOffered() bool { return markerExists(importOfferedMarker) }

func touchMarker(name string) {
	dir := markersStateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name), nil, 0o600)
}

func markerExists(name string) bool {
	_, err := os.Stat(filepath.Join(markersStateDir(), name))
	return err == nil
}
