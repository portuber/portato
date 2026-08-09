package forward

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/config"
)

// TestTuberStatusCarriesTags confirms Tags flow config.Tuber -> Tuber.Status()
// (the single construction site the daemon's IPC layer reads via Engine.List).
func TestTuberStatusCarriesTags(t *testing.T) {
	cfg := config.Tuber{
		Name:   "db-prod",
		Type:   "local",
		Local:  "5432",
		Remote: "10.0.0.5:5432",
		SSH:    "user@host:22",
		Tags:   []string{"prod", "db"},
	}
	tuber := NewTuber(context.Background(), cfg, config.Defaults{}, slog.Default(), nil, nil)
	got := tuber.Status().Tags
	if len(got) != 2 || got[0] != "prod" || got[1] != "db" {
		t.Errorf("Status().Tags = %v, want [prod db]", got)
	}
}

// TestTuberStatusNoTagsOmitsJSON confirms an empty Tags slice renders no tags
// key over IPC (the omitempty contract — non-tag-aware consumers are unaffected).
func TestTuberStatusNoTagsOmitsJSON(t *testing.T) {
	s := Status{Name: "a", Type: "local", Local: "1", Remote: "r", State: Off}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tags") {
		t.Errorf("empty Tags should be omitted, got %s", data)
	}
}
