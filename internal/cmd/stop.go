package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/daemon"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running portato daemon (graceful SIGTERM)",
	RunE:  stopRunE,
}

// Overridable so tests can shrink the wait.
var (
	stopPollInterval = 100 * time.Millisecond
	stopTimeout      = 5 * time.Second
)

// Overridable seams so tests can drive stopRunE without a real daemon or real
// signals (mirrors the handoff.go startCmd/probeSocket pattern).
var (
	stopResolveSocket = daemon.ResolveSocket
	stopMarkerPath    = daemon.DiscoveryPath
	stopProbe         = func(socket string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), stopPollInterval)
		defer cancel()
		return client.New(socket).HealthzCtx(ctx) == nil
	}
	// stopPidAlive reports whether a PID is an existing process; the daemon's
	// per-OS pidAlive surfaced as PidAlive. Used to recognise a wedged daemon
	// (alive PID, unreachable socket) and to poll for its exit.
	stopPidAlive = daemon.PidAlive
	// stopProcessIsPortato guards wedged recovery against PID reuse: only
	// signal a PID that is actually a portato process.
	stopProcessIsPortato = processIsPortato
)

// stopRunE gracefully terminates the running daemon. The normal path resolves
// the daemon socket, reads the discovery marker for the PID, SIGTERMs it and
// polls healthz until the socket goes silent. When no socket answers but the
// marker records an alive portato PID, the daemon is wedged (alive, holding
// the flock and the local ports, but its socket file was reaped — e.g. macOS
// $TMPDIR cleanup) and is recovered by PID, polling process liveness instead
// of the dead socket. Idempotent: prints "no daemon running" (exit 0) when
// nothing is alive, and cleans a stale marker on the way out.
func stopRunE(*cobra.Command, []string) error {
	socket, err := stopResolveSocket()
	if err != nil {
		return fmt.Errorf("resolve daemon socket: %w", err)
	}
	markPath, _ := stopMarkerPath()

	if socket != "" && stopProbe(socket) {
		return stopReachableDaemon(socket, markPath)
	}

	if markPath != "" {
		if m, merr := daemon.ReadMarker(markPath); merr == nil && m.PID > 0 {
			if stopPidAlive(m.PID) && stopProcessIsPortato(m.PID) {
				if err := stopWedgedDaemon(m.PID); err != nil {
					return err
				}
				_ = daemon.RemoveMarker(markPath)
				return nil
			}
		}
		_ = daemon.RemoveMarker(markPath)
	}
	fmt.Fprintln(os.Stdout, "no daemon running")
	return nil
}

// stopReachableDaemon handles the normal case: the daemon answers on its
// socket. Read the marker PID, SIGTERM it, and poll healthz until the socket
// goes silent.
func stopReachableDaemon(socket, markPath string) error {
	if markPath == "" {
		return fmt.Errorf("daemon is answering at %s but its discovery path is unavailable; stop it manually", socket)
	}
	m, merr := daemon.ReadMarker(markPath)
	if merr != nil || m.PID <= 0 {
		return fmt.Errorf("daemon is answering at %s but the discovery marker is unavailable; stop it manually", socket)
	}
	if err := stopKill(m.PID); err != nil {
		return fmt.Errorf("signal pid %d: %w", m.PID, err)
	}
	deadline := time.Now().Add(stopTimeout)
	for time.Now().Before(deadline) {
		if !stopProbe(socket) {
			fmt.Fprintf(os.Stdout, "daemon stopped (pid %d)\n", m.PID)
			return nil
		}
		time.Sleep(stopPollInterval)
	}
	return fmt.Errorf("daemon did not stop within %s (pid %d); try kill -KILL %d", stopTimeout, m.PID, m.PID)
}

// stopWedgedDaemon recovers a daemon that is alive (it owns the single-instance
// flock and the local listener ports) but whose IPC socket no longer answers —
// its socket file was reaped while the listener fd stayed open on an orphaned
// inode (macOS $TMPDIR cleanup). The healthz probe can never succeed here, so
// poll process liveness instead.
func stopWedgedDaemon(pid int) error {
	fmt.Fprintf(os.Stdout, "daemon socket unreachable; stopping by pid %d\n", pid)
	if err := stopKill(pid); err != nil {
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}
	deadline := time.Now().Add(stopTimeout)
	for time.Now().Before(deadline) {
		if !stopPidAlive(pid) {
			fmt.Fprintf(os.Stdout, "daemon stopped (pid %d; socket was unreachable)\n", pid)
			return nil
		}
		time.Sleep(stopPollInterval)
	}
	return fmt.Errorf("daemon did not stop within %s (pid %d); try kill -KILL %d", stopTimeout, pid, pid)
}

// processIsPortato reports whether pid is a running portato process, best
// effort, via `ps`. It guards wedged-daemon recovery against PID reuse (a dead
// daemon whose PID the OS reassigned to an unrelated process). A false negative
// only skips recovery — it never causes a wrong signal. The wedged scenario is
// unix-specific (macOS), so the windows lack of `ps` (a false negative) simply
// falls through to "no daemon running", which is correct there.
func processIsPortato(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "portato")
}
