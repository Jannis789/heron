package tui

import (
	"strings"
	"testing"

	"heron/internal/render"
)

// Zeichenselektion ueber FETTEN Text (User-Report: "fettgedruckte texte
// zu komischen symbolen"): Die Zellkarte (charView) muss sichtbaren Text
// ohne ANSI liefern — sonst driftet rangeHighlight (base zaehlt Runen,
// styleRange zaehlt Zellen) und charMarkdown kopiert \x1b-Bytes.
func TestCharSelectionBoldClean(t *testing.T) {
	th := &render.Theme{}
	nodes, err := render.Parse("Hallo **Welt** du da")
	if err != nil {
		t.Fatal(err)
	}
	widgets := BuildWidgets(nodes, th, nil)
	var l BlockList
	for _, w := range widgets {
		l = l.Append(w)
	}
	_, cls, ok := l.charView(0, 60)
	if !ok {
		t.Fatalf("block 0 not char-capable (type %T)", l.items[0])
	}
	for i, s := range cls {
		if strings.Contains(s, "\x1b") {
			t.Fatalf("cell map carries ANSI at line %d: %q", i, s)
		}
	}
	// "Hallo " sind 6 Zellen — [6,10) ist exakt das fette "Welt" (to exklusiv).
	l2 := l
	l2.char.active = true
	l2.char.block = 0
	l2.char.from, l2.char.to = 6, 10
	if got := l2.charMarkdown(); got != "Welt" {
		t.Fatalf("copy over bold word = %q, want %q", got, "Welt")
	}
}
