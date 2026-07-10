package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/forward"
	"github.com/portuber/portato/internal/secret"
)

// passphraseConfig is a test config whose single tuber has an identity, so the
// passphrase handler has a path to key on (via ResolvedIdentity).
func passphraseConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	dir := shortDir(t)
	idPath := filepath.Join(dir, "id_ed25519")
	cfg := &config.Config{
		Defaults: config.Defaults{Identity: idPath},
		Tubers: []config.Tuber{{
			Name:   "db",
			Type:   "local",
			Local:  "5432",
			Remote: "db:5432",
			SSH:    "u@h:22",
		}},
	}
	return cfg, idPath
}

// startPassphraseServer builds a server with a mem-backed passphrase store and
// starts it on a unix socket, returning a connected client, the store and the
// underlying backend (so persistence can be asserted).
func startPassphraseServer(t *testing.T, cfg *config.Config) (*client.Client, *secret.Store, *secret.MemBackend) {
	t.Helper()
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	sock := filepath.Join(dir, "portato.sock")
	marker := filepath.Join(dir, "daemon.socket")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, marker, slog.Default(), nil)

	backend := secret.NewMemBackend()
	store := secret.NewStore(backend, func() bool { return true }) // persist on → keyring (mem) is written
	s.secrets = store

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	startErr := make(chan error, 1)
	go func() { startErr <- s.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-startErr:
		case <-time.After(2 * time.Second):
		}
	})
	if err := waitForFile(sock, 2*time.Second); err != nil {
		t.Fatalf("socket not created: %v", err)
	}
	return client.New(sock), store, backend
}

// TestPassphrase_StoreAndPersist asserts POST /tubers/{name}/passphrase stores
// the passphrase in the cache and (with persist on) the keyring, keyed by the
// tuber's resolved identity path.
func TestPassphrase_StoreAndPersist(t *testing.T) {
	cfg, idPath := passphraseConfig(t)
	c, store, backend := startPassphraseServer(t, cfg)

	if err := c.SetPassphrase("db", "hunter2"); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	if v, ok := store.Get(idPath); !ok || v != "hunter2" {
		t.Errorf("cache miss after SetPassphrase: got %q,%v", v, ok)
	}
	if v, err := backend.Get(secret.Service, idPath); err != nil || v != "hunter2" {
		t.Errorf("keyring (mem) should hold the passphrase under the identity path; got %q,%v", v, err)
	}
}

// TestPassphrase_UnknownTuber asserts a 404-style error for a missing tuber.
func TestPassphrase_UnknownTuber(t *testing.T) {
	cfg, _ := passphraseConfig(t)
	c, _, _ := startPassphraseServer(t, cfg)
	if err := c.SetPassphrase("nope", "x"); err == nil {
		t.Error("expected an error for an unknown tuber")
	}
}

// TestPassphrase_NoIdentity asserts a 409 when the tuber has no identity to
// key a passphrase on.
func TestPassphrase_NoIdentity(t *testing.T) {
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{Tubers: []config.Tuber{{Name: "db", Type: "local", Local: "5432", Remote: "db:5432", SSH: "u@h:22"}}}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "portato.sock")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, filepath.Join(dir, "daemon.socket"), slog.Default(), nil)
	s.secrets = secret.NewStore(secret.NewMemBackend(), func() bool { return false })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	startErr := make(chan error, 1)
	go func() { startErr <- s.Start(ctx) }()
	if err := waitForFile(sock, 2*time.Second); err != nil {
		t.Fatalf("socket: %v", err)
	}
	c := client.New(sock)
	if err := c.SetPassphrase("db", "x"); err == nil {
		t.Error("expected a conflict error when the tuber has no identity")
	}
}

// TestPassphrase_StatusFieldSerializes asserts the new PendingPassphrase field
// round-trips through JSON so attached clients see it on the wire.
func TestPassphrase_StatusFieldSerializes(t *testing.T) {
	st := forward.Status{Name: "db", State: forward.Connecting, PendingPassphrase: "/keys/id"}
	got, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(got), `"pending_passphrase":"/keys/id"`) {
		t.Errorf("PendingPassphrase did not serialize; got %s", got)
	}
	// And omits it when empty.
	st.PendingPassphrase = ""
	got, _ = json.Marshal(st)
	if strings.Contains(string(got), "pending_passphrase") {
		t.Errorf("PendingPassphrase should be omitted when empty; got %s", got)
	}
}

// TestIdentities_SetAndForget covers the path-keyed RPCs used by
// `portato add-identity` / `forget-identity`: POST /identities loads the
// passphrase into the store (waking any blocked dial) and DELETE /identities
// drops it. The path is a query/body value, not a URL segment.
func TestIdentities_SetAndForget(t *testing.T) {
	cfg, _ := passphraseConfig(t)
	c, store, _ := startPassphraseServer(t, cfg)
	path := "/home/u/.ssh/id_ed25519"

	if err := c.AddIdentity(path, "letmein"); err != nil {
		t.Fatalf("AddIdentity: %v", err)
	}
	if v, ok := store.Get(path); !ok || v != "letmein" {
		t.Errorf("store should hold the identity passphrase; got %q,%v", v, ok)
	}

	if err := c.ForgetIdentity(path); err != nil {
		t.Fatalf("ForgetIdentity: %v", err)
	}
	if v, ok := store.Get(path); ok {
		t.Errorf("store should be empty after forget; got %q", v)
	}
}
