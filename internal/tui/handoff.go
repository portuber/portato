package tui

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/daemon"
	"github.com/portuber/portato/internal/fdpass"
	routelog "github.com/portuber/portato/internal/log"
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

// startCmd spawns a detached `portato daemon --config <cfgPath>` and, when
// listenFdsPath != "", also passes --listen-fds <path> so the spawned daemon
// dials back the transfer socket and adopts the standalone's live listeners
// (Phase 16). Package-level seam so tests can substitute a fake without
// forking.
//
// The child's stdout/stderr are routed to the daemon log so that a failure
// before the daemon sets up its own file logger (e.g. a config error) is not
// silently lost; the hand-off would otherwise only report a socket timeout.
var startCmd = func(cfgPath, listenFdsPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"daemon", "--config", cfgPath}
	if listenFdsPath != "" {
		args = append(args, "--listen-fds", listenFdsPath)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	if f, ferr := openDaemonLogAppend(); ferr == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer func() { _ = f.Close() }()
	}
	cmd.SysProcAttr = detachedSysProcAttr()
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

// probeSocket reports whether the daemon is answering, by reading the
// discovery marker and probing the advertised socket. Seam for tests;
// production uses a short-timeout healthz probe.
var probeSocket = func() bool {
	socket, err := daemon.ResolveSocket()
	if err != nil || socket == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), handoffPollInterval)
	defer cancel()
	return client.New(socket).HealthzCtx(ctx) == nil
}

// handoffToDaemon moves the standalone's live tubers to a detached daemon. It
// first tries the seamless Phase 16 FD hand-off -- passing the already-bound
// local listeners to the spawned daemon so the local ports never go down and the
// daemon never rebinds. If that is unavailable (no live local listeners, or any
// pre-spawn step fails), it falls back to the Phase 5 close+rebind path (a brief
// port blip). Either way the daemon comes up and the standalone exits.
func handoffToDaemon(ctrl controller.Controller, cfgPath string) error {
	spawned, err := handoffWithFDs(ctrl, cfgPath)
	if err == nil {
		return nil
	}
	if spawned {
		// The FD path already started a daemon; even though it did not become
		// ready in time, do NOT fall back to a second spawn -- it would race the
		// first (and the single-instance flock would reject it anyway).
		return err
	}
	return handoffLegacy(ctrl, cfgPath)
}

// handoffWithFDs performs the seamless FD hand-off. It reports whether it
// spawned the daemon and the error (nil on success). It returns nil error only
// when the spawned daemon has answered healthz (having adopted the listeners or
// bound its own). Before spawning it returns (false, err) WITHOUT closing ctrl,
// so handoffToDaemon can fall back to the legacy path; after spawning it returns
// (true, ...) -- success is defined by healthz answering, so a failed send
// merely means the daemon binds normally.
func handoffWithFDs(ctrl controller.Controller, cfgPath string) (bool, error) {
	files, err := ctrl.LiveListenerFiles()
	if err != nil {
		return false, fmt.Errorf("enumerate live listeners: %w", err)
	}
	if len(files) == 0 {
		return false, fmt.Errorf("no live local listeners to pass")
	}

	sockPath, xfer, err := openTransferSocket()
	if err != nil {
		closeFiles(files)
		return false, err
	}
	cleanupSocket := func() {
		_ = xfer.Close()
		_ = os.Remove(sockPath)
	}

	if err := startCmd(cfgPath, sockPath); err != nil {
		cleanupSocket()
		closeFiles(files)
		return false, fmt.Errorf("spawn daemon: %w", err)
	}

	// Send the offers on the accepted connection in parallel with waiting for
	// the daemon's healthz: the daemon dials the transfer socket, reads the
	// listeners, adopts them and starts serving. Closing xfer (on either path)
	// unblocks Accept if the daemon never connected.
	sendErr := make(chan error, 1)
	go func() {
		conn, aerr := xfer.Accept()
		if aerr != nil {
			sendErr <- aerr
			return
		}
		uc := conn.(*net.UnixConn)
		sendErr <- fdpass.Send(uc, buildOffers(files, ctrl.List()))
		_ = uc.Close()
	}()

	ready := waitForDaemon()
	cleanupSocket() // unblock the accept/send goroutine if it never connected
	sendResult := <-sendErr
	if !ready {
		return true, fmt.Errorf("daemon did not become ready within %s", handoffTimeout)
	}
	_ = sendResult // a failed send is harmless once the daemon is healthy
	// The daemon has adopted the listeners (or bound its own) and is healthy.
	// Now the standalone may release its own copies; the daemon's dup'd fds keep
	// the ports up.
	_ = ctrl.Close()
	return true, nil
}

// waitForDaemon polls the daemon's healthz until it answers or the hand-off
// timeout elapses.
func waitForDaemon() bool {
	deadline := time.Now().Add(handoffTimeout)
	for time.Now().Before(deadline) {
		if probeSocket() {
			return true
		}
		time.Sleep(handoffPollInterval)
	}
	return false
}

// handoffLegacy is the Phase 5 path: release the local ports, spawn the daemon
// (which rebinds them) and wait for its socket. The brief gap between release
// and rebind is the accepted MVP "blip" (SPEC §12); the FD hand-off removes it.
func handoffLegacy(ctrl controller.Controller, cfgPath string) error {
	_ = ctrl.Close()
	if err := startCmd(cfgPath, ""); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	if !waitForDaemon() {
		return fmt.Errorf("daemon did not become ready within %s", handoffTimeout)
	}
	return nil
}

// openTransferSocket binds a fresh SOCK_STREAM unix socket in the temp dir for
// the one-shot listener transfer. The path is uid- and time-scoped to avoid
// collisions, and mode 0600 so no other user can dial it.
func openTransferSocket() (string, net.Listener, error) {
	name := fmt.Sprintf("portato-handoff-%d-%d.sock", os.Getuid(), time.Now().UnixNano())
	path := filepath.Join(os.TempDir(), name)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", nil, fmt.Errorf("listen transfer socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("chmod transfer socket: %w", err)
	}
	return path, ln, nil
}

// buildOffers pairs each live listener fd with its tuber name and type (the
// type carried in the Header lets the daemon sanity-check it). Types come from
// the controller's status snapshot.
func buildOffers(files map[string]*os.File, statuses []controller.Status) []fdpass.Offer {
	types := make(map[string]string, len(statuses))
	for _, s := range statuses {
		types[s.Name] = s.Type
	}
	offers := make([]fdpass.Offer, 0, len(files))
	for name, f := range files {
		offers = append(offers, fdpass.Offer{Name: name, Type: types[name], File: f})
	}
	return offers
}

func closeFiles(files map[string]*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}
