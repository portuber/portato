package daemon

import (
	"path/filepath"
	"testing"
)

func TestLockPathUnderConfigHome(t *testing.T) {
	p, err := LockPath()
	if err != nil {
		t.Fatalf("LockPath: %v", err)
	}
	if filepath.Base(p) != lockFile {
		t.Fatalf("LockPath base = %q, want %q", filepath.Base(p), lockFile)
	}
	if filepath.Base(filepath.Dir(p)) != "portato" {
		t.Fatalf("LockPath should sit under .../portato/, got %q", p)
	}
}
