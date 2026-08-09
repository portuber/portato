package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/forward"
)

// TestPrintTable_Tags covers the Phase 46 list-table display: tags render as
// "#tag" tokens appended to the NAME cell (no new column), and a tuber with no
// tags is unchanged.
func TestPrintTable_Tags(t *testing.T) {
	statuses := []forward.Status{
		{Name: "db-prod", Type: "local", Local: "5432", Remote: "db:5432", State: forward.Off, Tags: []string{"prod", "db"}},
		{Name: "web", Type: "local", Local: "8080", Remote: "web:80", State: forward.Off},
	}
	var out bytes.Buffer
	printTable(&out, statuses)
	got := out.String()
	if !strings.Contains(got, "db-prod #prod #db") {
		t.Errorf("tagged tuber: expected NAME cell to contain %q, got:\n%s", "db-prod #prod #db", got)
	}
	if strings.Contains(got, "web #") {
		t.Errorf("untagged tuber: NAME cell should have no # tokens, got:\n%s", got)
	}
}

// TestPrintJSON_Tags confirms `list --json` carries the tags array end-to-end
// (the Status.Tags field flows config -> IPC -> here).
func TestPrintJSON_Tags(t *testing.T) {
	statuses := []forward.Status{
		{Name: "db-prod", Type: "local", Local: "5432", Remote: "db:5432", State: forward.Off, Tags: []string{"prod", "db"}},
	}
	var out bytes.Buffer
	if err := printJSON(&out, statuses); err != nil {
		t.Fatalf("printJSON: %v", err)
	}
	var dec []forward.Status
	if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(dec[0].Tags) != 2 || dec[0].Tags[0] != "prod" || dec[0].Tags[1] != "db" {
		t.Errorf("tags round-trip = %v, want [prod db]", dec[0].Tags)
	}
}
