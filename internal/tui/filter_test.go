package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kipkaev55/portato/internal/controller"
)

// threeTunnels is the fixture used across filter tests: two db tunnels and one
// web tunnel, so a "db" query matches 2 of 3 and "web" matches 1.
func threeTunnels() *fakeCtrl {
	return newFake(
		controller.Status{Name: "db", Type: "local", Local: "5432", Remote: "db:5432"},
		controller.Status{Name: "web", Type: "local", Local: "8080", Remote: "web:80"},
		controller.Status{Name: "db-replica", Type: "remote", Local: "6432", Remote: "db:6432"},
	)
}

func TestFilter_SlashOpensInput(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("/"))
	mm := next.(Model)
	if !mm.filtering {
		t.Fatal("/ should open the filter input (filtering=true)")
	}
}

func TestFilter_TypingAppendsAndNarrows(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})

	// Open the input, then type "d","b".
	next, _ := m.handleKey(keyPress("/"))
	m = next.(Model)
	for _, ch := range []string{"d", "b"} {
		next, _ = m.handleFilterKey(keyPress(ch))
		m = next.(Model)
	}
	if got := m.filter.Value(); got != "db" {
		t.Fatalf("filter value after typing db = %q, want db", got)
	}
	if m.visibleCount() != 2 {
		t.Errorf("visibleCount = %d, want 2 (db + db-replica)", m.visibleCount())
	}
	out := m.render()
	if strings.Contains(out, "web") {
		t.Errorf("filtered render should hide web\ngot:\n%s", out)
	}
	if !strings.Contains(out, "db") || !strings.Contains(out, "db-replica") {
		t.Errorf("filtered render should show both db tunnels\ngot:\n%s", out)
	}
	if !strings.Contains(out, "(2/3)") {
		t.Errorf("filter line should show matched/total count\ngot:\n%s", out)
	}
}

func TestFilter_MatchesAllFieldsCaseInsensitive(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})
	cases := map[string]int{
		"":       3,
		"db":     2, // name "db" + "db-replica" (and remote host db)
		"WEB":    1, // name + endpoint, case-insensitive
		"remote": 1, // type field
		"8080":   1, // endpoint substring
		"zzz":    0,
	}
	for q, want := range cases {
		m.filter.SetValue(q)
		if got := m.visibleCount(); got != want {
			t.Errorf("visibleCount(%q) = %d, want %d", q, got, want)
		}
	}
}

func TestFilter_EscWhileTypingClearsAndCloses(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("/"))
	m = next.(Model)
	next, _ = m.handleFilterKey(keyPress("d"))
	m = next.(Model)

	next, _ = m.handleFilterKey(specialKey(tea.KeyEsc))
	m = next.(Model)
	if m.filtering {
		t.Error("esc should close the filter input")
	}
	if m.filter.Value() != "" {
		t.Errorf("esc should clear the query, got %q", m.filter.Value())
	}
	if m.visibleCount() != 3 {
		t.Errorf("esc should restore the full list, got %d", m.visibleCount())
	}
}

func TestFilter_EnterKeepsAppliedThenEscClears(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})
	next, _ := m.handleKey(keyPress("/"))
	m = next.(Model)
	next, _ = m.handleFilterKey(keyPress("w"))
	m = next.(Model) // query "w" → matches web only

	next, _ = m.handleFilterKey(keyPress("enter"))
	m = next.(Model)
	if m.filtering {
		t.Error("enter should close the input")
	}
	if m.filter.Value() != "w" {
		t.Errorf("enter should keep the query applied, got %q", m.filter.Value())
	}
	if m.visibleCount() != 1 {
		t.Errorf("query should still narrow the list, got %d", m.visibleCount())
	}

	// In the applied state, esc clears the filter (not toggles help).
	next, _ = m.handleKey(specialKey(tea.KeyEsc))
	m = next.(Model)
	if m.filter.Value() != "" || m.visibleCount() != 3 {
		t.Errorf("esc in applied state should clear the filter: value=%q visible=%d",
			m.filter.Value(), m.visibleCount())
	}
}

func TestFilter_NavigationSkipsHiddenRows(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})
	m.filter.SetValue("db") // matches index 0 (db) and 2 (db-replica)
	(&m).clampCursor()
	if m.cursor != 0 {
		t.Fatalf("cursor should snap to first match (0), got %d", m.cursor)
	}

	// j skips the hidden "web" (index 1) to "db-replica" (index 2).
	next, _ := m.handleKey(keyPress("j"))
	m = next.(Model)
	if m.cursor != 2 {
		t.Errorf("j should skip the hidden row to index 2, got %d", m.cursor)
	}
	// k skips back to "db" (index 0).
	next, _ = m.handleKey(keyPress("k"))
	m = next.(Model)
	if m.cursor != 0 {
		t.Errorf("k should skip the hidden row back to index 0, got %d", m.cursor)
	}
}

func TestFilter_SpaceTogglesVisibleSelectionOnly(t *testing.T) {
	f := threeTunnels()
	// Make web Off and the db tunnels Off so toggle enables them.
	f.statuses[0].State = controller.Off // db
	f.statuses[1].State = controller.Off // web
	f.statuses[2].State = controller.Off // db-replica

	m := New(f, Options{Mode: "standalone"})
	m.filter.SetValue("db")
	(&m).clampCursor()
	if m.cursor != 0 {
		t.Fatalf("cursor should be on db (0), got %d", m.cursor)
	}

	// Move to db-replica (index 2), toggle: must enable db-replica, not web.
	next, _ := m.handleKey(keyPress("j"))
	m = next.(Model)
	next, _ = m.handleKey(specialKey(tea.KeySpace))
	m = next.(Model)
	if len(f.enabled) != 1 || f.enabled[0] != "db-replica" {
		t.Errorf("space should toggle the visible selection (db-replica), got %v", f.enabled)
	}
}

func TestFilter_NoMatchesRender(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})
	m.filter.SetValue("zzz")
	out := m.render()
	if !strings.Contains(out, "no tunnels match") {
		t.Errorf("render should show the no-match placeholder\ngot:\n%s", out)
	}
}

func TestFilter_SurvivesRedrawTick(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "standalone"})
	m.filter.SetValue("db")
	m.filtering = false // applied state
	before := m.visibleCount()

	next, _ := m.Update(redrawTickMsg{})
	mm := next.(Model)
	if mm.filter.Value() != "db" {
		t.Errorf("filter should survive a redraw tick, got value=%q", mm.filter.Value())
	}
	if mm.visibleCount() != before {
		t.Errorf("visible count changed across redraw tick: %d -> %d", before, mm.visibleCount())
	}
}

func TestFilter_WorksInAttachMode(t *testing.T) {
	m := New(threeTunnels(), Options{Mode: "attach @ /sock"})
	if !m.attach {
		t.Fatal("precondition: should be in attach mode")
	}
	m.filter.SetValue("web")
	if m.visibleCount() != 1 {
		t.Errorf("filter should narrow in attach mode too, got %d", m.visibleCount())
	}
}
