package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/portuber/portato/internal/controller"
)

// shiftKey builds a shift+<arrow> key press, which bubbletea v2 reports as
// "shift+up" / "shift+down".
func shiftKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: tea.ModShift} }

func namesOf(list []controller.Status) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

func TestModel_Reorder_MoveDown(t *testing.T) {
	f := newFake(
		controller.Status{Name: "a"},
		controller.Status{Name: "b"},
		controller.Status{Name: "c"},
	)
	m := New(f, Options{Mode: "standalone"})
	// cursor on "a" (index 0); shift+down swaps it with "b".
	next, _ := m.handleKey(shiftKey(tea.KeyDown))
	m = next.(Model)

	if got := namesOf(m.list); got[0] != "b" || got[1] != "a" || got[2] != "c" {
		t.Fatalf("order after shift+down = %v", got)
	}
	if m.cursor != 1 {
		t.Errorf("cursor should follow the moved tuber to index 1, got %d", m.cursor)
	}
	if len(f.moves) != 1 || f.moves[0].name != "a" || f.moves[0].delta != 1 {
		t.Errorf("fake moves = %+v", f.moves)
	}
}

func TestModel_Reorder_MoveUp(t *testing.T) {
	f := newFake(
		controller.Status{Name: "a"},
		controller.Status{Name: "b"},
		controller.Status{Name: "c"},
	)
	m := New(f, Options{Mode: "standalone"})
	m.cursor = 2 // on "c"
	next, _ := m.handleKey(shiftKey(tea.KeyUp))
	m = next.(Model)

	if got := namesOf(m.list); got[0] != "a" || got[1] != "c" || got[2] != "b" {
		t.Fatalf("order after shift+up = %v", got)
	}
	if m.cursor != 1 {
		t.Errorf("cursor should follow the moved tuber to index 1, got %d", m.cursor)
	}
}

func TestModel_Reorder_AtBoundsNoOp(t *testing.T) {
	f := newFake(
		controller.Status{Name: "a"},
		controller.Status{Name: "b"},
	)
	m := New(f, Options{Mode: "standalone"})
	// "a" is first: shift+up is a no-op.
	next, _ := m.handleKey(shiftKey(tea.KeyUp))
	m = next.(Model)
	if got := namesOf(m.list); got[0] != "a" || got[1] != "b" {
		t.Errorf("bounds up changed order: %v", got)
	}
	if m.cursor != 0 {
		t.Errorf("cursor moved on bounds no-op: %d", m.cursor)
	}

	// "b" is last: move to it and shift+down is a no-op.
	m.cursor = 1
	next, _ = m.handleKey(shiftKey(tea.KeyDown))
	m = next.(Model)
	if got := namesOf(m.list); got[0] != "a" || got[1] != "b" {
		t.Errorf("bounds down changed order: %v", got)
	}
	// The TUI forwards every reorder to the controller; the boundary no-op is
	// the controller's job (see config.MoveTuberNode), so the observable here is
	// the unchanged list above, not the call count.
}

func TestModel_Reorder_FilterActiveNoOp(t *testing.T) {
	f := newFake(
		controller.Status{Name: "a"},
		controller.Status{Name: "b"},
	)
	m := New(f, Options{Mode: "standalone"})
	m.filter.SetValue("a") // narrow the list to "a"

	next, _ := m.handleKey(shiftKey(tea.KeyDown))
	m = next.(Model)

	if got := namesOf(m.list); got[0] != "a" || got[1] != "b" {
		t.Errorf("reorder acted under a filter: %v", got)
	}
	if len(f.moves) != 0 {
		t.Errorf("expected no controller calls under filter, got %+v", f.moves)
	}
}

func TestModel_Reorder_EmptyListNoOp(t *testing.T) {
	f := newFake()
	m := New(f, Options{Mode: "standalone"})
	next, _ := m.handleKey(shiftKey(tea.KeyDown))
	m = next.(Model)
	if len(m.list) != 0 || m.cursor != 0 {
		t.Errorf("empty-list reorder should be a no-op: list=%v cursor=%d", m.list, m.cursor)
	}
}
