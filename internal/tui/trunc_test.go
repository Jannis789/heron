package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestTruncateDCS prueft, was bubbleteas Renderer (ansi.Truncate/
// StringWidth) mit einer timg-gleichen 15-25kB-DCS-Zeile macht: haelt er
// die Sequenz fuer Nullbreite (durchreichen) oder zaehlt er Base64 als
// druckbar (abschneiden -> Bilddaten zerstoert)?
func TestTruncateDCS(t *testing.T) {
	png := make([]byte, 3000) // ~4kB base64
	line := transmitDirectChunks(77, png)
	if !strings.HasPrefix(line, "\x1b_Ga=T") {
		t.Fatalf("kein timg-header: %q", line[:30])
	}
	w := ansi.StringWidth(line)
	tr := ansi.Truncate(line, 337, "")
	t.Logf("orig-len=%d stringWidth=%d truncate-len=%d kaputt=%v",
		len(line), w, len(tr), len(tr) < len(line))
	if len(tr) < len(line) {
		t.Fatalf("RENDERER SCHNEIDET AB: %d -> %d — die Bilddaten werden zerstoert", len(line), len(tr))
	}
}
