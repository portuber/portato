package logo

import (
	"encoding/base64"
	"fmt"
)

// inlineImage builds the iTerm2/WezTerm OSC 1337 inline-image sequence for a
// PNG, sized in terminal cells so it occupies the same 28x12 footprint as the
// ASCII variants. Modern iTerm2 and WezTerm honour the `Ncells` units in the
// width/height arguments; the trailing BEL (\x07) terminates the sequence.
//
// The PNG bytes are base64-encoded inline. bubbletea passes raw OSC through in
// the View() string, so the TUI can emit this directly.
func inlineImage(png []byte) string {
	return fmt.Sprintf(
		"\x1b]1337;File=inline=1;width=%dcells;height=%dcells;preserveAspectRatio=1:%s\x07",
		logoWidth, logoHeight, base64.StdEncoding.EncodeToString(png),
	)
}
