package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/importer"
)

var (
	importAll    bool
	importFrom   string
	importDryRun bool
	importYes    bool
)

var importCmd = &cobra.Command{
	Use:   "import [<host-pattern>...]",
	Short: "Import forwards from ~/.ssh/config into config.yaml (one-time copy)",
	Long: `Import forwards from ~/.ssh/config into config.yaml.

Scans LocalForward / RemoteForward / DynamicForward directives and creates
matching tubers with enabled: false — a one-time copy; ~/.ssh/config is
never modified, and imported tubers keep the raw host pattern in ssh: so
alias resolution applies at load.

Provide host patterns to import matching blocks, or --all for everything.
--dry-run lists the candidates without touching config.yaml. A confirmation
prompt is shown unless --yes is given (--yes is required without a terminal).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ArbitraryArgs,
	RunE:          importRunE,
}

// confirmImport asks the y/N question on the terminal and reports the answer.
// Overridable in tests; the production implementation reads stdin.
var confirmImport = defaultConfirmImport

// defaultConfirmImport prints the question and reads one line; y/yes
// (case-insensitive) confirms, anything else — including EOF — declines,
// like a plain y/N prompt.
func defaultConfirmImport(question string) bool {
	fmt.Printf("%s [y/N]: ", question)
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

func importRunE(cmd *cobra.Command, args []string) error {
	sshCfg, cands, err := importCandidates(cmd, args)
	if err != nil || cands == nil {
		return err
	}

	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	// A real import goes through the standard bootstrap (EnsureExample
	// creates config.yaml with the disabled example tuber when absent); a
	// dry run must not touch the file at all, so a missing config loads as
	// an empty one.
	if !importDryRun {
		if _, err := config.EnsureExample(path); err != nil {
			return fmt.Errorf("create config: %w", err)
		}
	}
	var cfg *config.Config
	if _, err := os.Stat(path); err == nil {
		cfg, err = config.Load(path)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	} else {
		cfg = &config.Config{}
	}
	plan, err := importer.PlanImport(cfg, sshCfg, cands)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}

	out := cmd.OutOrStdout()
	if len(plan.Add) == 0 {
		fmt.Fprintf(out, "nothing new to import (%d already configured)\n", len(plan.Skipped))
		return nil
	}
	printImportPreview(out, plan)

	proceed, err := confirmImportWrite(cmd, out, plan, path)
	if err != nil || !proceed {
		return err
	}

	// Validate the whole prospective config first (WithTuberAdded also runs
	// the prepare/resolve pass), then append each tuber through the
	// comment-preserving node patch — the same save path the TUI editor uses.
	if err := applyImportPlan(path, cfg, plan); err != nil {
		return err
	}
	for _, p := range plan.Add {
		fmt.Fprintf(out, "imported %s (disabled)\n", p.Tuber.Name)
	}
	return nil
}

// importCandidates loads the ssh config and applies the pattern/--all
// selection. A nil candidate slice with a nil error means "nothing to do"
// (the no-forwards message is already printed). Guidance errors (a pattern
// matching nothing, or a bare call without patterns/--all) are printed to
// stderr and returned — they teach the command instead of dead-ending.
func importCandidates(cmd *cobra.Command, args []string) (*ssh_config.Config, []importer.Candidate, error) {
	sshPath := importFrom
	if sshPath == "" {
		sshPath = importer.DefaultPath()
	}
	sshCfg, err := importer.Load(importFrom)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return nil, nil, err
	}
	all := importer.Scan(sshCfg)
	if len(all) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "no forwards found in %s; nothing to import\n", sshPath)
		return sshCfg, nil, nil
	}
	blocks := importer.Blocks(all)
	if len(args) > 0 {
		cands := importer.Filter(all, args)
		if len(cands) == 0 {
			err := fmt.Errorf("no forwards found for pattern(s) %s; blocks with forwards: %s",
				strings.Join(args, ", "), strings.Join(blocks, ", "))
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return nil, nil, err
		}
		return sshCfg, cands, nil
	}
	if !importAll {
		err := fmt.Errorf("specify a host pattern or --all; found %d importable block(s): %s",
			len(blocks), strings.Join(blocks, ", "))
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return nil, nil, err
	}
	return sshCfg, all, nil
}

// confirmImportWrite gates the actual write: --dry-run prints and stops,
// --yes proceeds, otherwise a TTY y/N confirmation is asked for (a non-TTY
// stdin without --yes is a clear error).
func confirmImportWrite(cmd *cobra.Command, out io.Writer, plan *importer.Plan, path string) (bool, error) {
	if importDryRun {
		fmt.Fprintln(out, "(dry run — nothing written)")
		return false, nil
	}
	if importYes {
		return true, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		err := errors.New("import needs a confirmation prompt; rerun with --yes (or --dry-run) when stdin is not a terminal")
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return false, err
	}
	if !confirmImport(fmt.Sprintf("import %d forward(s) into %s?", len(plan.Add), path)) {
		fmt.Fprintln(out, "aborted; nothing written")
		return false, nil
	}
	return true, nil
}

// applyImportPlan validates the whole prospective config with every planned
// tuber appended (WithTuberAdded also runs the prepare/resolve pass) and,
// when all of it validates, appends the tubers through the
// comment-preserving node patch — the same save path the TUI editor uses.
func applyImportPlan(path string, cfg *config.Config, plan *importer.Plan) error {
	prospective := cfg
	for _, p := range plan.Add {
		next, err := prospective.WithTuberAdded(p.Tuber)
		if err != nil {
			return fmt.Errorf("tuber %q: %w", p.Tuber.Name, err)
		}
		prospective = next
	}
	for _, p := range plan.Add {
		if err := config.AddTuberNode(path, p.Tuber); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
	}
	return nil
}

// printImportPreview lists the planned tubers (names included — they land in
// config.yaml verbatim) and the candidates skipped as already configured.
func printImportPreview(out io.Writer, plan *importer.Plan) {
	width := 0
	for _, p := range plan.Add {
		if len(p.Tuber.Name) > width {
			width = len(p.Tuber.Name)
		}
	}
	for _, p := range plan.Add {
		fmt.Fprintf(out, "  %-*s  %-7s  %s  [ssh: %s]\n",
			width, p.Tuber.Name, p.Tuber.Type, forwardSpec(p.Candidate), p.Candidate.SSHHost)
	}
	for _, c := range plan.Skipped {
		fmt.Fprintf(out, "  (skip) %s %s — already configured\n", c.SSHHost, forwardSpec(c))
	}
}

// forwardSpec renders a candidate as the forward the user declared in
// ssh_config: local/dynamic show the listen side first, remote shows the
// server-side listen receiving the destination.
func forwardSpec(c importer.Candidate) string {
	switch c.Type {
	case "dynamic":
		return fmt.Sprintf("socks5 %s", c.Local)
	case "remote":
		return fmt.Sprintf("%s <- %s", c.Remote, c.Local)
	default:
		return fmt.Sprintf("%s -> %s", c.Local, c.Remote)
	}
}

func init() {
	importCmd.Flags().BoolVar(&importAll, "all", false, "import every block with forwards (not just the given patterns)")
	importCmd.Flags().StringVar(&importFrom, "from", "", "path to the ssh config to import from (default ~/.ssh/config)")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "list the candidates without touching config.yaml")
	importCmd.Flags().BoolVar(&importYes, "yes", false, "skip the confirmation prompt (required without a terminal)")
}
