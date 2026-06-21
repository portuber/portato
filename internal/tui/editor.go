package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/controller"
)

// tunnelEditor is the Phase 10 form for creating/editing a tunnel. It is the
// first sub-model in the TUI: the main Model holds a *tunnelEditor (nil when
// inactive) and routes keys to it while it is open.
//
// The form edits the persistent fields only (name, type, ssh, local, remote,
// identity). Enabled is carried through unchanged from the edited tunnel (or
// false for a new one) — it stays controlled by the space toggle in the list.
// Passwords are never in the form: authentication is agent/identity only.
type tunnelEditor struct {
	mode     editorMode
	original string // name being edited ("" for new); used for rename + uniqueness
	enabled  bool   // preserved from the edited tunnel

	name     textinput.Model
	ssh      textinput.Model
	local    textinput.Model
	remote   textinput.Model
	identity textinput.Model

	typeIdx int // index into tunnelTypes

	focus  int
	width  int
	height int

	existing []string // current tunnel names, for uniqueness
	ctrl     controller.Controller

	errs   map[string]string
	status string

	saved bool
	done  bool
}

type editorMode int

const (
	modeEdit editorMode = iota
	modeNew
)

var tunnelTypes = []string{"local", "remote", "dynamic"}

const (
	fName = iota
	fType
	fSSH
	fLocal
	fRemote
	fIdentity
	fieldCount
)

// newTunnelEditor builds the form. For modeEdit, t is the current tunnel (its
// Enabled is preserved); existing lists the current names for uniqueness.
func newTunnelEditor(mode editorMode, t config.Tunnel, existing []string, ctrl controller.Controller) *tunnelEditor {
	e := &tunnelEditor{
		mode:     mode,
		original: t.Name,
		enabled:  t.Enabled,
		existing: existing,
		ctrl:     ctrl,
		focus:    fName,
		errs:     map[string]string{},
	}
	e.name = newInput(t.Name, "my-tunnel")
	e.ssh = newInput(t.SSH, "user@host:22")
	e.local = newInput(t.Local, "5432 or 127.0.0.1:5432")
	e.remote = newInput(t.Remote, "db:5432")
	e.identity = newInput(t.Identity, "~/.ssh/id_ed25519 (optional)")

	e.typeIdx = 0
	for i, ty := range tunnelTypes {
		if ty == t.Type {
			e.typeIdx = i
			break
		}
	}
	e.applyTypePlaceholders()
	return e
}

// applyTypePlaceholders tunes the remote/local hints to the selected type so
// the form reflects each type's semantics (e.g. a -R remote may be a bare
// port that binds loopback on the host).
func (e *tunnelEditor) applyTypePlaceholders() {
	switch tunnelTypes[e.typeIdx] {
	case "remote":
		e.remote.Placeholder = "9090 or 0.0.0.0:9090"
		e.local.Placeholder = "127.0.0.1:9090"
	case "local":
		e.remote.Placeholder = "db:5432"
		e.local.Placeholder = "5432 or 127.0.0.1:5432"
	case "dynamic":
		e.local.Placeholder = "1080 or 127.0.0.1:1080"
		e.remote.Placeholder = "unused"
	}
}

// typeNote is the one-line semantics hint shown under the Type field.
func (e *tunnelEditor) typeNote() string {
	switch tunnelTypes[e.typeIdx] {
	case "local":
		return "local: listened here · remote: destination dialed on the host"
	case "remote":
		return "remote: listened on the host — bare port binds loopback; non-loopback needs GatewayPorts"
	case "dynamic":
		return "local: SOCKS5 proxy here · remote unused (destination from the SOCKS request)"
	}
	return ""
}

func newInput(value, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = 256
	ti.SetWidth(40)
	ti.SetValue(value)
	return ti
}

// update mutates the editor in place and returns any command (e.g. cursor
// blink from focusing a text field).
func (e *tunnelEditor) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width, e.height = msg.Width, msg.Height
		return nil
	case tea.KeyPressMsg:
		return e.handleKey(msg)
	case tea.PasteMsg:
		// Forward bracketed-paste to the focused text field (textinput inserts
		// the content at the cursor). No-op when a non-text field (Type) is
		// focused.
		if ti := e.textInputFor(e.focus); ti != nil {
			var cmd tea.Cmd
			*ti, cmd = ti.Update(msg)
			return cmd
		}
		return nil
	}
	return nil
}

func (e *tunnelEditor) handleKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+s":
		e.trySave()
		return nil
	case "esc":
		e.done = true
		return nil
	case "tab", "enter":
		next := e.focus + 1
		if next >= fieldCount {
			next = 0
		}
		return e.setFocus(next)
	case "shift+tab":
		prev := e.focus - 1
		if prev < 0 {
			prev = fieldCount - 1
		}
		return e.setFocus(prev)
	case "left", "right":
		if e.focus == fType {
			if k.String() == "left" {
				e.cycleType(-1)
			} else {
				e.cycleType(1)
			}
			return nil
		}
	}
	if ti := e.textInputFor(e.focus); ti != nil {
		var cmd tea.Cmd
		*ti, cmd = ti.Update(k)
		return cmd
	}
	return nil
}

func (e *tunnelEditor) textInputFor(idx int) *textinput.Model {
	switch idx {
	case fName:
		return &e.name
	case fSSH:
		return &e.ssh
	case fLocal:
		return &e.local
	case fRemote:
		return &e.remote
	case fIdentity:
		return &e.identity
	}
	return nil
}

func (e *tunnelEditor) setFocus(idx int) tea.Cmd {
	if ti := e.textInputFor(e.focus); ti != nil {
		ti.Blur()
	}
	e.focus = idx
	if ti := e.textInputFor(idx); ti != nil {
		return ti.Focus()
	}
	return nil
}

func (e *tunnelEditor) cycleType(dir int) {
	e.typeIdx = (e.typeIdx + dir + len(tunnelTypes)) % len(tunnelTypes)
	e.applyTypePlaceholders()
}

func (e *tunnelEditor) tunnel() config.Tunnel {
	return config.Tunnel{
		Name:     e.name.Value(),
		Type:     tunnelTypes[e.typeIdx],
		SSH:      e.ssh.Value(),
		Local:    e.local.Value(),
		Remote:   e.remote.Value(),
		Identity: e.identity.Value(),
		Enabled:  e.enabled,
	}
}

// validate mirrors config.Validate per field so the form can highlight invalid
// inputs before calling the controller. The controller/daemon validate again
// (defence in depth).
func (e *tunnelEditor) validate() map[string]string {
	errs := map[string]string{}
	t := e.tunnel()

	name := strings.TrimSpace(t.Name)
	switch {
	case name == "":
		errs["name"] = "required"
	case !validEditorName(name):
		errs["name"] = "letters, digits, - or _ only"
	default:
		for _, n := range e.existing {
			if n == name && !(e.mode == modeEdit && n == e.original) {
				errs["name"] = "already exists"
				break
			}
		}
	}

	if strings.TrimSpace(t.Local) == "" {
		errs["local"] = "required"
	}
	if strings.TrimSpace(t.SSH) == "" {
		errs["ssh"] = "required"
	}
	if t.Type != "dynamic" && strings.TrimSpace(t.Remote) == "" {
		errs["remote"] = "required for " + t.Type
	}
	return errs
}

func (e *tunnelEditor) trySave() {
	e.errs = e.validate()
	if len(e.errs) > 0 {
		e.status = "fix the highlighted fields"
		return
	}
	e.errs = map[string]string{}
	t := e.tunnel()
	var err error
	if e.mode == modeNew {
		err = e.ctrl.AddTunnel(t)
	} else {
		err = e.ctrl.UpdateTunnel(e.original, t)
	}
	if err != nil {
		e.status = "error: " + err.Error()
		return
	}
	e.saved = true
	e.done = true
}

func (e *tunnelEditor) view() string {
	var b strings.Builder
	title := "New tunnel"
	if e.mode == modeEdit {
		title = "Edit tunnel: " + e.original
	}
	b.WriteString(editorTitleStyle.Render(title))
	b.WriteString("\n\n")

	b.WriteString(e.renderText("Name", &e.name, fName, "name"))
	b.WriteString(e.renderType())
	b.WriteString("          " + dimStyle.Render(e.typeNote()) + "\n")
	b.WriteString(e.renderText("SSH", &e.ssh, fSSH, "ssh"))
	b.WriteString(e.renderText("Local", &e.local, fLocal, "local"))
	b.WriteString(e.renderText("Remote", &e.remote, fRemote, "remote"))
	b.WriteString(e.renderText("Identity", &e.identity, fIdentity, "identity"))
	b.WriteString("\n")

	if e.status != "" {
		b.WriteString(errorStyle.Render(e.status))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("tab/enter next · shift+tab prev · ←/→ change type · ctrl+s save · esc cancel"))
	return modalStyle.Render(b.String())
}

func (e *tunnelEditor) renderText(label string, ti *textinput.Model, idx int, key string) string {
	focused := e.focus == idx
	lab := fmt.Sprintf("%-9s", label+":")
	if focused {
		lab = editorLabelStyle.Render(lab)
	} else {
		lab = dimStyle.Render(lab)
	}
	line := lab + " " + ti.View()
	if msg, ok := e.errs[key]; ok {
		line += "  " + errorStyle.Render("← "+msg)
	}
	return line + "\n"
}

func (e *tunnelEditor) renderType() string {
	focused := e.focus == fType
	lab := fmt.Sprintf("%-9s", "Type:")
	if focused {
		lab = editorLabelStyle.Render(lab)
	} else {
		lab = dimStyle.Render(lab)
	}
	val := tunnelTypes[e.typeIdx]
	if focused {
		val = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2")).Render(val) + "  " + dimStyle.Render("←/→")
	} else {
		val = dimStyle.Render(val)
	}
	return lab + " " + val + "\n"
}

func validEditorName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r == '-' || r == '_':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
