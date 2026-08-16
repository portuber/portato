package cmd

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/importer"
)

// The first-run import offer (Phase-48 nudge) runs only in the interactive
// launcher branch (runStandalone — the path that ends in the TUI), never in
// the daemon. Overridable in tests.
var (
	importNudgeGate        = func() bool { return config.FreshInstall() && !config.ImportOffered() }
	importNudgeInteractive = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	importNudgeDone        = config.MarkImportOffered
)

// maybeOfferImport is the one-time first-run offer: on the first interactive
// launch of a fresh install (config bootstrapped by portato, offer not yet
// made), scan ~/.ssh/config for forwards, show them and ask y/N — accepting
// imports them all as disabled tubers. Any outcome (accept, decline, zero
// candidates) consumes the offer for good; a non-TTY launch leaves it
// unconsumed so the next interactive run still offers. An upgrading install
// (config predates Phase 48, no fresh marker) is never nudged. Returns the
// config to continue with — reloaded from disk when tubers were imported.
func maybeOfferImport(path string, cfg *config.Config) *config.Config {
	if !importNudgeGate() || !importNudgeInteractive() {
		return cfg
	}
	sshCfg, err := importer.Load("")
	if err != nil || sshCfg == nil {
		return cfg
	}
	cands := importer.Scan(sshCfg)
	if len(cands) == 0 {
		importNudgeDone()
		return cfg
	}
	plan, err := importer.PlanImport(cfg, sshCfg, cands)
	if err != nil || len(plan.Add) == 0 {
		importNudgeDone()
		return cfg
	}
	printImportPreview(os.Stdout, plan)
	if !confirmImport(fmt.Sprintf("import all %d forward(s) from ~/.ssh/config?", len(plan.Add))) {
		importNudgeDone()
		return cfg
	}
	importNudgeDone()
	if err := applyImportPlan(path, cfg, plan); err != nil {
		return cfg
	}
	if reloaded, err := config.Load(path); err == nil {
		return reloaded
	}
	return cfg
}
