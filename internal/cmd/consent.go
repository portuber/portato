package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/portuber/portato/internal/config"
)

// The phase-49 one-time update-consent ask. defaults.update_check absent =
// "not asked yet"; the first interactive surface (launcher / install /
// doctor) asks once and persists the answer; after that the question never
// returns (`update consent ask` re-arms it deliberately). The daemon, CLI
// commands and attach never ask — a non-interactive run must not block.

var (
	// consentAskGate reports whether the question is still pending for the
	// given config. Overridable in tests.
	consentAskGate = func(cfg *config.Config) bool { return cfg.Defaults.UpdateCheck == nil }
	// consentAskInteractive reports whether stdin is a TTY. Overridable.
	consentAskInteractive = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	// consentAskPrompt is the terminal round-trip. Overridable.
	consentAskPrompt = defaultConsentAskPrompt
	// consentAskPersist writes the answer to the config file. Overridable.
	consentAskPersist = func(path string, answer bool) error {
		return config.SetDefaultsBoolNode(path, "update_check", &answer)
	}
)

// maybeAskUpdateConsent asks the one-time question when it is pending and
// stdin is a TTY, and persists either answer. Any failure (non-TTY, config
// read-only, EOF) leaves the pending state untouched — the next interactive
// launch asks again. Returns the config to continue with — reloaded from
// disk when the answer was persisted, the input otherwise.
func maybeAskUpdateConsent(path string, cfg *config.Config) *config.Config {
	if !consentAskGate(cfg) || !consentAskInteractive() {
		return cfg
	}
	fmt.Println()
	answer := consentAskPrompt("Check for portato updates in the background (GitHub, once a day)?")
	if err := consentAskPersist(path, answer); err != nil {
		// Best-effort by design: a failed persist keeps the question
		// pending (nil key), so the user is asked again next launch.
		fmt.Printf("(could not save the answer: %v)\n", err)
		return cfg
	}
	if answer {
		fmt.Println("update checks on — `portato update consent off` to disable")
	} else {
		fmt.Println("update checks off — `portato update consent on` to enable")
	}
	if reloaded, err := config.Load(path); err == nil {
		return reloaded
	}
	return cfg
}

// defaultConsentAskPrompt prints the question and reads one line. This is a
// Y/n prompt — Enter (empty line) means yes: the stakes are low (an
// anonymous once-a-day GET) and reversible with one command, so the
// friction-light default is on. Anything but n/N/no/EOF declines nothing:
// y/yes/empty accept; everything else is treated as no.
func defaultConsentAskPrompt(question string) bool {
	fmt.Printf("%s [Y/n]: ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}
	return false
}

// maybeAskConsentTTY adapts maybeAskUpdateConsent for install/doctor: it
// loads the config from path (they have no *Config in hand) and reports
// whether the answer was persisted, so callers can stay silent in
// non-interactive runs.
func maybeAskConsentTTY(path string) bool {
	cfg, err := config.Load(path)
	if err != nil {
		return false
	}
	before := cfg.Defaults.UpdateCheck
	maybeAskUpdateConsent(path, cfg)
	// Asked and persisted iff the key was absent before and set after.
	after, err := config.Load(path)
	if err != nil {
		return false
	}
	return before == nil && after.Defaults.UpdateCheck != nil
}
