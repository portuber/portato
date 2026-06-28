package forward

import (
	"testing"

	"github.com/armon/go-socks5"
)

// TestSocks5Credentials covers the Phase 20 helper that maps the resolved
// user/pass to a go-socks5 CredentialStore: a complete pair yields a
// StaticCredentials that validates them, an incomplete/empty pair yields nil
// (so the proxy stays NoAuth).
func TestSocks5Credentials(t *testing.T) {
	t.Run("empty is nil", func(t *testing.T) {
		if got := socks5Credentials("", ""); got != nil {
			t.Errorf("socks5Credentials(empty) = %v, want nil", got)
		}
	})
	t.Run("user only is nil", func(t *testing.T) {
		if got := socks5Credentials("u", ""); got != nil {
			t.Errorf("socks5Credentials(user only) = %v, want nil", got)
		}
	})
	t.Run("pass only is nil", func(t *testing.T) {
		if got := socks5Credentials("", "p"); got != nil {
			t.Errorf("socks5Credentials(pass only) = %v, want nil", got)
		}
	})
	t.Run("complete pair validates", func(t *testing.T) {
		store := socks5Credentials("alice", "wonderland")
		sc, ok := store.(socks5.StaticCredentials)
		if !ok {
			t.Fatalf("got %T, want socks5.StaticCredentials", store)
		}
		if !sc.Valid("alice", "wonderland") {
			t.Error("Valid(alice,wonderland) = false, want true")
		}
		if sc.Valid("alice", "wrong") {
			t.Error("Valid(alice,wrong) = true, want false")
		}
		if sc.Valid("bob", "wonderland") {
			t.Error("Valid(bob,wonderland) = true, want false")
		}
	})
}

// TestSocks5NewHonorsCredentials guards the wire-up: when Credentials is set,
// armon/go-socks5's New installs the UserPass method (0x02) on the config;
// when nil, it installs NoAuth (0x00). New mutates conf.AuthMethods in place,
// so we inspect it after the call. This is the library behaviour our helper
// relies on.
func TestSocks5NewHonorsCredentials(t *testing.T) {
	t.Run("nil credentials → NoAuth method", func(t *testing.T) {
		conf := &socks5.Config{Credentials: nil}
		if _, err := socks5.New(conf); err != nil {
			t.Fatalf("New: %v", err)
		}
		if got := len(conf.AuthMethods); got != 1 {
			t.Fatalf("got %d methods, want 1", got)
		}
		if code := conf.AuthMethods[0].GetCode(); code != socks5.NoAuth {
			t.Errorf("method code = %x, want %x (NoAuth)", code, socks5.NoAuth)
		}
	})
	t.Run("static credentials → UserPass method", func(t *testing.T) {
		conf := &socks5.Config{Credentials: socks5.StaticCredentials{"alice": "pw"}}
		if _, err := socks5.New(conf); err != nil {
			t.Fatalf("New: %v", err)
		}
		if got := len(conf.AuthMethods); got != 1 {
			t.Fatalf("got %d methods, want 1", got)
		}
		if code := conf.AuthMethods[0].GetCode(); code != socks5.UserPassAuth {
			t.Errorf("method code = %x, want %x (UserPass)", code, socks5.UserPassAuth)
		}
	})
}
