//go:build unix

package fdpass

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// TestSendRecvRoundTrip is the core Phase 16 DoD test: a real *net.TCPListener
// survives a Send -> Recv transfer over a unixpacket socket and the adopted
// listener still accepts a connection dialed to the original local address.
func TestSendRecvRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "transfer.sock")
	xfer, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer xfer.Close()

	tcp1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp1: %v", err)
	}
	defer tcp1.Close()
	tcp2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp2: %v", err)
	}
	defer tcp2.Close()

	offers, err := buildOffers(map[string]net.Listener{"alpha": tcp1, "beta": tcp2})
	if err != nil {
		t.Fatalf("build offers: %v", err)
	}

	sent := make(chan error, 1)
	go func() {
		conn, aerr := xfer.Accept()
		if aerr != nil {
			sent <- aerr
			return
		}
		uc := conn.(*net.UnixConn)
		sent <- Send(uc, offers)
		_ = uc.Close()
	}()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial transfer socket: %v", err)
	}
	uc := conn.(*net.UnixConn)
	got, rerr := Recv(uc)
	_ = uc.Close()
	if rerr != nil {
		t.Fatalf("Recv: %v", rerr)
	}
	if err := <-sent; err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("want 2 listeners, got %d", len(got))
	}
	for _, name := range []string{"alpha", "beta"} {
		if got[name] == nil {
			t.Errorf("missing listener %q", name)
		} else {
			defer got[name].Close()
		}
	}

	// The adopted listener shares the kernel socket with the still-open
	// tcp1/tcp2; since nothing else Accepts on them, the adopted listener must
	// catch a connection dialed to the original addr.
	checkAdopt(t, "alpha", got["alpha"], tcp1.Addr().String())
	checkAdopt(t, "beta", got["beta"], tcp2.Addr().String())
}

func checkAdopt(t *testing.T, name string, ln net.Listener, addr string) {
	t.Helper()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Errorf("%s: dial original addr %s: %v", name, addr, err)
		return
	}
	defer c.Close()
	select {
	case got := <-accepted:
		_ = got.Close()
	case <-time.After(time.Second):
		t.Errorf("%s: adopted listener did not accept on %s", name, addr)
	}
}

// buildOffers is the test mirror of the standalone's offer construction: dup
// each listener's fd (File) and tag it with its type for the Header.
func buildOffers(listeners map[string]net.Listener) ([]Offer, error) {
	var offers []Offer
	for name, ln := range listeners {
		tcp, ok := ln.(*net.TCPListener)
		if !ok {
			return nil, fmt.Errorf("%s: not a *net.TCPListener", name)
		}
		f, err := tcp.File()
		if err != nil {
			return nil, err
		}
		offers = append(offers, Offer{Name: name, Type: "local", File: f})
	}
	return offers, nil
}

// TestRecvEmptyClose covers the EOF path: the sender half-closes without sending
// anything (e.g. no live listeners to pass) and Recv returns an empty map.
func TestRecvEmptyClose(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "transfer.sock")
	xfer, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer xfer.Close()

	go func() {
		conn, _ := xfer.Accept()
		if conn != nil {
			uc := conn.(*net.UnixConn)
			_ = Send(uc, nil)
			_ = uc.Close()
		}
	}()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	uc := conn.(*net.UnixConn)
	defer uc.Close()
	got, err := Recv(uc)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty map, got %d", len(got))
	}
}
