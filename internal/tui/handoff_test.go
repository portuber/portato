package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/controller"
)

// handoffFakeCtrl records the order of operations against a shared log so a
// test can assert e.g. Close() happens before the daemon spawn.
type handoffFakeCtrl struct {
	log      *[]string
	statuses []controller.Status
	changes  chan struct{}
}

func (h *handoffFakeCtrl) List() []controller.Status { return h.statuses }
func (h *handoffFakeCtrl) Enable(string) error       { return nil }
func (h *handoffFakeCtrl) Disable(string) error      { return nil }
func (h *handoffFakeCtrl) Restart(string) error      { return nil }
func (h *handoffFakeCtrl) Reload() error             { return nil }
func (h *handoffFakeCtrl) Changes() <-chan struct{}  { return h.changes }
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

func TestHandoff_ClosesBeforeSpawn_WaitsForSocket(t *testing.T) {
	var log []string
	ctrl := &handoffFakeCtrl{log: &log, changes: make(chan struct{})}
	restoreHandoffSeams(t)

	startCmd = func(string) error { log = append(log, "spawn"); return nil }
	probes := 0
	probeSocket = func(string) bool {
		probes++
		log = append(log, "probe")
		return probes >= 2 // ready on the second probe
	}

	if err := handoffToDaemon(ctrl, "/cfg", "/sock"); err != nil {
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
	startCmd = func(string) error { return boom }
	probeSocket = func(string) bool { return true }

	err := handoffToDaemon(ctrl, "/cfg", "/sock")
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
	startCmd = func(string) error { return nil }
	probeSocket = func(string) bool { return false } // never becomes ready

	err := handoffToDaemon(ctrl, "/cfg", "/sock")
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
