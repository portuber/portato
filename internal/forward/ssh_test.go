package forward

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func writeKnownHosts(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "known_hosts")
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return p
}

func TestHostKeyAlgos(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		addr  string
		want  []string
	}{
		{
			name:  "ed25519 plain default port",
			lines: []string{"bastion.example.com ssh-ed25519 AAAA"},
			addr:  "bastion.example.com:22",
			want:  []string{ssh.KeyAlgoED25519},
		},
		{
			name:  "ecdsa plain",
			lines: []string{"h ecdsa-sha2-nistp256 AAAA"},
			addr:  "h:22",
			want:  []string{ssh.KeyAlgoECDSA256},
		},
		{
			name:  "rsa expands to sha2 variants",
			lines: []string{"h ssh-rsa AAAA"},
			addr:  "h:22",
			want:  []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
		},
		{
			name:  "multiple types deduped in file order",
			lines: []string{"h ssh-ed25519 AAAA", "h ecdsa-sha2-nistp256 BBBB"},
			addr:  "h:22",
			want:  []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256},
		},
		{
			name:  "unknown host -> nil (leave default)",
			lines: []string{"other ssh-ed25519 AAAA"},
			addr:  "h:22",
			want:  nil,
		},
		{
			name:  "bracketed non-default port matches",
			lines: []string{"[h]:2222 ssh-ed25519 AAAA"},
			addr:  "h:2222",
			want:  []string{ssh.KeyAlgoED25519},
		},
		{
			name:  "bracketed port does not match default port",
			lines: []string{"[h]:2222 ssh-ed25519 AAAA"},
			addr:  "h:22",
			want:  nil,
		},
		{
			name:  "plain entry does not match non-default port",
			lines: []string{"h ssh-ed25519 AAAA"},
			addr:  "h:2222",
			want:  nil,
		},
		{
			name:  "wildcard matches subdomain on default port",
			lines: []string{"*.example.com ssh-ed25519 AAAA"},
			addr:  "sub.example.com:22",
			want:  []string{ssh.KeyAlgoED25519},
		},
		{
			name:  "negation excludes the line",
			lines: []string{"!h,other ssh-ed25519 AAAA"},
			addr:  "h:22",
			want:  nil,
		},
		{
			name:  "comment after key ignored",
			lines: []string{"h ssh-ed25519 AAAA this is a comment"},
			addr:  "h:22",
			want:  []string{ssh.KeyAlgoED25519},
		},
		{
			name:  "marker lines skipped",
			lines: []string{"@cert-authority *.com ssh-ed25519 AAAA"},
			addr:  "a.com:22",
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeKnownHosts(t, tc.lines...)
			got := hostKeyAlgos(p, tc.addr)
			if !equalStringSlices(got, tc.want) {
				t.Errorf("hostKeyAlgos(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestHostKeyAlgosHashed(t *testing.T) {
	hashed := knownhosts.HashHostname("127.0.0.1")
	p := writeKnownHosts(t, hashed+" ssh-ed25519 AAAA")
	got := hostKeyAlgos(p, "127.0.0.1:22")
	want := []string{ssh.KeyAlgoED25519}
	if !equalStringSlices(got, want) {
		t.Errorf("hostKeyAlgos hashed = %v, want %v", got, want)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
