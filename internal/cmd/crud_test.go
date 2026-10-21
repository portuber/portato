package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/config"
)

// crudFixture writes a config with one fully-loaded tuber (the Phase 52
// fields included) and resets the CRUD command globals. The daemon seam is
// pointed at nil (config-direct fallback) unless a test overrides it.
func crudFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	fixture := `# managed here
tubers:
  - name: db
    type: local
    local: "5432"
    remote: db:5432
    ssh: u@h:22
    enabled: false
    jump: bastion:22
    password_auth: false
    tags: [prod]
    socks5_user: alice
    socks5_password: s3cret
`
	if err := os.WriteFile(cfgPath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	prevCfgFile := cfgFile
	cfgFile = cfgPath
	prev := crudState()
	crudReset()
	prevDaemon := newDaemonCRUD
	newDaemonCRUD = func() crudDaemon { return nil }
	t.Cleanup(func() {
		cfgFile = prevCfgFile
		crudRestore(prev)
		newDaemonCRUD = prevDaemon
	})
	return cfgPath
}

type crudVars struct {
	typ, ssh, local, remote, identity, jump, tags string
	enabled                                       bool
	passAuth, socks5User, socks5Pass              string
	yes                                           bool
}

func crudState() crudVars {
	return crudVars{crudType, crudSSH, crudLocal, crudRemote, crudIdentity,
		crudJump, crudTags, crudEnabled, crudPassAuth, crudSocks5User, crudSocks5Pass, crudYes}
}

func crudReset() {
	crudType, crudSSH, crudLocal, crudRemote = "local", "", "", ""
	crudIdentity, crudJump, crudTags = "", "", ""
	crudEnabled, crudPassAuth = false, ""
	crudSocks5User, crudSocks5Pass, crudYes = "", "", false
}

func crudRestore(v crudVars) {
	crudType, crudSSH, crudLocal, crudRemote = v.typ, v.ssh, v.local, v.remote
	crudIdentity, crudJump, crudTags = v.identity, v.jump, v.tags
	crudEnabled, crudPassAuth = v.enabled, v.passAuth
	crudSocks5User, crudSocks5Pass, crudYes = v.socks5User, v.socks5Pass, v.yes
}

// fakeCRUDDaemon records the IPC calls; used with newDaemonCRUD override.
type fakeCRUDDaemon struct {
	adds    []config.Tuber
	updates map[string]config.Tuber
	dels    []string
}

func (f *fakeCRUDDaemon) AddTuber(t config.Tuber) error {
	f.adds = append(f.adds, t)
	return nil
}

func (f *fakeCRUDDaemon) UpdateTuber(name string, t config.Tuber) error {
	if f.updates == nil {
		f.updates = map[string]config.Tuber{}
	}
	f.updates[name] = t
	return nil
}

func (f *fakeCRUDDaemon) DeleteTuber(name string) error {
	f.dels = append(f.dels, name)
	return nil
}

func crudLoad(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func crudTuberByName(cfg *config.Config, name string) *config.Tuber {
	for i := range cfg.Tubers {
		if cfg.Tubers[i].Name == name {
			return &cfg.Tubers[i]
		}
	}
	return nil
}

func TestAdd_FallbackWritesConfig(t *testing.T) {
	path := crudFixture(t)
	crudSSH, crudLocal, crudRemote, crudTags = "u@h:22", "8080", "web:80", "a, b"

	c, out, _ := captureCmd()
	if err := addRunE(c, []string{"web"}); err != nil {
		t.Fatalf("addRunE: %v", err)
	}
	if out.String() != "added: web\n" {
		t.Errorf("output = %q", out.String())
	}

	cfg := crudLoad(t, path)
	got := crudTuberByName(cfg, "web")
	if got == nil {
		t.Fatalf("web not added: %+v", cfg.Tubers)
	}
	if got.Type != "local" || got.SSH != "u@h:22" || got.Local != "8080" || got.Remote != "web:80" {
		t.Errorf("added tuber mismatch: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("tags = %v", got.Tags)
	}
	if got.Enabled {
		t.Error("add must create disabled unless --enabled")
	}
	if crudTuberByName(cfg, "db") == nil {
		t.Error("existing tuber lost")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# managed here") {
		t.Errorf("comment lost:\n%s", data)
	}
}

func TestAdd_DuplicateFails(t *testing.T) {
	path := crudFixture(t)
	before, _ := os.ReadFile(path)
	crudSSH, crudLocal, crudRemote = "u@h:22", "8080", "web:80"

	c, _, _ := captureCmd()
	if err := addRunE(c, []string{"db"}); err == nil {
		t.Fatal("duplicate add must fail")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("failed add must not touch the file")
	}
}

// TestCRUDErrorsPrintToStderr is the issue #4 regression test: add/set/rm run
// with SilenceErrors, so an error returned without being printed exited 1 with
// no output. Every failure path must reach stderr.
func TestCRUDErrorsPrintToStderr(t *testing.T) {
	t.Run("add load error", func(t *testing.T) {
		path := crudFixture(t)
		if err := os.WriteFile(path, []byte("tubers: ["), 0o600); err != nil {
			t.Fatal(err)
		}
		crudSSH, crudLocal, crudRemote = "u@h:22", "8080", "web:80"
		c, _, errOut := captureCmd()
		if err := addRunE(c, []string{"web"}); err == nil {
			t.Fatal("broken config must fail")
		}
		if !strings.Contains(errOut.String(), "load config") {
			t.Errorf("stderr = %q, want it to mention the load error", errOut.String())
		}
	})
	t.Run("set unknown tuber", func(t *testing.T) {
		crudFixture(t)
		c, _, errOut := captureCmd()
		if err := setRunE(c, []string{"nope"}); err == nil {
			t.Fatal("unknown tuber must fail")
		}
		if !strings.Contains(errOut.String(), `not found`) {
			t.Errorf("stderr = %q, want it to mention not-found", errOut.String())
		}
	})
	t.Run("add missing --ssh", func(t *testing.T) {
		crudFixture(t)
		c, _, errOut := captureCmd()
		if err := addRunE(c, []string{"web"}); err == nil {
			t.Fatal("missing --ssh must fail")
		}
		if !strings.Contains(errOut.String(), "--ssh is required") {
			t.Errorf("stderr = %q, want it to name the missing flag", errOut.String())
		}
	})
}

func TestAdd_DaemonPathSkipsFile(t *testing.T) {
	path := crudFixture(t)
	d := &fakeCRUDDaemon{}
	newDaemonCRUD = func() crudDaemon { return d }
	crudSSH, crudLocal, crudRemote = "u@h:22", "8081", "web:81"

	c, _, _ := captureCmd()
	if err := addRunE(c, []string{"web"}); err != nil {
		t.Fatalf("addRunE: %v", err)
	}
	if len(d.adds) != 1 || d.adds[0].Name != "web" {
		t.Errorf("daemon AddTuber not called: %+v", d.adds)
	}
	if crudTuberByName(crudLoad(t, path), "web") != nil {
		t.Error("daemon path must not patch the file directly")
	}
}

func TestSet_ChangesOnlyFlaggedFields(t *testing.T) {
	path := crudFixture(t)
	c := &cobra.Command{Use: "set"}
	addCrudFlags(c, false)
	if err := c.ParseFlags([]string{"--local", "9999"}); err != nil {
		t.Fatal(err)
	}

	if err := setRunE(c, []string{"db"}); err != nil {
		t.Fatalf("setRunE: %v", err)
	}

	got := crudTuberByName(crudLoad(t, path), "db")
	if got.Local != "9999" {
		t.Errorf("local = %q, want 9999", got.Local)
	}
	if got.SSH != "u@h:22" || got.Remote != "db:5432" {
		t.Errorf("untouched fields changed: %+v", got)
	}
	if got.Jump != "bastion:22" || got.Socks5User != "alice" || got.Socks5Password != "s3cret" {
		t.Errorf("phase-52 fields wiped by set: %+v", got)
	}
	if got.PasswordAuth == nil || *got.PasswordAuth {
		t.Errorf("password_auth: false wiped by set: %v", got.PasswordAuth)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "prod" {
		t.Errorf("tags wiped by set: %v", got.Tags)
	}
}

func TestSet_PasswordAuthTriState(t *testing.T) {
	cases := []struct {
		flag string
		want string
	}{
		{"--password-auth=on", "true"},
		{"--password-auth=off", "false"},
		{"--password-auth=inherit", ""},
	}
	for _, tc := range cases {
		path := crudFixture(t)
		c := &cobra.Command{Use: "set"}
		addCrudFlags(c, false)
		if err := c.ParseFlags([]string{tc.flag}); err != nil {
			t.Fatal(err)
		}
		if err := setRunE(c, []string{"db"}); err != nil {
			t.Fatalf("setRunE(%s): %v", tc.flag, err)
		}
		got := crudTuberByName(crudLoad(t, path), "db")
		switch tc.want {
		case "":
			if got.PasswordAuth != nil {
				t.Errorf("%s: PasswordAuth = %v, want nil", tc.flag, *got.PasswordAuth)
			}
		case "true":
			if got.PasswordAuth == nil || !*got.PasswordAuth {
				t.Errorf("%s: PasswordAuth = %v, want true", tc.flag, got.PasswordAuth)
			}
		case "false":
			if got.PasswordAuth == nil || *got.PasswordAuth {
				t.Errorf("%s: PasswordAuth = %v, want false", tc.flag, got.PasswordAuth)
			}
		}
	}
}

func TestSet_UnknownTuber(t *testing.T) {
	crudFixture(t)
	c := &cobra.Command{Use: "set"}
	addCrudFlags(c, false)
	_ = c.ParseFlags([]string{"--local", "1"})
	if err := setRunE(c, []string{"nope"}); err == nil {
		t.Fatal("set on unknown tuber must fail")
	}
}

func TestRm_RemovesWithYes(t *testing.T) {
	path := crudFixture(t)
	crudYes = true

	c, out, _ := captureCmd()
	if err := rmRunE(c, []string{"db"}); err != nil {
		t.Fatalf("rmRunE: %v", err)
	}
	if out.String() != "removed: db\n" {
		t.Errorf("output = %q", out.String())
	}
	cfg := crudLoad(t, path)
	if crudTuberByName(cfg, "db") != nil {
		t.Error("db still present")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# managed here") {
		t.Errorf("comment lost:\n%s", data)
	}
}

func TestRm_ConfirmDeclined(t *testing.T) {
	path := crudFixture(t)
	crudYes = false
	prevConfirm := confirmRemove
	confirmRemove = func(string) bool { return false }
	t.Cleanup(func() { confirmRemove = prevConfirm })

	c, out, _ := captureCmd()
	if err := rmRunE(c, []string{"db"}); err != nil {
		t.Fatalf("rmRunE: %v", err)
	}
	if out.String() != "aborted\n" {
		t.Errorf("output = %q", out.String())
	}
	if crudTuberByName(crudLoad(t, path), "db") == nil {
		t.Error("declined rm must keep the tuber")
	}
}

func TestRm_DaemonPath(t *testing.T) {
	path := crudFixture(t)
	d := &fakeCRUDDaemon{}
	newDaemonCRUD = func() crudDaemon { return d }
	crudYes = true

	c, _, _ := captureCmd()
	if err := rmRunE(c, []string{"db"}); err != nil {
		t.Fatalf("rmRunE: %v", err)
	}
	if len(d.dels) != 1 || d.dels[0] != "db" {
		t.Errorf("daemon DeleteTuber not called: %v", d.dels)
	}
	if crudTuberByName(crudLoad(t, path), "db") == nil {
		t.Error("daemon path must not patch the file directly")
	}
}
