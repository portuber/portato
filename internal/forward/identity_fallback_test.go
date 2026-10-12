package forward

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

func TestIdentityAfterUnrelatedAgentKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SSH_AUTH_SOCK regression requires the Unix agent transport")
	}
	if runtime.GOOS == "darwin" {
		t.Setenv("TMPDIR", "/tmp")
	}
	_, unrelated, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sock, stopAgent := startTestAgent(t, unrelated)
	defer stopAgent()
	t.Setenv("SSH_AUTH_SOCK", sock)

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	identity := filepath.Join(directory, "identity")
	if err := os.WriteFile(identity, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	server := sshtest.NewSSHD(t, authorized)
	server.Start()
	defer server.Stop()
	passwordAuth := false
	cfg := config.Tuber{User: "u", Host: "127.0.0.1", Port: server.Port, Identity: identity, PasswordAuth: &passwordAuth}
	defaults := config.Defaults{KnownHosts: filepath.Join(directory, "known_hosts"), AcceptNewHosts: true}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := dialSSH(ctx, cfg, defaults, slog.Default(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("identity must authenticate after agent key rejection: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentQueryHonorsCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		buffer := make([]byte, 4096)
		_, _ = server.Read(buffer)
		cancel()
	}()
	started := time.Now()
	_, err := boundedAgentSigners(ctx, client)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if time.Since(started) > time.Second {
		t.Fatal("agent query did not honor cancellation")
	}
}

func TestAgentQueryHonorsDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := boundedAgentSigners(ctx, client)
	if err == nil {
		t.Fatal("expected deadline error")
	}
	if time.Since(started) > time.Second {
		t.Fatal("agent query did not honor deadline")
	}
}
