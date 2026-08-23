package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/daemon"
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

var (
	applyYes   bool
	applyForce bool
	applyDry   bool
)

var updateApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Download and install the latest release",
	Long: `Download the latest release archive for this platform, verify it
against checksums.txt, and swap the running binary in place (a one-level
portato.old backup is kept; the rollback command is printed).

Package-managed installs are refused: brew / scoop / deb / rpm / apk /
go install each get their own upgrade command printed instead, so their
state never desyncs. --force overrides the refusal (except where the
binary is held by the Windows SCM service). --dry-run shows the plan
without touching anything; --yes skips the confirmation prompt (required
when stdin is not a terminal).`,
	RunE: updateApplyRunE,
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
	updateCmd.AddCommand(updateCheckCmd, updateConsentCmd, updateApplyCmd)
	updateApplyCmd.Flags().BoolVar(&applyYes, "yes", false, "skip the confirmation prompt (required in a non-TTY)")
	updateApplyCmd.Flags().BoolVar(&applyForce, "force", false, "override the package-manager refusal (never skips the checksum)")
	updateApplyCmd.Flags().BoolVar(&applyDry, "dry-run", false, "show the plan, touch nothing")
}

// applySeams collects the test seams of update apply.
var (
	applyExecutable = os.Executable
	applyConfirm    = func(out io.Writer, question string) bool {
		fmt.Fprintf(out, "%s [y/N]: ", question)
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true
		}
		return false
	}
	applyTTY = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

func updateApplyRunE(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	update.SetCurrentVersion(version)

	exe, err := applyExecutable()
	if err != nil {
		return fmt.Errorf("locate the running binary: %w", err)
	}
	ch := update.DetectChannel(exe)

	rel, err := fetchLatest(cmd.Context(), checkTimeout)
	if err != nil {
		return err
	}
	cur, curOK := update.ParseVersion(version)
	latest, latestOK := update.ParseVersion(rel.Version)
	if !curOK {
		return fmt.Errorf("this is a %q build, not a release — nothing to update (latest is %s)", version, rel.Version)
	}
	if !latestOK {
		return fmt.Errorf("latest release %q is not a strict vX.Y.Z", rel.Version)
	}
	if cur.Compare(latest) >= 0 {
		fmt.Fprintf(out, "up to date (%s)\n", cur)
		return nil
	}
	archive, checksumsAsset, err := update.FindAsset(rel, GOOS, GOARCH)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "update available: %s -> %s\n", cur, latest)
	fmt.Fprintf(out, "install: %s\n", exe)
	fmt.Fprintf(out, "channel: %s (%s)\n", ch.Channel, ch.Evidence)

	if done, err := applyChannelGate(out, ch); done {
		return err
	}
	if applyDry {
		fmt.Fprintln(out, "(dry run — nothing downloaded or installed)")
		return nil
	}
	if !applyYes {
		if !applyTTY() {
			return errors.New("update apply needs a confirmation prompt; rerun with --yes (or --dry-run) when stdin is not a terminal")
		}
		if !applyConfirm(out, fmt.Sprintf("install %s over %s?", latest, cur)) {
			fmt.Fprintln(out, "aborted; nothing changed")
			return nil
		}
	}
	return applySwap(cmd.Context(), out, exe, cur, latest, archive, checksumsAsset)
}

// applyChannelGate enforces the etiquette: an SCM-held binary is never
// swapped (not even with --force); a managed channel defers to its own
// upgrade command unless --force. done=true means apply stops here (err is
// non-nil only for the SCM refusal).
func applyChannelGate(out io.Writer, ch update.ChannelInfo) (done bool, err error) {
	if scmServiceInstalled() {
		if applyForce {
			fmt.Fprintln(out, "refusing: the Windows SCM service holds this binary; update via Scoop or reinstall the service")
			return true, errors.New("update apply: SCM-held binary (not overridable)")
		}
		fmt.Fprintln(out, "refusing: the Windows SCM service holds this binary.")
		fmt.Fprintln(out, "update through your install channel (e.g. scoop update portato) or reinstall the service.")
		return true, nil
	}
	if ch.Managed && !applyForce {
		fmt.Fprintf(out, "refusing the in-place swap: installed via %s (%s)\n", ch.Channel, ch.Evidence)
		fmt.Fprintf(out, "run: %s\n", ch.Upgrade)
		fmt.Fprintln(out, "(--force overrides, at your own risk)")
		return true, nil
	}
	return false, nil
}

// applySwap performs download -> checksum verify -> extract -> swap and
// prints the result, the rollback command and the daemon-restart hint.
func applySwap(ctx context.Context, out io.Writer, exe string, cur, latest update.Version, archive, checksumsAsset update.Asset) error {
	if ctx == nil {
		ctx = context.Background()
	}
	downloaded, err := downloadVerified(ctx, archive, checksumsAsset)
	if err != nil {
		return err
	}
	defer os.Remove(downloaded.Archive)
	defer os.Remove(downloaded.Checksums)

	newBin := downloaded.Archive + ".portato"
	mode := os.FileMode(0o755)
	if info, statErr := os.Stat(exe); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := update.ExtractBinary(downloaded.Archive, newBin, mode); err != nil {
		return err
	}
	defer os.Remove(newBin)

	if err := update.SwapBinary(exe, newBin); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated %s -> %s\n", cur, latest)
	fmt.Fprintf(out, "rollback: %s\n", update.RollbackCommand(exe))
	hintDaemonRestart(out)
	return nil
}

// downloadedPair holds the verified download's temp paths.
type downloadedPair struct {
	Archive   string
	Checksums string
}

// downloadVerified fetches the archive and checksums.txt into temp files
// and verifies the SHA-256 before anything approaches the install dir.
func downloadVerified(ctx context.Context, archive, checksums update.Asset) (downloadedPair, error) {
	tmp, err := os.MkdirTemp("", "portato-update")
	if err != nil {
		return downloadedPair{}, fmt.Errorf("update: temp dir: %w", err)
	}
	archPath := filepath.Join(tmp, archive.Name)
	sumsPath := filepath.Join(tmp, "checksums.txt")
	dl := update.NewDownloader()
	if _, err := dl.Download(ctx, checksums.URL, sumsPath); err != nil {
		return downloadedPair{}, err
	}
	if _, err := dl.Download(ctx, archive.URL, archPath); err != nil {
		return downloadedPair{}, err
	}
	if err := update.VerifyChecksum(sumsPath, archPath); err != nil {
		return downloadedPair{}, err
	}
	return downloadedPair{Archive: archPath, Checksums: sumsPath}, nil
}

// hintDaemonRestart tells the user a live daemon still runs the old bytes
// until restarted (an inode swap does not affect a running process).
func hintDaemonRestart(out io.Writer) {
	socket, err := daemon.ResolveSocket()
	if err != nil || socket == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	if client.New(socket).HealthzCtx(ctx) != nil {
		return
	}
	fmt.Fprintln(out, "note: the running daemon keeps the old version until restarted")
	fmt.Fprintln(out, "      (portato stop, then start it again — or reboot for autostart)")
}

// GOOS/GOARCH mirror runtime for the asset mapping (seams for tests).
var (
	GOOS   = runtime.GOOS
	GOARCH = runtime.GOARCH
)

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

// formatCheckAge renders how long ago the cache was last written ("2h ago",
// "never") for the doctor line.
func formatCheckAge(now, checked time.Time) string {
	if checked.IsZero() {
		return "never"
	}
	d := now.Sub(checked).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
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
