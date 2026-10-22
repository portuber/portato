package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/daemon"
	"github.com/portuber/portato/internal/ipctoken"
)

var socketPathJSON bool

var socketPathCmd = &cobra.Command{
	Use:           "socket-path",
	Short:         "Print the daemon IPC socket and token paths (no daemon required)",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          socketPathRunE,
}

func init() {
	socketPathCmd.Flags().BoolVar(&socketPathJSON, "json", false, "emit the paths as a single JSON document (machine-readable)")
}

// socketPaths is the `portato socket-path` payload: where the daemon's IPC
// socket and token file live (client discovery precedence — --socket /
// PORTATO_SOCKET, then the marker, then the runtime path — computed without a
// daemon) and the CLI binary's own build version.
type socketPaths struct {
	Socket    string `json:"socket"`
	TokenPath string `json:"token_path"`
	Version   string `json:"version"`
}

func socketPathRunE(cmd *cobra.Command, _ []string) error {
	socket, err := daemon.ResolveSocketStatic()
	if err != nil {
		return err
	}
	p := socketPaths{
		Socket:    socket,
		TokenPath: ipctoken.TokenPath(socket),
		Version:   version,
	}
	out := cmd.OutOrStdout()
	if socketPathJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	fmt.Fprintf(out, "socket:     %s\n", p.Socket)
	fmt.Fprintf(out, "token_path: %s\n", p.TokenPath)
	fmt.Fprintf(out, "version:    %s\n", p.Version)
	return nil
}
