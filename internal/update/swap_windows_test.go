//go:build windows

package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsStagedSwapCycle(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "portato.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Nothing staged: pending is false, completion is a no-op.
	if updateStagedPendingForTest(exe) {
		t.Fatal("pending on a clean install")
	}
	completeStagedForTest(exe)
	got, _ := os.ReadFile(exe)
	if string(got) != "old" {
		t.Fatalf("clean completion changed the binary: %q", got)
	}

	// apply stages portato.new.
	if err := swapBinaryForTest(exe, filepath.Join(dir, "incoming")); err != nil {
		t.Fatal(err)
	}
	if !updateStagedPendingForTest(exe) {
		t.Fatal("pending not reported after staging")
	}

	// Next launch completes: new in place, old backed up.
	completeStagedForTest(exe)
	if updateStagedPendingForTest(exe) {
		t.Fatal("still pending after completion")
	}
	got, _ = os.ReadFile(exe)
	if string(got) != "new" {
		t.Fatalf("binary = %q, want the staged payload", got)
	}
	old, err := os.ReadFile(exe + ".old")
	if err != nil || string(old) != "old" {
		t.Fatalf("backup = %q, %v; want the previous binary", old, err)
	}
}

func swapBinaryForTest(exe, staged string) error {
	if err := os.WriteFile(staged, []byte("new"), 0o644); err != nil {
		return err
	}
	return SwapBinary(exe, staged)
}

func updateStagedPendingForTest(exe string) bool { return StagedSwapPending(exe) }

func completeStagedForTest(exe string) { CompleteStagedSwap(exe) }
