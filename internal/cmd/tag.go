package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/forward"
)

// tagFlag holds the --tag value for enable/disable/restart. An empty string
// means the command was given a positional <name> instead (exactly one of the
// two is required — see resolveTagOrName).
var enableTag, disableTag, restartTag string

const tagFlagName = "tag"

// registerTagFlag wires --tag onto a command and returns it (for chaining). It
// is shared by enable/disable/restart; forward intentionally has no --tag.
func registerTagFlag(c *cobra.Command, p *string) {
	c.Flags().StringVar(p, tagFlagName, "", "operate on every tuber with this tag (exclusive with <name>)")
}

// resolveTagOrName enforces the exactly-one-of contract (--tag XOR <name>) and
// returns the list of tuber names to act on. With --tag it asks the daemon for
// the live roster (List) and picks every tuber whose Tags contain the value
// (case-insensitive exact, mirroring the TUI #tag filter). With a <name> it
// returns that single name. Both or neither is an error.
func resolveTagOrName(args []string, tag string, list func() ([]forward.Status, error)) ([]string, error) {
	hasTag := strings.TrimSpace(tag) != ""
	hasName := len(args) == 1
	switch {
	case hasTag && hasName:
		return nil, fmt.Errorf("--tag and <name> are mutually exclusive")
	case !hasTag && !hasName:
		return nil, fmt.Errorf("specify a tuber <name> or --tag <tag>")
	case hasName:
		return []string{args[0]}, nil
	}
	statuses, err := list()
	if err != nil {
		return nil, fmt.Errorf("list tubers: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(tag))
	var names []string
	for _, s := range statuses {
		for _, t := range s.Tags {
			if strings.ToLower(t) == want {
				names = append(names, s.Name)
				break
			}
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no tubers tagged %q", tag)
	}
	return names, nil
}
