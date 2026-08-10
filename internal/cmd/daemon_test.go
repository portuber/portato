package cmd

import (
	"log/slog"
	"testing"
)

// TestParseDaemonArgs_DropsLeadingSubcommand locks the SCM-launch parse: the
// service record's command line is `portato.exe daemon --config <abs>`, and the
// flag parser stops at the first non-flag positional ("daemon"), so the leading
// subcommand token must be skipped or --config would never be honoured under
// the Service Control Manager (Phase 47).
func TestParseDaemonArgs_DropsLeadingSubcommand(t *testing.T) {
	resetDaemonFlags(t)

	const cfgPath = `/C:/Users/me/config.yaml`
	ParseDaemonArgs([]string{"daemon", "--config", cfgPath, "--log-level", "debug"})

	if cfgFile != cfgPath {
		t.Errorf("cfgFile = %q, want %q (leading \"daemon\" must not block --config)", cfgFile, cfgPath)
	}
	if logLevel != slog.LevelDebug {
		t.Errorf("logLevel = %v, want debug", logLevel)
	}
}

// TestParseDaemonArgs_NoConfigUsesDefault covers the bare SCM command line
// (`portato.exe daemon`) — no --config means the daemon falls back to
// config.DefaultPath(), as the cobra path does.
func TestParseDaemonArgs_NoConfigUsesDefault(t *testing.T) {
	resetDaemonFlags(t)
	ParseDaemonArgs([]string{"daemon"})
	if cfgFile != "" {
		t.Errorf("cfgFile = %q, want empty (no --config)", cfgFile)
	}
}

// resetDaemonFlags saves and restores the package vars ParseDaemonArgs mutates,
// so the test does not leak state into other cmd tests.
func resetDaemonFlags(t *testing.T) {
	t.Helper()
	savedCfg, savedIpc, savedFds, savedLvl := cfgFile, ipcTokenFlag, listenFdsPath, logLevel
	t.Cleanup(func() {
		cfgFile, ipcTokenFlag, listenFdsPath, logLevel = savedCfg, savedIpc, savedFds, savedLvl
	})
	cfgFile, ipcTokenFlag, listenFdsPath, logLevel = "", "on", "", slog.LevelInfo
}
