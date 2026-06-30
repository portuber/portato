package forward

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// PassphraseProvider supplies an SSH identity passphrase, blocking until one is
// available. It is the forward-side view of internal/secret.Store (forward does
// not import secret, keeping the dial logic testable with a fake): a nil
// provider disables passphrase support, and a passphrase-protected identity
// simply fails to load (today's behaviour). Get is non-blocking; Wait blocks
// until a passphrase is provided elsewhere via Set (approach-C blocking dial:
// no reconnect-backoff spin while the user types); Delete invalidates a wrong
// passphrase so the next Wait blocks for a fresh submission.
type PassphraseProvider interface {
	Get(identityPath string) (string, bool)
	Wait(ctx context.Context, identityPath string) (string, bool)
	Delete(identityPath string) error
}

// passphraseSink receives the identity path that needs a passphrase (so the
// Tunnel can surface it via Status.PendingPassphrase for the UI to prompt), or
// an empty string to clear the pending need once a passphrase is accepted.
// Nil-safe.
type passphraseSink func(identityPath string)

// loadIdentityWithPassphrase reads the identity key at path and returns a
// Signer. If the key is passphrase-protected and provider is non-nil, it
// obtains a passphrase — provider.Get (cache/keyring) first, then provider.Wait
// which blocks until the UI/handler provides one — and retries
// ssh.ParsePrivateKeyWithPassphrase. A wrong passphrase is invalidated
// (provider.Delete) and the need re-surfaced (sink) for a fresh prompt.
// sink(path) is called before blocking so the UI can prompt; sink("") clears it
// once the passphrase is accepted. ctx cancellation (tunnel disable/shutdown)
// aborts the wait. With provider == nil it degrades to today's behaviour: a
// passphrase-protected key yields a parse error.
func loadIdentityWithPassphrase(ctx context.Context, path string, provider PassphraseProvider, sink passphraseSink) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity: %w", err)
	}
	signer, perr := ssh.ParsePrivateKey(data)
	if perr == nil {
		return signer, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(perr, &missing) || provider == nil {
		return nil, fmt.Errorf("parse identity: %w", perr)
	}

	// Passphrase-protected key. Loop: fetch (cache/keyring) → wait → parse.
	// Wrong passphrase → invalidate and re-prompt.
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pass, ok := provider.Get(path)
		if !ok {
			if sink != nil {
				sink(path) // surface PendingPassphrase so the UI prompts
			}
			pass, ok = provider.Wait(ctx, path)
			if !ok {
				return nil, ctx.Err()
			}
		}
		signer, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(pass))
		if err == nil {
			if sink != nil {
				sink("") // accepted — dismiss the prompt
			}
			return signer, nil
		}
		// Wrong passphrase: drop it so the next Wait blocks for a fresh one.
		_ = provider.Delete(path)
	}
}
