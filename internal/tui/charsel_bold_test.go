package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

// TestCursorModeWrappedBoldStaysBold (User-Report: "fettgedruckter text
// wird nicht richtig angezeigt, wenn ich im auswahlmode mit up/down bin"):
// Ein über die Zeile umgebrochener fett-Absatz behält \x1b[1m in JEDER
// Zeile — in Ruhe UND im Cursor-Mode mit erweiterter Auswahl (WrapWords
// reproduziert die aktive SGR-Kette an Folgelines, geschlossene Paare
// werden NICHT mitgeschleppt).
func TestCursorModeWrappedBoldStaysBold(t *testing.T) {
	withTrueColor(t)
	r := newRig(t).seed("**Ganze Absatz Fett der auch umbricht damit der Wrap über zwei Zeilen geht und beim Selektieren etwas passiert.**")
	r.size(60, 20)
	views := []string{r.m.chat.View()}
	r.do(tea.KeyMsg{Type: tea.KeyShiftTab}) // Cursor-Mode
	if !r.m.chat.charActive() {
		t.Fatal("cursor mode did not activate")
	}
	r.do(tea.KeyMsg{Type: tea.KeyShiftUp}) // Auswahl über die Zeilenkante
	views = append(views, r.m.chat.View())
	r.do(tea.KeyMsg{Type: tea.KeyShiftUp})
	views = append(views, r.m.chat.View())
	for vi, v := range views {
		lines := 0
		for _, ln := range strings.Split(v, "\n") {
			if !strings.Contains(ln, "Absatz") && !strings.Contains(ln, "zwei Zeilen") {
				continue
			}
			lines++
			if !strings.Contains(ln, "\x1b[1m") {
				t.Fatalf("view %d: fett-Absatzzeile verliert Bold: %q", vi, ln)
			}
			if strings.Contains(ln, "\x1b[1m\x1b[22m") {
				t.Fatalf("view %d: leeres Stil-Paar: %q", vi, ln)
			}
		}
		if lines < 2 {
			t.Fatalf("view %d: Absatz bei Breite 60 nicht umgebrochen (%d Zeilen)", vi, lines)
		}
	}
}
