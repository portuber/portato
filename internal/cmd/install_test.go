package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/service"
)

// fakeInstaller records the Options it is called with so tests can assert the
// cmd wiring without touching launchd/systemctl.
type fakeInstaller struct {
	installOpts   []service.Options
	uninstallOpts []service.Options
	installErr    error
	uninstallErr  error
}

func (f *fakeInstaller) Install(o service.Options) (string, error) {
	f.installOpts = append(f.installOpts, o)
	return "/fake/service/path", f.installErr
}

func (f *fakeInstaller) Uninstall(o service.Options) error {
	f.uninstallOpts = append(f.uninstallOpts, o)
	return f.uninstallErr
}

func (f *fakeInstaller) Status(service.Options) (string, error) { return "fake", nil }

func useFakeInstaller(t *testing.T) *fakeInstaller {
	t.Helper()
	f := &fakeInstaller{}
	prev := newServiceInstaller
	newServiceInstaller = func() service.Installer { return f }
	t.Cleanup(func() { newServiceInstaller = prev })
	return f
}

func resetLabel(t *testing.T) {
	t.Helper()
	prev := serviceLabel
	serviceLabel = ""
	t.Cleanup(func() { serviceLabel = prev })
}

// useStableExecutable injects a stable binary path so the go-run warning does
// not leak into the stderr assertions.
func useStableExecutable(t *testing.T) {
	t.Helper()
	prev := executablePath
	executablePath = func() (string, error) { return "/usr/local/bin/portato", nil }
	t.Cleanup(func() { executablePath = prev })
}

func TestInstall_InvokesInstallerWithAbsolutePaths(t *testing.T) {
	resetLabel(t)
	useStableExecutable(t)
	f := useFakeInstaller(t)

	c, out, errOut := captureCmd()
	if err := installRunE(c, nil); err != nil {
		t.Fatalf("installRunE: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Installed.") || !strings.Contains(out.String(), "/fake/service/path") {
		t.Errorf("stdout = %q", out.String())
	}
	if len(f.installOpts) != 1 {
		t.Fatalf("want 1 Install call, got %d", len(f.installOpts))
	}
	opts := f.installOpts[0]
	if opts.BinaryPath != "/usr/local/bin/portato" {
		t.Errorf("BinaryPath = %q", opts.BinaryPath)
	}
	if want := config.DefaultPath(); opts.ConfigPath != want {
		t.Errorf("ConfigPath = %q, want default %q", opts.ConfigPath, want)
	}
	if !filepath.IsAbs(opts.ConfigPath) {
		t.Errorf("ConfigPath must be absolute: %q", opts.ConfigPath)
	}
	if opts.Label != service.DefaultLabel {
		t.Errorf("Label = %q, want default %q", opts.Label, service.DefaultLabel)
	}
}

func TestInstall_LabelOverride(t *testing.T) {
	serviceLabel = "com.example.rw"
	t.Cleanup(func() { serviceLabel = "" })
	useStableExecutable(t)
	f := useFakeInstaller(t)

	c, _, _ := captureCmd()
	if err := installRunE(c, nil); err != nil {
		t.Fatalf("installRunE: %v", err)
	}
	if got := f.installOpts[0].Label; got != "com.example.rw" {
		t.Errorf("Label = %q", got)
	}
}

func TestInstall_RelativeConfigIsResolvedAbsolute(t *testing.T) {
	prev := cfgFile
	cfgFile = "./config.yaml"
	t.Cleanup(func() { cfgFile = prev })
	useStableExecutable(t)
	f := useFakeInstaller(t)

	c, _, _ := captureCmd()
	if err := installRunE(c, nil); err != nil {
		t.Fatalf("installRunE: %v", err)
	}
	if !filepath.IsAbs(f.installOpts[0].ConfigPath) {
		t.Errorf("relative --config must be resolved absolute, got %q", f.installOpts[0].ConfigPath)
	}
}

func TestInstall_SurfacesInstallerError(t *testing.T) {
	resetLabel(t)
	useStableExecutable(t)
	f := useFakeInstaller(t)
	f.installErr = errFed("boom")

	c, out, errOut := captureCmd()
	if err := installRunE(c, nil); err == nil {
		t.Fatal("expected error to propagate")
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Errorf("stderr should surface installer error; got %q", errOut.String())
	}
	if out.String() != "" {
		t.Errorf("stdout should be empty on error; got %q", out.String())
	}
}

func TestUninstall_InvokesInstaller(t *testing.T) {
	resetLabel(t)
	useStableExecutable(t)
	f := useFakeInstaller(t)

	c, out, errOut := captureCmd()
	if err := uninstallRunE(c, nil); err != nil {
		t.Fatalf("uninstallRunE: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Removed autostart entry.") {
		t.Errorf("stdout = %q", out.String())
	}
	if len(f.uninstallOpts) != 1 {
		t.Errorf("want 1 Uninstall call, got %d", len(f.uninstallOpts))
	}
}

func TestWarnIfUnstableBinary(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		warns bool
	}{
		{"go-build cache", filepath.Join(os.TempDir(), "go-build123", "portato"), true},
		{"tmp dir", filepath.Join(os.TempDir(), "portato"), true},
		{"stable", "/usr/local/bin/portato", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, errOut := captureCmd()
			warnIfUnstableBinary(c, tc.path)
			got := strings.Contains(errOut.String(), "warning")
			if got != tc.warns {
				t.Errorf("warnIfUnstableBinary(%q): warning=%v, want %v\nstderr: %q", tc.path, got, tc.warns, errOut.String())
			}
		})
	}
}

func TestResolveConfigPath_Default(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got, err := resolveConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := config.DefaultPath(); got != want {
		t.Errorf("resolveConfigPath() = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("default config path must be absolute: %q", got)
	}
}

type errFed string

func (e errFed) Error() string { return string(e) }
