package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// rec returns a unique, fixed 9-byte record per index so tests can tell
// surviving archives from dropped ones. "%08d\n" -> 9 bytes for i in 1..99.
func rec(i int) string { return fmt.Sprintf("%08d\n", i) }

func TestRotatingWriter_RotatesAtCapAndKeepsArchives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portato.log")
	w, err := NewRotatingWriter(path, 16, 3) // 16-byte cap: 2 records rotate
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}

	// 9 writes of 9 bytes with cap 16 rotate after every second write. Traced
	// through, the final state is:
	//   portato.log   = [rec9]
	//   portato.log.1 = [rec7, rec8]
	//   portato.log.2 = [rec5, rec6]
	//   portato.log.3 = [rec3, rec4]
	// and rec1, rec2 (the oldest) are dropped once the keep=3 cap is reached.
	for i := 1; i <= 9; i++ {
		if _, err := w.Write([]byte(rec(i))); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := read(t, path); got != rec(9) {
		t.Errorf("current file = %q, want %q", got, rec(9))
	}
	for n := 1; n <= 3; n++ {
		if !exists(archivePath(path, n)) {
			t.Errorf("archive .%d should exist", n)
		}
	}
	if exists(archivePath(path, 4)) {
		t.Error("archive .4 must not exist (keep=3)")
	}
	// The oldest surviving archive is .3 = [rec3, rec4]; rec1/rec2 are dropped.
	oldest := read(t, archivePath(path, 3))
	if !strings.Contains(oldest, rec(3)) || !strings.Contains(oldest, rec(4)) {
		t.Errorf("oldest archive .3 = %q, want rec3+rec4", oldest)
	}
	if strings.Contains(oldest, rec(1)) || strings.Contains(oldest, rec(2)) {
		t.Errorf("oldest archive .3 = %q, rec1/rec2 should have been dropped", oldest)
	}
}

func TestRotatingWriter_CurrentResetsAndLastRotateSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.log")
	w, err := NewRotatingWriter(path, 8, 2)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	if !w.LastRotate().IsZero() {
		t.Errorf("LastRotate should be zero before any rotation, got %v", w.LastRotate())
	}
	// One 9-byte write crosses the 8-byte cap: the written bytes move to .1
	// and the current file is reset to empty.
	if _, err := w.Write([]byte(rec(1))); err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.LastRotate().IsZero() {
		t.Error("LastRotate should be set after a rotation")
	}
	if got := read(t, archivePath(path, 1)); got != rec(1) {
		t.Errorf("archive .1 = %q, want %q (the rotated-out content)", got, rec(1))
	}
	if got := read(t, path); got != "" {
		t.Errorf("current file = %q, want empty after rotation", got)
	}
}

func TestRotatingWriter_ResumesExistingSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.log")
	// Pre-seed 6 bytes; cap 8 means a single 9-byte write (total 15) must
	// rotate, proving NewRotatingWriter counts existing file bytes.
	if err := os.WriteFile(path, []byte("seeded"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := NewRotatingWriter(path, 8, 2)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()
	if _, err := w.Write([]byte(rec(1))); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !exists(archivePath(path, 1)) {
		t.Error("expected a rotation given the pre-seeded size")
	}
}

func TestRotatingWriter_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.log")
	w, err := NewRotatingWriter(path, 64, 3)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer w.Close()

	const goroutines, perG = 16, 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			<-start
			for i := 0; i < perG; i++ {
				if _, err := w.Write([]byte(fmt.Sprintf("g%d-%d\n", id, i))); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}(g)
	}
	close(start)
	wg.Wait()

	// No panic / no lost writer; at most keep archives plus the current file.
	for n := w.keep + 1; ; n++ {
		p := archivePath(path, n)
		if !exists(p) {
			break
		}
		t.Errorf("archive .%d should not exist (keep=%d)", n, w.keep)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
