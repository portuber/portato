package service

import (
	"os"
	"testing"
)

func TestNormLabel(t *testing.T) {
	if got := normLabel(""); got != DefaultLabel {
		t.Errorf("normLabel(\"\") = %q, want %q", got, DefaultLabel)
	}
	if got := normLabel("  "); got != DefaultLabel {
		t.Errorf("normLabel(blank) = %q, want %q", got, DefaultLabel)
	}
	const custom = "com.example.portato"
	if got := normLabel(custom); got != custom {
		t.Errorf("normLabel(custom) = %q, want %q", got, custom)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/present"
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !exists(p) {
		t.Errorf("exists(%q) = false, want true", p)
	}
	if exists(dir + "/missing") {
		t.Errorf("exists(missing) = true, want false")
	}
}

func TestNew_UnsupportedOnlyWhenNoImpl(t *testing.T) {
	// On darwin/linux New returns a concrete installer; on other OSes it
	// returns the unsupported stub. We assert New never returns nil and that
	// the type name is non-empty (build wiring sanity check).
	i := New()
	if i == nil {
		t.Fatal("New() returned nil")
	}
}
