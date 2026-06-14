package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	m := New(f, "standalone")

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
	m := New(f, "standalone")

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
	m := New(f, "standalone")

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
	m := New(f, "standalone")
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
	)
	m := New(f, "standalone")
	m.width = 100

	out := m.render()
	for _, want := range []string{"Portato", "mode: standalone", "alpha", "beta", "5432 → db:5432"} {
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
	m := New(f, "standalone")
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
