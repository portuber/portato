//go:build e2e && unix

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestImportMarkersE2E_DaemonFirstDoesNotConsumeOffer is the Phase-48 marker
// flow with the real binary: a daemon that bootstraps a missing config sets
// the fresh_install marker but never import_offered (the nudge is
// interactive-only and never runs in `portato daemon`), so the first later
// interactive launch still gets the one-time import offer.
func TestImportMarkersE2E_DaemonFirstDoesNotConsumeOffer(t *testing.T) {
	setupE2EEnv(t)
	root := t.TempDir()
	t.Setenv("HOME", root)

	cfgPath := filepath.Join(root, "config.yaml")
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

	socket := os.Getenv("PORTATO_SOCKET")
	if !waitSocket(socket, 10*time.Second) {
		t.Fatalf("daemon IPC socket never appeared\n%s", dumpDaemonLog(t))
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("daemon did not bootstrap the config: %v", err)
	}
	stateDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "portato")
	if _, err := os.Stat(filepath.Join(stateDir, "fresh_install")); err != nil {
		t.Errorf("config bootstrapped by portato but fresh_install marker missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "import_offered")); err == nil {
		t.Errorf("daemon must not consume the import offer (import_offered present)")
	}
}
