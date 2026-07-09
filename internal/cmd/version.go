package cmd

import (
	"fmt"
	"io"

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

// printVersion writes the logo banner + version line to w. The banner is plain
// braille/block art with no ANSI and no inline-image escape, so it is safe in
// a pipe (`portato --version | head` stays clean).
func printVersion(w io.Writer) {
	fmt.Fprintln(w, logo.VersionBanner(version, commit, date))
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Portato version",
	RunE: func(cmd *cobra.Command, _ []string) error {
		printVersion(cmd.OutOrStdout())
		return nil
	},
}
