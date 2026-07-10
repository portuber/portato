package tui

import (
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/fdpass"
	routelog "github.com/portuber/portato/internal/log"
)

// handoffFakeCtrl records the order of operations against a shared log so a
// test can assert e.g. Close() happens before the daemon spawn.
type handoffFakeCtrl struct {
	log       *[]string
	statuses  []controller.Status
	changes   chan struct{}
	liveFiles map[string]*os.File // returned by LiveListenerFiles (nil -> empty -> legacy path)
	liveErr   error               // when set, LiveListenerFiles returns this error (forces fallback)
}

func (h *handoffFakeCtrl) List() []controller.Status { return h.statuses }
func (h *handoffFakeCtrl) Enable(string) error       { return nil }
func (h *handoffFakeCtrl) Disable(string) error      { return nil }
func (h *handoffFakeCtrl) Restart(string) error      { return nil }
func (h *handoffFakeCtrl) Reload() error             { return nil }
func (h *handoffFakeCtrl) Changes() <-chan struct{}  { return h.changes }
func (h *handoffFakeCtrl) Config() (*config.Config, error) {
	return &config.Config{}, nil
}
func (h *handoffFakeCtrl) AddTuber(config.Tuber) error            { return nil }
func (h *handoffFakeCtrl) UpdateTuber(string, config.Tuber) error { return nil }
func (h *handoffFakeCtrl) DeleteTuber(string) error               { return nil }
func (h *handoffFakeCtrl) Logs(string) ([]routelog.Entry, error)  { return nil, nil }
func (h *handoffFakeCtrl) AcceptHost(string) error                { return nil }
func (h *handoffFakeCtrl) AcceptPassphrase(string, string) error  { return nil }
func (h *handoffFakeCtrl) LiveListenerFiles() (map[string]*os.File, error) {
	if h.liveErr != nil {
		return nil, h.liveErr
	}
	return h.liveFiles, nil
}
func (h *handoffFakeCtrl) Close() error {
	*h.log = append(*h.log, "close")
	return nil
}

func restoreHandoffSeams(t *testing.T) {
	t.Helper()
	sc, ps, pi, to := startCmd, probeSocket, handoffPollInterval, handoffTimeout
	t.Cleanup(func() {
		startCmd, probeSocket = sc, ps
		handoffPollInterval, handoffTimeout = pi, to
	})
}

// TestHandoff_Fallback_CloseBeforeSpawn exercises the Phase 5 fallback path:
// when the controller has no live local listeners (handoffFakeCtrl.liveFiles is
// nil), the FD hand-off is skipped and the legacy close->spawn->probe order
// runs. Close releases the ports before the spawn so the daemon can rebind them.
func TestHandoff_Fallback_CloseBeforeSpawn(t *testing.T) {
	var log []string
	ctrl := &handoffFakeCtrl{log: &log, changes: make(chan struct{})}
	restoreHandoffSeams(t)

	startCmd = func(string, string) error { log = append(log, "spawn"); return nil }
	probes := 0
	probeSocket = func() bool {
		probes++
		log = append(log, "probe")
		return probes >= 2 // ready on the second probe
	}

	if err := handoffToDaemon(ctrl, "/cfg"); err != nil {
		t.Fatalf("handoffToDaemon: %v", err)
	}
	if probes < 2 {
		t.Errorf("expected at least 2 probes, got %d", probes)
	}
	if log[0] != "close" {
		t.Errorf("first op should be close (release ports before bind); got order %v", log)
	}
	spawnIdx := indexOf(log, "spawn")
	if spawnIdx < 0 || spawnIdx != 1 {
		t.Errorf("spawn should follow close; got order %v", log)
	}
}

func TestHandoff_SpawnError(t *testing.T) {
	var log []string
	ctrl := &handoffFakeCtrl{log: &log, changes: make(chan struct{})}
	restoreHandoffSeams(t)

	boom := errors.New("no such binary")
	startCmd = func(string, string) error { return boom }
	probeSocket = func() bool { return true }

	err := handoffToDaemon(ctrl, "/cfg")
	if err == nil || !strings.Contains(err.Error(), "spawn daemon") {
		t.Errorf("expected spawn error, got %v", err)
	}
	if log[0] != "close" {
		t.Errorf("ports should still be released before the (failed) spawn; got %v", log)
	}
}

func TestHandoff_Timeout(t *testing.T) {
	var log []string
	ctrl := &handoffFakeCtrl{log: &log, changes: make(chan struct{})}
	restoreHandoffSeams(t)

	handoffPollInterval = 5 * time.Millisecond
	handoffTimeout = 20 * time.Millisecond
	startCmd = func(string, string) error { return nil }
	probeSocket = func() bool { return false } // never becomes ready

	err := handoffToDaemon(ctrl, "/cfg")
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Errorf("expected timeout error, got %v", err)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func contains(s []string, v string) bool { return indexOf(s, v) >= 0 }

// TestHandoff_FDPass is the core Phase 16 invariant: the standalone's live
// local listener reaches the spawned daemon intact and keeps accepting on the
// original local port, and the standalone is closed AFTER the spawn (not before,
// as in the fallback). The fake startCmd simulates the daemon: it dials the
// transfer socket, Recvs the listener via fdpass, and probeSocket flips to true
// once it has arrived.
func TestHandoff_FDPass(t *testing.T) {
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer tcpLn.Close()
	tcp, ok := tcpLn.(*net.TCPListener)
	if !ok {
		t.Fatalf("want *net.TCPListener, got %T", tcpLn)
	}
	liveFile, err := tcp.File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer liveFile.Close()

	var log []string
	ctrl := &handoffFakeCtrl{
		log:       &log,
		changes:   make(chan struct{}),
		statuses:  []controller.Status{{Name: "db", Type: "local", State: controller.Connected, Local: tcpLn.Addr().String()}},
		liveFiles: map[string]*os.File{"db": liveFile},
	}
	restoreHandoffSeams(t)

	var (
		mu      sync.Mutex
		adopted map[string]net.Listener
	)
	startCmd = func(_, sockPath string) error {
		log = append(log, "spawn")
		go func() {
			conn, derr := net.Dial("unix", sockPath)
			if conn == nil || derr != nil {
				return
			}
			uc := conn.(*net.UnixConn)
			got, rerr := fdpass.Recv(uc)
			_ = uc.Close()
			if rerr != nil {
				return
			}
			mu.Lock()
			adopted = got
			mu.Unlock()
		}()
		return nil
	}
	probeSocket = func() bool {
		mu.Lock()
		defer mu.Unlock()
		return adopted != nil
	}

	if err := handoffToDaemon(ctrl, "/cfg"); err != nil {
		t.Fatalf("handoffToDaemon: %v", err)
	}

	// FD path: ctrl is closed AFTER spawn (the standalone stays up until the
	// daemon has adopted).
	spawnIdx := indexOf(log, "spawn")
	closeIdx := indexOf(log, "close")
	if spawnIdx < 0 {
		t.Fatalf("spawn not logged: %v", log)
	}
	if closeIdx < 0 {
		t.Fatalf("ctrl not closed: %v", log)
	}
	if closeIdx < spawnIdx {
		t.Errorf("FD path: close should follow spawn; got order %v", log)
	}

	mu.Lock()
	a := adopted
	mu.Unlock()
	if a == nil || a["db"] == nil {
		t.Fatalf("daemon did not receive listener; adopted=%v", a)
	}
	defer a["db"].Close()
	// The adopted listener shares the kernel socket with the still-open tcpLn,
	// so it must accept a connection dialed to the original local port -- the
	// "port never goes down" invariant.
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := a["db"].Accept()
		if e == nil {
			accepted <- c
		}
	}()
	c, err := net.DialTimeout("tcp", tcpLn.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("dial local port: %v", err)
	}
	defer c.Close()
	select {
	case got := <-accepted:
		_ = got.Close()
	case <-time.After(time.Second):
		t.Errorf("adopted listener did not accept on %s", tcpLn.Addr())
	}
}

// TestHandoff_FallbackOnEnumerateError: when LiveListenerFiles errors, the FD
// path aborts before spawning and the legacy close->spawn->probe order runs.
func TestHandoff_FallbackOnEnumerateError(t *testing.T) {
	var log []string
	ctrl := &handoffFakeCtrl{log: &log, changes: make(chan struct{}), liveErr: errors.New("boom")}
	restoreHandoffSeams(t)
	startCmd = func(string, string) error { log = append(log, "spawn"); return nil }
	probeSocket = func() bool { log = append(log, "probe"); return true }

	if err := handoffToDaemon(ctrl, "/cfg"); err != nil {
		t.Fatalf("handoffToDaemon: %v", err)
	}
	if !contains(log, "spawn") {
		t.Errorf("legacy spawn should run; got %v", log)
	}
	if log[0] != "close" {
		t.Errorf("fallback: close should precede spawn; got order %v", log)
	}
}

// TestHandoff_FDTimeout: with a live listener but a daemon that never adopts,
// the FD path times out (and the spawn did happen, so no second spawn).
func TestHandoff_FDTimeout(t *testing.T) {
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer tcpLn.Close()
	liveFile, err := tcpLn.(*net.TCPListener).File()
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer liveFile.Close()

	var log []string
	ctrl := &handoffFakeCtrl{
		log:       &log,
		changes:   make(chan struct{}),
		liveFiles: map[string]*os.File{"db": liveFile},
	}
	restoreHandoffSeams(t)
	handoffPollInterval = 5 * time.Millisecond
	handoffTimeout = 20 * time.Millisecond
	spawns := 0
	startCmd = func(string, string) error { spawns++; log = append(log, "spawn"); return nil }
	probeSocket = func() bool { return false } // daemon never becomes ready

	err = handoffToDaemon(ctrl, "/cfg")
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Errorf("expected timeout error, got %v", err)
	}
	if spawns != 1 {
		t.Errorf("expected exactly one spawn (no fallback double-spawn), got %d", spawns)
	}
}
