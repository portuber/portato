package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/controller"
	routelog "github.com/kipkaev55/portato/internal/log"
)

const (
	handoffPollIntervalDefault = 100 * time.Millisecond
	handoffTimeoutDefault      = 5 * time.Second
)

// Overridable per-run loop parameters so tests can shrink the wait.
var (
	handoffPollInterval = handoffPollIntervalDefault
	handoffTimeout      = handoffTimeoutDefault
)

// handoffDoneMsg is delivered when the background hand-off finishes (success
// or failure).
type handoffDoneMsg struct{ err error }

// startCmd spawns a detached `portato daemon --config <cfgPath>`. It is a
// package-level seam so tests can substitute a fake without forking.
//
// The child's stdout/stderr are routed to the daemon log so that a failure
// before the daemon sets up its own file logger (e.g. a config error) is not
// silently lost; the hand-off would otherwise only report a socket timeout.
var startCmd = func(cfgPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon", "--config", cfgPath)
	cmd.Stdin = nil
	if f, ferr := openDaemonLogAppend(); ferr == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer func() { _ = f.Close() }()
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// openDaemonLogAppend opens the daemon log for appending (creating its dir),
// so the spawned daemon's early output is captured next to the daemon's own
// log lines.
func openDaemonLogAppend() (*os.File, error) {
	path := routelog.DaemonPath()
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// probeSocket reports whether the daemon is answering on the socket. Seam for
// tests; production uses a short-timeout healthz probe.
var probeSocket = func(socket string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), handoffPollInterval)
	defer cancel()
	return client.New(socket).HealthzCtx(ctx) == nil
}

// handoffToDaemon releases the standalone's local ports (so the daemon can
// bind them), spawns a detached daemon and waits for its socket to answer.
//
// Ports are released before the spawn on purpose: the daemon binds its tunnel
// listeners once at startup (see forward.Tunnel.Start) and does not retry a
// failed bind, so the ports must be free by the time it starts. The brief
// gap between release and the daemon rebinding is the accepted MVP "blip"
// (SPEC §12). FD-passing would remove it — post-MVP.
func handoffToDaemon(ctrl controller.Controller, cfgPath, socket string) error {
	_ = ctrl.Close()
	if err := startCmd(cfgPath); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	deadline := time.Now().Add(handoffTimeout)
	for time.Now().Before(deadline) {
		if probeSocket(socket) {
			return nil
		}
		time.Sleep(handoffPollInterval)
	}
	return fmt.Errorf("daemon did not become ready within %s", handoffTimeout)
}
