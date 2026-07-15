package forward

import (
	"context"
	"log/slog"
	"testing"

	"github.com/portuber/portato/internal/config"
)

// TestStopClearsPending asserts Fix 1: stopping a tuber clears every pending
// prompt, so an Off tuber shows no stale "password?"/"passphrase?" hint and no
// modal auto-opens over a dead tuber.
func TestStopClearsPending(t *testing.T) {
	tn := NewTuber(context.Background(), config.Tuber{Name: "x", Type: "local", Local: "1", Remote: "r", Host: "h", Port: 22}, config.Defaults{}, slog.Default(), nil, nil)

	// Surface all three pending prompts as if a dial were blocked on them.
	tn.recordUnknownHost("h", "fp", "line")
	tn.passphraseSink("/keys/id")
	tn.passwordSink("password:u@h:22")
	// Simulate one password rejection so PasswordAttempts > 0.
	tn.passwordSink("password:u@h:22")

	st := tn.Status()
	if st.PendingHost == "" || st.PendingPassphrase == "" || st.PendingPassword == "" {
		t.Fatalf("pending fields should be set before Stop; got %+v", st)
	}
	if st.PasswordAttempts != 1 {
		t.Fatalf("PasswordAttempts = %d, want 1 after a re-prompt", st.PasswordAttempts)
	}

	if err := tn.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st = tn.Status()
	if st.PendingHost != "" || st.PendingFingerprint != "" || st.PendingHostLine != "" ||
		st.PendingPassphrase != "" || st.PendingPassword != "" {
		t.Errorf("Stop must clear all pending fields; got %+v", st)
	}
	if st.PasswordAttempts != 0 {
		t.Errorf("Stop must reset PasswordAttempts; got %d", st.PasswordAttempts)
	}
}

// TestPasswordSinkRejectionCount asserts Fix 3: passwordSink derives the server
// rejection count — the first prompt is 0, each subsequent re-prompt (a non-empty
// account while one is already pending) is a rejection, and clearPendingPassword
// resets it. The TUI reads Status.PasswordAttempts for an accurate hint.
func TestPasswordSinkRejectionCount(t *testing.T) {
	tn := NewTuber(context.Background(), config.Tuber{Name: "x", Type: "local", Local: "1", Remote: "r", Host: "h", Port: 22}, config.Defaults{}, slog.Default(), nil, nil)

	tn.passwordSink("password:u@h:22") // initial prompt
	if got := tn.Status().PasswordAttempts; got != 0 {
		t.Fatalf("initial prompt: PasswordAttempts = %d, want 0", got)
	}
	tn.passwordSink("password:u@h:22") // re-prompt after a rejection
	if got := tn.Status().PasswordAttempts; got != 1 {
		t.Fatalf("after one rejection: PasswordAttempts = %d, want 1", got)
	}
	tn.passwordSink("password:u@h:22") // another rejection
	if got := tn.Status().PasswordAttempts; got != 2 {
		t.Fatalf("after two rejections: PasswordAttempts = %d, want 2", got)
	}
	tn.passwordSink("") // success — clears the pending account, keeps the count
	if st := tn.Status(); st.PendingPassword != "" || st.PasswordAttempts != 2 {
		t.Fatalf("success should clear pending but keep the count; got %+v", st)
	}
	tn.clearPendingPassword() // new dial attempt resets
	if got := tn.Status().PasswordAttempts; got != 0 {
		t.Fatalf("after clearPendingPassword: PasswordAttempts = %d, want 0", got)
	}
}

// TestPassphraseSinkRejectionCount is the Phase 19 parity of the above:
// passphraseSink derives the dial's rejection count (first prompt 0, each
// re-prompt +1), surfaced via Status.PassphraseAttempts.
func TestPassphraseSinkRejectionCount(t *testing.T) {
	tn := NewTuber(context.Background(), config.Tuber{Name: "x", Type: "local", Local: "1", Remote: "r", Host: "h", Port: 22}, config.Defaults{}, slog.Default(), nil, nil)

	tn.passphraseSink("/keys/id") // initial prompt
	if got := tn.Status().PassphraseAttempts; got != 0 {
		t.Fatalf("initial prompt: PassphraseAttempts = %d, want 0", got)
	}
	tn.passphraseSink("/keys/id") // re-prompt = a wrong passphrase was rejected
	if got := tn.Status().PassphraseAttempts; got != 1 {
		t.Fatalf("after one rejection: PassphraseAttempts = %d, want 1", got)
	}
	tn.passphraseSink("") // success — clears pending, keeps the count
	if st := tn.Status(); st.PendingPassphrase != "" || st.PassphraseAttempts != 1 {
		t.Fatalf("success should clear pending but keep the count; got %+v", st)
	}
	tn.clearPendingPassphrase() // new dial resets
	if got := tn.Status().PassphraseAttempts; got != 0 {
		t.Fatalf("after clearPendingPassphrase: PassphraseAttempts = %d, want 0", got)
	}
}
