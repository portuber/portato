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

// passwordConfig is a test config whose single tuber has password_auth on and a
// populated account (user@host:port), so the password handler has an account
// key to key on (via config.Tuber.PasswordAccountKey).
func passwordConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Tubers: []config.Tuber{{
			Name:   "db",
			Type:   "local",
			Local:  "5432",
			Remote: "db:5432",
			SSH:    "u@h:22",
			User:   "u",
			Host:   "h",
			Port:   22,
		}},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return cfg
}

// startPasswordServer builds a server with a mem-backed password store and
// starts it on a unix socket, returning a connected client, the store and the
// backend.
func startPasswordServer(t *testing.T, cfg *config.Config) (*client.Client, *secret.Store, *secret.MemBackend) {
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
	s.passwords = store

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

// TestPassword_StoreAndPersist asserts POST /tubers/{name}/password stores the
// password in the cache and (with persist on) the keyring, keyed by the tuber's
// server account.
func TestPassword_StoreAndPersist(t *testing.T) {
	cfg := passwordConfig(t)
	c, store, backend := startPasswordServer(t, cfg)

	if err := c.SetPassword("db", "hunter2"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	key := "password:u@h:22"
	if v, ok := store.Get(key); !ok || v != "hunter2" {
		t.Errorf("cache miss after SetPassword: got %q,%v", v, ok)
	}
	if v, err := backend.Get(secret.Service, key); err != nil || v != "hunter2" {
		t.Errorf("keyring (mem) should hold the password under the account key; got %q,%v", v, err)
	}
}

// TestPassword_UnknownTuber asserts an error for a missing tuber.
func TestPassword_UnknownTuber(t *testing.T) {
	cfg := passwordConfig(t)
	c, _, _ := startPasswordServer(t, cfg)
	if err := c.SetPassword("nope", "x"); err == nil {
		t.Error("expected an error for an unknown tuber")
	}
}

// TestPassword_StatusFieldSerializes asserts the new PendingPassword field
// round-trips through JSON so attached clients see it on the wire.
func TestPassword_StatusFieldSerializes(t *testing.T) {
	st := forward.Status{Name: "db", State: forward.Connecting, PendingPassword: "password:u@h:22"}
	got, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(got), `"pending_password":"password:u@h:22"`) {
		t.Errorf("PendingPassword did not serialize; got %s", got)
	}
	// And omits it when empty.
	st.PendingPassword = ""
	got, _ = json.Marshal(st)
	if strings.Contains(string(got), "pending_password") {
		t.Errorf("PendingPassword should be omitted when empty; got %s", got)
	}
}
