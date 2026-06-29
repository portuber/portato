package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/logo"
)

// version, commit, date are the build metadata. They default to "dev"/
// "unknown" and are overridden at release time by goreleaser via -ldflags
// "-X github.com/kipkaev55/portato/internal/cmd.version=<tag>" (and likewise
// for commit and date).
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// showVersion is set by the root `--version` flag; rootRunE prints the banner
// and exits before any daemon/standalone work when it is true.
var showVersion bool

// isTerminal reports whether f is a terminal (a char device). It gates the
// --version banner's pipe-safety: a non-terminal stdout (a pipe or a file)
// gets the braille logo with no inline image and no ANSI, so
// `portato --version | head` stays clean.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// printVersion writes the logo banner + version line to w. tty controls
// pipe-safety (see isTerminal): when false the inline image and ANSI are
// suppressed and the braille variant is used.
func printVersion(w io.Writer, tty bool) {
	fmt.Fprintln(w, logo.VersionBanner(version, commit, date, tty))
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Portato version",
	RunE: func(cmd *cobra.Command, _ []string) error {
		printVersion(cmd.OutOrStdout(), isTerminal(os.Stdout))
		return nil
	},
}
