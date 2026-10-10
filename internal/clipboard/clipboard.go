// Package clipboard writes to the terminal clipboard via OSC52 with an eye to
// native fallbacks (wl-copy/xclip/pbcopy) as a next step.
package clipboard

import (
	"encoding/base64"
	"os"
)

// osc52Frame returns the OSC52 sequence for text (works over SSH).
func osc52Frame(text string) string {
	payload := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + payload + "\x07"
}

// Copy writes text to the terminal clipboard via OSC52.
func Copy(text string) {
	_, _ = os.Stdout.WriteString(osc52Frame(text))
}
