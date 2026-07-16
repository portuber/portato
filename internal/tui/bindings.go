package tui

// binding is one TUI key binding. It is the single source of truth shared by
// the footer (which renders foot) and the help view (which renders help). The
// two renderers used to be independent hand-maintained strings and had already
// drifted once ("q quit" in the footer vs "q quit (stops all tubers)" in help);
// co-locating them here makes that drift impossible by construction.
type binding struct {
	// foot is the compact token shown in the footer, e.g. "↑↓/jk move" or
	// "? help". Its visible width drives the footer's width fit.
	foot string
	// help is the pre-formatted line(s) the help view shows for this binding.
	// One footer token may expand to several help lines (navigation up/down,
	// enable/disable all); the slice keeps that 1-to-many mapping explicit.
	help []string
}

// tuberBindings returns the ordered list of list-view bindings. The order is
// the footer's natural reading order. The last two entries (? help, q quit)
// are the UI-unlocking keys the footer always reserves at its tail; the help
// view simply renders every binding's help lines in the same order.
func tuberBindings() []binding {
	return []binding{
		{foot: "↑↓/jk move", help: []string{
			"↑ / k        move cursor up",
			"↓ / j        move cursor down",
		}},
		{foot: "space toggle", help: []string{
			"space        toggle selected tuber (on/off)",
		}},
		{foot: "p passphrase", help: []string{
			"p            enter passphrase for the selected tuber",
		}},
		{foot: "o password", help: []string{
			"o            enter SSH password for the selected tuber (also auto-opens)",
		}},
		{foot: "r restart", help: []string{
			"r            restart selected tuber",
		}},
		{foot: "a/x all", help: []string{
			"a            enable all tubers",
			"x            disable all tubers",
		}},
		{foot: "e edit", help: []string{
			"e            edit the selected tuber",
		}},
		{foot: "n new", help: []string{
			"n            create a new tuber",
		}},
		{foot: "C duplicate", help: []string{
			"C            duplicate the selected tuber",
		}},
		{foot: "d delete", help: []string{
			"d            delete the selected tuber",
		}},
		{foot: "l logs", help: []string{
			"l            view the selected tuber's logs",
		}},
		{foot: "/ filter", help: []string{
			"/            filter the list (name/type/endpoint; esc clears)",
		}},
		{foot: "R reload", help: []string{
			"R            reload config from disk",
		}},
		{foot: "? help", help: []string{
			"? / esc      toggle this help",
		}},
		{foot: "q quit", help: []string{
			"q / ctrl+c   quit (stops all tubers)",
		}},
	}
}

// helpLines flattens the bindings into the ordered help-view line list. The
// help view renders these verbatim (Task B); keeping the assembly here lets
// the footer and help share one source.
func helpLines() []string {
	b := tuberBindings()
	out := make([]string, 0, len(b)+2)
	for _, e := range b {
		out = append(out, e.help...)
	}
	return out
}
