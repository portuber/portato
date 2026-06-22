package tui

// All TUI styles resolve through a single palette chosen by detectKind() at
// package init (Phase 11: dark / light / monochrome). The package-level
// variables below are aliases into that palette, so view.go / editor.go /
// logs.go reference them unchanged regardless of the active theme.
var pal = resolvePalette(detectKind())

var (
	titleStyle       = pal.title
	modeStyle        = pal.mode
	headerStyle      = pal.header
	selectedStyle    = pal.selected
	cursorStyle      = pal.cursor
	dimStyle         = pal.dim
	errorStyle       = pal.err
	warnStyle        = pal.warn
	footerStyle      = pal.footer
	helpTitle        = pal.helpTitle
	helpPanel        = pal.helpPanel
	modalStyle       = pal.modal
	editorTitleStyle = pal.editorTitle
	editorLabelStyle = pal.editorLabel
)

var stateStyle = pal.state
