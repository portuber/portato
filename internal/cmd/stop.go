package cmd

import (
	"context"
	"fmt"
	"os"
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
)

// stopRunE gracefully terminates the running daemon: resolve its socket, read
// the discovery marker for the PID, SIGTERM it, and poll healthz until the
// socket goes silent. Idempotent: prints "no daemon running" (exit 0) when
// nothing is listening, and cleans a stale marker on the way out.
func stopRunE(*cobra.Command, []string) error {
	socket, err := stopResolveSocket()
	if err != nil {
		return fmt.Errorf("resolve daemon socket: %w", err)
	}
	markPath, _ := stopMarkerPath()

	if socket == "" || !stopProbe(socket) {
		if markPath != "" {
			_ = daemon.RemoveMarker(markPath) // stale -> tidy up
		}
		fmt.Fprintln(os.Stdout, "no daemon running")
		return nil
	}

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
