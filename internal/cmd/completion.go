package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/portuber/portato/internal/config"
)

// tuberNameCompletion is the cobra ValidArgsFunction (and logs --tuber flag
// completion) that TAB-completes tuber names from config.yaml. It reads the
// file directly rather than via config.Load: config.Load runs EnsureExample
// (which would create a config.yaml as a side effect of pressing TAB),
// prepare() (~/.ssh/config resolution — slow, can error) and Validate(). A
// missing/unreadable/malformed config simply yields no completions.
func tuberNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// These commands take a single positional name, so once one is present we
	// offer none. The logs --tuber consumer has no positionals, so this is a
	// no-op there. Revisit if a >1-positional command reuses this helper.
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, t := range cfg.Tubers {
		if strings.HasPrefix(t.Name, toComplete) {
			names = append(names, t.Name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	enableCmd.ValidArgsFunction = tuberNameCompletion
	disableCmd.ValidArgsFunction = tuberNameCompletion
	restartCmd.ValidArgsFunction = tuberNameCompletion
	forwardCmd.ValidArgsFunction = tuberNameCompletion
}

// tagValueCompletion is the --tag flag completer for enable/disable/restart. It
// returns the distinct tag values across all tubers in config.yaml, read
// directly (not via config.Load — see tuberNameCompletion for the reasons). A
// missing/unreadable/malformed config yields no completions.
func tagValueCompletion(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	seen := make(map[string]struct{})
	var values []string
	for _, t := range cfg.Tubers {
		for _, tag := range t.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			if strings.HasPrefix(tag, toComplete) {
				values = append(values, tag)
			}
		}
	}
	return values, cobra.ShellCompDirectiveNoFileComp
}
