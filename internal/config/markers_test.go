package config

import (
	"os"
	"path/filepath"
	"testing"
)

func redirectMarkers(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PORTATO_STATE_HOME", dir)
	return dir
}

func TestMarkers_RoundTrip(t *testing.T) {
	redirectMarkers(t)
	if FreshInstall() || ImportOffered() {
		t.Fatal("fresh state dir must start with no markers")
	}
	MarkFreshInstall()
	if !FreshInstall() {
		t.Error("FreshInstall after MarkFreshInstall")
	}
	if ImportOffered() {
		t.Error("import offer must not be consumed by the fresh marker")
	}
	MarkImportOffered()
	if !ImportOffered() {
		t.Error("ImportOffered after MarkImportOffered")
	}
}

func TestMarkers_EmptyRegularFiles(t *testing.T) {
	dir := redirectMarkers(t)
	MarkFreshInstall()
	MarkImportOffered()
	for _, name := range []string{"fresh_install", "import_offered"} {
		fi, err := os.Stat(filepath.Join(dir, "portato", name))
		if err != nil {
			t.Fatalf("marker %s: %v", name, err)
		}
		if fi.IsDir() {
			t.Errorf("marker %s must be a regular file", name)
		}
		if fi.Size() != 0 {
			t.Errorf("marker %s must be empty", name)
		}
	}
}

func TestEnsureExample_WritesFreshMarkerOnCreate(t *testing.T) {
	redirectMarkers(t)
	p := filepath.Join(t.TempDir(), "config.yaml")

	created, err := EnsureExample(p)
	if err != nil || !created {
		t.Fatalf("EnsureExample (create): created=%v err=%v", created, err)
	}
	if !FreshInstall() {
		t.Error("config creation must set the fresh_install marker")
	}

	// An existing config (the upgrading user) is left alone: no new marker
	// semantics change — FreshInstall stays as it was, ImportOffered never
	// appears on mere loads.
	created2, err := EnsureExample(p)
	if err != nil || created2 {
		t.Fatalf("EnsureExample (exist): created=%v err=%v", created2, err)
	}
	if ImportOffered() {
		t.Error("loads must never consume the import offer")
	}
}

// The daemon-first flow: a non-interactive run creates the config (fresh
// marker set) but must not consume the offer; the markers API keeps the two
// independent, asserted end-to-end here.
func TestMarkers_DaemonFirstDoesNotConsumeOffer(t *testing.T) {
	redirectMarkers(t)
	p := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := EnsureExample(p); err != nil {
		t.Fatal(err)
	}
	if !FreshInstall() || ImportOffered() {
		t.Fatal("after daemon-first bootstrap: fresh must be set, offered must not")
	}
}
