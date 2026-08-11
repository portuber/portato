package cmd

// Windows-only install/uninstall flag values (Phase 47). Registered by the
// build-tagged registerWindows*Flags helpers in service_flags_windows.go; on
// darwin/linux (service_flags_other.go) they are never registered and stay
// zero-valued, so the installers there behave byte-identically.
var (
	serviceAccount string
	passwordFile   string
	legacyRunKey   bool
)
