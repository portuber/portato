//go:build e2e && unix

package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// TestSSHConfigE2E_ForwardsThroughAlias is the Phase 44 black-box proof: a real
// `portato daemon` binary reads a config whose `ssh:` is a ~/.ssh/config alias
// (no `jump:`), resolves HostName/Port/IdentityFile/ProxyJump from it, dials the
// target THROUGH the alias's bastion, and a -L forward carries traffic end to
// end. This is the integrated path the in-package config tests can't reach:
// config load -> ssh-config resolution -> Phase 43 dialConn chain -> forward ->
// IPC `list` reports Connected. It also confirms the resolved chain self-heals
// when the bastion drops.
func TestSSHConfigE2E_ForwardsThroughAlias(t *testing.T) {
	setupE2EEnv(t)
	echoAddr := startE2EEcho(t)
	cfgPath, localAddr, edge, _, cleanup := buildSSHAliasConfig(t, echoAddr)
	defer cleanup()

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

	const name = "echo-via-alias"
	socket := os.Getenv("PORTATO_SOCKET")
	if !waitSocket(socket, 10*time.Second) {
		t.Fatalf("daemon IPC socket %s never appeared", socket)
	}
	if !waitE2EStatus(socket, name, controller.Connected, 20*time.Second) {
		t.Fatalf("alias tuber did not reach Connected through the bastion\n%s", dumpDaemonLog(t))
	}

	if !waitPing(localAddr, 5*time.Second) {
		t.Fatalf("forward through the ssh-config alias never carried traffic\n%s", dumpDaemonLog(t))
	}

	edge.Stop()
	if !waitE2ENotState(socket, name, controller.Connected, 10*time.Second) {
		t.Fatalf("tuber stayed Connected after the bastion was killed")
	}
	edge.Restart()
	if !waitE2EStatus(socket, name, controller.Connected, 20*time.Second) {
		t.Fatalf("alias tuber did not reconnect after the bastion restarted\n%s", dumpDaemonLog(t))
	}
	if !waitPing(localAddr, 5*time.Second) {
		t.Fatalf("forward did not carry traffic after reconnect\n%s", dumpDaemonLog(t))
	}
}

// buildSSHAliasConfig stands up two SSH servers (a bastion `edge` and a
// `target`, both authorizing the one client key), writes a ~/.ssh/config whose
// `Host thetarget` resolves to the target and ProxyJumps through the edge, then
// a one-tuber config whose `ssh:` is that alias (no `jump:` — the chain comes
// from ssh-config). HOME is pointed at the temp dir so the spawned daemon reads
// this ~/.ssh/config. The tuber has no explicit identity, so the alias's
// IdentityFile is the one exercised.
func buildSSHAliasConfig(t *testing.T, echoAddr string) (cfgPath, localAddr string, edge, target *sshtest.SSHD, cleanup func()) {
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

	sshDir := filepath.Join(root, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	sshConfig := fmt.Sprintf("Host thetarget\n  HostName 127.0.0.1\n  Port %d\n  User u\n  IdentityFile %s\n  ProxyJump u@127.0.0.1:%d\n",
		target.Port, idPath, edge.Port)
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(sshConfig), 0o600); err != nil {
		t.Fatalf("write ~/.ssh/config: %v", err)
	}
	t.Setenv("HOME", root)

	localPort := freeE2EPort(t)
	localAddr = fmt.Sprintf("127.0.0.1:%d", localPort)
	cfgPath = filepath.Join(root, "config.yaml")
	cfg := &config.Config{
		Defaults: config.Defaults{
			KnownHosts:     filepath.Join(root, "known_hosts"),
			AcceptNewHosts: true,
		},
		Tubers: []config.Tuber{{
			Name: "echo-via-alias", Type: "local", Local: strconv.Itoa(localPort),
			Remote: echoAddr, SSH: "thetarget", Enabled: true,
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
