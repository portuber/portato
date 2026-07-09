package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/secret"
)

// keyringBackend is the OS keyring the identity commands write to. Overridable
// in tests so they never touch the real keychain.
var keyringBackend secret.Backend = secret.DefaultBackend()

// readPassphrase reads a single passphrase (no echo) after printing prompt. It
// is a var so tests can inject a non-interactive reader.
var readPassphrase = readPassphraseInteractive

var addIdentityCmd = &cobra.Command{
	Use:   "add-identity <path>",
	Short: "Store an SSH identity passphrase in the OS keyring",
	Long: `Store the passphrase for an SSH identity key in the OS keyring
(macOS Keychain / Linux Secret Service / Windows Credential Manager) so a
passphrase-protected key connects without ssh-agent.

The path is keyed exactly as the tunnels reference it (~ is expanded), so it
must match the identity configured for a tunnel. If the daemon is running, it
is notified so any dial currently waiting for the passphrase resumes
immediately.`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          addIdentityRunE,
}

var forgetIdentityCmd = &cobra.Command{
	Use:   "forget-identity <path>",
	Short: "Remove a stored SSH identity passphrase from the OS keyring",
	Long: `Delete the passphrase stored for an identity path from the OS keyring
(and the running daemon's cache, if any). The path must match the identity
configured for a tunnel (~ is expanded the same way).`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          forgetIdentityRunE,
}

func addIdentityRunE(cmd *cobra.Command, args []string) error {
	path := config.ExpandTilde(args[0])
	pass, err := readPassphrase("Enter passphrase for " + path + ": ")
	if err != nil {
		return fmt.Errorf("read passphrase: %w", err)
	}
	confirm, err := readPassphrase("Confirm passphrase: ")
	if err != nil {
		return fmt.Errorf("read passphrase: %w", err)
	}
	if pass != confirm {
		return fmt.Errorf("passphrases do not match")
	}
	if err := keyringBackend.Set(secret.Service, path, pass); err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	// Best-effort: wake a running daemon so a dial blocked on this identity
	// resumes immediately. A missing daemon is fine (the keyring is enough
	// for future dials / daemon restarts).
	if c, derr := dialDaemon(); derr == nil {
		_ = c.AddIdentity(path, pass)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "stored passphrase for %s\n", path)
	return nil
}

func forgetIdentityRunE(cmd *cobra.Command, args []string) error {
	path := config.ExpandTilde(args[0])
	if err := keyringBackend.Delete(secret.Service, path); err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	if c, derr := dialDaemon(); derr == nil {
		_ = c.ForgetIdentity(path)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "forgot passphrase for %s\n", path)
	return nil
}

// readPassphraseInteractive reads a line from the terminal without echo.
func readPassphraseInteractive(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	// Not a TTY (piped input): read a line plainly so the command is still
	// scriptable.
	var line string
	if _, err := fmt.Fscanln(os.Stdin, &line); err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
