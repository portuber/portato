package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	tok1, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	tok2, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(tok1) != 64 {
		t.Fatalf("token len = %d, want 64 (32 bytes hex)", len(tok1))
	}
	if tok1 == tok2 {
		t.Fatalf("two generated tokens are identical — not random?")
	}
}

func TestTokenPath(t *testing.T) {
	got := TokenPath("/tmp/portato-1000.sock")
	want := filepath.Join("/tmp", tokenFile)
	if got != want {
		t.Fatalf("TokenPath = %q, want %q", got, want)
	}
}

func TestWriteReadTokenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tokenFile)

	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if err := WriteToken(path, tok); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	got, err := ReadToken(path)
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}
	if got != tok {
		t.Fatalf("round-trip mismatch: got %q want %q", got, tok)
	}
}

func TestWriteTokenIsMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tokenFile)
	if err := WriteToken(path, "abc"); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestReadTokenMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadToken(filepath.Join(dir, tokenFile))
	if err == nil {
		t.Fatal("expected an error for a missing token file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("error should satisfy os.IsNotExist, got %v", err)
	}
}

func TestReadTokenOverwriteOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tokenFile)
	if err := WriteToken(path, "first"); err != nil {
		t.Fatalf("WriteToken first: %v", err)
	}
	if err := WriteToken(path, "second-that-is-longer"); err != nil {
		t.Fatalf("WriteToken second: %v", err)
	}
	got, err := ReadToken(path)
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}
	if got != "second-that-is-longer" {
		t.Fatalf("overwrite lost: got %q", got)
	}
}

func TestRemoveToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tokenFile)
	if err := WriteToken(path, "x"); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	if err := RemoveToken(path); err != nil {
		t.Fatalf("RemoveToken: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token still present after RemoveToken: %v", err)
	}
	// Removing a missing file is not an error.
	if err := RemoveToken(path); err != nil {
		t.Fatalf("RemoveToken on missing file should be nil, got %v", err)
	}
}
