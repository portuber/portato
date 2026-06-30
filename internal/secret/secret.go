// Package secret stores SSH identity passphrases off-disk: an in-memory cache
// (per process, so reconnects don't re-prompt) backed by the OS keyring (macOS
// Keychain / Linux Secret Service / Windows Credential Manager via
// github.com/zalando/go-keyring) for cross-restart persistence. Nothing is
// ever written to disk in plaintext — only the cache and the keyring.
//
// The Store is the seam between a dial that NEEDS a passphrase (it calls
// Get/Wait) and the UI/handler that PROVIDES one (it calls Set). Approach-C
// blocking dial: a dial that finds no passphrase records the need and blocks in
// Wait until Set arrives (or its context is cancelled), instead of spinning the
// reconnect backoff. Keys are absolute identity file paths.
package secret

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

// Service is the keyring service name every portato secret is filed under.
const Service = "portato"

// ErrNotFound is returned by Backend.Get when no secret exists for the key. It
// is also the error Store treats as a cache miss (as opposed to a real failure).
var ErrNotFound = errors.New("secret: not found")

// Backend is the injectable keyring backend. Production uses the OS keyring
// (DefaultBackend); tests use NewMemBackend so they never touch the real
// keychain and run hermetically on any host/CI.
type Backend interface {
	Get(service, key string) (string, error)
	Set(service, key, value string) error
	Delete(service, key string) error
}

// osBackend wraps github.com/zalando/go-keyring, mapping its "not found" error
// to ErrNotFound so callers need not import keyring to detect a miss.
type osBackend struct{}

func (osBackend) Get(service, key string) (string, error) {
	v, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (osBackend) Set(service, key, value string) error { return keyring.Set(service, key, value) }

func (osBackend) Delete(service, key string) error {
	err := keyring.Delete(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// DefaultBackend returns the OS keyring backend.
func DefaultBackend() Backend { return osBackend{} }

// Store is the process-wide passphrase cache plus optional keyring
// persistence. It is safe for concurrent use. Get/Wait consult the cache then
// the keyring; Set populates the cache, wakes any dial blocked in Wait, and
// (when persist reports true) also writes the keyring. Delete clears both.
type Store struct {
	backend Backend
	// persist is consulted on Set to decide whether the keyring is written.
	// It is a closure (not a bool) so a config reload that flips
	// identity_passphrase_store takes effect without rebuilding the Store.
	persist func() bool

	mu      sync.Mutex
	cache   map[string]string
	waiters map[string][]chan struct{}
}

// NewStore returns a Store over backend. persist reports whether Set should also
// write the OS keyring (the identity_passphrase_store config flag). A nil
// persist is treated as "never persist".
func NewStore(backend Backend, persist func() bool) *Store {
	if persist == nil {
		persist = func() bool { return false }
	}
	return &Store{
		backend: backend,
		persist: persist,
		cache:   make(map[string]string),
		waiters: make(map[string][]chan struct{}),
	}
}

// Get returns the passphrase for path from the cache, falling back to the
// keyring (and caching it on a hit). ok is false when neither has it.
func (s *Store) Get(path string) (string, bool) {
	s.mu.Lock()
	if v, ok := s.cache[path]; ok {
		s.mu.Unlock()
		return v, true
	}
	s.mu.Unlock()

	// Keyring read happens outside the lock (it is I/O — /usr/bin/security on
	// macOS, D-Bus on Linux — and must not block other Get/Set callers).
	v, err := s.backend.Get(Service, path)
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	s.cache[path] = v
	s.mu.Unlock()
	return v, true
}

// Wait blocks until a passphrase for path is available via Set, or until ctx is
// done. It first re-checks the cache/keyring (a Set may have arrived between the
// caller surfacing the need and calling Wait). The caller MUST surface
// PendingPassphrase before calling so a UI/handler can prompt; Wait itself is a
// passive wait. Returns the passphrase and whether one arrived in time.
func (s *Store) Wait(ctx context.Context, path string) (string, bool) {
	if v, ok := s.Get(path); ok {
		return v, true
	}
	ch := make(chan struct{})
	s.mu.Lock()
	s.waiters[path] = append(s.waiters[path], ch)
	s.mu.Unlock()

	// On cancellation, drop our waiter so a later Set does not leak a signal
	// to a dead channel (Set closes it, which is fine, but we avoid leaving
	// the entry dangling for the lifetime of the Store).
	defer s.removeWaiter(path, ch)

	select {
	case <-ch:
		// Set populated the cache; read it back (keyring-only sets also cache).
		v, ok := s.Get(path)
		return v, ok
	case <-ctx.Done():
		return "", false
	}
}

// Set stores the passphrase in the cache and wakes any dial blocked in Wait on
// path. When persist() is true it also writes the OS keyring (cross-restart
// reuse); otherwise the value lives only in memory for this process.
func (s *Store) Set(path, passphrase string) error {
	s.setCacheAndWake(path, passphrase)
	if s.persist() {
		if err := s.backend.Set(Service, path, passphrase); err != nil {
			return fmt.Errorf("secret: keyring set %s: %w", path, err)
		}
	}
	return nil
}

// Delete removes the passphrase from the cache and the keyring. It does NOT
// wake a dial blocked in Wait: a blocked dial has no value to forget, so waking
// it would only make it re-Wait immediately. A missing keyring entry is not an
// error. The keyring is always cleared (forget-identity / wrong-passphrase),
// regardless of persist(), so a stale value never lingers.
func (s *Store) Delete(path string) error {
	s.mu.Lock()
	delete(s.cache, path)
	s.mu.Unlock()

	if err := s.backend.Delete(Service, path); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("secret: keyring delete %s: %w", path, err)
	}
	return nil
}

// setCacheAndWake stores the value and closes all waiter channels for path.
func (s *Store) setCacheAndWake(path, passphrase string) {
	s.mu.Lock()
	s.cache[path] = passphrase
	wakers := s.waiters[path]
	delete(s.waiters, path)
	s.mu.Unlock()
	wake(wakers)
}

func (s *Store) removeWaiter(path string, ch chan struct{}) {
	s.mu.Lock()
	waiters := s.waiters[path]
	for i, w := range waiters {
		if w == ch {
			s.waiters[path] = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(s.waiters[path]) == 0 {
		delete(s.waiters, path)
	}
	s.mu.Unlock()
}

func wake(chs []chan struct{}) {
	for _, ch := range chs {
		close(ch)
	}
}

// MemBackend is an in-memory Backend for tests. It is safe for concurrent use.
type MemBackend struct {
	mu      sync.Mutex
	secrets map[string]map[string]string // service -> key -> value
	err     map[string]error             // optional forced errors, keyed "service|key"
}

// NewMemBackend returns an empty in-memory backend.
func NewMemBackend() *MemBackend {
	return &MemBackend{
		secrets: make(map[string]map[string]string),
		err:     make(map[string]error),
	}
}

func memKey(service, key string) string { return service + "|" + key }

// SetError forces the next operation on service/key to return err (nil clears).
func (m *MemBackend) SetError(service, key string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.err, memKey(service, key))
		return
	}
	m.err[memKey(service, key)] = err
}

func (m *MemBackend) Get(service, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.err[memKey(service, key)]; ok {
		return "", e
	}
	if svc, ok := m.secrets[service]; ok {
		if v, ok := svc[key]; ok {
			return v, nil
		}
	}
	return "", ErrNotFound
}

func (m *MemBackend) Set(service, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.err[memKey(service, key)]; ok {
		return e
	}
	if m.secrets[service] == nil {
		m.secrets[service] = make(map[string]string)
	}
	m.secrets[service][key] = value
	return nil
}

func (m *MemBackend) Delete(service, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.err[memKey(service, key)]; ok {
		return e
	}
	if svc, ok := m.secrets[service]; ok {
		if _, ok := svc[key]; ok {
			delete(svc, key)
			return nil
		}
	}
	return ErrNotFound
}
