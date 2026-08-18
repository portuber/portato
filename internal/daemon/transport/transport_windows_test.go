//go:build windows

package transport

import (
	"regexp"
	"testing"
)

// TestPipeSDDLFormat pins the descriptor shape: a protected DACL granting
// GENERIC_ALL to SYSTEM, Administrators, and the process user's SID (S-1-…).
// The SID itself varies by account, so the user ACE is matched by pattern.
func TestPipeSDDLFormat(t *testing.T) {
	sddl, err := pipeSDDL()
	if err != nil {
		t.Fatalf("pipeSDDL: %v", err)
	}
	want := `^D:P` +
		`\(A;;GA;;;SY\)` +
		`\(A;;GA;;;BA\)` +
		`\(A;;GA;;;S-1-[0-5]-[0-9-]+\)$`
	if !regexp.MustCompile(want).MatchString(sddl) {
		t.Errorf("pipeSDDL = %q, want SYSTEM+BA+user-SID GENERIC_ALL ACEs", sddl)
	}
}
