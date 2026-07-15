package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/portuber/portato/internal/controller"
)

// TestPasswordModal_OpenTypeSubmitCancel drives the Phase 35 password modal
// end-to-end against a fake controller: o opens it on a pending-password row,
// typed chars accumulate in the masked input, enter submits via AcceptPassword
// (and bumps the attempt counter / clears the field), and esc closes it.
func TestPasswordModal_OpenTypeSubmitCancel(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassword: "password:u@h:22",
	})
	m := New(f, Options{Mode: "standalone"})

	// o opens the password modal (the row has PendingPassword set). p is taken
	// by the identity passphrase, so o is the password affordance.
	next, _ := m.handleKey(keyPress("o"))
	m = next.(Model)
	if !m.enteringPassword || m.passwordTarget != "db" {
		t.Fatalf("o should open the password modal; got entering=%v target=%q", m.enteringPassword, m.passwordTarget)
	}

	// typing accumulates in the masked input.
	for _, c := range []string{"s", "e", "c", "r", "e", "t"} {
		next, _ = m.handlePasswordKey(keyPress(c))
		m = next.(Model)
	}
	if got := m.passwordInput.Value(); got != "secret" {
		t.Fatalf("typed password = %q, want secret", got)
	}

	// enter submits via AcceptPassword and clears the field for a retry.
	next, _ = m.handlePasswordKey(keyPress("enter"))
	m = next.(Model)
	if got := f.passwords["db"]; got != "secret" {
		t.Errorf("AcceptPassword not called with the typed value; got %q", got)
	}
	if m.passwordInput.Value() != "" {
		t.Errorf("input should clear after submit; got %q", m.passwordInput.Value())
	}

	// esc cancels the modal.
	next, _ = m.handlePasswordKey(keyPress("esc"))
	m = next.(Model)
	if m.enteringPassword {
		t.Error("esc should close the password modal")
	}
}

// TestPasswordModal_SpaceNoOpWhenIdle asserts space toggles normally when no
// password is pending (the modal must not steal space from enable/disable).
func TestPasswordModal_SpaceNoOpWhenIdle(t *testing.T) {
	f := newFake(controller.Status{Name: "a", Type: "local", Local: "1", Remote: "r", State: controller.Off})
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(specialKey(tea.KeySpace))
	m = next.(Model)
	if m.enteringPassword {
		t.Error("password modal must not open without PendingPassword")
	}
	if len(f.enabled) != 1 || f.enabled[0] != "a" {
		t.Errorf("space should toggle enable; got %v", f.enabled)
	}
}

// TestPasswordModal_AutoCloseOnAccept simulates the dial accepting the
// password: the tick handler refreshes the list (PendingPassword now empty) and
// the modal auto-closes.
func TestPasswordModal_AutoCloseOnAccept(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassword: "password:u@h:22",
	})
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("o"))
	m = next.(Model)
	if !m.enteringPassword {
		t.Fatal("modal should be open")
	}

	// Simulate the dial clearing the pending need, then drive a tick.
	f.mu.Lock()
	f.statuses[0].PendingPassword = ""
	f.mu.Unlock()
	next, _ = m.Update(tickMsg{})
	m = next.(Model)
	if m.enteringPassword {
		t.Error("modal should auto-close once PendingPassword clears")
	}
}

// TestPasswordModal_SpaceDisablesPending is the Phase 30 regression applied to
// passwords: pressing space on a connecting / password-pending tuber must
// DISABLE it, not open the modal (so a blocked tuber can still be turned off).
func TestPasswordModal_SpaceDisablesPending(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassword: "password:u@h:22",
	})
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(specialKey(tea.KeySpace))
	mm := next.(Model)
	if mm.enteringPassword {
		t.Fatal("space on a password-pending tuber must not open the modal")
	}
	if len(f.disabled) != 1 || f.disabled[0] != "db" {
		t.Errorf("space should disable the pending tuber; got disabled=%v", f.disabled)
	}
	if len(f.enabled) != 0 {
		t.Errorf("space must not enable a pending tuber; got enabled=%v", f.enabled)
	}
}

// TestPasswordModal_LeadingSpaceIgnored asserts Fix 2: a space pressed on an
// empty password field is ignored (an accidental space-press is invisible under
// the mask and would otherwise corrupt the value), while a normal character is
// still accepted.
func TestPasswordModal_LeadingSpaceIgnored(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassword: "password:u@h:22",
	})
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("o"))
	m = next.(Model)

	// A leading spacebar on the empty field must not be added.
	next, _ = m.handlePasswordKey(specialKey(tea.KeySpace))
	m = next.(Model)
	if got := m.passwordInput.Value(); got != "" {
		t.Fatalf("leading space should be ignored; input = %q", got)
	}

	// A normal character is still accepted.
	next, _ = m.handlePasswordKey(keyPress("a"))
	m = next.(Model)
	if got := m.passwordInput.Value(); got != "a" {
		t.Fatalf("letter should be accepted; input = %q want %q", got, "a")
	}
}

// TestPasswordModal_PortubbingState asserts the "portubbing…" connecting
// state: enter submits and enters it; a dial rejection (PasswordAttempts grows)
// drops back to the input; success (PendingPassword clears) closes the modal.
func TestPasswordModal_PortubbingState(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassword: "password:u@h:22",
	})
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("o"))
	m = next.(Model)
	next, _ = m.handlePasswordKey(keyPress("a"))
	m = next.(Model)
	next, _ = m.handlePasswordKey(keyPress("enter"))
	m = next.(Model)
	if !m.passwordConnecting {
		t.Fatal("enter should enter the portubbing state")
	}

	// The dial rejects the password → PasswordAttempts grows past the submit
	// baseline → drop back to the input (modal stays open).
	f.mu.Lock()
	f.statuses[0].PasswordAttempts = 1
	f.mu.Unlock()
	next, _ = m.Update(tickMsg{})
	m = next.(Model)
	if m.passwordConnecting {
		t.Error("a rejection should drop the portubbing state back to the input")
	}
	if !m.enteringPassword {
		t.Error("the modal should stay open after a rejection")
	}

	// The dial accepts → PendingPassword clears → modal closes.
	f.mu.Lock()
	f.statuses[0].PendingPassword = ""
	f.mu.Unlock()
	next, _ = m.Update(tickMsg{})
	m = next.(Model)
	if m.enteringPassword {
		t.Error("the modal should close on success")
	}
}

// TestPasswordModal_OKeyOpensAfterDismissal covers the case autoOpen cannot:
// once the user dismisses the prompt (esc records dismissedPending, which the
// tick auto-open honours so it won't re-pop), o is the way back into the modal.
func TestPasswordModal_OKeyOpensAfterDismissal(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Connecting, PendingPassword: "password:u@h:22",
	})
	m := New(f, Options{Mode: "standalone"})

	m = tick(m)
	if !m.enteringPassword {
		t.Fatal("tick should auto-open the password modal")
	}
	mm, _ := m.handlePasswordKey(keyPress("esc"))
	m = mm.(Model)
	if m.enteringPassword {
		t.Fatal("esc should close the modal")
	}

	m = tick(m)
	if m.enteringPassword {
		t.Fatal("auto-open must not re-pop a dismissed prompt")
	}

	next, _ := m.handleKey(keyPress("o"))
	m = next.(Model)
	if !m.enteringPassword || m.passwordTarget != "db" {
		t.Fatalf("o should reopen the dismissed password modal; got entering=%v target=%q", m.enteringPassword, m.passwordTarget)
	}
}
