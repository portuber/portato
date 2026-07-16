package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// footSet returns the set of valid footer tokens, so a test can assert the fit
// algorithm never emits a partial (mid-word) token.
func footSet() map[string]bool {
	set := make(map[string]bool)
	for _, e := range tuberBindings() {
		set[e.foot] = true
	}
	return set
}

// parseFooterTokens splits a rendered footer string into its visible tokens
// (the " · "-separated entries), stripping ANSI styling first. lipgloss styles
// wrap the whole string in SGR codes (Faint/Foreground), so without stripping
// the first/last tokens would carry stray escape sequences.
func parseFooterTokens(rendered string) []string {
	stripped := stripAnsi(rendered)
	if stripped == "" {
		return nil
	}
	return strings.Split(stripped, " · ")
}

// stripAnsi removes CSI escape sequences (\x1b[...<final>) from s. That is all
// the footer styles emit (Faint in dark/mono, a 256-colour Foreground in
// light), so it is sufficient for parsing a rendered footer into tokens.
func stripAnsi(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				c := s[j]
				j++
				if c >= 0x40 && c <= 0x7e { // final byte terminates the CSI
					break
				}
			}
			i = j - 1
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// TestFooter_FitsWidth asserts the Phase 38 width-aware footer behaviour:
// the tail (? help · q quit) is always reserved, no token is ever cut
// mid-word, and the rendered width never exceeds the available cells.
func TestFooter_FitsWidth(t *testing.T) {
	set := footSet()
	for _, w := range []int{0, 200, 120, 80, 60, 40, 30} {
		m := New(newFake(), Options{Mode: "standalone"})
		m.width = w
		rendered := m.footer()
		tokens := parseFooterTokens(rendered)
		if len(tokens) < 2 {
			t.Fatalf("width=%d: footer has too few tokens: %q", w, rendered)
		}
		// Tail pair always present and contiguous at the end.
		if got := tokens[len(tokens)-2] + " · " + tokens[len(tokens)-1]; got != "? help · q quit" {
			t.Errorf("width=%d: tail = %q, want %q (footer=%q)", w, got, "? help · q quit", rendered)
		}
		// Every token is a complete foot entry — never a mid-word fragment.
		for _, tok := range tokens {
			if !set[tok] {
				t.Errorf("width=%d: partial/unknown token %q (footer=%q)", w, tok, rendered)
			}
		}
		// Middle tokens are a contiguous prefix of the natural order.
		if !validFooterShape(tokens) {
			t.Errorf("width=%d: token order is not prefix+tail (footer=%q)", w, rendered)
		}
		// Rendered width honours the available budget (except width=0, which
		// renders the full fixed string by design).
		if w > 0 {
			avail := w - 2*sideMargin
			if vis := lipgloss.Width(rendered); vis > avail {
				t.Errorf("width=%d: footer visible width %d > avail %d (%q)", w, vis, avail, rendered)
			}
		}
	}
}

// validFooterShape reports whether the token slice is exactly [contiguous
// prefix of the natural-order middle] + ["? help", "q quit"].
func validFooterShape(tokens []string) bool {
	b := tuberBindings()
	middle := b[:len(b)-2]
	if len(tokens) < 2 {
		return false
	}
	if tokens[len(tokens)-2] != "? help" || tokens[len(tokens)-1] != "q quit" {
		return false
	}
	mid := tokens[:len(tokens)-2]
	if len(mid) > len(middle) {
		return false
	}
	for i, tok := range mid {
		if middle[i].foot != tok {
			return false
		}
	}
	return true
}

// TestFooter_SharedSource pins the contract that the footer and the help view
// draw from one bindings list: the full footer string is stable (backward
// compatible with the pre-Phase-38 fixed string) and the help lines are the
// flattened binding.help entries in order.
func TestFooter_SharedSource(t *testing.T) {
	wantFull := "↑↓/jk move · space toggle · p passphrase · o password · " +
		"r restart · a/x all · e edit · n new · C duplicate · d delete · " +
		"l logs · / filter · R reload · ? help · q quit"
	if got := joinFeet(tuberBindings(), " · "); got != wantFull {
		t.Errorf("joinFeet mismatch:\n got: %q\nwant: %q", got, wantFull)
	}
	// Help line count matches the audit's 17 (up/down and a/x expand to two).
	lines := helpLines()
	if len(lines) != 17 {
		t.Errorf("helpLines len = %d, want 17", len(lines))
	}
	// The "q quit" wording is shared: footer foot and help line agree on the
	// key surface (this is the drift the audit flagged — "q quit" vs
	// "q quit (stops all tubers)" — now both live on one binding).
	if !strings.Contains(helpLines()[len(lines)-1], "quit (stops all tubers)") {
		t.Errorf("last help line lost the (stops all tubers) tail: %q", helpLines()[len(lines)-1])
	}
}
