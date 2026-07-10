package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/portuber/portato/internal/config"
)

func newEditorFake() *fakeCtrl {
	f := newFake()
	f.cfg = &config.Config{
		Tubers: []config.Tuber{
			{Name: "db", Type: "local", Local: "5432", Remote: "db:5432", SSH: "u@h:22"},
		},
	}
	return f
}

func editorForEdit(f *fakeCtrl) *tuberEditor {
	e := newTuberEditor(modeEdit, f.cfg.Tubers[0], []string{"db"}, f)
	e.setFocus(fName)
	return e
}

func editorForNew(f *fakeCtrl) *tuberEditor {
	e := newTuberEditor(modeNew, config.Tuber{}, []string{"db"}, f)
	e.setFocus(fName)
	return e
}

func TestEditor_PrefillsFromExisting(t *testing.T) {
	f := newEditorFake()
	e := editorForEdit(f)
	if e.name.Value() != "db" || e.ssh.Value() != "u@h:22" || e.local.Value() != "5432" {
		t.Errorf("editor not prefilled: name=%q ssh=%q local=%q", e.name.Value(), e.ssh.Value(), e.local.Value())
	}
	if tuberTypes[e.typeIdx] != "local" {
		t.Errorf("type idx = %d (%s), want local", e.typeIdx, tuberTypes[e.typeIdx])
	}
}

func TestEditor_Validate_RequiredFields(t *testing.T) {
	f := newEditorFake()
	e := newTuberEditor(modeNew, config.Tuber{}, []string{}, f)

	errs := e.validate()
	if _, ok := errs["name"]; !ok {
		t.Error("empty name should be flagged")
	}
	if _, ok := errs["ssh"]; !ok {
		t.Error("empty ssh should be flagged")
	}
	if _, ok := errs["local"]; !ok {
		t.Error("empty local should be flagged (required for all types)")
	}
}

func TestEditor_Validate_BadName(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.name.SetValue("bad name!")
	if errs := e.validate(); errs["name"] == "" {
		t.Error("name with space/! should be invalid")
	}
}

func TestEditor_Validate_DuplicateName(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.name.SetValue("db") // already exists
	e.ssh.SetValue("u@h:22")
	e.remote.SetValue("x:1")
	if errs := e.validate(); errs["name"] == "" {
		t.Error("duplicate name should be flagged for new tuber")
	}
}

func TestEditor_Validate_EditKeepsOwnName(t *testing.T) {
	f := newEditorFake()
	e := editorForEdit(f) // editing "db", name stays "db"
	errs := e.validate()
	if errs["name"] != "" {
		t.Errorf("keeping own name on edit should be valid, got %q", errs["name"])
	}
}

func TestEditor_Validate_DynamicRequiresLocal(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.name.SetValue("dyn")
	e.ssh.SetValue("u@h:22")
	e.typeIdx = 2 // dynamic
	e.local.SetValue("")
	if errs := e.validate(); errs["local"] == "" {
		t.Error("dynamic with empty local should be flagged")
	}
	e.local.SetValue("1080")
	if errs := e.validate(); errs["local"] != "" {
		t.Error("dynamic with local should be valid")
	}
}

func TestEditor_Validate_NonDynamicRequiresRemote(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.name.SetValue("loc")
	e.ssh.SetValue("u@h:22")
	e.local.SetValue("8080")
	e.remote.SetValue("")
	if errs := e.validate(); errs["remote"] == "" {
		t.Error("local type with empty remote should be flagged")
	}
}

func TestEditor_Validate_LocalRequiredForAllTypes(t *testing.T) {
	f := newEditorFake()
	for _, ty := range tuberTypes {
		e := editorForNew(f)
		e.name.SetValue("x")
		e.ssh.SetValue("u@h:22")
		e.typeIdx = indexOf(tuberTypes, ty)
		e.local.SetValue("")
		e.remote.SetValue("r:1") // valid for non-dynamic; unused for dynamic
		if errs := e.validate(); errs["local"] == "" {
			t.Errorf("type %s: empty local should be flagged", ty)
		}
		e.local.SetValue("1080")
		if errs := e.validate(); errs["local"] != "" {
			t.Errorf("type %s: local set should clear the error (got %q)", ty, errs["local"])
		}
	}
}

func TestEditor_SaveNew_CallsAddTuber(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.name.SetValue("web")
	e.ssh.SetValue("u@h:22")
	e.remote.SetValue("web:80")
	e.local.SetValue("8080")

	e.handleKey(keyPress("ctrl+s"))

	if !e.done || !e.saved {
		t.Fatalf("expected saved+done, got done=%v saved=%v status=%q", e.done, e.saved, e.status)
	}
	if len(f.adds) != 1 || f.adds[0].Name != "web" {
		t.Errorf("AddTuber not called correctly: %+v", f.adds)
	}
}

func TestEditor_SaveEdit_CallsUpdateTuber(t *testing.T) {
	f := newEditorFake()
	e := editorForEdit(f) // editing "db"
	e.local.SetValue("9999")

	e.handleKey(keyPress("ctrl+s"))

	if !e.done || !e.saved {
		t.Fatalf("expected saved+done, status=%q errs=%v", e.status, e.errs)
	}
	if len(f.updates) != 1 || f.updates[0].Local != "9999" {
		t.Errorf("UpdateTuber not called correctly: %+v", f.updates)
	}
}

func TestEditor_SaveInvalid_DoesNotSave(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.name.SetValue("") // invalid
	e.handleKey(keyPress("ctrl+s"))
	if e.saved || e.done {
		t.Error("invalid save should not mark saved/done")
	}
	if len(f.adds) != 0 {
		t.Errorf("AddTuber should not be called on invalid, got %+v", f.adds)
	}
}

func TestEditor_SaveServerError_StaysOpen(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.name.SetValue("web")
	e.ssh.SetValue("u@h:22")
	e.remote.SetValue("web:80")
	e.local.SetValue("8080")

	// Force the controller to reject the add (e.g. server-side duplicate).
	f.tunErr = assertErr("boom")
	e.handleKey(keyPress("ctrl+s"))

	if e.done || e.saved {
		t.Error("on controller error the editor should stay open")
	}
	if e.status == "" {
		t.Error("status should carry the error message")
	}
}

func TestEditor_EscCancels(t *testing.T) {
	f := newEditorFake()
	e := editorForEdit(f)
	e.handleKey(specialKey(tea.KeyEsc))
	if !e.done || e.saved {
		t.Errorf("esc should cancel without saving: done=%v saved=%v", e.done, e.saved)
	}
	if len(f.updates) != 0 {
		t.Errorf("no update on cancel, got %+v", f.updates)
	}
}

func TestEditor_TypeCycling(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.focus = fType
	start := e.typeIdx

	e.handleKey(keyPress("right"))
	if e.typeIdx != (start+1)%len(tuberTypes) {
		t.Errorf("right: typeIdx=%d want %d", e.typeIdx, (start+1)%len(tuberTypes))
	}
	e.handleKey(keyPress("left"))
	if e.typeIdx != start {
		t.Errorf("left should revert: typeIdx=%d want %d", e.typeIdx, start)
	}
}

func TestEditor_TypePlaceholdersAreContextual(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)

	// remote type: remote is a bare port or host:port on the host side.
	e.typeIdx = indexOf(tuberTypes, "remote")
	e.applyTypePlaceholders()
	if e.remote.Placeholder != "9090 or 0.0.0.0:9090" {
		t.Errorf("remote placeholder = %q", e.remote.Placeholder)
	}

	// dynamic type: remote is unused.
	e.typeIdx = indexOf(tuberTypes, "dynamic")
	e.applyTypePlaceholders()
	if e.remote.Placeholder != "unused" {
		t.Errorf("dynamic remote placeholder = %q", e.remote.Placeholder)
	}

	// cycling updates the placeholder too.
	e.typeIdx = indexOf(tuberTypes, "local")
	e.cycleType(1) // -> remote
	if tuberTypes[e.typeIdx] != "remote" || e.remote.Placeholder != "9090 or 0.0.0.0:9090" {
		t.Errorf("cycle to remote: type=%s placeholder=%q", tuberTypes[e.typeIdx], e.remote.Placeholder)
	}
}

func TestEditor_TypeNoteNonEmpty(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	for _, ty := range tuberTypes {
		e.typeIdx = indexOf(tuberTypes, ty)
		if e.typeNote() == "" {
			t.Errorf("type %s has empty note", ty)
		}
	}
}

func TestEditor_TabCyclesFocus(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	if e.focus != fName {
		t.Fatalf("initial focus = %d, want %d", e.focus, fName)
	}
	e.handleKey(keyPress("tab"))
	if e.focus != fType {
		t.Errorf("after tab focus=%d, want %d", e.focus, fType)
	}
}

func TestEditor_PasteInsertsIntoFocusedField(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f) // focus on Name
	e.name.SetValue("x")
	e.update(tea.PasteMsg{Content: "pasted"})
	if got := e.name.Value(); got != "xpasted" {
		t.Errorf("paste into focused Name: got %q, want %q", got, "xpasted")
	}
	// Other fields are untouched.
	if e.ssh.Value() != "" {
		t.Errorf("non-focused SSH field changed: %q", e.ssh.Value())
	}
}

func TestEditor_PasteOnTypeFieldIsNoOp(t *testing.T) {
	f := newEditorFake()
	e := editorForNew(f)
	e.focus = fType // no textinput behind the Type field
	e.update(tea.PasteMsg{Content: "pasted"})
	if e.name.Value() != "" {
		t.Errorf("paste on Type field should not touch Name, got %q", e.name.Value())
	}
}

// assertErr is a tiny error used to flip fakeCtrl into its error mode for the
// Add/Update/Delete paths.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func assertErr(s string) error { return errSentinel(s) }
