//go:build unix

package ipctoken

import "path/filepath"

// TokenPath returns the token file path for a given IPC socket path: the token
// lives in the socket's directory, next to the unix-domain socket file.
func TokenPath(socketPath string) string {
	return filepath.Join(filepath.Dir(socketPath), tokenFile)
}
