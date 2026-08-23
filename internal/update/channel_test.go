package update

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestDetectChannelPaths(t *testing.T) {
	cases := []struct {
		path string
		want Channel
	}{
		{"/opt/homebrew/bin/portato", ChannelHomebrew},
		{"/usr/local/Cellar/portato/1.7.0/portato", ChannelHomebrew},
		{"/usr/local/Caskroom/portato/1.7.0/portato", ChannelHomebrew},
		{"/Users/alice/scoop/apps/portato/current/portato", ChannelScoop},
		{"C:/Users/alice/scoop/apps/portato/current/portato", ChannelScoop},
		{"/Users/dev/go/bin/portato", ChannelGoInstall},
		{"/home/dev/go/bin/portato", ChannelGoInstall},
		{"/usr/local/bin/portato", ChannelDirect},
		{"/Users/dev/bin/portato", ChannelDirect},
		{"/home/dev/.local/bin/portato", ChannelDirect},
		{"", ChannelDirect},
	}
	for _, tc := range cases {
		got := DetectChannel(tc.path)
		if got.Channel != tc.want {
			t.Errorf("DetectChannel(%q) = %s (%s), want %s", tc.path, got.Channel, got.Evidence, tc.want)
		}
	}
}

func TestDetectChannelManagedFlag(t *testing.T) {
	for _, path := range []string{
		"/opt/homebrew/bin/portato",
		"/Users/alice/scoop/apps/portato/current/portato",
		"/Users/dev/go/bin/portato",
	} {
		ci := DetectChannel(path)
		if !ci.Managed || ci.Upgrade == "" {
			t.Errorf("DetectChannel(%q): Managed=%v Upgrade=%q; want managed with an upgrade command", path, ci.Managed, ci.Upgrade)
		}
	}
	if ci := DetectChannel("/usr/local/bin/portato"); ci.Managed || ci.Upgrade != "" {
		t.Errorf("direct install reports managed (%v, %q)", ci.Managed, ci.Upgrade)
	}
}

func TestDetectChannelUpgradeCommands(t *testing.T) {
	want := map[Channel]string{
		ChannelHomebrew:  "brew upgrade --cask portuber/tap/portato",
		ChannelScoop:     "scoop update portato",
		ChannelGoInstall: "go install github.com/portuber/portato/cmd/portato@latest",
	}
	for path, ch := range map[string]Channel{
		"/opt/homebrew/bin/portato":                   ChannelHomebrew,
		"/Users/a/scoop/apps/portato/current/portato": ChannelScoop,
		"/Users/dev/go/bin/portato":                   ChannelGoInstall,
	} {
		if got := DetectChannel(path).Upgrade; got != want[ch] {
			t.Errorf("upgrade for %s = %q, want %q", ch, got, want[ch])
		}
	}
}

func TestDetectChannelPackageManagerLookups(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dpkg/rpm/apk ownership lookups are a linux-only path")
	}
	prev := lookUpCommand
	t.Cleanup(func() { lookUpCommand = prev })

	// dpkg owns it → deb channel with the apt hint.
	lookUpCommand = func(name string, args ...string) (string, error) {
		if name == "dpkg-query" && strings.Join(args, " ") == "-S /usr/bin/portato" {
			return "portato: /usr/bin/portato\n", nil
		}
		return "", errors.New("not found")
	}
	ci := DetectChannel("/usr/bin/portato")
	if ci.Channel != ChannelDeb || ci.Upgrade != "sudo apt upgrade portato" || !ci.Managed {
		t.Errorf("dpkg-owned = %+v, want deb/apt", ci)
	}

	// rpm owns it → rpm channel.
	lookUpCommand = func(name string, args ...string) (string, error) {
		if name == "rpm" && strings.Join(args, " ") == "-qf /usr/bin/portato" {
			return "portato-1.7.0-1.x86_64\n", nil
		}
		return "", errors.New("not found")
	}
	ci = DetectChannel("/usr/bin/portato")
	if ci.Channel != ChannelRPM || ci.Upgrade != "sudo dnf upgrade portato" {
		t.Errorf("rpm-owned = %+v, want rpm/dnf", ci)
	}

	// apk owns it → apk channel.
	lookUpCommand = func(name string, args ...string) (string, error) {
		if name == "apk" && strings.Join(args, " ") == "info --who-owns /usr/bin/portato" {
			return "portato: /usr/bin/portato\n", nil
		}
		return "", errors.New("not found")
	}
	ci = DetectChannel("/usr/bin/portato")
	if ci.Channel != ChannelAPK || ci.Upgrade != "sudo apk upgrade portato" {
		t.Errorf("apk-owned = %+v, want apk/apk", ci)
	}

	// No tool owns it → direct even under /usr.
	lookUpCommand = func(string, ...string) (string, error) { return "", errors.New("no such tool") }
	ci = DetectChannel("/usr/bin/portato")
	if ci.Channel != ChannelDirect {
		t.Errorf("unowned /usr path = %+v, want direct", ci)
	}
}
