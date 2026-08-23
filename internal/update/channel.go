package update

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Channel is how this binary was installed — the etiquette gate for
// `update apply`: a package manager must never discover its files replaced
// behind its back, so managed channels refuse the in-place swap and print
// their own upgrade command instead.
type Channel string

const (
	ChannelDirect    Channel = "direct"    // tarball/zip manual install
	ChannelHomebrew  Channel = "homebrew"  // brew cask (macOS, /opt/homebrew or Cellar)
	ChannelScoop     Channel = "scoop"     // scoop (Windows, ~\scoop)
	ChannelDeb       Channel = "deb"       // dpkg-owned
	ChannelRPM       Channel = "rpm"       // rpm-owned
	ChannelAPK       Channel = "apk"       // apk-owned (Alpine)
	ChannelGoInstall Channel = "goinstall" // GOBIN/go install
)

// ChannelInfo is the detection result: the channel plus the upgrade command
// `apply` defers to (empty for direct).
type ChannelInfo struct {
	Channel  Channel
	Upgrade  string
	Managed  bool // true for every non-direct channel
	Evidence string
}

// lookUpCommand is the dpkg/rpm/apk lookup seam (tests fake ownership
// without the tools).
var lookUpCommand = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// DetectChannel classifies the given executable path (os.Executable() in
// production) into an install channel. Path heuristics first (cheap,
// always available), then package-manager ownership lookups where the
// tools exist. The classification is advisory in `update check` and
// blocking in `update apply` (except SCM-held binaries on Windows — never
// swapped; the SCM service itself is detected by the caller).
func DetectChannel(exe string) ChannelInfo {
	if ci, ok := detectByPath(exe); ok {
		return ci
	}
	return detectByPackageManager(exe)
}

// detectByPath applies the pure path heuristics: scoop, homebrew, go
// install. ok=false means no marker matched.
func detectByPath(exe string) (ChannelInfo, bool) {
	if exe == "" {
		return ChannelInfo{Channel: ChannelDirect}, true
	}
	dir, base := filepath.Split(exe)
	dir = filepath.ToSlash(dir)

	// Scoop: ~\scoop\apps\portato\current\portato — the Phase-47 stable
	// junction; swapping in place would desync the bucket state.
	if strings.Contains(dir, "/scoop/apps/") {
		return ChannelInfo{Channel: ChannelScoop, Managed: true, Evidence: "path under ~\\scoop\\apps",
			Upgrade: "scoop update portato"}, true
	}
	// Homebrew cask: the binary is symlinked into /opt/homebrew/bin (or
	// /usr/local on Intel) with the real file under Caskroom/portato.
	if strings.HasPrefix(dir, "/opt/homebrew/") || strings.HasPrefix(dir, "/usr/local/Cellar/") ||
		strings.Contains(dir, "/Caskroom/") {
		return ChannelInfo{Channel: ChannelHomebrew, Managed: true, Evidence: "path under the Homebrew prefix",
			Upgrade: "brew upgrade --cask portuber/tap/portato"}, true
	}
	// go install: the module binary lands in GOBIN (~/go/bin by default).
	if strings.HasSuffix(strings.TrimSuffix(dir, "/"), "/go/bin") && base == "portato" {
		return ChannelInfo{Channel: ChannelGoInstall, Managed: true, Evidence: "path under GOBIN",
			Upgrade: "go install github.com/portuber/portato/cmd/portato@latest"}, true
	}
	return ChannelInfo{}, false
}

// detectByPackageManager asks dpkg/rpm/apk whether they own the binary
// (linux /usr paths only); unowned or non-linux is direct.
func detectByPackageManager(exe string) ChannelInfo {
	if runtime.GOOS == "linux" && strings.HasPrefix(filepath.ToSlash(filepath.Dir(exe)), "/usr/") {
		if out, err := lookUpCommand("dpkg-query", "-S", exe); err == nil && strings.Contains(out, "portato") {
			return ChannelInfo{Channel: ChannelDeb, Managed: true, Evidence: "dpkg owns the file",
				Upgrade: "sudo apt upgrade portato"}
		}
		if out, err := lookUpCommand("rpm", "-qf", exe); err == nil && strings.Contains(out, "portato") {
			return ChannelInfo{Channel: ChannelRPM, Managed: true, Evidence: "rpm owns the file",
				Upgrade: "sudo dnf upgrade portato"}
		}
		if out, err := lookUpCommand("apk", "info", "--who-owns", exe); err == nil && strings.Contains(out, "portato") {
			return ChannelInfo{Channel: ChannelAPK, Managed: true, Evidence: "apk owns the file",
				Upgrade: "sudo apk upgrade portato"}
		}
	}
	return ChannelInfo{Channel: ChannelDirect, Evidence: "no package-manager markers on the path"}
}
