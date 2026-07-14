package forward

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/portuber/portato/internal/config"
	"golang.org/x/crypto/ssh"
)

// PasswordProvider supplies an SSH account password, blocking until one is
// available. It is the forward-side view of internal/secret.Store keyed by
// account (forward does not import secret, keeping the dial logic testable with
// a fake): a nil provider disables password support. Get is non-blocking (cache
// / keyring); Wait blocks until a password is provided elsewhere via Set (no
// reconnect-backoff spin while the user types); Delete invalidates a wrong
// password so the next Wait blocks for a fresh submission. It mirrors
// PassphraseProvider but is keyed by server account rather than identity path.
type PasswordProvider interface {
	Get(account string) (string, bool)
	Wait(ctx context.Context, account string) (string, bool)
	Delete(account string) error
}

// passwordSink receives the server account that needs a password (so the Tuber
// can surface it via Status.PendingPassword for the UI to prompt), or an empty
// string to clear the pending need once a password is accepted. Nil-safe.
type passwordSink func(account string)

// dialWithPasswordPrompt authenticates with a password when no usable key works,
// mirroring loadIdentityWithPassphrase but with the dial itself as the
// validation step (a password can only be checked by the server).
//
// Unlike a passphrase (validated locally by ssh.ParsePrivateKeyWithPassphrase),
// golang.org/x/crypto/ssh calls the password method once per handshake and does
// not retry it within one handshake (client_auth.go:202; authenticate dedupes
// via tried), and portato's 5s handshake deadline would time out an interactive
// prompt. The re-prompt loop is therefore dial-level:
//
//  1. Probe keys (agent → identity) WITHOUT prompting — a working key
//     authenticates and never triggers a password prompt.
//  2. If keys failed auth (or none exist) and a provider is configured, loop:
//     Get the password (cache/keyring), else surface PendingPassword and Wait;
//     dial once with ssh.Password(pw); on success clear the prompt and return;
//     on a wrong password (server still offers "password") Delete it and
//     re-prompt with no backoff; if the server does not offer password at all,
//     bail with a clear error; any other error returns for the tuber's backoff.
//
// State stays Connecting throughout (no reconnect spin); ctx cancellation
// (disable/shutdown) aborts the Wait.
func dialWithPasswordPrompt(
	ctx context.Context,
	cfg config.Tuber,
	def config.Defaults,
	log *slog.Logger,
	sink hostKeySink,
	provider PassphraseProvider,
	passSink passphraseSink,
	pwProvider PasswordProvider,
	pwSink passwordSink,
) (*ssh.Client, error) {
	// 1. Probe keys first: a working key must never prompt for a password.
	keyMethods, closeAgent := authMethods(ctx, cfg, def, log, provider, passSink)
	defer closeAgent()
	if len(keyMethods) > 0 {
		client, err := dialOnce(ctx, cfg, def, log, sink, keyMethods)
		if err == nil {
			return client, nil
		}
		if !isAuthFailed(err) {
			// A non-auth failure (refused, timeout, host key...) — a password
			// won't help; return for the tuber's normal reconnect backoff.
			return nil, err
		}
		// Keys were rejected: fall through to the password loop.
	}

	if pwProvider == nil {
		return nil, errors.New("ssh auth failed: no usable key and password auth is not available (no password provider)")
	}
	account := cfg.PasswordAccountKey()
	// 2. Password loop: Get -> (miss: sink + Wait) -> dial -> on auth-fail
	//    Delete + re-prompt (no backoff); bail if the server offers no password.
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pw, ok := pwProvider.Get(account)
		if !ok {
			if pwSink != nil {
				pwSink(account) // surface PendingPassword so the UI prompts
			}
			pw, ok = pwProvider.Wait(ctx, account)
			if !ok {
				return nil, ctx.Err()
			}
		}
		client, err := dialOnce(ctx, cfg, def, log, sink, []ssh.AuthMethod{ssh.Password(pw)})
		if err == nil {
			if pwSink != nil {
				pwSink("") // accepted — dismiss the prompt
			}
			return client, nil
		}
		if !isAuthFailed(err) {
			return nil, err // other error -> tuber backoff
		}
		if !passwordAuthAvailable(err) {
			// Server does not offer the password method: retrying is hopeless.
			return nil, fmt.Errorf("auth failed: server does not offer password authentication for %s: %w", account, err)
		}
		// Wrong password: drop it so the next Wait blocks for a fresh one.
		_ = pwProvider.Delete(account)
	}
}

// isAuthFailed reports whether err is an SSH authentication failure (a wrong
// key/password, or exhausted methods). It reuses mapDialError's sentinel
// phrases so the classification stays consistent with what the TUI shows.
func isAuthFailed(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain")
}

// passwordAuthAvailable inspects an auth-failure error from a password dial and
// reports whether re-prompting is worthwhile. x/crypto/ssh's failure message
// reads "...unable to authenticate, attempted methods [<methods actually
// sent>], no supported methods remain". When "password" appears in the
// attempted-methods list the server accepted the password method and our value
// was simply wrong (re-prompt); when it is absent the server never offered
// password (a key-only server) and retrying is hopeless, so the caller bails.
// Verified empirically against golang.org/x/crypto/ssh@v0.53.0:
//
//	wrong password, password-capable server -> "attempted methods [none password]"
//	password not offered (key-only server)  -> "attempted methods [none]"
//
// When no attempted-methods list is present the call is ambiguous and the
// historical re-prompt behaviour is kept.
func passwordAuthAvailable(err error) bool {
	msg := err.Error()
	if i := strings.Index(msg, "attempted methods ["); i >= 0 {
		return strings.Contains(msg[i:], "password")
	}
	return true
}
