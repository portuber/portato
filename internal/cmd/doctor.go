package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/daemon"
	routelog "github.com/portuber/portato/internal/log"
	"github.com/portuber/portato/internal/service"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the Portato setup (config, keys, agent, daemon)",
	Long: `Diagnose the local Portato setup: config validity, identity keys and
ssh-agent, known_hosts, daemon reachability and IPC socket permissions, and
(Linux) lingering. Prints a line per check and exits non-zero on any failure.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          doctorRunE,
}

const doctorProbeTimeout = 500 * time.Millisecond

func doctorRunE(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	d := newDoctor(out)

	fmt.Fprintf(out, "portato %s (commit %s, built %s)\n\n", version, commit, date)

	// 1. Config file exists and loads.
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		d.fail("config", "%s: %v", cfgPath, err)
		// Without a config the remaining checks have nothing to key off; stop.
		d.summary()
		return fmt.Errorf("doctor: config check failed")
	}
	d.ok("config", "%s", cfgPath)

	// 2. The config directory must be writable (tubers/config are persisted
	// there; a read-only mount would break enable/disable and reload).
	checkConfigDir(d, cfgPath)

	// 3. known_hosts (auto-created on first connect, so absent is only a hint).
	hosts := cfg.Defaults.ResolvedKnownHosts()
	if fileExists(hosts) {
		d.ok("known_hosts", "%s", hosts)
	} else {
		d.info("known_hosts", "%s not found (created automatically on first connect)", hosts)
	}

	// 4. ssh-agent: if SSH_AUTH_SOCK points at a reachable socket, good; if it
	// is set but unreachable, that is a failure; if unset, only a hint.
	if sock := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK")); sock != "" {
		if isReachableSock(sock) {
			d.ok("ssh-agent", "%s", sock)
		} else {
			d.fail("ssh-agent", "SSH_AUTH_SOCK=%s is not reachable", sock)
		}
	} else {
		d.info("ssh-agent", "SSH_AUTH_SOCK unset (configure an identity key or start ssh-agent)")
	}

	// 5. Each tuber's resolved identity file (when one is configured).
	for _, t := range cfg.Tubers {
		id := t.ResolvedIdentity(cfg.Defaults)
		if id == "" {
			continue
		}
		if fileExists(id) {
			d.ok("identity", "%s (%s)", id, t.Name)
		} else {
			d.fail("identity", "%s not found (tuber %s)", id, t.Name)
		}
	}

	// 6. Log file + rotation. The daemon/standalone write a size-rotated file
	// under the state home; report its path and the most recent rotation
	// (evidenced by the newest archive's mtime — doctor is a separate process
	// and cannot read the in-memory RotatingWriter state).
	paths := logStatePaths()
	logPath := paths[0]
	if fileExists(logPath) {
		if t, ok := lastRotation(paths...); ok {
			d.ok("logs", "%s (last rotated %s)", logPath, t.Format("2006-01-02 15:04:05"))
		} else {
			d.ok("logs", "%s (no rotation yet)", logPath)
		}
	} else {
		d.info("logs", "%s (created when the daemon or standalone runs)", logPath)
	}

	// 7. Daemon reachability (the daemon is optional, so absent is informational).
	socket, err := daemon.ResolveSocket()
	if err == nil && socket != "" {
		ctx, cancel := context.WithTimeout(context.Background(), doctorProbeTimeout)
		herr := client.New(socket).HealthzCtx(ctx)
		cancel()
		if herr == nil {
			d.ok("daemon", "reachable at %s", socket)
			// 8. The IPC socket must be owner-only (SPEC §6: 0600).
			if info, statErr := os.Stat(socket); statErr == nil {
				if perm := info.Mode().Perm(); perm != 0o600 {
					d.fail("socket perms", "%s mode %o, expected 0600", socket, perm)
				}
			}
		} else {
			d.info("daemon", "not reachable at %s (start with `portato daemon` or `portato install`)", socket)
		}
	} else {
		d.info("daemon", "not running (start with `portato daemon` or `portato install`)")
	}

	// 9. The binary should be on PATH so autostart can launch `portato daemon`.
	checkBinary(d)

	// 10. Autostart definition (launchd plist on macOS, systemd --user unit on
	// Linux); absent is informational — autostart is optional.
	checkAutostart(d)

	// 11. Linux: lingering must be enabled for the daemon to survive logout.
	if runtime.GOOS == "linux" {
		checkLinger(d)
	}

	d.summary()
	if d.failed > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", d.failed)
	}
	return nil
}

type doctor struct {
	out    io.Writer
	failed int
}

func newDoctor(w io.Writer) *doctor { return &doctor{out: w} }

func (d *doctor) ok(name, format string, args ...any) {
	fmt.Fprintf(d.out, "✓ %-12s %s\n", name, fmt.Sprintf(format, args...))
}

func (d *doctor) info(name, format string, args ...any) {
	fmt.Fprintf(d.out, "· %-12s %s\n", name, fmt.Sprintf(format, args...))
}

func (d *doctor) fail(name, format string, args ...any) {
	d.failed++
	fmt.Fprintf(d.out, "✗ %-12s %s\n", name, fmt.Sprintf(format, args...))
}

func (d *doctor) summary() {
	verb := "checks passed"
	if d.failed > 0 {
		verb = fmt.Sprintf("%d check(s) failed", d.failed)
	}
	fmt.Fprintf(d.out, "\n%s\n", verb)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// isReachableSock reports whether a unix socket at path can be opened.
func isReachableSock(path string) bool {
	if info, err := os.Stat(path); err != nil || info.Mode()&os.ModeSocket == 0 {
		return false
	}
	c, err := net.Dial("unix", path)
	if c != nil {
		_ = c.Close()
	}
	return err == nil
}

// logStatePaths returns the candidate log paths doctor inspects (the daemon
// log first, then the standalone one). It is a variable so tests can point it
// at a temp dir without depending on the real XDG state home.
var logStatePaths = func() []string {
	return []string{routelog.DaemonPath(), routelog.DefaultPath()}
}

// lastRotation reports the mtime of the most recent archive among the given log
// paths (path + ".1"), so `portato doctor` can show when logs last rotated.
// Returns ok=false when no archive exists yet.
func lastRotation(paths ...string) (time.Time, bool) {
	for _, p := range paths {
		if info, err := os.Stat(p + ".1"); err == nil {
			return info.ModTime(), true
		}
	}
	return time.Time{}, false
}

// checkLinger runs `loginctl show-user` and inspects the Linger property.
func checkLinger(d *doctor) {
	user := os.Getenv("USER")
	if user == "" {
		user = "current user"
	}
	out, err := exec.Command("loginctl", "show-user", user).Output()
	if err != nil {
		d.info("lingering", "loginctl unavailable (%v)", err)
		return
	}
	if strings.Contains(string(out), "Linger=yes") {
		d.ok("lingering", "enabled for %s", user)
	} else {
		d.info("lingering", "not enabled (run: loginctl enable-linger %s) so the daemon survives logout", user)
	}
}

// lookPath resolves a binary on PATH (exec.LookPath by default); a seam so
// tests can fake it without depending on the host's PATH.
var lookPath = exec.LookPath

// autostartArtefact returns the autostart definition path for the current OS
// (launchd plist on macOS, systemd --user unit on Linux), or "" where autostart
// is unsupported. Overridable in tests.
var autostartArtefact = defaultAutostartArtefact

func defaultAutostartArtefact() string {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "Library", "LaunchAgents", service.DefaultLabel+".plist")
	case "linux":
		return filepath.Join(xdg.ConfigHome, "systemd", "user", "portato.service")
	default:
		return ""
	}
}

// checkConfigDir verifies the directory holding the config file is writable
// (portato persists enable state and reloads config there).
func checkConfigDir(d *doctor, cfgPath string) {
	dir := filepath.Dir(cfgPath)
	probe := filepath.Join(dir, ".doctor-probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		d.fail("config dir", "%s not writable: %v", dir, err)
		return
	}
	_ = os.Remove(probe)
	d.ok("config dir", "%s (writable)", dir)
}

// checkBinary reports the running binary's path and whether `portato` is
// resolvable on PATH (autostart launches `portato daemon`).
func checkBinary(d *doctor) {
	exe, err := os.Executable()
	if err != nil {
		d.info("binary", "running path unknown (%v)", err)
		return
	}
	if lp, err := lookPath("portato"); err == nil {
		d.ok("binary", "%s (on PATH: %s)", exe, lp)
	} else {
		d.info("binary", "%s (not on PATH; install it so `portato install` can launch the daemon)", exe)
	}
}

// checkAutostart reports whether the autostart definition is installed.
func checkAutostart(d *doctor) {
	p := autostartArtefact()
	if p == "" {
		return
	}
	if fileExists(p) {
		d.ok("autostart", "%s", p)
	} else {
		d.info("autostart", "not installed (run `portato install`)")
	}
}
