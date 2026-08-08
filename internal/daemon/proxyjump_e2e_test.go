//go:build e2e && unix

package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// e2eBin is a real built portato binary, produced once in TestMain. The jump
// E2E spawns it as the daemon, so this is a true black-box E2E (real binary,
// real SSH servers, real -L traffic) rather than an in-process mock.
var e2eBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "portato-pj-e2e-bin")
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

// TestProxyJumpE2E_ForwardsThroughBastion is the Phase 43 black-box proof: a
// real `portato daemon` binary reads a jump config, dials the target THROUGH a
// bastion (two real in-process SSH servers), and a -L forward carries traffic
// end to end. It also confirms the chain self-heals when the bastion drops.
//
// It complements the in-package forward tests (which exercise dialConn/Tuber
// directly against the same fixtures) by proving the integrated binary path —
// config load -> daemon StartEnabledWith -> dialConn chain -> forward -> IPC
// `list` reports Connected — exactly what a user observes.
func TestProxyJumpE2E_ForwardsThroughBastion(t *testing.T) {
	setupE2EEnv(t)
	echoAddr := startE2EEcho(t)
	cfgPath, localAddr, edge, _, cleanup := buildJumpConfig(t, echoAddr)
	defer cleanup()

	// Spawn the REAL built binary as the daemon. It binds the IPC socket at
	// PORTATO_SOCKET (set by setupE2EEnv) and auto-starts the enabled jump
	// tuber at boot (StartEnabledWith), so no IPC Enable is needed.
	daemonCmd := exec.Command(e2eBin, "daemon", "--config", cfgPath)
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("spawn daemon: %v", err)
	}
	t.Cleanup(func() {
		if daemonCmd.Process != nil {
			_ = daemonCmd.Process.Signal(syscall.SIGTERM)
			_, _ = daemonCmd.Process.Wait()
		}
	})

	const name = "echo-via-bastion"
	socket := os.Getenv("PORTATO_SOCKET")
	if !waitSocket(socket, 10*time.Second) {
		t.Fatalf("daemon IPC socket %s never appeared", socket)
	}
	if !waitE2EStatus(socket, name, controller.Connected, 20*time.Second) {
		t.Fatalf("jump tuber did not reach Connected through the bastion\n%s", dumpDaemonLog(t))
	}

	// Primary proof: real -L traffic round-trips through edge -> target.
	if !waitPing(localAddr, 5*time.Second) {
		t.Fatalf("forward through the bastion never carried traffic\n%s", dumpDaemonLog(t))
	}

	// Resilience: drop the bastion -> the chain dies -> tuber leaves Connected;
	// restart the bastion -> reconnect rebuilds the chain and traffic flows.
	edge.Stop()
	if !waitE2ENotState(socket, name, controller.Connected, 10*time.Second) {
		t.Fatalf("tuber stayed Connected after the bastion was killed")
	}
	edge.Restart()
	if !waitE2EStatus(socket, name, controller.Connected, 20*time.Second) {
		t.Fatalf("jump tuber did not reconnect after the bastion restarted\n%s", dumpDaemonLog(t))
	}
	if !waitPing(localAddr, 5*time.Second) {
		t.Fatalf("forward did not carry traffic after reconnect\n%s", dumpDaemonLog(t))
	}
}

// buildJumpConfig stands up two SSH servers (a bastion `edge` and a `target`,
// both authorizing the one client key — the shared-identity model), writes a
// one-tuber config whose `ssh:` is the target and `jump:` is the bastion, and
// returns the config path + the tuber's local -L dial address. The tuber is
// `enabled: true` so the daemon auto-starts it at boot.
func buildJumpConfig(t *testing.T, echoAddr string) (cfgPath, localAddr string, edge, target *sshtest.SSHD, cleanup func()) {
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

	edge = sshtest.NewSSHD(t, authorizedKey)
	edge.Start()
	target = sshtest.NewSSHD(t, authorizedKey)
	target.Start()

	localPort := freeE2EPort(t)
	localAddr = fmt.Sprintf("127.0.0.1:%d", localPort)
	cfgPath = filepath.Join(root, "config.yaml")
	cfg := &config.Config{
		Defaults: config.Defaults{
			Identity:       idPath,
			KnownHosts:     filepath.Join(root, "known_hosts"),
			AcceptNewHosts: true,
		},
		Tubers: []config.Tuber{{
			Name: "echo-via-bastion", Type: "local", Local: strconv.Itoa(localPort),
			Remote: echoAddr, SSH: "u@" + target.Addr(), Jump: "u@" + edge.Addr(),
			Identity: idPath, User: "u", Host: "127.0.0.1", Port: target.Port, Enabled: true,
		}},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return cfgPath, localAddr, edge, target, func() {
		edge.Stop()
		target.Stop()
	}
}

// setupE2EEnv isolates the spawned daemon (socket/marker/lock/logs) from any
// real daemon on the host and forces identity-file auth (no host ssh-agent).
// Unix-socket paths are kept short (macOS's ~104-char limit): the t.TempDir()
// paths are too long for a unix-socket bind, so the socket lives in a short
// /tmp dir while the file-based XDG paths use a normal temp dir.
func setupE2EEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SSH_AUTH_SOCK", "")
	short, err := os.MkdirTemp("/tmp", "pjpj")
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

func waitSocket(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func waitE2EStatus(socket, name string, state controller.State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sts, err := client.New(socket).List()
		if err == nil {
			for _, s := range sts {
				if s.Name == name && s.State == state {
					return true
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func waitE2ENotState(socket, name string, state controller.State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sts, err := client.New(socket).List()
		if err == nil {
			for _, s := range sts {
				if s.Name == name && s.State != state {
					return true
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitPing dials the local -L port, sends a marker, and expects the echo
// backend to return it (the forward path end to end through the chain).
func waitPing(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			msg := []byte("pj-e2e")
			if _, err := c.Write(msg); err == nil {
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, len(msg))
				if _, err := io.ReadFull(c, buf); err == nil && string(buf) == string(msg) {
					_ = c.Close()
					return true
				}
			}
			_ = c.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// dumpDaemonLog prints the spawned daemon's log (XDG_STATE_HOME) to aid
// debugging when an E2E assertion fails (e.g. the chain never connected).
func dumpDaemonLog(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		return "(no XDG_STATE_HOME)"
	}
	data, err := os.ReadFile(filepath.Join(dir, "portato", "daemon.log"))
	if err != nil {
		return fmt.Sprintf("(daemon log: read %s: %v)", filepath.Join(dir, "portato", "daemon.log"), err)
	}
	return "=== daemon.log ===\n" + string(data)
}
