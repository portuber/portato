package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/daemon"
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

	// 2. known_hosts (auto-created on first connect, so absent is only a hint).
	hosts := cfg.Defaults.ResolvedKnownHosts()
	if fileExists(hosts) {
		d.ok("known_hosts", "%s", hosts)
	} else {
		d.info("known_hosts", "%s not found (created automatically on first connect)", hosts)
	}

	// 3. ssh-agent: if SSH_AUTH_SOCK points at a reachable socket, good; if it
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

	// 4. Each tunnel's resolved identity file (when one is configured).
	for _, t := range cfg.Tunnels {
		id := t.ResolvedIdentity(cfg.Defaults)
		if id == "" {
			continue
		}
		if fileExists(id) {
			d.ok("identity", "%s (%s)", id, t.Name)
		} else {
			d.fail("identity", "%s not found (tunnel %s)", id, t.Name)
		}
	}

	// 5. Daemon reachability (the daemon is optional, so absent is informational).
	socket, err := daemon.SocketPath()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), doctorProbeTimeout)
		err := client.New(socket).HealthzCtx(ctx)
		cancel()
		if err == nil {
			d.ok("daemon", "reachable at %s", socket)
			// 6. The IPC socket must be owner-only (SPEC §6: 0600).
			if info, statErr := os.Stat(socket); statErr == nil {
				if perm := info.Mode().Perm(); perm != 0o600 {
					d.fail("socket perms", "%s mode %o, expected 0600", socket, perm)
				}
			}
		} else {
			d.info("daemon", "not reachable at %s (start with `portato daemon` or `portato install`)", socket)
		}
	}

	// 7. Linux: lingering must be enabled for the daemon to survive logout.
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
