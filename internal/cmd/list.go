package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/forward"
)

var listJSON bool

var listCmd = &cobra.Command{
	Use:           "list",
	Short:         "List the status of all tubers (stdout)",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          listRunE,
}

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "emit status as a single JSON document (machine-readable)")
}

func listRunE(cmd *cobra.Command, _ []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	statuses, err := c.List()
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	if listJSON {
		return printJSON(cmd.OutOrStdout(), statuses)
	}
	printTable(cmd.OutOrStdout(), statuses)
	return nil
}

// printJSON writes statuses as one JSON document. A nil/empty slice renders as
// `[]` (not `null`) so `jq '.[0]'` is well-defined for the zero-tuber case.
func printJSON(out io.Writer, statuses []forward.Status) error {
	if statuses == nil {
		statuses = []forward.Status{}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(statuses)
}

func printTable(out io.Writer, statuses []forward.Status) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tTYPE\tENDPOINT\tSTATUS\tUPTIME")
	for _, s := range statuses {
		indicator := "●"
		switch s.State {
		case forward.Off:
			indicator = "○"
		case forward.Error:
			indicator = "✗"
		}
		endpoint := s.Endpoint()
		status := s.State.String()
		if s.Error != "" {
			status += " " + s.Error
		}
		fmt.Fprintf(w, "  %s %s\t%s\t%s\t%s\t%s\n",
			indicator, s.Name, s.Type, endpoint, status, formatUptimeCLI(s))
	}
	_ = w.Flush()
}

func formatUptimeCLI(s forward.Status) string {
	if s.State != forward.Connected {
		return ""
	}
	d := s.Uptime()
	if d <= 0 {
		return ""
	}
	return formatDuration(d)
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours()/24), int(d.Hours())%24)
	}
}
