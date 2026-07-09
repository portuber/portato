package tui

import (
	"testing"

	"github.com/portuber/portato/internal/controller"
)

// tick drives one status refresh on m (the same path the SSE change signal
// takes) and returns the resulting model.
func tick(m Model) Model {
	mm, _ := m.Update(tickMsg{})
	return mm.(Model)
}

// TestPassphraseModal_AutoOpenOnPending asserts that when the tunnel under the
// cursor starts blocking on a passphrase (no keypress from the user), the tick
// refresh auto-opens the prompt — the "press space a second time" gap is gone.
func TestPassphraseModal_AutoOpenOnPending(t *testing.T) {
	f := newFake(controller.Status{Name: "db", Type: "local", Local: "1", Remote: "r", State: controller.Off})
	m := New(f, Options{Mode: "standalone"})

	// The dial blocks and surfaces PendingPassphrase.
	f.mu.Lock()
	f.statuses[0].State = controller.Connecting
	f.statuses[0].PendingPassphrase = "/keys/id"
	f.mu.Unlock()

	m = tick(m)
	if !m.enteringPassphrase || m.passphraseTarget != "db" {
		t.Fatalf("tick should auto-open the passphrase modal for the cursor's tunnel; got entering=%v target=%q", m.enteringPassphrase, m.passphraseTarget)
	}
}

// TestPassphraseModal_AutoOpenSkippedWhenBusy asserts the auto-open does not
// interrupt the user mid-interaction (here: typing in the /-filter).
func TestPassphraseModal_AutoOpenSkippedWhenBusy(t *testing.T) {
	f := newFake(controller.Status{Name: "db", Type: "local", Local: "1", Remote: "r", State: controller.Connecting, PendingPassphrase: "/keys/id"})
	m := New(f, Options{Mode: "standalone"})
	m.filtering = true // user is typing in the filter

	m = tick(m)
	if m.enteringPassphrase {
		t.Fatal("auto-open must not fire while the user is typing in the filter")
	}
}

// TestPassphraseModal_AutoOpenNotReopenedAfterEsc asserts that after the user
// cancels the prompt, the tick does not re-pop it endlessly for the same
// pending passphrase (a manual space still reopens).
func TestPassphraseModal_AutoOpenNotReopenedAfterEsc(t *testing.T) {
	f := newFake(controller.Status{Name: "db", Type: "local", Local: "1", Remote: "r", State: controller.Connecting, PendingPassphrase: "/keys/id"})
	m := New(f, Options{Mode: "standalone"})

	m = tick(m)
	if !m.enteringPassphrase {
		t.Fatal("modal should auto-open")
	}
	// Cancel.
	mm, _ := m.handlePassphraseKey(keyPress("esc"))
	m = mm.(Model)
	if m.enteringPassphrase {
		t.Fatal("esc should close the modal")
	}
	if m.dismissedPending == "" {
		t.Fatal("esc should record the dismissed prompt key")
	}
	// Another tick with the same pending passphrase must NOT reopen it.
	m = tick(m)
	if m.enteringPassphrase {
		t.Fatal("auto-open must not re-pop a dismissed prompt")
	}
	// But a manual p still reopens on demand (Phase 30: space toggles, so p is
	// the manual passphrase affordance).
	mm2, _ := m.handleKey(keyPress("p"))
	if mm, ok := mm2.(Model); ok {
		m = mm
	}
	if !m.enteringPassphrase {
		t.Fatal("a manual p should reopen the dismissed prompt")
	}
}

// TestAcceptModal_AutoOpenOnPendingHost asserts the same auto-open covers the
// TOFU unknown-host prompt (so enabling a tunnel whose host key is unknown
// surfaces the accept modal without a second keypress).
func TestAcceptModal_AutoOpenOnPendingHost(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r", State: controller.Error,
		PendingHost: "host", PendingFingerprint: "fp", PendingHostLine: "host ssh-ed25519 AAAA",
	})
	m := New(f, Options{Mode: "standalone"})

	m = tick(m)
	if !m.confirmAccept || m.acceptTarget != "db" {
		t.Fatalf("tick should auto-open the accept-host modal; got confirm=%v target=%q", m.confirmAccept, m.acceptTarget)
	}
}
