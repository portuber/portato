package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kipkaev55/portato/internal/controller"
)

type fakeCtrl struct {
	mu        sync.Mutex
	statuses  []controller.Status
	enabled   []string
	disabled  []string
	restarted []string
	reloads   int
	changes   chan struct{}
}

func (f *fakeCtrl) List() []controller.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	if m.help {
		t.Fatal("help should start hidden")
	}
	next, _ := m.handleKey(keyPress("?"))
	m = next.(Model)
	if !m.help {
		t.Error("? should toggle help on")
	}
	next, _ = m.handleKey(specialKey(tea.KeyEsc))
	m = next.(Model)
	if m.help {
		t.Error("esc should toggle help off")
	}

	_, cmd := m.handleKey(keyPress("q"))
	if cmd == nil {
		t.Error("q should return a quit command")
	}
}

func TestModel_RenderContainsTunnels(t *testing.T) {
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

	m.help = true
	if !strings.Contains(m.render(), "move cursor up") {
		t.Error("render should show help block when help=true")
	}
}

func TestModel_EmptyList(t *testing.T) {
	f := newFake()
	m := New(f, Options{Mode: "standalone"})
	m2, _ := m.handleKey(keyPress("space"))
	if mm, ok := m2.(Model); !ok || mm.cursor != 0 {
		t.Error("space on empty list should be a no-op")
	}
	if !strings.Contains(m.render(), "no tunnels") {
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

func TestFitEndpoint(t *testing.T) {
	const max = colEndpoint // 32
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
			got := fitEndpoint(tc.in, max)
			if tc.unchanged {
				if got != tc.in {
					t.Errorf("expected unchanged %q, got %q", tc.in, got)
				}
				return
			}
			if w := lipgloss.Width(got); w > max {
				t.Errorf("result width %d > max %d: %q", w, max, got)
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

func TestModel_QuitStandaloneLiveShowsModal(t *testing.T) {
	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "standalone", CfgPath: "/cfg", SocketPath: "/sock"})

	next, cmd := m.handleKey(keyPress("q"))
	m = next.(Model)
	if m.confirmQuit {
		// expected
	} else {
		t.Fatal("standalone q with live tunnels should raise confirm modal")
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
		t.Error("standalone q with no live tunnels should quit immediately")
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
	startCmd = func(string) error { return nil }
	probeSocket = func(string) bool { return true }

	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "standalone", CfgPath: "/cfg", SocketPath: "/sock"})
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
	m2 := New(f, Options{Mode: "standalone", CfgPath: "/cfg", SocketPath: "/sock"})
	m2.list = []controller.Status{{Name: "a", State: controller.Connected}}
	m2.confirmQuit = true
	next, cmd = m2.handleKey(keyPress("n"))
	mm := next.(Model)
	if !mm.quit || mm.confirmQuit || cmd == nil {
		t.Errorf("n: quit=%v confirmQuit=%v cmd=%v", mm.quit, mm.confirmQuit, cmd)
	}

	// enter declines (same as n): stop + exit
	mEnter := New(f, Options{Mode: "standalone", CfgPath: "/cfg", SocketPath: "/sock"})
	mEnter.list = []controller.Status{{Name: "a", State: controller.Connected}}
	mEnter.confirmQuit = true
	next, cmd = mEnter.handleKey(keyPress("enter"))
	mm = next.(Model)
	if !mm.quit || mm.confirmQuit || cmd == nil {
		t.Errorf("enter: quit=%v confirmQuit=%v cmd=%v", mm.quit, mm.confirmQuit, cmd)
	}

	// esc cancels the modal: back to the list, no quit, tunnels untouched
	mEsc := New(f, Options{Mode: "standalone", CfgPath: "/cfg", SocketPath: "/sock"})
	mEsc.list = []controller.Status{{Name: "a", State: controller.Connected}}
	mEsc.confirmQuit = true
	next, cmd = mEsc.handleKey(keyPress("esc"))
	mm = next.(Model)
	if mm.quit || mm.confirmQuit || cmd != nil {
		t.Errorf("esc: want cancel (quit=false confirmQuit=false cmd=nil), got quit=%v confirmQuit=%v cmd=%v", mm.quit, mm.confirmQuit, cmd)
	}
}

func TestModel_TickIgnoredDuringHandoff(t *testing.T) {
	f := newFake(controller.Status{Name: "a", State: controller.Connected})
	m := New(f, Options{Mode: "standalone", CfgPath: "/cfg", SocketPath: "/sock"})
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
