//go:build darwin

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// darwinInstaller manages a per-user launchd LaunchAgent. SPEC §13.
type darwinInstaller struct {
	exec execFunc
}

func newInstaller() Installer { return &darwinInstaller{exec: realExec} }

func (d *darwinInstaller) Install(o Options) (string, error) {
	label := normLabel(o.Label)
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	plist := plistPath(home, label)
	domain := domainTarget()
	job := domain + "/" + label

	// Idempotency: if already loaded, drop it before bootstrapping the fresh
	// definition (bootstrap fails on an already-loaded label).
	_, _ = d.exec("launchctl", "bootout", job)

	stdoutLog, stderrLog := logPaths(home)
	body := renderPlist(label, o.BinaryPath, o.ConfigPath, stdoutLog, stderrLog)
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return "", fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write plist: %w", err)
	}
	if _, err := d.exec("launchctl", "bootstrap", domain, plist); err != nil {
		return "", fmt.Errorf("load agent: %w", err)
	}
	return plist, nil
}

func (d *darwinInstaller) Uninstall(o Options) error {
	label := normLabel(o.Label)
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	_, _ = d.exec("launchctl", "bootout", domainTarget()+"/"+label)
	plist := plistPath(home, label)
	if exists(plist) {
		if err := os.Remove(plist); err != nil {
			return fmt.Errorf("remove plist: %w", err)
		}
	}
	return nil
}

func (d *darwinInstaller) Status(o Options) (string, error) {
	label := normLabel(o.Label)
	out, err := d.exec("launchctl", "print", domainTarget()+"/"+label)
	if err != nil {
		return "not loaded", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func plistPath(home, label string) string {
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func logPaths(home string) (out, errPath string) {
	return filepath.Join(home, "Library", "Logs", "portato.log"),
		filepath.Join(home, "Library", "Logs", "portato.err.log")
}

// domainTarget is the per-user GUI domain launchd uses on modern macOS
// (10.10+): gui/<uid>.
func domainTarget() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

// renderPlist builds the LaunchAgent plist. launchd does not expand ~ in
// StandardOutPath/StandardErrorPath, so all paths are absolute.
func renderPlist(label, binary, config, outPath, errPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, xml(label), xml(binary), xml(config), xml(outPath), xml(errPath))
}

// xml escapes the few characters that would break a plist string value.
func xml(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
