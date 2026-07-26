# Portato hero demo — vhs tape (vhs 0.11.0 syntax).
#
# Prereq:
#   - ./bin/portato built (make build)
#   - ./config.yaml present (defaults.accept_new_hosts: true so the first
#     connect auto-trusts the host — no TOFU modal in the recording)
#   - SSH key for the target host in ssh-agent (ssh-add -l shows it)
#   - brew install vhs ttyd ffmpeg ;  mkdir -p docs/landing/assets
#
# vhs 0.11.0 cannot script Shift+arrow (ExecuteShift only handles Enter/Tab),
# so the list reorder uses the Ctrl+K (up) / Ctrl+J (down) aliases — vhs CAN
# send Ctrl+<letter>. The typed launch command is `portato ...` (what an
# installed user runs); ./bin is put on PATH so it resolves to the local build.
#
# Run:  PATH="$PWD/bin:$PATH" ROD_SHOW=1 vhs demo.tape
#       → docs/landing/assets/hero.gif + still PNGs.
#       (ROD_SHOW=1: headless vhs hangs on Chrome 149.)

Output docs/landing/assets/hero.gif

Set FontSize 12
Set FontFamily "Meslo LG M DZ for Powerline"
Set Width 1200
Set Height 650
Set Padding 20
Set Theme "nord"
Set TypingSpeed 35ms

# Launch the standalone TUI at debug level so the `L` toggle reveals DBG entries.
Type "portato --log-level debug --config ./config.yaml"
Sleep 400ms
Enter
Sleep 1500ms

# First connect: the host is pre-trusted (accept_new_hosts: true) → straight to connected.
Space                           # db-stage: connecting → ● connected
Sleep 2000ms

# admin-ui: the host is already trusted → connects with no prompt.
Down
Sleep 500ms
Space
Sleep 3000ms

# Live-log screen, then capital L to reveal the debug-level entries.
Type "l"
Sleep 1500ms
Type "L"                        # toggle debug view (logs.go case "L")
Sleep 1500ms
Screenshot "docs/landing/assets/log.png"
Escape
Sleep 600ms

# Editor (e / n / d).
Type "e"
Sleep 2000ms
Screenshot "docs/landing/assets/editor.png"
Escape
Sleep 600ms

# Help overlay (?).
Type "?"
Sleep 2000ms
Screenshot "docs/landing/assets/help.png"
Escape
Sleep 600ms

# Reorder: move the focused tuber up one slot. vhs can't send shift+up, so use
# the Ctrl+K alias (commit 48bf7cd). Cursor is on admin-ui (index 1) → it jumps
# above db-stage.
Ctrl+k
Sleep 1200ms

# Toggle the selected tuber off (●→○) and end on the list.
Space
Sleep 1500ms
