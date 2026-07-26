package forward

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// writeProbeKey generates an unencrypted ed25519 client key, writes it to a
// temp file, and returns the path + the parsed public key (which the test
// passes to the sshtest server's authorized-key set).
func writeProbeKey(t *testing.T) (idPath string, pub ssh.PublicKey) {
	t.Helper()
	rawPub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	pub, _ = ssh.NewPublicKey(rawPub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal client priv: %v", err)
	}
	idPath = filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(idPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return idPath, pub
}

func TestProbeForwarding_Healthy(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "") // hermetic: no host agent
	idPath, pub := writeProbeKey(t)
	srv := sshtest.NewSSHD(t, pub)
	srv.Start()
	defer srv.Stop()

	cfg := config.Tuber{
		Name: "probe", Type: "local", Local: "1", Remote: "127.0.0.1:1",
		SSH: "u@" + srv.Addr(), Identity: idPath, User: "u", Host: "127.0.0.1", Port: srv.Port,
	}
	def := config.Defaults{KnownHosts: filepath.Join(filepath.Dir(idPath), "known_hosts"), AcceptNewHosts: true}

	res := ProbeForwarding(context.Background(), cfg, def, slog.Default())
	if res.Outcome != ProbeHealthy {
		t.Fatalf("got %s, want healthy; detail=%s", res.Outcome, res.Detail)
	}
}

func TestProbeForwarding_AllowTcpForwardingNo(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	idPath, pub := writeProbeKey(t)
	srv := sshtest.NewSSHDNoForward(t, pub)
	srv.Start()
	defer srv.Stop()

	cfg := config.Tuber{
		Name: "probe", Type: "local", Local: "1", Remote: "127.0.0.1:1",
		SSH: "u@" + srv.Addr(), Identity: idPath, User: "u", Host: "127.0.0.1", Port: srv.Port,
	}
	def := config.Defaults{KnownHosts: filepath.Join(filepath.Dir(idPath), "known_hosts"), AcceptNewHosts: true}

	res := ProbeForwarding(context.Background(), cfg, def, slog.Default())
	if res.Outcome != ProbeForwardingDenied {
		t.Fatalf("got %s, want forwarding-denied; detail=%s", res.Outcome, res.Detail)
	}
}

func TestProbeForwarding_ConnectFailed(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	idPath, _ := writeProbeKey(t)
	closedPort := freePort(t) // a port with nothing listening → connection refused

	cfg := config.Tuber{
		Name: "x", Type: "local", Local: "1", Remote: "127.0.0.1:1",
		SSH: fmt.Sprintf("u@127.0.0.1:%d", closedPort), Identity: idPath,
		User: "u", Host: "127.0.0.1", Port: closedPort,
	}
	def := config.Defaults{KnownHosts: filepath.Join(filepath.Dir(idPath), "known_hosts"), AcceptNewHosts: true}

	res := ProbeForwarding(context.Background(), cfg, def, slog.Default())
	if res.Outcome != ProbeConnectFailed {
		t.Fatalf("got %s, want connect-failed; detail=%s", res.Outcome, res.Detail)
	}
}

func TestProbeForwarding_AuthUnavailable(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	// No identity, no agent → authMethods returns empty.
	cfg := config.Tuber{
		Name: "x", Type: "local", Local: "1", Remote: "127.0.0.1:1",
		SSH: "u@127.0.0.1:2222", User: "u", Host: "127.0.0.1", Port: 2222,
	}
	def := config.Defaults{KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), AcceptNewHosts: true}

	res := ProbeForwarding(context.Background(), cfg, def, slog.Default())
	if res.Outcome != ProbeAuthUnavailable {
		t.Fatalf("got %s, want auth-unavailable; detail=%s", res.Outcome, res.Detail)
	}
}

func TestIsNonLoopbackBind(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{":16379", true},
		{"*:16379", true},
		{"0.0.0.0:16379", true},
		{"[::]:16379", true},
		{"127.0.0.1:16379", false},
		{"203.0.113.5:16379", true},
		{"localhost:16379", false},
	}
	for _, c := range cases {
		if got := isNonLoopbackBind(c.addr); got != c.want {
			t.Errorf("isNonLoopbackBind(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

func TestDialHintMsg(t *testing.T) {
	prohibited := &ssh.OpenChannelError{Reason: ssh.Prohibited, Message: "admin"}
	connFailed := &ssh.OpenChannelError{Reason: ssh.ConnectionFailed, Message: "refused"}

	if got := dialHintMsg("dial remote failed", errors.New("boom")); got != "dial remote failed" {
		t.Errorf("plain error: got %q, want prefix unchanged", got)
	}
	got := dialHintMsg("dial remote failed", prohibited)
	if !strings.Contains(got, "AllowTcpForwarding") {
		t.Errorf("prohibited rejection: got %q, want the AllowTcpForwarding hint", got)
	}
	if got := dialHintMsg("socks5 dial failed", connFailed); got != "socks5 dial failed" {
		t.Errorf("non-prohibited rejection: got %q, want prefix unchanged", got)
	}
}
