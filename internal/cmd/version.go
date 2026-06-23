package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// version is the build version. It defaults to "dev" and is overridden at
// release time by goreleaser via -ldflags "-X github.com/kipkaev55/portato/internal/cmd.version=<tag>".
var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Portato version",
	RunE: func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "portato %s (%s/%s, %s)\n",
			version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return nil
	},
}
