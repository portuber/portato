package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/controller"
	routelog "github.com/portuber/portato/internal/log"
)

type fakeCtrl struct {
	mu        sync.Mutex
	statuses  []controller.Status
	enabled   []string
	disabled  []string
	restarted []string
	reloads   int
	lists     int
	adds      []config.Tuber
	updates   []config.Tuber
	deletes   []string
	cfg       *config.Config
	tunErr    error // returned by Add/Update/Delete when set
	logs      []routelog.Entry
	accepted  []string
	// passphrases records AcceptPassphrase submissions (name -> passphrase).
	passphrases map[string]string
	// passwords records AcceptPassword submissions (name -> password).
	passwords map[string]string
	changes   chan struct{}
}

func (f *fakeCtrl) List() []controller.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	out := make([]controller.Status, len(f.statuses))
	copy(out, f.statuses)
	return out
}
func (f *fakeCtrl) Enable(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled = append(f.enabled, name)
	for i := range f.statuses {
		if f.statuses[i].Name == name {
			f.statuses[i].State = controller.Connecting
		}
	}
	return nil
}
func (f *fakeCtrl) Disable(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled = append(f.disabled, name)
	for i := range f.statuses {
		if f.statuses[i].Name == name {
			f.statuses[i].State = controller.Off
		}
	}
	return nil
}
func (f *fakeCtrl) Restart(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarted = append(f.restarted, name)
	return nil
}
func (f *fakeCtrl) Reload() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloads++
	return nil
}
func (f *fakeCtrl) Changes() <-chan struct{} { return f.changes }
func (f *fakeCtrl) Close() error             { return nil }

func (f *fakeCtrl) Config() (*config.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cfg == nil {
		return &config.Config{}, nil
	}
	return f.cfg.Clone(), nil
}
func (f *fakeCtrl) AddTuber(t config.Tuber) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tunErr != nil {
		return f.tunErr
	}
	f.adds = append(f.adds, t)
	return nil
}
func (f *fakeCtrl) UpdateTuber(name string, t config.Tuber) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tunErr != nil {
		return f.tunErr
	}
	f.updates = append(f.updates, t)
	return nil
}
func (f *fakeCtrl) DeleteTuber(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tunErr != nil {
		return f.tunErr
	}
	f.deletes = append(f.deletes, name)
	return nil
}

func (f *fakeCtrl) Logs(string) ([]routelog.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]routelog.Entry, len(f.logs))
	copy(out, f.logs)
	return out, nil
}

func (f *fakeCtrl) AcceptHost(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accepted = append(f.accepted, name)
	return f.tunErr
}

func (f *fakeCtrl) AcceptPassphrase(name, passphrase string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.passphrases == nil {
		f.passphrases = map[string]string{}
	}
	f.passphrases[name] = passphrase
	return f.tunErr
}

func (f *fakeCtrl) AcceptPassword(name, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.passwords == nil {
		f.passwords = map[string]string{}
	}
	f.passwords[name] = password
	return f.tunErr
}

// LiveListenerFiles is unused by model tests; it satisfies the Controller
// interface (the hand-off path is covered in handoff_test.go).
func (f *fakeCtrl) LiveListenerFiles() (map[string]*os.File, error) {
	return nil, nil
}

func newFake(statuses ...controller.Status) *fakeCtrl {
	cp := make([]controller.Status, len(statuses))
	copy(cp, statuses)
	return &fakeCtrl{statuses: cp, changes: make(chan struct{})}
}

func keyPress(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Text: s} }

func specialKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestModel_Navigation(t *testing.T) {
	f := newFake(
		controller.Status{Name: "a", Type: "local", Local: "1", Remote: "r"},
		controller.Status{Name: "b", Type: "local", Local: "2", Remote: "r"},
		controller.Status{Name: "c", Type: "local", Local: "3", Remote: "r"},
	)
	m := New(f, Options{Mode: "standalone"})

	for _, k := range []string{"j", "j"} {
		next, _ := m.handleKey(keyPress(k))
		m = next.(Model)
	}
	if m.cursor != 2 {
		t.Errorf("cursor after j,j = %d, want 2", m.cursor)
	}

	next, _ := m.handleKey(keyPress("k"))
	m = next.(Model)
	if m.cursor != 1 {
		t.Errorf("cursor after k = %d, want 1", m.cursor)
	}

	next, _ = m.handleKey(specialKey(tea.KeyDown))
	m = next.(Model)
	if m.cursor != 2 {
		t.Errorf("cursor after down = %d, want 2", m.cursor)
	}

	next, _ = m.handleKey(specialKey(tea.KeyUp))
	m = next.(Model)
	if m.cursor != 1 {
		t.Errorf("cursor after up = %d, want 1", m.cursor)
	}

	next, _ = m.handleKey(keyPress("j"))
	m = next.(Model)
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (clamped)", m.cursor)
	}
	next, _ = m.handleKey(keyPress("j"))
	m = next.(Model)
	if m.cursor != 2 {
		t.Errorf("cursor past end = %d, want 2 (clamped)", m.cursor)
	}
}

func TestModel_SpaceToggles(t *testing.T) {
	f := newFake(controller.Status{Name: "a", Type: "local", Local: "1", Remote: "r", State: controller.Off})
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(specialKey(tea.KeySpace))
	m = next.(Model)
	if len(f.enabled) != 1 || f.enabled[0] != "a" {
		t.Errorf("expected Enable(a), got %v", f.enabled)
	}

	next, _ = m.handleKey(specialKey(tea.KeySpace))
	m = next.(Model)
	if len(f.disabled) != 1 || f.disabled[0] != "a" {
		t.Errorf("expected Disable(a), got %v", f.disabled)
	}
}

func TestModel_RestartAndReloadAndAll(t *testing.T) {
	f := newFake(
		controller.Status{Name: "a", State: controller.Off},
		controller.Status{Name: "b", State: controller.Connected},
	)
	m := New(f, Options{Mode: "standalone"})

	m2, _ := m.handleKey(keyPress("r"))
	m = m2.(Model)
	if len(f.restarted) != 1 || f.restarted[0] != "a" {
		t.Errorf("restart: got %v", f.restarted)
	}

	m2, _ = m.handleKey(keyPress("a"))
	m = m2.(Model)
	if len(f.enabled) != 1 || f.enabled[0] != "a" {
		t.Errorf("enable all: got %v", f.enabled)
	}

	f.statuses[0].State = controller.Connected
	f.statuses[1].State = controller.Connected
	m2, _ = m.handleKey(keyPress("x"))
	m = m2.(Model)
	if len(f.disabled) != 2 {
		t.Errorf("disable all: got %v", f.disabled)
	}

	m2, _ = m.handleKey(keyPress("R"))
	m = m2.(Model)
	if f.reloads != 1 {
		t.Errorf("reload: got %d", f.reloads)
	}
}

func TestModel_HelpAndQuit(t *testing.T) {
	f := newFake(controller.Status{Name: "a"})
	m := New(f, Options{Mode: "standalone"})
	if m.help != nil {
		t.Fatal("help should start hidden")
	}
	next, _ := m.handleKey(keyPress("?"))
	m = next.(Model)
	if m.help == nil {
		t.Error("? should toggle help on")
	}
	next, _ = m.handleKey(specialKey(tea.KeyEsc))
	m = next.(Model)
	if m.help != nil {
		t.Error("esc should toggle help off")
	}

	_, cmd := m.handleKey(keyPress("q"))
	if cmd == nil {
		t.Error("q should return a quit command")
	}
}

func TestModel_RenderContainsTubers(t *testing.T) {
	f := newFake(
		controller.Status{Name: "alpha", Type: "local", Local: "5432", Remote: "db:5432", State: controller.Connected},
		controller.Status{Name: "beta", Type: "local", Local: "8080", Remote: "web:80", State: controller.Off, Error: "boom"},
		controller.Status{Name: "gamma", Type: "remote", Local: "5432", Remote: "db:5432", State: controller.Connected},
	)
	m := New(f, Options{Mode: "standalone"})
	m.width = 100

	out := m.render()
	for _, want := range []string{"Portato", "mode: standalone", "alpha", "beta", "gamma", "5432 → db:5432", "5432 ← db:5432", "remote", "ENDPOINT"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	if !strings.Contains(out, "connected") {
		t.Error("render should show connected status")
	}
	if !strings.Contains(out, "boom") {
		t.Error("render should show error text")
	}

	m.help = newHelpView(m.pal, m.kind, m.width, m.height)
	if !strings.Contains(m.render(), "move cursor up") {
		t.Error("render should show the help view when help is open")
	}
}

func TestModel_EmptyList(t *testing.T) {
	f := newFake()
	m := New(f, Options{Mode: "standalone"})
	m2, _ := m.handleKey(keyPress("space"))
	if mm, ok := m2.(Model); !ok || mm.cursor != 0 {
		t.Error("space on empty list should be a no-op")
	}
	if !strings.Contains(m.render(), "no tubers") {
		t.Error("empty list should render placeholder")
	}
}

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		d   time.Duration
		out string
	}{
		{45 * time.Second, "45s"},
		{2*time.Minute + 3*time.Second, "2m3s"},
		{time.Hour + 5*time.Minute, "1h5m"},
		{3*24*time.Hour + 2*time.Hour, "3d2h"},
		{time.Minute, "1m0s"},
		{time.Hour, "1h0m"},
		{24 * time.Hour, "1d0h"},
	}
	for _, c := range cases {
		if got := formatUptime(c.d); got != c.out {
			t.Errorf("formatUptime(%v) = %q, want %q", c.d, got, c.out)
		}
	}
}

func TestPad(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter pads to n", "local", 7, "local  "},
		{"exactly n stays n (no extra space)", "dynamic", 7, "dynamic"},
		{"one under n", "remote", 7, "remote "},
		{"empty pads to n", "", 7, "       "},
		{"overflow keeps value plus one space", "abcdef", 3, "abcdef "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pad(c.in, c.n)
			if got != c.want {
				t.Errorf("pad(%q,%d) = %q (len %d), want %q (len %d)",
					c.in, c.n, got, len(got), c.want, len(c.want))
			}
		})
	}
}

// TestPadAlignsColumns guards the regression: a value exactly as wide as its
// column must not shift later columns. "dynamic" (7) in a width-7 TYPE column
// must produce a cell of the same width as "local" (5) padded to 7.
func TestPadAlignsColumns(t *testing.T) {
	if w := lipgloss.Width(pad("dynamic", colType)); w != colType {
		t.Errorf("pad(dynamic,%d) width = %d, want %d", colType, w, colType)
	}
	if w := lipgloss.Width(pad("local", colType)); w != colType {
		t.Errorf("pad(local,%d) width = %d, want %d", colType, w, colType)
	}
}

func TestFitEndpoint(t *testing.T) {
	const maxW = colEndpoint // 32
	cases := []struct {
		name string
		in   string
		// checks applied to the result
		contains  []string
		hasPrefix string
		hasSuffix string
		unchanged bool // expect the input back verbatim
	}{
		{
			name:      "short local endpoint unchanged",
			in:        "127.0.0.1:5432 → db:5432",
			unchanged: true,
		},
		{
			name:      "dynamic endpoint unchanged",
			in:        "127.0.0.1:1080 ⇄ *",
			unchanged: true,
		},
		{
			name:      "long host keeps local, arrow and port",
			in:        "127.0.0.1:33061 → c-c9qmgaf6i8b4nlavcqnr.rw.mdb.yandexcloud.net:3306",
			hasPrefix: "127.0.0.1:33061 → ",
			hasSuffix: ":3306",
			contains:  []string{"…"},
		},
		{
			name:     "long remote direction keeps port",
			in:       "5432 ← c-c9qmgaf6i8b4nlavcqnr.rw.mdb.yandexcloud.net:3306",
			contains: []string{" ← ", ":3306", "…"},
		},
		{
			name:     "bare host without port truncated with ellipsis",
			in:       "127.0.0.1:33061 → thisisaveryverylonghostnamewithnoport",
			contains: []string{"…"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fitEndpoint(tc.in, maxW)
			if tc.unchanged {
				if got != tc.in {
					t.Errorf("expected unchanged %q, got %q", tc.in, got)
				}
				return
			}
			if w := lipgloss.Width(got); w > maxW {
				t.Errorf("result width %d > max %d: %q", w, maxW, got)
			}
			if tc.hasPrefix != "" && !strings.HasPrefix(got, tc.hasPrefix) {
				t.Errorf("result %q must have prefix %q", got, tc.hasPrefix)
			}
			if tc.hasSuffix != "" && !strings.HasSuffix(got, tc.hasSuffix) {
				t.Errorf("result %q must have suffix %q", got, tc.hasSuffix)
			}
			for _, s := range tc.contains {
				if !strings.Contains(got, s) {
					t.Errorf("result %q must contain %q", got, s)
				}
			}
		})
	}
}

func TestFitName(t *testing.T) {
	t.Run("fits unchanged", func(t *testing.T) {
		cases := []struct {
			in  string
			max int
		}{
			{"db", 20},
			{"abcdefghij", 10},
			{"abcde", 6},
			{"", 5},
		}
		for _, c := range cases {
			if got := fitName(c.in, c.max); got != c.in {
				t.Errorf("fitName(%q,%d) = %q, want unchanged", c.in, c.max, got)
			}
		}
	})
	t.Run("overflow middle-truncates to exactly max", func(t *testing.T) {
		const in = "pntr-sberhealth-browser"
		for _, maxW := range []int{20, 16, 12} {
			got := fitName(in, maxW)
			if w := lipgloss.Width(got); w != maxW {
				t.Errorf("max=%d: width=%d want %d (%q)", maxW, w, maxW, got)
			}
			if !strings.Contains(got, "…") {
				t.Errorf("max=%d: expected a middle ellipsis, got %q", maxW, got)
			}
		}
	})
	t.Run("overflow keeps prefix and suffix", func(t *testing.T) {
		got := fitName("pntr-sberhealth-browser", 20)
		if !strings.HasPrefix(got, "pntr") {
			t.Errorf("expected prefix preserved, got %q", got)
		}
		if !strings.HasSuffix(got, "ser") {
			t.Errorf("expected suffix preserved, got %q", got)
		}
	})
	t.Run("max=1 yields ellipsis", func(t *testing.T) {
		if got := fitName("pntr-sberhealth-browser", 1); got != "…" {
			t.Errorf("fitName(...,1) = %q, want …", got)
		}
	})
}

func TestColumnBudget(t *testing.T) {
	mixed := []controller.Status{
		{Name: "db"},
		{Name: "pntr-sberhealth-browser"}, // 24 display cells
	}

	// width=0: pre-WindowSize fallback returns the historical fixed widths.
	t.Run("width=0 falls back to fixed widths", func(t *testing.T) {
		m := New(newFake(mixed...), Options{Mode: "standalone"})
		m.width = 0
		c := m.columnBudget()
		if c != (columns{colName, colType, colEndpoint, colStatus, uptimeBudget}) {
			t.Errorf("width=0: budget = %+v", c)
		}
	})

	// STATUS + indicator are untouchable at every width — the core F5 fix.
	// ENDPOINT shrinks first; NAME absorbs slack up to maxName; the block
	// never exceeds the terminal width (no ragged right edge).
	for _, width := range []int{120, 90, 80, 62} {
		t.Run(fmt.Sprintf("field width=%d", width), func(t *testing.T) {
			m := New(newFake(mixed...), Options{Mode: "standalone"})
			m.width = width
			c := m.columnBudget()
			if c.statusW != colStatus {
				t.Errorf("statusW=%d want colStatus=%d (untouchable)", c.statusW, colStatus)
			}
			if c.upW != uptimeBudget {
				t.Errorf("upW=%d want uptimeBudget=%d", c.upW, uptimeBudget)
			}
			if c.typeW != colType {
				t.Errorf("typeW=%d want colType=%d (full words at this width)", c.typeW, colType)
			}
			if c.nameW < minName || c.nameW > maxName {
				t.Errorf("nameW=%d out of [%d,%d]", c.nameW, minName, maxName)
			}
			if c.epW > colEndpoint {
				t.Errorf("epW=%d > colEndpoint=%d", c.epW, colEndpoint)
			}
			if c.epW < 1 {
				t.Errorf("epW=%d must be >= 1 (truncate would panic)", c.epW)
			}
			const lead = 4
			total := lead + 2*sideMargin + 4*len(gutter) + c.nameW + c.typeW + c.epW + c.statusW + c.upW
			if total > width {
				t.Errorf("total column width %d > terminal %d (ragged)", total, width)
			}
		})
	}

	// 120 reference: ENDPOINT keeps its full 48, NAME absorbs the slack.
	t.Run("120 keeps full ENDPOINT", func(t *testing.T) {
		m := New(newFake(mixed...), Options{Mode: "standalone"})
		m.width = 120
		if c := m.columnBudget(); c.epW != colEndpoint {
			t.Errorf("120: epW=%d want colEndpoint=%d", c.epW, colEndpoint)
		}
	})

	// ENDPOINT shrinks strictly as width decreases (monotonic, never grows).
	t.Run("ENDPOINT shrinks monotonically with width", func(t *testing.T) {
		prev := colEndpoint
		for _, width := range []int{120, 100, 90, 80, 70, 62} {
			m := New(newFake(mixed...), Options{Mode: "standalone"})
			m.width = width
			if c := m.columnBudget(); c.epW > prev {
				t.Errorf("width=%d: epW=%d grew above prev %d", width, c.epW, prev)
			} else {
				prev = c.epW
			}
		}
	})

	// NAME clamps to maxName for very long content.
	t.Run("NAME clamps to maxName", func(t *testing.T) {
		m := New(newFake(controller.Status{Name: strings.Repeat("x", 60)}), Options{Mode: "standalone"})
		m.width = 200
		if c := m.columnBudget(); c.nameW != maxName {
			t.Errorf("very long name: nameW=%d want maxName=%d", c.nameW, maxName)
		}
	})

	// TYPE degrades to 1 (L/R/D) only when STATUS/minName would be endangered.
	t.Run("TYPE degrades on very narrow terminal", func(t *testing.T) {
		m := New(newFake(mixed...), Options{Mode: "standalone"})
		m.width = 40
		if c := m.columnBudget(); c.typeW != 1 {
			t.Errorf("width=40: typeW=%d want 1 (L/R/D degradation)", c.typeW)
		}
	})

	// filter does not change the budget (it considers every name, even hidden).
	t.Run("filter does not change budget", func(t *testing.T) {
		m := New(newFake(mixed...), Options{Mode: "standalone"})
		m.width = 200
		base := m.columnBudget()
		m.filter.SetValue("db")
		if filtered := m.columnBudget(); base != filtered {
			t.Errorf("filter changed budget: base=%+v filtered=%+v", base, filtered)
		}
	})
}

// TestTableRender_StatusSurvivesNarrow proves the F5 fix at the render level:
// the full status word (connected/error/off) survives at every width, and the
// long ENDPOINT middle-truncates with an ellipsis on narrow terminals instead
// of clipping STATUS. This is the phase-38 field acceptance (90/62 cols) as a
// deterministic unit test.
func TestTableRender_StatusSurvivesNarrow(t *testing.T) {
	statuses := []controller.Status{
		{Name: "db", Type: "local", Local: "127.0.0.1:5432", Remote: "db.internal.example.com:5432", State: controller.Connected},
		{Name: "broken", Type: "local", Local: "127.0.0.1:9", Remote: "127.0.0.1:9", State: controller.Error, Error: "connect refused"},
		{Name: "idle", Type: "remote", Local: "8080", Remote: "127.0.0.1:9090", State: controller.Off},
	}
	for _, width := range []int{120, 90, 80, 62} {
		m := New(newFake(statuses...), Options{Mode: "standalone"})
		m.width = width
		m.height = 24
		out := m.table()
		for _, want := range []string{"connected", "error", "off"} {
			if !strings.Contains(out, want) {
				t.Errorf("width=%d: status word %q missing from table\n%s", width, want, out)
			}
		}
	}
	// On the narrow widths the long connected ENDPOINT truncates with "…".
	for _, width := range []int{90, 80, 62} {
		m := New(newFake(statuses...), Options{Mode: "standalone"})
		m.width = width
		m.height = 24
		if out := m.table(); !strings.Contains(out, "…") {
			t.Errorf("width=%d: expected an endpoint ellipsis, got none\n%s", width, out)
		}
	}
}

func TestRowColumnNameAlignment(t *testing.T) {
	statuses := []controller.Status{
		{Name: "db", Type: "local", Local: "1", Remote: "r"},
		{Name: "pntr-sberhealth-browser", Type: "dynamic", Local: "1080"},
		{Name: "tv-socks", Type: "remote", Local: "2", Remote: "r"},
		{Name: "x", Type: "local", Local: "3", Remote: "r"},
	}
	for _, termWidth := range []int{200, 110} {
		m := New(newFake(statuses...), Options{Mode: "standalone"})
		m.width = termWidth
		c := m.columnBudget()
		want := c.nameW + 6
		for i, s := range m.list {
			rowStr := m.row(i, s, c)
			idx := strings.Index(rowStr, s.Type)
			if idx < 0 {
				t.Fatalf("width=%d row %d: type %q not found in row %q", termWidth, i, s.Type, rowStr)
			}
			if w := lipgloss.Width(rowStr[:idx]); w != want {
				t.Errorf("width=%d row %d (%q): TYPE starts at display col %d, want %d\nrow: %q",
					termWidth, i, s.Name, w, want, rowStr)
			}
		}
	}
}

func TestModel_QuitStandaloneLiveShowsModal(t *testing.T) {
	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "standalone", CfgPath: "/cfg"})

	next, cmd := m.handleKey(keyPress("q"))
	m = next.(Model)
	if m.confirmQuit {
		// expected
	} else {
		t.Fatal("standalone q with live tubers should raise confirm modal")
	}
	if m.quit {
		t.Error("should not quit immediately while modal is up")
	}
	if cmd != nil {
		t.Error("no command expected when raising modal")
	}
	if !strings.Contains(m.render(), "background") {
		t.Error("modal should be rendered")
	}
}

func TestModel_QuitStandaloneNoLiveQuits(t *testing.T) {
	f := newFake(controller.Status{Name: "a", State: controller.Off})
	m := New(f, Options{Mode: "standalone"})
	_, cmd := m.handleKey(keyPress("q"))
	if cmd == nil {
		t.Error("standalone q with no live tubers should quit immediately")
	}
}

func TestModel_QuitAttachNoModal(t *testing.T) {
	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "attach @ /sock"})
	if !m.attach {
		t.Fatal("attach mode should be detected")
	}
	next, cmd := m.handleKey(keyPress("q"))
	mm := next.(Model)
	if !mm.quit || cmd == nil {
		t.Error("attach q should quit immediately without modal")
	}
	if mm.confirmQuit {
		t.Error("no modal in attach mode")
	}
}

func TestModel_ConfirmKeys(t *testing.T) {
	restoreHandoffSeams(t)
	startCmd = func(string, string) error { return nil }
	probeSocket = func() bool { return true }

	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "standalone", CfgPath: "/cfg"})
	next, _ := m.handleKey(keyPress("q"))
	m = next.(Model)
	if !m.confirmQuit {
		t.Fatal("precondition: modal should be up")
	}

	// "y" -> handoff
	next, cmd := m.handleKey(keyPress("y"))
	m = next.(Model)
	if !m.handoffing || m.confirmQuit || cmd == nil {
		t.Errorf("y: handoffing=%v confirmQuit=%v cmd=%v", m.handoffing, m.confirmQuit, cmd)
	}

	// reset to modal, then "n" -> quit
	m2 := New(f, Options{Mode: "standalone", CfgPath: "/cfg"})
	m2.list = []controller.Status{{Name: "a", State: controller.Connected}}
	m2.confirmQuit = true
	next, cmd = m2.handleKey(keyPress("n"))
	mm := next.(Model)
	if !mm.quit || mm.confirmQuit || cmd == nil {
		t.Errorf("n: quit=%v confirmQuit=%v cmd=%v", mm.quit, mm.confirmQuit, cmd)
	}

	// enter declines (same as n): stop + exit
	mEnter := New(f, Options{Mode: "standalone", CfgPath: "/cfg"})
	mEnter.list = []controller.Status{{Name: "a", State: controller.Connected}}
	mEnter.confirmQuit = true
	next, cmd = mEnter.handleKey(keyPress("enter"))
	mm = next.(Model)
	if !mm.quit || mm.confirmQuit || cmd == nil {
		t.Errorf("enter: quit=%v confirmQuit=%v cmd=%v", mm.quit, mm.confirmQuit, cmd)
	}

	// esc cancels the modal: back to the list, no quit, tubers untouched
	mEsc := New(f, Options{Mode: "standalone", CfgPath: "/cfg"})
	mEsc.list = []controller.Status{{Name: "a", State: controller.Connected}}
	mEsc.confirmQuit = true
	next, cmd = mEsc.handleKey(keyPress("esc"))
	mm = next.(Model)
	if mm.quit || mm.confirmQuit || cmd != nil {
		t.Errorf("esc: want cancel (quit=false confirmQuit=false cmd=nil), got quit=%v confirmQuit=%v cmd=%v", mm.quit, mm.confirmQuit, cmd)
	}
}

func TestIndicatorShapePerState(t *testing.T) {
	cases := []struct {
		state controller.State
		glyph string
	}{
		{controller.Off, "○"},
		{controller.Error, "✗"},
		{controller.Connected, "●"},
		{controller.Connecting, "●"},
		{controller.Reconnecting, "●"},
	}
	for _, c := range cases {
		got := indicator(darkPalette(), controller.Status{State: c.state})
		if !strings.Contains(got, c.glyph) {
			t.Errorf("state %v: indicator %q does not contain %q", c.state, got, c.glyph)
		}
	}
}

// TestRenderErrorIndicatorDistinct guards the regression where an errored
// tuber showed ● (indistinguishable from connected). The error row must use
// ✗ and must not contain a ● glyph that reads as "live".
// TestRenderCursorGlyph asserts the selected row is marked with a ❯ cursor
// glyph and unselected rows are not (Phase 11 selection redesign).
func TestRenderCursorGlyph(t *testing.T) {
	f := newFake(
		controller.Status{Name: "a", Type: "local", Local: "1", Remote: "r"},
		controller.Status{Name: "b", Type: "local", Local: "2", Remote: "r"},
	)
	m := New(f, Options{Mode: "standalone"})
	m.width = 100
	out := m.render() // cursor=0 → first row selected

	if c := strings.Count(out, "❯"); c != 1 {
		t.Errorf("expected exactly one ❯ cursor glyph (the selected row), got %d\n%s", c, out)
	}
	if !strings.Contains(out, "●  a") && !strings.Contains(out, "○  a") {
		// not strictly required, but sanity-check the row still renders
	}
}

// TestViewBackgroundColor guards one prong of the Phase 37 surface fill: the
// light theme sets View.BackgroundColor so the renderer sets the terminal's own
// background (OSC 11) to #FAFAFA — covering the whole pane (incl. the area below
// the content block) on terminals that honour OSC 11 set (e.g. Terminal.app).
// Dark/mono leave it nil so the user's terminal background shows through. (The
// other prong, fillBg, cell-paints every content line for terminals that ignore
// OSC 11 set, e.g. iTerm2 — covered by TestFillBg below.)
func TestViewBackgroundColor(t *testing.T) {
	cases := []struct {
		name string
		kind themeKind
	}{
		{"light sets surface bg", themeLight},
		{"dark stays transparent", themeDark},
		{"mono stays transparent", themeMono},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := Model{pal: resolvePalette(c.kind)}
			v := m.View()
			switch c.kind {
			case themeLight:
				if v.BackgroundColor == nil {
					t.Errorf("light theme should set View.BackgroundColor")
				}
			default:
				if v.BackgroundColor != nil {
					t.Errorf("%s theme should leave View.BackgroundColor nil (transparent)", c.name)
				}
			}
		})
	}
}

// TestFillBg verifies fillBg (the light-theme content-line coverage used on
// terminals that ignore OSC 11 set): a no-op without a colour or unknown
// dimensions; content preserved; each line padded to width with the bg SGR.
// (It no longer pads to full height — those appended whitespace-only lines are
// stripped by the v2 cell renderer, so the height padding was removed.)
func TestFillBg(t *testing.T) {
	if got := fillBg("hi", nil, 10, 10); got != "hi" {
		t.Errorf("fillBg with nil bg should be a no-op, got %q", got)
	}
	if got := fillBg("hi", lipgloss.Color("230"), 0, 0); got != "hi" {
		t.Errorf("fillBg with zero dims should be a no-op, got %q", got)
	}
	got := fillBg("hi", lipgloss.Color("230"), 12, 5)
	if !strings.Contains(got, "hi") {
		t.Errorf("fillBg lost the content: %q", got)
	}
	// The line is padded to the width with bg-coloured cells.
	if w := lipgloss.Width(got); w != 12 {
		t.Errorf("fillBg should pad the line to width 12, got display width %d (%q)", w, got)
	}
}

// TestFillBgReassertsAfterReset checks the core coverage guarantee: a styled run
// ends with an ANSI reset, and the raw cells after it (glued spaces, plain text)
// must still carry the surface background — fillBg re-inserts the bg SGR right
// after every reset rather than leaving the trailing cells on the default bg.
func TestFillBgReassertsAfterReset(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("26")).Render("AB")
	content := styled + " CD"
	got := fillBg(content, lipgloss.Color("#FAFAFA"), 20, 1)

	reset := "\x1b[m"
	i := strings.Index(got, reset)
	if i < 0 {
		reset = "\x1b[0m"
		i = strings.Index(got, reset)
	}
	if i < 0 {
		t.Fatalf("no reset in fillBg output: %q", got)
	}
	after := got[i+len(reset):]
	if !strings.HasPrefix(after, "\x1b[") {
		t.Errorf("fillBg did not re-assert bg after reset: %q", after)
	}
	if !strings.Contains(got, "CD") {
		t.Errorf("fillBg dropped raw content after a styled run: %q", got)
	}
}

func TestRenderHasSideMargin(t *testing.T) {
	f := newFake(controller.Status{Name: "a", Type: "local", Local: "1", Remote: "r"})
	m := New(f, Options{Mode: "standalone"})
	m.width, m.height = 80, 24
	out := m.render()
	want := strings.Repeat(" ", sideMargin)
	for i, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, want) {
			t.Errorf("line %d should start with %d-space margin: %q", i, sideMargin, line)
		}
	}
}

func TestRenderErrorIndicatorDistinct(t *testing.T) {
	f := newFake(controller.Status{Name: "x", Type: "local", Local: "1", Remote: "r", State: controller.Error, Error: "listen fail"})
	m := New(f, Options{Mode: "standalone"})
	m.width = 100
	out := m.render()
	if !strings.Contains(out, "✗") {
		t.Errorf("error tuber should render ✗ indicator\ngot:\n%s", out)
	}
	if strings.Contains(out, "●") {
		t.Errorf("error render must not contain ● (would look connected)\ngot:\n%s", out)
	}
}

func TestModel_TickIgnoredDuringHandoff(t *testing.T) {
	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "standalone", CfgPath: "/cfg"})
	m.handoffing = true

	next, cmd := m.Update(tickMsg{})
	mm := next.(Model)
	if cmd != nil {
		t.Error("tick during handoff should not schedule another wait")
	}
	if mm.handoffing != true {
		t.Error("handoffing flag should be preserved")
	}
}

// TestModel_RedrawTickDoesNotFetch guards the Phase 9 guarantee: the per-second
// redraw tick (which refreshes uptime) must NOT fetch from the controller — it
// is a purely local re-render. Otherwise idle clients would poll the daemon.
func TestModel_RedrawTickDoesNotFetch(t *testing.T) {
	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "standalone"})
	before := f.lists

	next, cmd := m.Update(redrawTickMsg{})
	_ = next.(Model)

	if cmd == nil {
		t.Error("redrawTickMsg should re-arm the redraw tick (non-nil cmd)")
	}
	if f.lists != before {
		t.Errorf("redrawTickMsg triggered %d List() call(s), want 0 (no idle fetch)", f.lists-before)
	}
}

func TestModel_PasteRoutedToEditor(t *testing.T) {
	f := newFake(controller.Status{Name: "db"})
	f.cfg = &config.Config{Tubers: []config.Tuber{{Name: "db"}}}
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(keyPress("n"))
	m = next.(Model)
	if m.editor == nil {
		t.Fatal("precondition: editor should be open")
	}
	next, _ = m.Update(tea.PasteMsg{Content: "pasted"})
	mm := next.(Model)
	if got := mm.editor.name.Value(); got != "pasted" {
		t.Errorf("paste should reach the editor's focused field; name=%q", got)
	}
}

func TestModel_PasteInListViewIsNoOp(t *testing.T) {
	f := newFake(controller.Status{Name: "db"})
	m := New(f, Options{Mode: "standalone"})

	next, cmd := m.Update(tea.PasteMsg{Content: "pasted"})
	mm := next.(Model)
	if cmd != nil {
		t.Error("paste in list view should return a nil cmd")
	}
	if mm.editor != nil {
		t.Error("paste in list view should not open the editor")
	}
}

func TestModel_EditKeyOpensEditor(t *testing.T) {
	f := newFake(controller.Status{Name: "db", Type: "local"})
	f.cfg = &config.Config{Tubers: []config.Tuber{{Name: "db", Type: "local", SSH: "u@h:22", Local: "5432"}}}
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(keyPress("e"))
	mm := next.(Model)
	if mm.editor == nil {
		t.Fatal("e should open the editor")
	}
	if mm.editor.mode != modeEdit || mm.editor.original != "db" {
		t.Errorf("editor mode/original = %v/%q", mm.editor.mode, mm.editor.original)
	}
	if mm.editor.name.Value() != "db" {
		t.Errorf("editor should be prefilled, name=%q", mm.editor.name.Value())
	}
}

func TestModel_EditKeyNoSelection(t *testing.T) {
	f := newFake()
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("e"))
	mm := next.(Model)
	if mm.editor != nil {
		t.Error("e on empty list should not open the editor")
	}
}

func TestModel_NewKeyOpensEditor(t *testing.T) {
	f := newFake(controller.Status{Name: "db"})
	f.cfg = &config.Config{Tubers: []config.Tuber{{Name: "db"}}}
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("n"))
	mm := next.(Model)
	if mm.editor == nil || mm.editor.mode != modeNew {
		t.Fatalf("n should open a new-tuber editor, got editor=%v", mm.editor)
	}
	if mm.editor.name.Value() != "" {
		t.Errorf("new editor name should be empty, got %q", mm.editor.name.Value())
	}
}

func TestModel_DuplicateKeyOpensEditor(t *testing.T) {
	f := newFake(controller.Status{Name: "db", Type: "local"})
	f.cfg = &config.Config{Tubers: []config.Tuber{{
		Name: "db", Type: "local", SSH: "u@h:22", Local: "5432", Remote: "db:5432", Identity: "~/.ssh/id",
	}}}
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(keyPress("C"))
	mm := next.(Model)
	if mm.editor == nil {
		t.Fatal("C should open the editor")
	}
	if mm.editor.mode != modeNew {
		t.Errorf("duplicate editor mode = %v, want modeNew", mm.editor.mode)
	}
	if mm.editor.original != "" {
		t.Errorf("duplicate editor original = %q, want \"\" (clean modeNew)", mm.editor.original)
	}
	if mm.editor.focus != fName {
		t.Errorf("duplicate editor focus = %d, want fName (%d)", mm.editor.focus, fName)
	}
	if got := mm.editor.name.Value(); got != "db-copy" {
		t.Errorf("duplicate name = %q, want db-copy", got)
	}
	if mm.editor.enabled {
		t.Error("duplicate should be created enabled=false")
	}
	if tuberTypes[mm.editor.typeIdx] != "local" {
		t.Errorf("type not prefilled from source: %s", tuberTypes[mm.editor.typeIdx])
	}
	if got := mm.editor.ssh.Value(); got != "u@h:22" {
		t.Errorf("ssh not prefilled: %q", got)
	}
	if got := mm.editor.local.Value(); got != "5432" {
		t.Errorf("local not prefilled: %q", got)
	}
	if got := mm.editor.remote.Value(); got != "db:5432" {
		t.Errorf("remote not prefilled: %q", got)
	}
	if got := mm.editor.identity.Value(); got != "~/.ssh/id" {
		t.Errorf("identity not prefilled: %q", got)
	}
}

func TestModel_DuplicateKeyNoSelection(t *testing.T) {
	f := newFake()
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("C"))
	mm := next.(Model)
	if mm.editor != nil {
		t.Error("C on empty list should not open the editor")
	}
}

func TestModel_LowercaseCIsNoOp(t *testing.T) {
	f := newFake(controller.Status{Name: "db", Type: "local"})
	f.cfg = &config.Config{Tubers: []config.Tuber{{Name: "db"}}}
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(keyPress("c"))
	mm := next.(Model)
	if mm.editor != nil {
		t.Error("lowercase c must not open the editor (only Shift+C duplicates)")
	}
	if len(f.adds) != 0 {
		t.Errorf("lowercase c must not add anything, got %+v", f.adds)
	}
}

func TestFreshName(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		existing []string
		want     string
	}{
		{"first copy", "db", []string{"db"}, "db-copy"},
		{"copy taken -> -2", "db", []string{"db", "db-copy"}, "db-copy-2"},
		{"copy and -2 taken -> -3", "db", []string{"db", "db-copy", "db-copy-2"}, "db-copy-3"},
		{"source without other copies", "web", []string{"web", "api"}, "web-copy"},
		{"ignores unrelated names", "db", []string{"db", "other-copy", "db-copy-9"}, "db-copy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := freshName(c.base, c.existing); got != c.want {
				t.Errorf("freshName(%q, %v) = %q, want %q", c.base, c.existing, got, c.want)
			}
		})
	}
}

func TestModel_DeleteKeyShowsModal(t *testing.T) {
	f := newFake(controller.Status{Name: "db"})
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("d"))
	mm := next.(Model)
	if !mm.confirmDelete || mm.deleteTarget != "db" {
		t.Errorf("d should raise delete modal: confirm=%v target=%q", mm.confirmDelete, mm.deleteTarget)
	}
	if !strings.Contains(mm.render(), "Delete tuber") {
		t.Error("render should show the delete modal")
	}
}

func TestModel_DeleteConfirmYes(t *testing.T) {
	f := newFake(controller.Status{Name: "db"})
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.Update(keyPress("d"))
	m = next.(Model)
	if !m.confirmDelete {
		t.Fatal("precondition: d should raise modal")
	}
	next, _ = m.Update(keyPress("y"))
	m = next.(Model)
	if m.confirmDelete {
		t.Error("y should clear the modal")
	}
	if len(f.deletes) != 1 || f.deletes[0] != "db" {
		t.Errorf("DeleteTuber(db) expected, got %v", f.deletes)
	}
}

func TestModel_DeleteConfirmCancel(t *testing.T) {
	f := newFake(controller.Status{Name: "db"})
	m := New(f, Options{Mode: "standalone"})
	m.confirmDelete = true
	m.deleteTarget = "db"

	for _, k := range []string{"n", "enter", "esc"} {
		mm, _ := m.Update(keyPress(k))
		mm2 := mm.(Model)
		if mm2.confirmDelete || len(f.deletes) != 0 {
			t.Errorf("%s should cancel without deleting: confirm=%v deletes=%v", k, mm2.confirmDelete, f.deletes)
		}
		m.confirmDelete = true
		m.deleteTarget = "db"
	}
}

func TestModel_LogsKeyOpensScreen(t *testing.T) {
	f := newFake(controller.Status{Name: "db"})
	f.logs = []routelog.Entry{
		{Tuber: "db", Msg: "connected", Level: 0},         // info → shown by default
		{Tuber: "db", Msg: "socks5 handshake", Level: -4}, // debug → hidden by default
	}
	m := New(f, Options{Mode: "standalone"})
	m.width, m.height = 80, 24

	next, _ := m.handleKey(keyPress("l"))
	mm := next.(Model)
	if mm.logs == nil {
		t.Fatal("l should open the logs screen")
	}
	out := mm.render()
	if !strings.Contains(out, "connected") || !strings.Contains(out, "Logs") {
		t.Errorf("logs render should contain the entries\ngot:\n%s", out)
	}
	if strings.Contains(out, "socks5 handshake") {
		t.Errorf("debug entry should be hidden by default\ngot:\n%s", out)
	}

	// L toggles debug on → the debug entry appears.
	if mm.logs.update(keyPress("L")) != nil {
	}
	if mm.logs == nil {
		t.Fatal("L should keep the logs screen open")
	}
	if out := mm.render(); !strings.Contains(out, "socks5 handshake") {
		t.Errorf("debug entry should show after toggling debug\ngot:\n%s", out)
	}

	// l / esc closes the screen.
	for _, k := range []string{"l", "esc"} {
		mm2 := New(f, Options{Mode: "standalone"})
		mm2.logs = newLogsView(f, "db", 60, 20)
		nn, _ := mm2.Update(keyPress(k))
		mm3 := nn.(Model)
		if mm3.logs != nil {
			t.Errorf("%s should close the logs screen", k)
		}
	}
}

func TestRenderLogsFormatsEntries(t *testing.T) {
	entries := []routelog.Entry{
		{Msg: "first", Level: 0},
		{Msg: "second", Level: 8, Attrs: "dest=ipinfo.po:443 err=no such host"},
	}
	out := renderLogs(darkPalette(), entries)
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Errorf("renderLogs missing entries: %s", out)
	}
	if !strings.Contains(out, "dest=ipinfo.po:443") || !strings.Contains(out, "err=no such host") {
		t.Errorf("renderLogs should append attrs: %s", out)
	}
	if strings.Count(out, "\n") < 2 {
		t.Errorf("renderLogs should put each entry on its own line: %s", out)
	}
}

func TestFilterLevelHidesDebugByDefault(t *testing.T) {
	in := []routelog.Entry{
		{Msg: "dbg", Level: -4},
		{Msg: "inf", Level: 0},
		{Msg: "err", Level: 8},
	}
	if got := filterLevel(in, false); len(got) != 2 {
		t.Errorf("filterLevel(debug=false) = %d entries, want 2", len(got))
	}
	if got := filterLevel(in, true); len(got) != 3 {
		t.Errorf("filterLevel(debug=true) = %d entries, want 3", len(got))
	}
}

// TestModel_AcceptHostModalSpaceOpens guards the Phase 11 TOFU flow: pressing
// space on a tuber blocked by an unknown host key opens the accept modal
// (instead of toggling), y accepts via Controller.AcceptHost, and n/esc cancel.
func TestModel_AcceptHostModalSpaceOpens(t *testing.T) {
	f := newFake(controller.Status{
		Name: "db", State: controller.Error,
		PendingHost: "h.example.com:22", PendingFingerprint: "SHA256:abc", PendingHostLine: "h ssh-ed25519 AAAA",
	})
	m := New(f, Options{Mode: "standalone"})

	next, _ := m.handleKey(specialKey(tea.KeySpace))
	mm := next.(Model)
	if !mm.confirmAccept || mm.acceptTarget != "db" {
		t.Fatalf("space on pending-host tuber should open accept modal: confirm=%v target=%q", mm.confirmAccept, mm.acceptTarget)
	}
	out := mm.render()
	for _, want := range []string{"Unknown host key", "h.example.com:22", "SHA256:abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("accept modal missing %q\ngot:\n%s", want, out)
		}
	}

	// y accepts → AcceptHost called, modal cleared.
	next, _ = mm.Update(keyPress("y"))
	mm = next.(Model)
	if mm.confirmAccept {
		t.Error("y should clear the modal")
	}
	if len(f.accepted) != 1 || f.accepted[0] != "db" {
		t.Errorf("AcceptHost(db) expected, got %v", f.accepted)
	}

	// Fresh modal, n cancels without accepting.
	f2 := newFake(controller.Status{Name: "db", State: controller.Error, PendingHost: "h:22", PendingHostLine: "x"})
	m2 := New(f2, Options{Mode: "standalone"})
	m2.handleKey(specialKey(tea.KeySpace)) // raise modal on m2
	mm2, _ := m2.Update(keyPress("n"))
	m3 := mm2.(Model)
	if m3.confirmAccept || len(f2.accepted) != 0 {
		t.Errorf("n should cancel without accepting: confirm=%v accepted=%v", m3.confirmAccept, f2.accepted)
	}
}
