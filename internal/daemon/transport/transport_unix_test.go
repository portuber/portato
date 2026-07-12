//go:build unix

package transport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUnixListenDialRoundTrip exercises the unix transport end to end: Listen
// binds the socket (creating its parent dir and chmodding it 0600), and Dial
// reaches a listening address. The address is rooted directly in os.TempDir()
// (mirroring the real RuntimeSocketPath) so the path stays under the unix
// socket-name limit on macOS, where t.TempDir() can be too long.
func TestUnixListenDialRoundTrip(t *testing.T) {
	addr := filepath.Join(os.TempDir(), fmt.Sprintf("ptran-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(addr) })

	ln, err := Default.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	if info, err := os.Stat(addr); err != nil {
		t.Fatalf("stat socket: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket perm = %o, want 0600", info.Mode().Perm())
	}

	// Listen also works when the parent dir does not exist yet.
	addr2 := filepath.Join(os.TempDir(), fmt.Sprintf("ptran-nested-%d", time.Now().UnixNano()), "x.sock")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(addr2)) })
	ln2, err := Default.Listen(addr2)
	if err != nil {
		t.Fatalf("Listen with missing parent dir: %v", err)
	}
	defer ln2.Close()

	conn, err := Default.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close()
}
