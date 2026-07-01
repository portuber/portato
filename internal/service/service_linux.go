//go:build linux

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

// linuxUnit is the systemd --user service name; linuxSocketUnit is the matching
// socket-activation unit. systemd forbids dots in unit names.
const (
	linuxUnit       = "portato.service"
	linuxSocketUnit = "portato.socket"
)

// linuxInstaller manages a systemd --user service. SPEC §13.
type linuxInstaller struct {
	exec execFunc
}

func newInstaller() Installer { return &linuxInstaller{exec: realExec} }

func (l *linuxInstaller) Install(o Options) (string, error) {
	label := normLabel(o.Label)
	uid := os.Getuid()
	unit := unitPath()
	socket := socketUnitPath()
	serviceBody := renderUnit(label, o.BinaryPath, o.ConfigPath)
	socketBody := renderSocketUnit(uid)

	existed := exists(unit)
	if err := os.MkdirAll(filepath.Dir(unit), 0o700); err != nil {
		return "", fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(unit, []byte(serviceBody), 0o644); err != nil {
		return "", fmt.Errorf("write unit: %w", err)
	}
	if err := os.WriteFile(socket, []byte(socketBody), 0o644); err != nil {
		return "", fmt.Errorf("write socket unit: %w", err)
	}
	if _, err := l.exec("systemctl", "--user", "daemon-reload"); err != nil {
		return "", fmt.Errorf("daemon-reload: %w", err)
	}
	// Enable + start the socket unit so systemd owns the IPC socket and
	// socket-activates the service. The service Requires+After the socket, so
	// when it starts systemd hands it the listening fd (LISTEN_FDS) instead of
	// the daemon binding its own; on a boot start the service still runs (manages
	// tunnels), it just serves on the inherited socket. Phase 22.
	if _, err := l.exec("systemctl", "--user", "enable", "--now", linuxSocketUnit); err != nil {
		return "", fmt.Errorf("enable socket unit: %w", err)
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
	_, _ = l.exec("systemctl", "--user", "disable", "--now", linuxSocketUnit)
	_, _ = l.exec("systemctl", "--user", "disable", "--now", linuxUnit)
	unit := unitPath()
	socket := socketUnitPath()
	for _, p := range []string{unit, socket} {
		if exists(p) {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("remove %s: %w", p, err)
			}
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

func socketUnitPath() string {
	return filepath.Join(xdg.ConfigHome, "systemd", "user", linuxSocketUnit)
}

// renderUnit builds the service unit. It Requires + follows the socket unit so
// systemd hands the daemon the pre-bound IPC socket via LISTEN_FDS (socket
// activation, Phase 22); the daemon serves on that fd instead of binding.
func renderUnit(label, binary, config string) string {
	return fmt.Sprintf(`[Unit]
Description=%s — SSH port-forwarding manager
Requires=%s
After=network.target %s

[Service]
ExecStart=%s daemon --config %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, desc(label), linuxSocketUnit, linuxSocketUnit, binary, config)
}

// renderSocketUnit builds portato.socket: systemd binds the IPC socket at the
// canonical runtime path and socket-activates the service on first connection
// (or at boot, since the service is WantedBy=default.target and Requires this
// socket). SocketMode 0600 keeps it owner-only (SPEC §6). Phase 22.
func renderSocketUnit(uid int) string {
	return fmt.Sprintf(`[Unit]
Description=Portato IPC socket (socket activation)

[Socket]
ListenStream=/run/user/%d/portato-%d.sock
SocketMode=0600

[Install]
WantedBy=sockets.target
`, uid, uid)
}

func desc(label string) string {
	if strings.TrimSpace(label) == "" {
		return "portato"
	}
	return "portato (" + label + ")"
}
