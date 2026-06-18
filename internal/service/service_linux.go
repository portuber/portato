//go:build linux

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

// linuxUnit is the systemd --user unit name (no dots — systemd forbids them).
const linuxUnit = "portato.service"

// linuxInstaller manages a systemd --user service. SPEC §13.
type linuxInstaller struct {
	exec execFunc
}

func newInstaller() Installer { return &linuxInstaller{exec: realExec} }

func (l *linuxInstaller) Install(o Options) (string, error) {
	label := normLabel(o.Label)
	unit := unitPath()
	body := renderUnit(label, o.BinaryPath, o.ConfigPath)

	existed := exists(unit)
	if err := os.MkdirAll(filepath.Dir(unit), 0o700); err != nil {
		return "", fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(unit, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write unit: %w", err)
	}
	if _, err := l.exec("systemctl", "--user", "daemon-reload"); err != nil {
		return "", fmt.Errorf("daemon-reload: %w", err)
	}
	if existed {
		// Idempotency: a changed unit file only takes effect after a restart.
		if _, err := l.exec("systemctl", "--user", "restart", linuxUnit); err != nil {
			return "", fmt.Errorf("restart unit: %w", err)
		}
	} else {
		if _, err := l.exec("systemctl", "--user", "enable", "--now", linuxUnit); err != nil {
			return "", fmt.Errorf("enable unit: %w", err)
		}
	}
	// Best-effort: enables the user manager outside of an active session.
	// Required for the daemon to survive logout (SPEC §13). Ignored on error
	// since some systems need polkit; verifiable via `loginctl show-user`.
	_, _ = l.exec("loginctl", "enable-linger")
	return unit, nil
}

func (l *linuxInstaller) Uninstall(o Options) error {
	// disable --now is a no-op (non-fatal) when the unit is not loaded.
	_, _ = l.exec("systemctl", "--user", "disable", "--now", linuxUnit)
	unit := unitPath()
	if exists(unit) {
		if err := os.Remove(unit); err != nil {
			return fmt.Errorf("remove unit: %w", err)
		}
	}
	if _, err := l.exec("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

func (l *linuxInstaller) Status(o Options) (string, error) {
	out, err := l.exec("systemctl", "--user", "status", linuxUnit)
	if err != nil {
		return "not loaded", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func unitPath() string {
	return filepath.Join(xdg.ConfigHome, "systemd", "user", linuxUnit)
}

func renderUnit(label, binary, config string) string {
	return fmt.Sprintf(`[Unit]
Description=%s — SSH port-forwarding manager
After=network.target

[Service]
ExecStart=%s daemon --config %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, desc(label), binary, config)
}

func desc(label string) string {
	if strings.TrimSpace(label) == "" {
		return "portato"
	}
	return "portato (" + label + ")"
}
