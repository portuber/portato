package cmd

import (
	"strings"
	"testing"

	"github.com/portuber/portato/internal/client"
)

func TestReload_Ok(t *testing.T) {
	s := newStubServer(t, sampleStatuses())
	useStub(t, s)

	c, out, errOut := captureCmd()
	if err := reloadRunE(c, nil); err != nil {
		t.Fatalf("reloadRunE: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "config reloaded") {
		t.Errorf("output = %q, want to contain %q", out.String(), "config reloaded")
	}
	if s.reloads != 1 {
		t.Errorf("want 1 reload RPC, got %d", s.reloads)
	}
}

func TestReload_DaemonDown(t *testing.T) {
	prev := dialDaemon
	dialDaemon = func() (*client.Client, error) { return nil, errDaemonDown }
	t.Cleanup(func() { dialDaemon = prev })

	c, out, errOut := captureCmd()
	if err := reloadRunE(c, nil); err == nil {
		t.Fatal("expected error when daemon down")
	}
	if !strings.Contains(errOut.String(), "portato daemon is not running") {
		t.Errorf("stderr should contain daemon-down hint; got %q", errOut.String())
	}
	if out.String() != "" {
		t.Errorf("stdout should be empty when daemon down; got %q", out.String())
	}
}

func TestReload_DaemonError(t *testing.T) {
	s := newStubServer(t, sampleStatuses())
	s.reloadFail = true
	useStub(t, s)

	c, out, errOut := captureCmd()
	if err := reloadRunE(c, nil); err == nil {
		t.Fatal("expected error when daemon reload fails")
	}
	if !strings.Contains(errOut.String(), "reload config") {
		t.Errorf("stderr should surface daemon error; got %q", errOut.String())
	}
	if out.String() != "" {
		t.Errorf("stdout should be empty on error; got %q", out.String())
	}
	if s.reloads != 0 {
		t.Errorf("failed reload should not count; got %d", s.reloads)
	}
}
