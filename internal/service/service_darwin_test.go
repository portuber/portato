//go:build darwin

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeExec records every invocation as "name arg arg..." and returns canned
// responses keyed by the program name. Lets tests assert the exact launchd /
// systemctl command sequence without touching the system.
type fakeExec struct {
	mu    sync.Mutex
	calls []string
	resp  map[string][]byte
	errOn map[string]error
}

func newFakeExec() *fakeExec {
	return &fakeExec{resp: map[string][]byte{}, errOn: map[string]error{}}
}

func (f *fakeExec) run(name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	if err, ok := f.errOn[name]; ok {
		return nil, err
	}
	return f.resp[name], nil
}

func (f *fakeExec) joined() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

func TestDarwin_RenderPlist(t *testing.T) {
	const binary = "/usr/local/bin/portato"
	const config = "/Users/test/.config/portato/config.yaml"
	got := renderPlist(DefaultLabel, binary, config,
		"/Users/test/Library/Logs/portato.log",
		"/Users/test/Library/Logs/portato.err.log")

	for _, want := range []string{
		"<string>" + DefaultLabel + "</string>",
		"<string>" + binary + "</string>",
		"<string>daemon</string>",
		"<string>--config</string>",
		"<string>" + config + "</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"StandardOutPath",
		"StandardErrorPath",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	// launchd does not expand ~; log paths must be absolute.
	if strings.Contains(got, "~/") {
		t.Errorf("plist must not contain '~' (launchd will not expand it):\n%s", got)
	}
}

func TestDarwin_RenderPlist_EscapesXML(t *testing.T) {
	got := renderPlist(DefaultLabel, "/a&b/c", "/x<y/z", "/out", "/err")
	if !strings.Contains(got, "/a&amp;b/c") || strings.Contains(got, "/a&b/c") {
		t.Errorf("& not escaped in plist:\n%s", got)
	}
	if !strings.Contains(got, "/x&lt;y/z") {
		t.Errorf("< not escaped in plist:\n%s", got)
	}
}

func TestDarwin_Install_CommandSequenceAndFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fx := newFakeExec()
	d := &darwinInstaller{exec: fx.run}

	const binary = "/opt/portato/bin/portato"
	const config = "/opt/portato/config.yaml"
	plist, err := d.Install(Options{BinaryPath: binary, ConfigPath: config})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	wantPlist := plistPath(home, DefaultLabel)
	if plist != wantPlist {
		t.Errorf("Install returned %q, want %q", plist, wantPlist)
	}
	if !exists(plist) {
		t.Errorf("plist not written at %q", plist)
	}

	want := []string{
		fmt.Sprintf("launchctl bootout gui/%d/%s", os.Getuid(), DefaultLabel),
		fmt.Sprintf("launchctl bootstrap gui/%d %s", os.Getuid(), wantPlist),
	}
	for _, c := range want {
		if !strings.Contains(fx.joined(), c) {
			t.Errorf("missing command %q\ngot:\n%s", c, fx.joined())
		}
	}

	body, _ := os.ReadFile(plist)
	if !strings.Contains(string(body), "<string>"+binary+"</string>") {
		t.Errorf("plist does not reference the binary:\n%s", body)
	}
	if !strings.Contains(string(body), "<string>"+config+"</string>") {
		t.Errorf("plist does not reference the config:\n%s", body)
	}
}

func TestDarwin_Install_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fx := newFakeExec()
	d := &darwinInstaller{exec: fx.run}

	opts := Options{BinaryPath: "/bin/portato", ConfigPath: "/etc/c.yaml"}
	if _, err := d.Install(opts); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Overwrite the plist to prove the second install replaces it.
	_ = os.WriteFile(plistPath(home, DefaultLabel), []byte("STALE"), 0o644)
	if _, err := d.Install(opts); err != nil {
		t.Fatalf("second install: %v", err)
	}

	// Two installs → two bootout/bootstrap pairs, exactly one plist file.
	bootouts := strings.Count(fx.joined(), "launchctl bootout ")
	bootstraps := strings.Count(fx.joined(), "launchctl bootstrap ")
	if bootouts != 2 || bootstraps != 2 {
		t.Errorf("idempotency: want 2 bootout + 2 bootstrap, got %d/%d\n%s", bootouts, bootstraps, fx.joined())
	}
	entries, _ := os.ReadDir(filepath.Join(home, "Library", "LaunchAgents"))
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".plist") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want exactly 1 plist, got %d", count)
	}
	body, _ := os.ReadFile(plistPath(home, DefaultLabel))
	if strings.Contains(string(body), "STALE") {
		t.Errorf("plist was not overwritten on second install")
	}
}

func TestDarwin_Uninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plist := plistPath(home, DefaultLabel)
	_ = os.MkdirAll(filepath.Dir(plist), 0o755)
	_ = os.WriteFile(plist, []byte("x"), 0o644)

	fx := newFakeExec()
	d := &darwinInstaller{exec: fx.run}
	if err := d.Uninstall(Options{}); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !strings.Contains(fx.joined(), fmt.Sprintf("launchctl bootout gui/%d/%s", os.Getuid(), DefaultLabel)) {
		t.Errorf("uninstall did not bootout:\n%s", fx.joined())
	}
	if exists(plist) {
		t.Errorf("plist was not removed")
	}
}

func TestDarwin_Status(t *testing.T) {
	fx := newFakeExec()
	fx.resp["launchctl"] = []byte("pid = 1234\nstate = running")
	d := &darwinInstaller{exec: fx.run}
	got, err := d.Status(Options{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != "pid = 1234\nstate = running" {
		t.Errorf("Status = %q", got)
	}

	// When launchctl errors (job not loaded), Status stays informational.
	fx2 := newFakeExec()
	fx2.errOn["launchctl"] = fmt.Errorf("no such job")
	d2 := &darwinInstaller{exec: fx2.run}
	got2, _ := d2.Status(Options{})
	if got2 != "not loaded" {
		t.Errorf("Status on error = %q, want %q", got2, "not loaded")
	}
}
