package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
)

// The Phase 53 CRUD commands. They mutate config.yaml (the source of truth)
// through the same comment-preserving node patches as the TUI editor and
// portato import: when a daemon is running they go over IPC so the engine
// applies the change immediately; otherwise they patch the file directly and
// a running daemon would still pick the change up through the Phase 28 file
// watcher (which is also how import reaches it).

var (
	crudType       string
	crudSSH        string
	crudLocal      string
	crudRemote     string
	crudIdentity   string
	crudJump       string
	crudTags       string
	crudEnabled    bool
	crudPassAuth   string
	crudSocks5User string
	crudSocks5Pass string
	crudYes        bool
)

// confirmRemove asks the y/N question before rm unless --yes. Overridable in
// tests; the production implementation is the shared y/N reader (import's).
var confirmRemove = defaultConfirmImport

func addCrudFlags(cmd *cobra.Command, withType bool) {
	fs := cmd.Flags()
	if withType {
		fs.StringVar(&crudType, "type", "local", "tunnel type: local (L), remote (R) or dynamic (D, SOCKS5)")
	}
	fs.StringVar(&crudSSH, "ssh", "", "SSH target user@host[:port]")
	fs.StringVar(&crudLocal, "local", "", "local listen address (port or host:port)")
	fs.StringVar(&crudRemote, "remote", "", "remote target host:port (unused for dynamic)")
	fs.StringVar(&crudIdentity, "identity", "", "path to a private key (optional)")
	fs.StringVar(&crudJump, "jump", "", "ProxyJump hop or comma-chain (optional)")
	fs.StringVar(&crudTags, "tags", "", "comma-separated tags (optional)")
	fs.BoolVar(&crudEnabled, "enabled", false, "create the tuber enabled (add) / set enabled state (set)")
	fs.StringVar(&crudPassAuth, "password-auth", "", "SSH password fallback: on, off or inherit (default)")
	fs.StringVar(&crudSocks5User, "socks5-user", "", "SOCKS5 username for dynamic (overrides defaults)")
	fs.StringVar(&crudSocks5Pass, "socks5-pass", "", "SOCKS5 password for dynamic (overrides defaults)")
}

func registerSetRemoveFlags(cmd *cobra.Command) {
	cmd.ValidArgsFunction = tuberNameCompletion
}

var addCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a tuber in config.yaml from flags (no TUI needed)",
	Long: `Create a tuber in config.yaml from flags.

Validates against the current config and appends through the
comment-preserving patch — the same save path the TUI editor uses. When a
daemon is running the change is applied over IPC (validate, persist, reload
the engine); otherwise the file is patched directly and a daemon picks it
up through the config watcher. The new tuber is created disabled unless
--enabled is given.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ExactArgs(1),
	RunE:          addRunE,
}

var setCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Update the given fields of an existing tuber (others untouched)",
	Long: `Update the given fields of an existing tuber.

Read-modify-persist: only the flags you pass change; every other field
(including socks5 credentials, password_auth and jump) is carried over
untouched, the same guarantee the Phase 52 editor overlay gives. When a
daemon is running the update is applied over IPC; otherwise the file is
patched directly.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ExactArgs(1),
	RunE:          setRunE,
}

var rmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a tuber from config.yaml (asks unless --yes)",
	Long: `Remove a tuber from config.yaml.

Asks for confirmation unless --yes is given (--yes is required without a
terminal). When a daemon is running the tuber is stopped and dropped
immediately over IPC; otherwise the file is patched directly.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ExactArgs(1),
	RunE:          rmRunE,
}

func init() {
	addCrudFlags(addCmd, true)
	addCrudFlags(setCmd, false)
	rmCmd.Flags().BoolVar(&crudYes, "yes", false, "do not ask for confirmation")
	registerSetRemoveFlags(setCmd)
	registerSetRemoveFlags(rmCmd)
}

// crudConfigPath resolves the config path like import: the --config flag or
// the XDG default, bootstrapped by EnsureExample on real writes (add).
func crudConfigPath() string {
	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	return path
}

// parsePassAuth maps --password-auth to the tri-state *bool ("" = inherit).
func parsePassAuth(s string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "inherit":
		return nil, nil
	case "on", "true":
		v := true
		return &v, nil
	case "off", "false":
		v := false
		return &v, nil
	default:
		return nil, fmt.Errorf("invalid --password-auth %q (want on, off or inherit)", s)
	}
}

// crudTuberFromFlags builds the tuber for `add` from the flag set.
func crudTuberFromFlags(name string) (config.Tuber, error) {
	t := config.Tuber{
		Name:     name,
		Type:     crudType,
		SSH:      crudSSH,
		Local:    crudLocal,
		Remote:   crudRemote,
		Identity: crudIdentity,
		Jump:     crudJump,
		Enabled:  crudEnabled,
	}
	var err error
	if t.PasswordAuth, err = parsePassAuth(crudPassAuth); err != nil {
		return t, err
	}
	if crudTags != "" {
		t.Tags = splitComma(crudTags)
	}
	t.Socks5User = crudSocks5User
	t.Socks5Password = crudSocks5Pass
	return t, nil
}

// applySetFlags overlays only the flags explicitly passed onto t — the CLI
// mirror of the Phase 52 editor overlay: untouched fields survive by
// construction.
func applySetFlags(cmd *cobra.Command, t *config.Tuber) error {
	fs := cmd.Flags()
	if fs.Changed("ssh") {
		t.SSH = crudSSH
	}
	if fs.Changed("local") {
		t.Local = crudLocal
	}
	if fs.Changed("remote") {
		t.Remote = crudRemote
	}
	if fs.Changed("identity") {
		t.Identity = crudIdentity
	}
	if fs.Changed("jump") {
		t.Jump = crudJump
	}
	if fs.Changed("type") {
		t.Type = crudType
	}
	if fs.Changed("tags") {
		t.Tags = nil
		if crudTags != "" {
			t.Tags = splitComma(crudTags)
		}
	}
	if fs.Changed("enabled") {
		t.Enabled = crudEnabled
	}
	if fs.Changed("password-auth") {
		pa, err := parsePassAuth(crudPassAuth)
		if err != nil {
			return err
		}
		t.PasswordAuth = pa
	}
	if fs.Changed("socks5-user") {
		t.Socks5User = crudSocks5User
	}
	if fs.Changed("socks5-pass") {
		t.Socks5Password = crudSocks5Pass
	}
	return nil
}

func crudValidateTuber(t config.Tuber, dynamicOptionalRemote bool) error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(t.SSH) == "" {
		return fmt.Errorf("--ssh is required")
	}
	if strings.TrimSpace(t.Local) == "" {
		return fmt.Errorf("--local is required")
	}
	if !dynamicOptionalRemote || t.Type != "dynamic" {
		if strings.TrimSpace(t.Remote) == "" {
			return fmt.Errorf("--remote is required for type %s", t.Type)
		}
	}
	return nil
}

// newDaemonCRUD returns the daemon IPC seam when a daemon is alive, nil for
// the config-direct fallback. Overridable in tests.
var newDaemonCRUD = func() crudDaemon {
	if c, err := dialDaemon(); err == nil {
		return &daemonCRUDClient{c}
	}
	return nil
}

type crudDaemon interface {
	AddTuber(t config.Tuber) error
	UpdateTuber(name string, t config.Tuber) error
	DeleteTuber(name string) error
}

type daemonCRUDClient struct{ c *client.Client }

func (d *daemonCRUDClient) AddTuber(t config.Tuber) error { return d.c.AddTuber(t) }
func (d *daemonCRUDClient) UpdateTuber(name string, t config.Tuber) error {
	return d.c.UpdateTuber(name, t)
}
func (d *daemonCRUDClient) DeleteTuber(name string) error { return d.c.DeleteTuber(name) }

func addRunE(cmd *cobra.Command, args []string) error {
	path := crudConfigPath()
	if _, err := config.EnsureExample(path); err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	t, err := crudTuberFromFlags(args[0])
	if err != nil {
		return err
	}
	if err := crudValidateTuber(t, true); err != nil {
		return err
	}
	// Fail fast on the prospective config before any write path.
	if _, err := cfg.WithTuberAdded(t); err != nil {
		return err
	}
	if d := newDaemonCRUD(); d != nil {
		if err := d.AddTuber(t); err != nil {
			return err
		}
	} else if err := config.AddTuberNode(path, t); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "added: %s\n", t.Name)
	return nil
}

func setRunE(cmd *cobra.Command, args []string) error {
	name := args[0]
	path := crudConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	var cur *config.Tuber
	for i := range cfg.Tubers {
		if cfg.Tubers[i].Name == name {
			cur = &cfg.Tubers[i]
			break
		}
	}
	if cur == nil {
		return fmt.Errorf("tuber %q not found", name)
	}
	t := *cur
	if err := applySetFlags(cmd, &t); err != nil {
		return err
	}
	if err := crudValidateTuber(t, true); err != nil {
		return err
	}
	if _, err := cfg.WithTuberReplaced(name, t); err != nil {
		return err
	}
	if d := newDaemonCRUD(); d != nil {
		if err := d.UpdateTuber(name, t); err != nil {
			return err
		}
	} else if err := config.ReplaceTuberNode(path, name, t); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "updated: %s\n", t.Name)
	return nil
}

func rmRunE(cmd *cobra.Command, args []string) error {
	name := args[0]
	path := crudConfigPath()
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	found := false
	for _, t := range cfg.Tubers {
		if t.Name == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("tuber %q not found", name)
	}
	if !crudYes && !confirmRemove(fmt.Sprintf("remove tuber %q from config.yaml", name)) {
		fmt.Fprintln(cmd.OutOrStdout(), "aborted")
		return nil
	}
	if _, err := cfg.WithTuberRemoved(name); err != nil {
		return err
	}
	if d := newDaemonCRUD(); d != nil {
		if err := d.DeleteTuber(name); err != nil {
			return err
		}
	} else if err := config.DeleteTuberNode(path, name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed: %s\n", name)
	return nil
}

// splitComma splits a comma-separated flag value into clean tokens.
func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
