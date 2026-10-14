//go:build !unix

package tui

import (
	"os"
	"time"
)

// probeRead ist der Nicht-Unix-Fallback: SetReadDeadline nach bestem
// Willen. Hauptplattform (Linux/macOS, Ghostty/Kitty) nutzt die select(2)-
// Variante in termprobe_unix.go.
func probeRead(f *os.File, buf []byte, d time.Duration) (int, error) {
	_ = f.SetReadDeadline(time.Now().Add(d))
	n, err := f.Read(buf)
	_ = f.SetReadDeadline(time.Time{})
	return n, err
}
