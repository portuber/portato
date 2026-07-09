package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/portuber/portato/internal/controller"
)

// TestPassphraseModal_OpenTypeSubmitCancel drives the Phase 19 passphrase modal
// end-to-end against a fake controller: space opens it on a pending-passphrase
// row, typed chars accumulate in the masked input, enter submits via
// AcceptPassphrase (and bumps the attempt counter / clears the field), and esc
// closes it.
func TestPassphraseModal_OpenTypeSubmitCancel(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassphrase: "/keys/id",
	})
	m := New(f, Options{Mode: "standalone"})

	// p opens the passphrase modal (the row has PendingPassphrase set); since
	// Phase 30 space only toggles, p is the manual affordance.
	next, _ := m.handleKey(keyPress("p"))
	m = next.(Model)
	if !m.enteringPassphrase || m.passphraseTarget != "db" {
		t.Fatalf("space should open the passphrase modal; got entering=%v target=%q", m.enteringPassphrase, m.passphraseTarget)
	}

	// typing accumulates in the masked input.
	for _, c := range []string{"h", "u", "n", "t", "e", "r", "2"} {
		next, _ = m.handlePassphraseKey(keyPress(c))
		m = next.(Model)
	}
	if got := m.passphraseInput.Value(); got != "hunter2" {
		t.Fatalf("typed passphrase = %q, want hunter2", got)
	}

	// enter submits via AcceptPassphrase and clears the field for a retry.
	next, _ = m.handlePassphraseKey(keyPress("enter"))
	m = next.(Model)
	if got := f.passphrases["db"]; got != "hunter2" {
		t.Errorf("AcceptPassphrase not called with the typed value; got %q", got)
	}
	if m.passphraseAttempts != 1 {
		t.Errorf("attempts = %d, want 1 after a submit", m.passphraseAttempts)
	}
	if m.passphraseInput.Value() != "" {
		t.Errorf("input should clear after submit; got %q", m.passphraseInput.Value())
	}

	// esc cancels the modal.
	next, _ = m.handlePassphraseKey(keyPress("esc"))
	m = next.(Model)
	if m.enteringPassphrase {
		t.Error("esc should close the passphrase modal")
	}
}

// TestPassphraseModal_SpaceNoOpWhenIdle asserts space toggles normally when no
// passphrase is pending (the modal must not steal space from enable/disable).
func TestPassphraseModal_SpaceNoOpWhenIdle(t *testing.T) {
	f := newFake(controller.Status{Name: "a", Type: "local", Local: "1", Remote: "r", State: controller.Off})
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(specialKey(tea.KeySpace))
	m = next.(Model)
	if m.enteringPassphrase {
		t.Error("passphrase modal must not open without PendingPassphrase")
	}
	if len(f.enabled) != 1 || f.enabled[0] != "a" {
		t.Errorf("space should toggle enable; got %v", f.enabled)
	}
}

// TestPassphraseModal_AutoCloseOnAccept simulates the dial accepting the
// passphrase: the tick handler refreshes the list (PendingPassphrase now empty)
// and the modal auto-closes.
func TestPassphraseModal_AutoCloseOnAccept(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassphrase: "/keys/id",
	})
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("p"))
	m = next.(Model)
	if !m.enteringPassphrase {
		t.Fatal("modal should be open")
	}

	// Simulate the dial clearing the pending need: drop it from the fake's
	// statuses, then drive a tick.
	f.mu.Lock()
	f.statuses[0].PendingPassphrase = ""
	f.mu.Unlock()
	next, _ = m.Update(tickMsg{})
	m = next.(Model)
	if m.enteringPassphrase {
		t.Error("modal should auto-close once PendingPassphrase clears")
	}
	if m.passphraseAttempts != 0 {
		t.Errorf("attempts should reset on close; got %d", m.passphraseAttempts)
	}
}

// TestPassphraseModal_SpaceDisablesPending is the Phase 30 regression: pressing
// space on a connecting / passphrase-pending tunnel must DISABLE it (call
// Disable via the controller), not open the modal. Previously space was trapped
// into opening the passphrase prompt, so a blocked tunnel could never be turned
// off.
func TestPassphraseModal_SpaceDisablesPending(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassphrase: "/keys/id",
	})
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(specialKey(tea.KeySpace))
	mm := next.(Model)
	if mm.enteringPassphrase {
		t.Fatal("space on a passphrase-pending tunnel must not open the modal")
	}
	if len(f.disabled) != 1 || f.disabled[0] != "db" {
		t.Errorf("space should disable the pending tunnel; got disabled=%v", f.disabled)
	}
	if len(f.enabled) != 0 {
		t.Errorf("space must not enable a pending tunnel; got enabled=%v", f.enabled)
	}
}

// TestPassphraseModal_PKeyOpensAfterDismissal covers the case autoOpen cannot:
// once the user dismisses the prompt (esc records dismissedPending, which the
// tick auto-open honours so it won't re-pop), p is the way back into the modal.
func TestPassphraseModal_PKeyOpensAfterDismissal(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassphrase: "/keys/id",
	})
	m := New(f, Options{Mode: "standalone"})

	m = tick(m)
	if !m.enteringPassphrase {
		t.Fatal("tick should auto-open the passphrase modal")
	}
	mm, _ := m.handlePassphraseKey(keyPress("esc"))
	m = mm.(Model)
	if m.enteringPassphrase {
		t.Fatal("esc should close the modal")
	}

	m = tick(m)
	if m.enteringPassphrase {
		t.Fatal("auto-open must not re-pop a dismissed prompt")
	}

	next, _ := m.handleKey(keyPress("p"))
	m = next.(Model)
	if !m.enteringPassphrase || m.passphraseTarget != "db" {
		t.Fatalf("p should reopen the dismissed passphrase modal; got entering=%v target=%q", m.enteringPassphrase, m.passphraseTarget)
	}
}
