package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestEasterEggFooter_TextAndEmoji asserts the footer always carries the pun
// and appends the potato emoji only when the Phase 24 emoji gate is on. The
// gate is driven purely via PORTATO_LOGO_EMOJI here (logo.goos is unexported,
// so the GOOS-based default is exercised by internal/logo/logo_test.go).
func TestEasterEggFooter_TextAndEmoji(t *testing.T) {
	cases := []struct {
		name  string
		emoji string
	}{
		{"emoji on", "on"},
		{"emoji off", "off"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PORTATO_LOGO_EMOJI", c.emoji)
			got := easterEggFooter()
			if !strings.Contains(got, "pórtate bien") {
				t.Errorf("footer %q missing the 'pórtate bien' pun", got)
			}
			if c.emoji == "on" && !strings.Contains(got, "🥔") {
				t.Errorf("footer %q should contain the potato emoji", got)
			}
			if c.emoji == "off" && strings.Contains(got, "🥔") {
				t.Errorf("footer %q should not contain the potato emoji", got)
			}
		})
	}
}

// TestRootHelp_HasFooter renders the root command's help (whose template was
// extended with the footer in init) and asserts the pun is present. The emoji
// is intentionally not asserted here: the template is baked once at init with
// the launch-time env, so only the stable text line is checked.
func TestRootHelp_HasFooter(t *testing.T) {
	out := &bytes.Buffer{}
	rootCmd.SetOut(out)
	if err := rootCmd.Help(); err != nil {
		t.Fatalf("rootCmd.Help: %v", err)
	}
	if !strings.Contains(out.String(), "pórtate bien") {
		t.Errorf("root --help should contain the 'pórtate bien' footer\ngot:\n%s", out.String())
	}
}

// TestSubcommandHelp_NoFooter mirrors Execute(): listCmd is attached to the
// root and pinned to the default help template, then must not show the footer.
func TestSubcommandHelp_NoFooter(t *testing.T) {
	rootCmd.AddCommand(listCmd)
	t.Cleanup(func() { rootCmd.RemoveCommand(listCmd) })
	listCmd.SetHelpTemplate(defaultHelpTemplate)

	out := &bytes.Buffer{}
	listCmd.SetOut(out)
	if err := listCmd.Help(); err != nil {
		t.Fatalf("listCmd.Help: %v", err)
	}
	if strings.Contains(out.String(), "pórtate bien") {
		t.Errorf("list --help must not contain the easter-egg footer\ngot:\n%s", out.String())
	}
}

// TestSubcommandHelp_NeedsPin is the regression guard: cobra's
// HelpTemplate() inherits the parent's template when a command has none of its
// own, so an attached-but-unpinned subcommand WOULD inherit the root footer.
// This documents why Execute() pins every subcommand to the default template.
func TestSubcommandHelp_NeedsPin(t *testing.T) {
	// A bare command with no template of its own, parented under root.
	rootCmd.AddCommand(listCmd)
	t.Cleanup(func() { rootCmd.RemoveCommand(listCmd) })
	// Do NOT pin: simulate the pre-fix state.
	prev := listCmd.HelpTemplate()
	listCmd.SetHelpTemplate("") // force inheritance from root
	t.Cleanup(func() { listCmd.SetHelpTemplate(prev) })

	if got := listCmd.HelpTemplate(); got != rootCmd.HelpTemplate() {
		t.Fatalf("unpinned subcommand should inherit root's template; got %q", got)
	}
	if !strings.Contains(listCmd.HelpTemplate(), "pórtate bien") {
		t.Fatalf("inherited template should contain the footer; got %q", listCmd.HelpTemplate())
	}
}
