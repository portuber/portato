// Package ipctoken holds the IPC bearer-token primitives shared between the
// daemon (which generates and writes the token) and its clients (which read it
// best-effort and attach it as an Authorization header). It is a leaf package
// so the daemon and client packages can both depend on it without forming an
// import cycle.
//
// The token is generated at daemon startup, written next to the unix socket
// the daemon binds (<dir(socket)>/portato.token), and read best-effort by
// clients + the discovery probe. A missing file means "old daemon or
// --ipc-token off": clients send no header and an unauthenticated daemon
// answers 200. See SPEC §6/§16.
package ipctoken

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// tokenFile is the filename of the IPC bearer token, placed in the same
// directory as the unix socket the daemon listens on.
const tokenFile = "portato.token"

// TokenPath returns the token file path for a given unix-socket path: the
// token lives in the socket's directory. Both the daemon (write) and clients
// (read) derive it identically, so no extra discovery is needed beyond the
// socket path the client already resolved.
func TokenPath(socketPath string) string {
	return filepath.Join(filepath.Dir(socketPath), tokenFile)
}

// GenerateToken returns a fresh 32-byte bearer token, hex-encoded (64 chars).
// crypto/rand is the source so the token is unpredictable.
func GenerateToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// WriteToken atomically writes the token at path with mode 0600: tmp-file +
// rename, so a partial write never leaves a corrupt credential. An existing
// file is replaced.
func WriteToken(path, token string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create token dir: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".token.*.tmp")
	if err != nil {
		return fmt.Errorf("create token tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after the rename
	if _, err := tmp.WriteString(token); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write token tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename token: %w", err)
	}
	return nil
}

// ReadToken reads the bearer token at path. A missing file is reported via an
// error that satisfies os.IsNotExist — callers treat that as "no token
// configured" (old daemon or escape-hatch off) rather than a hard failure.
func ReadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RemoveToken removes the token file (best-effort; missing is fine). Called
// from the daemon's cleanup alongside socket + marker removal.
func RemoveToken(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
