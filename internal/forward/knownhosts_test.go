package forward

import (
	"bytes"
	"crypto/ed25519"
	crand "crypto/rand"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func captureWarns(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})), &buf
}

func testHostKey(t *testing.T) (ssh.PublicKey, ssh.PublicKey) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("new public key: %v", err)
	}
	otherRaw, _, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	other, err := ssh.NewPublicKey(otherRaw)
	if err != nil {
		t.Fatalf("new other public key: %v", err)
	}
	return sshPub, other
}

// TestLoadKnownHosts_SkipsMalformedLine reproduces a real-world file where a
// typo'd key type ("ssh-ed25518") sat next to valid entries: the loader must
// skip that line with a warning instead of failing the whole file, and the
// valid entries must keep authenticating.
func TestLoadKnownHosts_SkipsMalformedLine(t *testing.T) {
	sshPub, other := testHostKey(t)
	good := knownhosts.Line([]string{knownhosts.Normalize("h.example.com:22")}, sshPub)
	malformed := strings.Replace(good, "ssh-ed25519", "ssh-ed25518", 1)

	p := writeKnownHosts(t,
		"# plain comment",
		good,
		malformed,
	)

	log, buf := captureWarns(t)
	cb, err := loadKnownHosts(p, log)
	if err != nil {
		t.Fatalf("loadKnownHosts: %v", err)
	}
	if err := cb("h.example.com:22", &net.TCPAddr{}, sshPub); err != nil {
		t.Errorf("valid entry should authenticate: %v", err)
	}
	if err := cb("h.example.com:22", &net.TCPAddr{}, other); err == nil {
		t.Error("a different key for a known host must still be rejected")
	}

	out := buf.String()
	if !strings.Contains(out, "known_hosts: skipping malformed line") {
		t.Errorf("expected a skip warning, got: %s", out)
	}
	if !strings.Contains(out, "line=3") {
		t.Errorf("warning should point at line 3, got: %s", out)
	}
	if !strings.Contains(out, p) {
		t.Errorf("warning should name the original file, got: %s", out)
	}
	if !strings.Contains(out, "ssh-ed25518") {
		t.Errorf("warning should carry the underlying reason, got: %s", out)
	}
}

// TestLoadKnownHosts_WarnsOncePerLine locks the per-process dedupe: a second
// load of the same file must stay silent.
func TestLoadKnownHosts_WarnsOncePerLine(t *testing.T) {
	sshPub, _ := testHostKey(t)
	good := knownhosts.Line([]string{knownhosts.Normalize("h.example.com:22")}, sshPub)
	malformed := strings.Replace(good, "ssh-ed25519", "ssh-ed25518", 1)
	p := writeKnownHosts(t, malformed, good)

	log, buf := captureWarns(t)
	if _, err := loadKnownHosts(p, log); err != nil {
		t.Fatalf("first loadKnownHosts: %v", err)
	}
	if !strings.Contains(buf.String(), "skipping malformed line") {
		t.Fatalf("first load should warn, got: %s", buf.String())
	}

	log2, buf2 := captureWarns(t)
	if _, err := loadKnownHosts(p, log2); err != nil {
		t.Fatalf("second loadKnownHosts: %v", err)
	}
	if strings.Contains(buf2.String(), "skipping malformed line") {
		t.Errorf("second load of the same file must not warn again, got: %s", buf2.String())
	}
}

// TestLoadKnownHosts_CleanFileFastPath ensures a well-formed file loads
// without any warnings.
func TestLoadKnownHosts_CleanFileFastPath(t *testing.T) {
	sshPub, _ := testHostKey(t)
	good := knownhosts.Line([]string{knownhosts.Normalize("h.example.com:22")}, sshPub)
	p := writeKnownHosts(t, "# comment", good, "")

	log, buf := captureWarns(t)
	cb, err := loadKnownHosts(p, log)
	if err != nil {
		t.Fatalf("loadKnownHosts: %v", err)
	}
	if err := cb("h.example.com:22", &net.TCPAddr{}, sshPub); err != nil {
		t.Errorf("valid entry should authenticate: %v", err)
	}
	if out := buf.String(); out != "" {
		t.Errorf("clean file must not warn, got: %s", out)
	}
}

// TestLoadKnownHosts_MissingFileIsFatal keeps the pre-existing behaviour for
// I/O problems: an unreadable file is an error, not an empty trust database.
func TestLoadKnownHosts_MissingFileIsFatal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent_known_hosts")
	log, _ := captureWarns(t)
	_, err := loadKnownHosts(p, log)
	if err == nil {
		t.Fatal("missing file must be an error")
	}
	if !strings.Contains(err.Error(), "load known_hosts") {
		t.Errorf("error = %q, want a load known_hosts failure", err)
	}
}

// TestLoadKnownHosts_AllMalformedYieldsEmptyDB documents the agreed
// behaviour when every entry is broken: loading succeeds with an empty
// database (every host is unknown, TOFU can offer to re-add) rather than
// failing every tunnel.
func TestLoadKnownHosts_AllMalformedYieldsEmptyDB(t *testing.T) {
	sshPub, _ := testHostKey(t)
	malformed := "h.example.com ssh-ed25519 " + strings.Repeat("A", 80)
	p := writeKnownHosts(t, malformed)

	log, buf := captureWarns(t)
	cb, err := loadKnownHosts(p, log)
	if err != nil {
		t.Fatalf("loadKnownHosts: %v", err)
	}
	if err := cb("h.example.com:22", &net.TCPAddr{}, sshPub); err == nil {
		t.Error("host must be untrusted with no valid entries")
	}
	if !strings.Contains(buf.String(), "line=1") {
		t.Errorf("the malformed line should be reported, got: %s", buf.String())
	}
}

// TestLoadKnownHosts_SkipsOverlongLine covers lines beyond the 64 KiB
// bufio.Scanner limit: knownhosts.New cannot attribute those to a line, so
// they are skipped up front and the remaining entries still load.
func TestLoadKnownHosts_SkipsOverlongLine(t *testing.T) {
	sshPub, _ := testHostKey(t)
	good := knownhosts.Line([]string{knownhosts.Normalize("h.example.com:22")}, sshPub)
	overlong := "far.example.com ssh-ed25519 " + strings.Repeat("A", 70_000)
	p := writeKnownHosts(t, overlong, good)

	log, buf := captureWarns(t)
	cb, err := loadKnownHosts(p, log)
	if err != nil {
		t.Fatalf("loadKnownHosts: %v", err)
	}
	if err := cb("h.example.com:22", &net.TCPAddr{}, sshPub); err != nil {
		t.Errorf("valid entry should authenticate: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "line=1") || !strings.Contains(out, "line too long") {
		t.Errorf("overlong line should be skipped with a warning, got: %s", out)
	}
}
