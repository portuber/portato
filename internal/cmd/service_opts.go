package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/service"
)

// newServiceInstaller is the seam used by install/uninstall. Tests override it
// to assert behaviour without touching launchd/systemctl.
var newServiceInstaller = func() service.Installer { return service.New() }

// executablePath resolves the running binary path. A seam so tests can inject a
// stable path (the real test binary lives in a go-build temp dir, which would
// otherwise trip the "running from go run" warning).
var executablePath = os.Executable

// serviceLabel is shared by the install and uninstall --label flags.
var serviceLabel string

// buildServiceOptions resolves the Options (absolute binary + config paths,
// label) that every service operation needs. It prints a warning (but does not
// abort) when the binary looks like a `go run` artefact, whose path is unstable
// across runs — the plist/unit would point at a tmp dir that disappears.
func buildServiceOptions(cmd *cobra.Command, label string) (service.Options, error) {
	binary, err := executablePath()
	if err != nil {
		return service.Options{}, fmt.Errorf("resolve executable path: %w", err)
	}
	warnIfUnstableBinary(cmd, binary)

	cfgPath, err := resolveConfigPath()
	if err != nil {
		return service.Options{}, fmt.Errorf("resolve config path: %w", err)
	}
	return service.Options{BinaryPath: binary, ConfigPath: cfgPath, Label: service.EffectiveLabel(label)}, nil
}

// warnIfUnstableBinary warns when the running binary lives in the OS temp dir
// or the go-build cache — the hallmark of `go run ./cmd/portato install`.
func warnIfUnstableBinary(cmd *cobra.Command, binary string) {
	if strings.Contains(binary, "go-build") {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: running from `go run`; the binary path is unstable and the autostart entry may break.")
		fmt.Fprintln(cmd.ErrOrStderr(), "         Build with 'make build' and run ./bin/portato install instead.")
		return
	}
	if tmp := os.TempDir(); strings.HasPrefix(binary, tmp) {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: the binary lives in the temp dir; the autostart entry may break when it is cleaned up.")
	}
}

// resolveConfigPath returns the absolute config path: the --config value when
// set (tilde-expanded), otherwise the XDG default.
func resolveConfigPath() (string, error) {
	p := cfgFile
	if strings.TrimSpace(p) == "" {
		return config.DefaultPath(), nil
	}
	return filepath.Abs(expandHome(p))
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
