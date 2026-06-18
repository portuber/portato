//go:build linux

package service

import (
	"os"
	"strings"
	"testing"
)

func TestLinux_RenderUnit(t *testing.T) {
	const binary = "/usr/local/bin/portato"
	const config = "/home/test/.config/portato/config.yaml"
	got := renderUnit(DefaultLabel, binary, config)

	for _, want := range []string{
		"Description=",
		"After=network.target",
		"ExecStart=" + binary + " daemon --config " + config,
		"Restart=on-failure",
		"RestartSec=3",
		"WantedBy=default.target",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("unit missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestLinux_Install_New_CommandSequence(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	fx := newFakeExec()
	l := &linuxInstaller{exec: fx.run}

	unit, err := l.Install(Options{BinaryPath: "/bin/portato", ConfigPath: "/etc/c.yaml"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !exists(unit) {
		t.Errorf("unit not written at %q", unit)
	}
	j := fx.joined()
	for _, want := range []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable --now " + linuxUnit,
		"loginctl enable-linger",
	} {
		if !strings.Contains(j, want) {
			t.Errorf("missing command %q\ngot:\n%s", want, j)
		}
	}
	// A brand-new unit must restart, not be merely enabled.
	if strings.Contains(j, "systemctl --user restart ") {
		t.Errorf("new unit should not be restarted:\n%s", j)
	}
}

func TestLinux_Install_Existing_Restarts(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	// Pre-create the unit so Install detects it as existing.
	unit := unitPath()
	_ = os.MkdirAll(strings.TrimSuffix(unit, "/"+linuxUnit), 0o700)
	_ = os.WriteFile(unit, []byte("STALE"), 0o644)

	fx := newFakeExec()
	l := &linuxInstaller{exec: fx.run}
	if _, err := l.Install(Options{BinaryPath: "/bin/portato", ConfigPath: "/etc/c.yaml"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	j := fx.joined()
	if !strings.Contains(j, "systemctl --user restart "+linuxUnit) {
		t.Errorf("existing unit should be restarted:\n%s", j)
	}
	if strings.Contains(j, "enable --now") {
		t.Errorf("existing unit should not be (re)enabled:\n%s", j)
	}
	body, _ := os.ReadFile(unit)
	if strings.Contains(string(body), "STALE") {
		t.Errorf("unit not overwritten on reinstall")
	}
}

func TestLinux_Uninstall(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	unit := unitPath()
	_ = os.MkdirAll(strings.TrimSuffix(unit, "/"+linuxUnit), 0o700)
	_ = os.WriteFile(unit, []byte("x"), 0o644)

	fx := newFakeExec()
	l := &linuxInstaller{exec: fx.run}
	if err := l.Uninstall(Options{}); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	j := fx.joined()
	for _, want := range []string{
		"systemctl --user disable --now " + linuxUnit,
		"systemctl --user daemon-reload",
	} {
		if !strings.Contains(j, want) {
			t.Errorf("missing command %q\ngot:\n%s", want, j)
		}
	}
	if exists(unit) {
		t.Errorf("unit was not removed")
	}
}

func TestLinux_Status(t *testing.T) {
	fx := newFakeExec()
	fx.resp["systemctl"] = []byte("Active: active (running)")
	l := &linuxInstaller{exec: fx.run}
	got, err := l.Status(Options{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(got, "active (running)") {
		t.Errorf("Status = %q", got)
	}

	fx2 := newFakeExec()
	fx2.errOn["systemctl"] = os.ErrNotExist
	l2 := &linuxInstaller{exec: fx2.run}
	if got2, _ := l2.Status(Options{}); got2 != "not loaded" {
		t.Errorf("Status on error = %q, want %q", got2, "not loaded")
	}
}
