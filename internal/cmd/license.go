package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/portuber/portato" // package licensetext — embeds the root LICENSE
)

// licenseSourceURL is the canonical source/repo URL shown by the license
// command (kept in sync with the README / go.mod module path).
const licenseSourceURL = "https://github.com/portuber/portato"

// showLicense is set by the root `--license` flag; rootRunE prints the license
// summary and exits before any daemon/standalone work when it is true.
var showLicense bool

// licenseFull is set by the `license` subcommand's local `--full` flag.
var licenseFull bool

// printLicense writes the license summary to w: project + version, the MIT
// license + source URL, a note that the binary embeds MIT/Apache-2.0/BSD-3
// software, a pointer to the bundled THIRD_PARTY_LICENSES.txt, and a
// `--full` hint. When full is true the embedded MIT LICENSE text is appended
// verbatim under a separator. The short summary is byte-identical with and
// without full so the subcommand and the root flag always agree.
func printLicense(w io.Writer, full bool) {
	fmt.Fprintf(w, "portato %s (%s, %s)\n", version, commit, date)
	fmt.Fprintln(w, "License: MIT")
	fmt.Fprintf(w, "Source:  %s\n", licenseSourceURL)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This binary embeds MIT / Apache-2.0 / BSD-3-Clause-licensed software.")
	fmt.Fprintln(w, "Third-party notices ship in THIRD_PARTY_LICENSES.txt (in the release")
	fmt.Fprintln(w, "archive; at /usr/share/doc/portato/THIRD_PARTY_LICENSES.txt on deb/rpm).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `portato license --full` for the full MIT License text.")
	if full {
		fmt.Fprintf(w, "\n--- MIT License ---\n\n%s", licensetext.MIT)
	}
}

var licenseCmd = &cobra.Command{
	Use:   "license",
	Short: "Print license information",
	Long: `Print Portato's license information: the project's MIT license, the source
URL, and a pointer to the bundled third-party notices.

Use --full to also print the full MIT License text (embedded in the binary).`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		printLicense(cmd.OutOrStdout(), licenseFull)
		return nil
	},
}

func init() {
	licenseCmd.Flags().BoolVar(&licenseFull, "full", false, "print the full MIT License text")
}
