package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/update"
)

// checkTimeout bounds the explicit `update check` network round-trip.
const checkTimeout = 15 * time.Second

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for portato updates (GitHub Releases)",
	Long: `Check for portato updates against GitHub Releases.

  portato update check            one-shot check: current vs latest + release URL
  portato update consent <state>  set the update-check consent:
                                    on  — check daily (the daemon polls GitHub)
                                    off — never check, never ask again
                                    ask — forget the answer, re-ask on next run

The background check is an anonymous GET to api.github.com at most once per
24h and only after consent; nothing but the latest tag is recorded. Self-
update (download + swap) arrives in a later release; today the command only
reports.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var updateCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for a newer release now",
	Long: `Fetch the latest release from GitHub and compare it with the running
binary. Works regardless of consent and cache age — an explicit check.

Exit code 0 both when up to date and when an update is available;
non-zero only on error (network, rate limit, malformed response).`,
	RunE: updateCheckRunE,
}

var updateConsentCmd = &cobra.Command{
	Use:   "consent [on|off|ask]",
	Short: "Set the update-check consent",
	Long: `Set defaults.update_check in config.yaml:

  on   — the daemon checks GitHub once a day (update_check: true)
  off  — no background checks, the one-time question never returns (false)
  ask  — remove the answer; the next interactive launch asks again

Exactly one of on|off|ask is required.`,
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New("exactly one argument required: on|off|ask")
		}
		switch args[0] {
		case "on", "off", "ask":
			return nil
		default:
			return fmt.Errorf("unknown consent state %q (want on|off|ask)", args[0])
		}
	},
	RunE: updateConsentRunE,
}

func init() {
	updateCmd.AddCommand(updateCheckCmd, updateConsentCmd)
}

func updateCheckRunE(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	rel, err := fetchLatest(cmd.Context(), checkTimeout)
	if err != nil {
		return err
	}

	cur, curOK := update.ParseVersion(version)
	latest, latestOK := update.ParseVersion(rel.Version)
	fmt.Fprintf(out, "current:  %s\n", version)
	fmt.Fprintf(out, "latest:   %s\n", rel.Version)

	switch {
	case !latestOK:
		fmt.Fprintf(out, "release %q is not a strict vX.Y.Z; see %s\n", rel.Version, rel.URL)
	case !curOK:
		fmt.Fprintf(out, "current build is not a release version (not comparable); see %s\n", rel.URL)
	case cur.Compare(latest) < 0:
		fmt.Fprintf(out, "update available: %s -> %s\n  %s\n", cur, latest, rel.URL)
		writeCheckCache(rel.Version)
	default:
		fmt.Fprintln(out, "up to date")
		writeCheckCache(rel.Version)
	}
	return nil
}

// fetchLatest wraps the client call with a timeout. Exported-by-convention
// seam: the doctor line and the daemon ticker (later commits) reuse it.
func fetchLatest(ctx context.Context, timeout time.Duration) (update.Release, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rel, err := update.NewClient("portato/" + version).Latest(ctx)
	if err != nil {
		if errors.Is(err, update.ErrRateLimited) {
			return update.Release{}, errors.New("github rate limit reached — try again later")
		}
		return update.Release{}, fmt.Errorf("check for updates: %w", err)
	}
	return rel, nil
}

// writeCheckCache persists an explicit check's result so the TUI header and
// doctor show it without their own network I/O. Best-effort: a cache write
// failure must not fail the check itself.
func writeCheckCache(latest string) {
	_ = update.SaveCache(update.CheckCache{
		LastCheck: time.Now(),
		Latest:    latest,
	})
}

func updateConsentRunE(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}

	state := args[0]
	var value *bool
	switch state {
	case "on":
		t := true
		value = &t
	case "off":
		f := false
		value = &f
	}
	if err := config.SetDefaultsBoolNode(path, "update_check", value); err != nil {
		return fmt.Errorf("persist update_check: %w", err)
	}

	switch state {
	case "on":
		fmt.Fprintln(out, "update checks on — the daemon will check GitHub daily")
		fmt.Fprintln(out, "(a running daemon picks this up on its config reload)")
	case "off":
		fmt.Fprintln(out, "update checks off — no background checks, no more questions")
	case "ask":
		fmt.Fprintln(out, "consent reset — the next interactive launch asks again")
	}
	return nil
}
