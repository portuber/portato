package tui

import (
	"strings"
	"testing"

	"github.com/portuber/portato/internal/controller"
)

// TestRender_TagsDetailStrip covers Phase 46: a selected row with tags (and no
// error) shows a "#tag" strip above the footer. The strip is ≤1 line in every
// case and reuses the Phase 39 ↳ prefix.
func TestRender_TagsDetailStrip(t *testing.T) {
	f := newFake(controller.Status{Name: "db-prod", Type: "local", Local: "1", Remote: "r", State: controller.Connected, Tags: []string{"prod", "db"}})
	m := New(f, Options{Mode: "standalone"})
	m.width, m.height = 100, 24
	out := m.render()
	if !strings.Contains(out, "↳") {
		t.Errorf("tags strip should use the ↳ prefix\ngot:\n%s", out)
	}
	if !strings.Contains(out, "#prod #db") {
		t.Errorf("tags strip should render #prod #db\ngot:\n%s", out)
	}
}

// TestRender_TagsStripNoTags: an untagged, non-erroring selected row shows no
// strip (regression: the pre-Phase-46 empty strip is preserved).
func TestRender_TagsStripNoTags(t *testing.T) {
	f := newFake(controller.Status{Name: "x", Type: "local", Local: "1", Remote: "r", State: controller.Connected})
	m := New(f, Options{Mode: "standalone"})
	m.width, m.height = 100, 24
	if strings.Contains(m.render(), "↳") {
		t.Errorf("no strip expected for an untagged non-erroring row\ngot:\n%s", m.render())
	}
}

// TestRender_ErrorWinsOverTags: when the selected row has BOTH an error and
// tags, the error strip wins and the tags strip is suppressed — the strip stays
// ≤1 line (no 2-line jitter), and error is the actionable detail (Phase 39 F13).
func TestRender_ErrorWinsOverTags(t *testing.T) {
	fullErr := "listen tcp 127.0.0.1:3306: bind: address already in use"
	f := newFake(controller.Status{
		Name: "db", Type: "local", Local: "1", Remote: "r",
		State: controller.Error, Error: fullErr, Tags: []string{"prod", "db"},
	})
	m := New(f, Options{Mode: "standalone"})
	m.width, m.height = 100, 24
	out := m.render()
	if !strings.Contains(out, fullErr) {
		t.Errorf("error strip should be shown (error wins)\ngot:\n%s", out)
	}
	// The tags line must NOT also appear (no 2-line stacking). Count ↳ arrows.
	if c := strings.Count(out, "↳"); c != 1 {
		t.Errorf("exactly one strip line expected (error wins, no tags line), got %d ↳ arrows\ngot:\n%s", c, out)
	}
}
