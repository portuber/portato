//go:build windows

package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// ServiceName is the SCM service / display name. It matches the Phase-17
// HKCU Run-key value name so a `--legacy-runkey` install and an SCM install
// share the same user-facing identifier.
const ServiceName = "Portato"

// scmServicePath is the registry path SCM stores the service record under; it
// is the SCM equivalent of the launchd plist / systemd unit path and is what
// `portato install` prints on success.
const scmServicePath = `HKLM\SYSTEM\CurrentControlSet\Services\` + ServiceName

// runKeyPath / runValueName are the per-user Phase-17 autostart location. Kept
// as the `--legacy-runkey` fallback for environments where SCM service creation
// is blocked by GPO / AV. SPEC §13.
const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Portato"
)

// windowsInstaller manages the Portato autostart on Windows. By default it
// registers a real Service Control Manager service (Phase 47); with
// Options.Legacy it falls back to the Phase-17 HKCU Run-key entry.
type windowsInstaller struct {
	scm scmAPI
}

func newInstaller() Installer { return &windowsInstaller{scm: realSCM{}} }

// Install registers the autostart. SCM (default) creates the service, sets
// restart-on-failure recovery, and starts it immediately so `portato list`
// works the moment install returns — closing the macOS/Linux parity gap. The
// --legacy-runkey path keeps the Phase-17 behaviour (Run key, no immediate
// start) for locked-down environments.
func (w windowsInstaller) Install(o Options) (string, error) {
	if o.Legacy {
		return w.runKeyInstall(o)
	}
	return w.scmInstall(o)
}

// Uninstall removes the autostart. Under SCM it stops and deletes the service
// (idempotent: a missing service is a no-op) and also cleans up any stray
// legacy Run-key entry. Under --legacy-runkey it only removes the Run value.
func (w windowsInstaller) Uninstall(o Options) error {
	if o.Legacy {
		return w.runKeyUninstall()
	}
	if err := w.scmUninstall(); err != nil {
		return err
	}
	// Best-effort: a host that migrated from --legacy-runkey to SCM may still
	// carry the old Run value; clear it so doctor reports a clean state.
	_ = w.runKeyUninstall()
	return nil
}

// Status reports the autostart state. It prefers the SCM service when installed
// and falls back to the Run-key entry so both mechanisms are covered.
func (w windowsInstaller) Status(o Options) (string, error) {
	if SCMInstalled() {
		return w.scmStatus()
	}
	return w.runKeyStatus()
}

func (w windowsInstaller) scmInstall(o Options) (string, error) {
	cfg := mgr.Config{
		ServiceType:      windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:        mgr.StartAutomatic,
		ErrorControl:     mgr.ErrorNormal,
		DisplayName:      ServiceName,
		Description:      "Portato — SSH port-forwarding manager (daemon)",
		ServiceStartName: localSystemServiceStartName(o.Account),
		Password:         o.Password,
		DelayedAutoStart: true,
		Dependencies:     []string{"Tcpip"},
	}
	args := []string{"daemon"}
	if o.ConfigPath != "" {
		args = append(args, "--config", o.ConfigPath)
	}
	s, err := w.scm.create(ServiceName, o.BinaryPath, cfg, args)
	if err != nil {
		return "", err
	}
	defer s.close()
	// Restart on failure after 30s with a 1-minute reset window: the Windows
	// equivalent of launchd KeepAlive / systemd Restart=on-failure.
	if err := s.setRecoveryActions(
		[]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 30 * time.Second}},
		60,
	); err != nil {
		return "", fmt.Errorf("set recovery actions: %w", err)
	}
	// Start now — parity with `launchctl bootstrap` (RunAtLoad) and
	// `systemctl --user enable --now`.
	if err := s.start(); err != nil {
		return "", fmt.Errorf("start service: %w", err)
	}
	return scmServicePath, nil
}

func (w windowsInstaller) scmUninstall() error {
	s, err := w.scm.open(ServiceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil // nothing to remove — idempotent no-op
		}
		return err
	}
	defer s.close()
	// Best-effort graceful stop before delete: a deleted-but-running service
	// lingers until the process exits. Ignore a "not running" / control error.
	if st, qerr := s.query(); qerr == nil && stoppable(st.State) {
		_, _ = s.control(svc.Stop)
	}
	if err := s.delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func (w windowsInstaller) scmStatus() (string, error) {
	s, err := w.scm.open(ServiceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return "not installed", nil
		}
		return "", err
	}
	defer s.close()
	st, err := s.query()
	if err != nil {
		return "", err
	}
	out := stateLabel(st.State)
	if st.State == svc.Stopped && st.Win32ExitCode != 0 && st.Win32ExitCode != uint32(windows.NO_ERROR) {
		out += fmt.Sprintf(" (exit %d)", st.Win32ExitCode)
	}
	return out, nil
}

// stateLabel maps an SCM state to the human-readable token doctor / status print.
func stateLabel(s svc.State) string {
	switch s {
	case svc.Stopped:
		return "stopped"
	case svc.Running:
		return "running"
	case svc.StartPending:
		return "starting"
	case svc.StopPending:
		return "stopping"
	case svc.Paused:
		return "paused"
	default:
		return fmt.Sprintf("state %d", uint32(s))
	}
}

// localSystemServiceStartName normalizes an account name for mgr.Config. SCM
// uses an empty ServiceStartName for LocalSystem; explicit "LocalSystem" /
// "NT AUTHORITY\SYSTEM" (any case) collapse to "".
func localSystemServiceStartName(account string) string {
	switch strings.ToLower(strings.TrimSpace(account)) {
	case "", "localsystem", `nt authority\system`:
		return ""
	}
	return account
}

// IsLocalSystemAccount reports whether the given account selects LocalSystem
// (no password required, but no user-profile access either). Exported so the
// install command can skip the password prompt and the success caveat for the
// system account.
func IsLocalSystemAccount(account string) bool {
	return localSystemServiceStartName(account) == ""
}

// ---- Phase-17 HKCU Run-key fallback (--legacy-runkey) ----

func (windowsInstaller) runKeyInstall(o Options) (string, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("open HKCU Run key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(runValueName, runCommand(o)); err != nil {
		return "", fmt.Errorf("set Run value: %w", err)
	}
	return `HKCU\` + runKeyPath, nil
}

func (windowsInstaller) runKeyUninstall() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	if err := key.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("remove Run value: %w", err)
	}
	return nil
}

func (windowsInstaller) runKeyStatus() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "Run key unreadable (" + err.Error() + ")", nil
	}
	defer key.Close()
	val, _, err := key.GetStringValue(runValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return "not installed", nil
		}
		return "Run value unreadable (" + err.Error() + ")", nil
	}
	return "installed (Run key): " + val, nil
}

// runCommand builds the command line the Run key launches at login:
// `"<binary>" daemon --config "<config>"`. Paths are quoted so a location with
// spaces (e.g. Program Files) survives the shell's command-line parse.
func runCommand(o Options) string {
	return fmt.Sprintf(`"%s" daemon --config "%s"`, o.BinaryPath, o.ConfigPath)
}
