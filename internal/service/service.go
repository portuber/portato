package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DefaultLabel is the launchd job Label (and reverse-DNS identifier) used when
// the caller does not override it via Options.Label. SPEC §13.
const DefaultLabel = "dev.portato.daemon"

// Options describes a single install/uninstall invocation. Every call rebuilds
// the artefact from these values (uninstall is a separate process, so Options
// is never carried in memory between commands).
type Options struct {
	// BinaryPath is the absolute path to the portato executable that the
	// service manager launches (os.Executable() at the call site).
	BinaryPath string
	// ConfigPath is the absolute path to the config file passed to the daemon
	// via `--config`.
	ConfigPath string
	// Label is the launchd job Label. On Linux it only affects the unit
	// Description (the unit file name stays portato.service, as systemd
	// forbids dots).
	Label string
}

// Installer abstracts the per-OS autostart mechanism. New returns the
// build-tagged implementation for the current OS (launchd on darwin,
// systemd --user on linux, an "unsupported" stub elsewhere).
type Installer interface {
	// Install writes the service definition and loads/enables it. It returns
	// the absolute path of the written plist/unit (for display) and is
	// idempotent: a repeated install overwrites and reloads instead of failing.
	Install(Options) (string, error)
	// Uninstall stops and removes the service definition.
	Uninstall(Options) error
	// Status returns a human-readable status line from the service manager.
	Status(Options) (string, error)
}

// New returns the Installer for the current OS.
func New() Installer { return newInstaller() }

// execFunc runs a command and returns its combined stdout+stderr. It is a seam
// so tests can assert the exact command sequence without touching launchd /
// systemctl. The production default is realExec.
type execFunc func(name string, args ...string) ([]byte, error)

// realExec is the production execFunc: it captures combined output and wraps
// non-zero exits with the captured output for a readable error.
func realExec(name string, args ...string) ([]byte, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, joinArgs(args), err, string(out))
	}
	return out, nil
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// exists reports whether path is present on disk.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// normLabel falls back to DefaultLabel when label is blank/whitespace.
func normLabel(label string) string {
	if strings.TrimSpace(label) == "" {
		return DefaultLabel
	}
	return label
}

// EffectiveLabel returns the label that will actually be used for a given raw
// value (DefaultLabel when blank). Callers that want to display or store the
// resolved label should use this instead of the raw flag value.
func EffectiveLabel(label string) string { return normLabel(label) }
