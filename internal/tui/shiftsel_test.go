package tui

import (
	"strings"
	"testing"
)

// TestShiftSelectionCopiesRange: shift+down startet die Block-Auswahl am
// obersten sichtbaren Block (shift+enter ist der Zeichen-Cursor, FR-42) und
// erweitert blockweise ueber ALLE Bloecke — auch Codeblock gefolgt von
// Fliesstext OHNE Ueberschrift (das Raetsel) — und c kopiert NUR den Bereich.
func TestShiftSelectionCopiesRange(t *testing.T) {
	r := newRig(t)
	r.seed("# H\n\ntext a\n\n```go\nx := 1\n```\n\nplain text ohne heading\n")
	// Bloecke: 0=Heading, 1=Text, 2=Code, 3=Text. Antwort-Modus betreten
	// (tab — shift+down startet keine Auswahl mehr aus der Eingabe) und
	// den Fokus ans OBERSTE Segment fahren: Der Anker sitzt oben.
	r.key("tab")        // Fokus: letztes sichtbares Segment (3)
	r.key("up").key("up").key("up") // Fokus auf Block 0
	r.key("shift+down")
	if !r.m.chat.selActive() || r.m.chat.selCur != 1 || r.m.chat.selFrom != 0 {
		t.Fatalf("shift+down muss Anker 0 + Cursor 1 setzen (naechster Abschnitt sofort): sel=%v from=%d cur=%d",
			r.m.chat.sel, r.m.chat.selFrom, r.m.chat.selCur)
	}
	// Underscore-Cursor sichtbar: erste Content-Zeile unterstrichen.
	if !strings.Contains(r.view(), "\x1b[4m") {
		t.Fatal("Underscore-Cursor fehlt im View")
	}
	// 1 weiterer shift+down: Bereich 0..2 = Ueberschrift + Text + CODEBLOCK.
	r.key("shift+down")
	if lo, hi := r.m.chat.selBounds(); lo != 0 || hi != 2 {
		t.Fatalf("Bereich 0..2 erwartet, got %d..%d", lo, hi)
	}
	r.key("c")
	if len(r.copied) == 0 {
		t.Fatal("c hat nicht kopiert")
	}
	got := r.copied[len(r.copied)-1]
	for _, want := range []string{"# H", "text a", "```go", "x := 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Bereichskopie fehlt %q: %q", want, got)
		}
	}
	if strings.Contains(got, "plain text ohne heading") {
		t.Fatalf("Bereichskopie darf Block 3 nicht enthalten: %q", got)
	}
}

// TestShiftSelectionExtendsDownAcrossHeterogeneous: shift+down laeuft auch
// ueber NICHT fokussierbare Textbloecke (Codeblock-gefolgt-von-Text).
func TestShiftSelectionExtendsDownAcrossHeterogeneous(t *testing.T) {
	r := newRig(t)
	r.seed("```go\nx := 1\n```\n\nplain text\n\nnoch text\n")
	// Bloecke: 0=Code(fokussierbar), 1=Text, 2=Text. Antwort-Modus (tab),
	// Fokus auf das oberste Segment — dann erweitert shift+down die
	// Auswahl ueber ALLE Bloecke, auch nicht klassisch fokussierbare
	// Textbloecke (Codeblock-gefolgt-von-Text).
	r.key("tab")        // Fokus: letztes sichtbares Segment (2)
	r.key("up").key("up") // Fokus auf Block 0
	for i := 0; i < 3; i++ {
		r.key("shift+down")
	}
	if lo, hi := r.m.chat.selBounds(); lo != 0 || hi != 2 {
		t.Fatalf("Bereich 0..2 erwartet (Code+Text+Text), got %d..%d", lo, hi)
	}
	r.key("c")
	got := r.copied[len(r.copied)-1]
	if !strings.Contains(got, "x := 1") || !strings.Contains(got, "plain text") || !strings.Contains(got, "noch text") {
		t.Fatalf("heterogener Bereich unvollstaendig: %q", got)
	}
}

// TestShiftSelectionEscClears: esc kollabiert die Auswahl, c kopiert danach
// wieder das normale Ziel (fokussierter Block).
func TestShiftSelectionEscClears(t *testing.T) {
	r := newRig(t)
	r.seed("eins\n\nzwei\n")
	r.key("shift+tab")
	r.key("shift+down")
	r.key("esc")
	if r.m.chat.selActive() {
		t.Fatal("esc muss die Shift-Auswahl aufheben")
	}
	if strings.Count(r.view(), "\x1b[4m") > 0 && !strings.Contains(r.view(), "\x1b[4m") {
		t.Fatal("unerwartet")
	}
}
