package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/forward"
)

// TestPrintJSON covers the Phase 20 `portato list --json` path: the output is
// valid JSON for 0/1/N tubers, a nil slice renders as `[]` (not `null`), and
// the first element's name round-trips so `jq '.[0].name'` works.
func TestPrintJSON(t *testing.T) {
	t.Run("nil renders as empty array", func(t *testing.T) {
		var out bytes.Buffer
		if err := printJSON(&out, nil); err != nil {
			t.Fatalf("printJSON: %v", err)
		}
		trim := strings.TrimSpace(out.String())
		if trim != "[]" {
			t.Errorf("nil slice: got %q, want []", trim)
		}
		// Must decode as a non-null array.
		var dec []forward.Status
		if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
			t.Errorf("nil slice: invalid JSON: %v", err)
		}
	})

	t.Run("one and many", func(t *testing.T) {
		statuses := []forward.Status{
			{Name: "db-stage", Type: "local", Local: "127.0.0.1:5432", Remote: "10.0.0.5:5432", State: forward.Connected},
			{Name: "web", Type: "dynamic", Local: "127.0.0.1:1080", State: forward.Off},
		}
		var out bytes.Buffer
		if err := printJSON(&out, statuses); err != nil {
			t.Fatalf("printJSON: %v", err)
		}
		var dec []forward.Status
		if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out.String())
		}
		if len(dec) != 2 {
			t.Fatalf("got %d entries, want 2", len(dec))
		}
		if dec[0].Name != "db-stage" {
			t.Errorf("first name = %q, want db-stage", dec[0].Name)
		}
		if dec[1].Name != "web" {
			t.Errorf("second name = %q, want web", dec[1].Name)
		}
	})

	t.Run("empty non-nil renders as empty array", func(t *testing.T) {
		var out bytes.Buffer
		if err := printJSON(&out, []forward.Status{}); err != nil {
			t.Fatalf("printJSON: %v", err)
		}
		if strings.TrimSpace(out.String()) != "[]" {
			t.Errorf("empty slice: got %q, want []", out.String())
		}
	})
}
