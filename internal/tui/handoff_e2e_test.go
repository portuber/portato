//go:build e2e

package tui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// e2eBin is a real built portato binary, produced once in TestMain. The hand-off
// spawns it as the daemon, so this is a true black-box E2E (real binary, real
// SSH server, real SCM_RIGHTS transfer) rather than an in-process mock.
var e2eBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "portato-e2e-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: mkdtemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	e2eBin = filepath.Join(dir, "portato")
	build := exec.Command("go", "build", "-o", e2eBin, "github.com/portuber/portato/cmd/portato")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: build portato: %v\n%s\n", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestHandoffE2E_PortStaysUp is the black-box Phase 16 proof: a real portato
// daemon binary adopts the standalone's live local listener over SCM_RIGHTS, and
// the local port NEVER refuses a connection across the standalone->daemon
// transition (the DoD's "nc -z never fails" invariant). A tight dial loop runs
// before, during and after the hand-off and must see zero ECONNREFUSED.
func TestHandoffE2E_PortStaysUp(t *testing.T) {
	setupE2EEnv(t)

	echoAddr := startE2EEcho(t)
	cfgPath, ctrl, localAddr, cleanup := buildE2EStandalone(t, echoAddr, "e2e", "local")
	defer cleanup()

	// Start the tunnel in the standalone and wait for it to connect.
	if err := ctrl.Enable("e2e"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !waitStatus(func() ([]controller.Status, error) { return ctrl.List(), nil }, "e2e", controller.Connected, 5*time.Second) {
		t.Fatalf("standalone tunnel did not reach Connected")
	}
	if !pingE2E(t, localAddr) {
		t.Fatalf("baseline ping through standalone tunnel failed")
	}

	// Dial the local port continuously; it must NEVER be refused across the
	// hand-off (the seamless-port invariant).
	var refused int32
	stopDials := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopDials:
				return
			default:
			}
			c, derr := net.DialTimeout("tcp", localAddr, 200*time.Millisecond)
			if derr != nil {
				if errors.Is(derr, syscall.ECONNREFUSED) {
					atomic.AddInt32(&refused, 1)
				}
			} else {
				_ = c.Close()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Restore seams (startCmd/probeSocket) at the end; override startCmd to spawn
	// the REAL built binary as the daemon, and kill it on cleanup.
	restoreHandoffSeams(t)
	var daemonCmd *exec.Cmd
	startCmd = func(cfg, listenFds string) error {
		c := exec.Command(e2eBin, "daemon", "--config", cfg, "--listen-fds", listenFds)
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := c.Start(); err != nil {
			return err
		}
		daemonCmd = c
		return nil
	}
	t.Cleanup(func() {
		if daemonCmd != nil && daemonCmd.Process != nil {
			_ = daemonCmd.Process.Signal(syscall.SIGTERM)
			_, _ = daemonCmd.Process.Wait()
		}
	})

	// The hand-off: passes the live listener to the spawned daemon over the
	// transfer socket and waits for its healthz.
	if err := handoffToDaemon(ctrl, cfgPath); err != nil {
		close(stopDials)
		wg.Wait()
		t.Fatalf("handoffToDaemon: %v", err)
	}

	// Give the port a moment to be served purely by the adopted daemon, then stop
	// the dial loop and assert zero refusals across the whole transition.
	time.Sleep(300 * time.Millisecond)
	close(stopDials)
	wg.Wait()
	if got := atomic.LoadInt32(&refused); got != 0 {
		dumpE2EDaemonLog(t)
		t.Errorf("local port refused %d time(s) during hand-off; want 0 (the seamless-port invariant)", got)
	}

	// The daemon must now own the port and forward through its re-dialled SSH
	// session (uptime is fresh, but new connections are seamless).
	if !waitPingE2E(localAddr, 5*time.Second) {
		t.Fatalf("daemon tunnel did not forward after hand-off")
	}
	socket := os.Getenv("PORTATO_SOCKET")
	if !waitStatus(func() ([]controller.Status, error) { return client.New(socket).List() }, "e2e", controller.Connected, 5*time.Second) {
		t.Fatalf("daemon did not report the tunnel Connected after hand-off")
	}
}

// TestHandoffE2E_Fallback exercises the Phase 5 close+rebind fallback: when the
// FD transfer is unavailable (LiveListenerFiles errors), the standalone falls
// back to stopping its tunnels, spawning the daemon and letting it rebind. The
// daemon still comes up and forwards (with a brief port blip, which is expected
// here and not measured).
func TestHandoffE2E_Fallback(t *testing.T) {
	setupE2EEnv(t)

	echoAddr := startE2EEcho(t)
	cfgPath, ctrl, localAddr, cleanup := buildE2EStandalone(t, echoAddr, "fb", "local")
	defer cleanup()

	if err := ctrl.Enable("fb"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !waitStatus(func() ([]controller.Status, error) { return ctrl.List(), nil }, "fb", controller.Connected, 5*time.Second) {
		t.Fatalf("standalone tunnel did not reach Connected")
	}

	// Wrap the controller so LiveListenerFiles always errors -> handoffWithFDs
	// aborts before spawning and handoffToDaemon falls back to the legacy path.
	wrapped := &fallbackCtrl{Controller: ctrl}

	restoreHandoffSeams(t)
	var daemonCmd *exec.Cmd
	startCmd = func(cfg, _ string) error {
		// Legacy path spawns WITHOUT --listen-fds.
		c := exec.Command(e2eBin, "daemon", "--config", cfg)
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := c.Start(); err != nil {
			return err
		}
		daemonCmd = c
		return nil
	}
	t.Cleanup(func() {
		if daemonCmd != nil && daemonCmd.Process != nil {
			_ = daemonCmd.Process.Signal(syscall.SIGTERM)
			_, _ = daemonCmd.Process.Wait()
		}
	})

	if err := handoffToDaemon(wrapped, cfgPath); err != nil {
		t.Fatalf("handoffToDaemon (fallback): %v", err)
	}

	// Fallback rebinds the port: the daemon must end up serving it.
	if !waitPingE2E(localAddr, 5*time.Second) {
		t.Fatalf("daemon tunnel did not forward after fallback hand-off")
	}
}

// fallbackCtrl delegates everything to the wrapped controller but forces the FD
// path to be unavailable, exercising the legacy fallback.
type fallbackCtrl struct{ controller.Controller }

func (fallbackCtrl) LiveListenerFiles() (map[string]*os.File, error) {
	return nil, errors.New("forced fallback")
}

// setupE2EEnv isolates the spawned daemon (socket/marker/lock/logs) from any real
// daemon running on the host and forces identity-file auth (no host ssh-agent).
// Unix-domain socket paths are kept short (under macOS's ~104-char limit): the
// t.TempDir() paths are too long for a unix-socket bind, so the sockets live in
// a short /tmp dir while the file-based XDG paths use a normal temp dir.
func setupE2EEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SSH_AUTH_SOCK", "")
	short, err := os.MkdirTemp("/tmp", "pte2e")
	if err != nil {
		t.Fatalf("mkdtemp /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	t.Setenv("TMPDIR", short)
	t.Setenv("PORTATO_SOCKET", filepath.Join(short, "portato.sock"))
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
}

// buildE2EStandalone stands up a real SSH server, writes a one-tunnel config to
// disk, builds a Local controller over it (without starting the tunnel), and
// returns the config path, controller, the tunnel's local dial address, and a
// cleanup. The caller starts the tunnel via ctrl.Enable(name).
func buildE2EStandalone(t *testing.T, echoAddr, name, typ string) (string, controller.Controller, string, func()) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	authorizedKey, _ := ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal priv: %v", err)
	}
	root := t.TempDir()
	idPath := filepath.Join(root, "id_ed25519")
	if err := os.WriteFile(idPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()

	localPort := freeE2EPort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)

	cfgPath := filepath.Join(root, "config.yaml")
	cfg := &config.Config{
		Defaults: config.Defaults{
			Identity:       idPath,
			KnownHosts:     filepath.Join(root, "known_hosts"),
			AcceptNewHosts: true,
		},
		Tunnels: []config.Tunnel{{
			Name: name, Type: typ, Local: strconv.Itoa(localPort),
			Remote: echoAddr, SSH: "u@" + srv.Addr(), Identity: idPath,
			User: "u", Host: "127.0.0.1", Port: srv.Port, Enabled: true,
		}},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctrl := controller.NewLocal(cfg, cfgPath, slog.Default(), nil)
	return cfgPath, ctrl, localAddr, func() {
		_ = ctrl.Close()
		srv.Stop()
	}
}

// waitStatus polls list until a tunnel named name reaches state, or timeout.
func waitStatus(list func() ([]controller.Status, error), name string, state controller.State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sts, err := list()
		if err == nil {
			for _, s := range sts {
				if s.Name == name && s.State == state {
					return true
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func waitPingE2E(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pingE2E(nil, addr) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// pingE2E dials the local port, sends a marker, and expects the echo backend to
// return it (proving the forward path end to end).
func pingE2E(t *testing.T, addr string) bool {
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		if t != nil {
			t.Logf("ping dial %s: %v", addr, err)
		}
		return false
	}
	defer c.Close()
	msg := []byte("e2e-ping")
	if _, err := c.Write(msg); err != nil {
		return false
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		return false
	}
	return string(buf) == string(msg)
}

func startE2EEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); _, _ = io.Copy(c, c) }(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func freeE2EPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

// dumpE2EDaemonLog prints the spawned daemon's log (XDG_STATE_HOME) to aid
// debugging when an E2E assertion fails (e.g. FD adoption not happening).
func dumpE2EDaemonLog(t *testing.T) {
	t.Helper()
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, "portato", "daemon.log"))
	if err != nil {
		t.Logf("daemon log: read %s: %v", filepath.Join(dir, "portato", "daemon.log"), err)
		return
	}
	t.Logf("=== daemon.log ===\n%s", data)
}
