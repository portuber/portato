package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/secret"
)

// withIdentityDeps swaps the identity-command dependencies (keyring backend,
// passphrase reader, daemon dialer) for the test and restores them on cleanup.
func withIdentityDeps(t *testing.T, backend secret.Backend, passphrases []string, daemonUp bool) {
	t.Helper()
	prevBackend := keyringBackend
	prevRead := readPassphrase
	prevDial := dialDaemon
	keyringBackend = backend
	idx := 0
	readPassphrase = func(string) (string, error) {
		if idx >= len(passphrases) {
			return "", errors.New("no more test passphrases")
		}
		p := passphrases[idx]
		idx++
		return p, nil
	}
	if !daemonUp {
		dialDaemon = func() (*client.Client, error) { return nil, errDaemonDown }
	}
	t.Cleanup(func() {
		keyringBackend = prevBackend
		readPassphrase = prevRead
		dialDaemon = prevDial
	})
}

// homeKey returns the keyring key for a ~/... identity path, the same way the
// dial (config.ResolvedIdentity → config.ExpandTilde) computes it.
func homeKey(t *testing.T, rel string) string {
	t.Helper()
	return config.ExpandTilde(rel)
}

func TestAddIdentity_StoresInKeyringNoDaemon(t *testing.T) {
	backend := secret.NewMemBackend()
	withIdentityDeps(t, backend, []string{"hunter2", "hunter2"}, false)

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	c := addIdentityCmd
	c.SetOut(out)
	c.SetErr(errOut)

	if err := addIdentityRunE(c, []string{"~/.ssh/id_ed25519"}); err != nil {
		t.Fatalf("addIdentityRunE: %v", err)
	}
	key := homeKey(t, "~/.ssh/id_ed25519")
	v, gerr := backend.Get(secret.Service, key)
	if gerr != nil || v != "hunter2" {
		t.Errorf("keyring should hold the passphrase under the expanded path; got %q,%v", v, gerr)
	}
}

func TestAddIdentity_ConfirmMismatchFails(t *testing.T) {
	backend := secret.NewMemBackend()
	withIdentityDeps(t, backend, []string{"a", "b"}, false)

	if err := addIdentityRunE(addIdentityCmd, []string{"~/.ssh/id"}); err == nil {
		t.Fatal("expected an error when the confirmation does not match")
	}
	if v, err := backend.Get(secret.Service, homeKey(t, "~/.ssh/id")); err == nil {
		t.Errorf("nothing should be stored on mismatch; got %q", v)
	}
}

func TestForgetIdentity_RemovesFromKeyring(t *testing.T) {
	backend := secret.NewMemBackend()
	path := homeKey(t, "~/.ssh/id")
	if err := backend.Set(secret.Service, path, "pw"); err != nil {
		t.Fatal(err)
	}
	withIdentityDeps(t, backend, nil, false)

	if err := forgetIdentityRunE(forgetIdentityCmd, []string{"~/.ssh/id"}); err != nil {
		t.Fatalf("forgetIdentityRunE: %v", err)
	}
	if _, err := backend.Get(secret.Service, path); err == nil {
		t.Error("keyring entry should be gone after forget-identity")
	}
}
