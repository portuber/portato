//go:build !windows

package cmd

// scmServiceInstalled reports whether the Portato SCM service is registered
// (Windows only; the update apply refuses to swap a service-held binary).
// No-op false on other platforms.
var scmServiceInstalled = func() bool { return false }
