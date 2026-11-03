package tui

import (
	"strconv"
	"strings"
	"testing"
)

// TestShiftUpStartsSelectionAtReadingPosition: Der Nutzer sitzt ganz unten im
// Chat (letzter Block = PlantUML, quirks-test.md Abschnitt 78) und drueckt
// shift+up — die Auswahl muss DORT anfangen (Anker = unterster sichtbarer
// Block) und einen Block nach oben wachsen. Vorher ankerte extendSel am
// OBEN sichtbaren Block: Die Auswahl leuchtete ~2 Abschnitte hoeher
// (76 statt 78), der gelesene PlantUML-Block wurde nie ausgewaehlt.
func TestShiftUpStartsSelectionAtReadingPosition(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		b.WriteString("## Abschnitt " + strconv.Itoa(i) + "\n\n```mermaid\nflowchart LR\n  A --> B\n```\n\n")
	}
	b.WriteString("## Abschnitt 41\n\n```plantuml\n@startuml\nA -> B\nB --> C\n@enduml\n```\n\nEnde der Referenz. Beim Durchsehen pruefen.\n")

	r := newRig(t).seed(b.String())
	// PlantUML-Block (letzter Codeblock) suchen — DAS Ziel der Auswahl.
	puml := -1
	for i, it := range r.m.chat.items {
		if cb, ok := it.(CodeBlock); ok && cb.lang == "plantuml" {
			puml = i
		}
	}
	if puml < 0 {
		t.Fatal("kein PlantUML-Block im Seed")
	}
	// tab in den Antwort-Modus: Der Fokus landet auf dem letzten sichtbaren
	// Segment — dem NACHSATZ UNTERHALB des PlantUML (FR-40: Auch reiner
	// Text nach einem Codeblock ohne Ueberschrift ist ein eigenes Segment;
	// der Nutzer "sitzt" dort unten am Chat-Ende).
	r.key("tab")
	if !r.m.chat.Focused() || r.m.chat.focus != puml+1 {
		t.Fatalf("tab muss den Nachsatz-Text %d fokussieren (FR-40), got focus=%d", puml+1, r.m.chat.focus)
	}
	r.key("shift+up")
	lo, hi := r.m.chat.selBounds()
	if !r.m.chat.selActive() {
		t.Fatal("shift+up muss die Auswahl starten")
	}
	// Der Ausloeser greift SOFORT den naechsten Abschnitt: Anker-Einheit
	// (Nachsatz) PLUS den PlantUML-Block darueber — kein "verschenkter"
	// Erstdruck (User-Report: "2 statt 1 mal shift+up").
	if lo != puml || hi != puml+1 {
		t.Fatalf("Ausloeser muss den PlantUML-Block sofort greifen (%d..%d), got %d..%d", puml, puml+1, lo, hi)
	}
	if r.m.chat.selDir != -1 {
		t.Fatalf("shift+up muss die Richtung -1 setzen (Cursor am Ende), got %d", r.m.chat.selDir)
	}
	// Noch ein shift+up: Der Cursor wandert UM EINEN Block weiter nach oben
	// — die Unterkante bleibt am Nachsatz haengen.
	r.key("shift+up")
	lo2, hi2 := r.m.chat.selBounds()
	if hi2 != puml+1 || lo2 != puml-1 {
		t.Fatalf("shift+up waechst um einen Block: erwartet %d..%d, got %d..%d", puml-1, puml+1, lo2, hi2)
	}
	// c kopiert den Bereich: der PlantUML-Quelltext MUSS enthalten sein.
	r.key("c")
	if len(r.copied) == 0 {
		t.Fatal("c hat nicht kopiert")
	}
	got := r.copied[len(r.copied)-1]
	if !strings.Contains(got, "@startuml") {
		t.Fatalf("Bereichskopie fehlt der PlantUML-Block: %q", got)
	}
}

// TestSelCursorEdgeByDirection (FR-42): Die AUSLOESERRICHTUNG bestimmt die
// Kante des Selection-Cursors — shift+down parkt ihn am ANFANG der Einheit
// (erste nicht-leere Zeile, hier der Blockkopf), shift+up am ENDE (letzte
// nicht-leere Zeile, hier der Blockfuss). Der Ausloeser selbst springt NICHT.
func TestSelCursorEdgeByDirection(t *testing.T) {
	underlineLine := func(view string) (int, string) {
		for i, ln := range strings.Split(view, "\n") {
			if strings.Contains(ln, "\x1b[4m") {
				return i, ln
			}
		}
		return -1, ""
	}
	downRig := newRig(t).seed("```go\na := 1\nb := 2\n```")
	downRig.key("tab") // Antwort-Modus: Fokus auf dem (einzigen) Codeblock
	downRig.key("shift+down")
	if !downRig.m.chat.selActive() || downRig.m.chat.selDir != 1 {
		t.Fatalf("shift+down: sel=%v dir=%d", downRig.m.chat.selActive(), downRig.m.chat.selDir)
	}
	if _, ln := underlineLine(downRig.view()); !strings.Contains(ln, "┌") {
		t.Fatalf("shift+down muss den Cursor am ANFANG (Blockkopf) parken: %q", ln)
	}

	upRig := newRig(t).seed("```go\na := 1\nb := 2\n```")
	upRig.key("tab")
	upRig.key("shift+up")
	if !upRig.m.chat.selActive() || upRig.m.chat.selDir != -1 {
		t.Fatalf("shift+up: sel=%v dir=%d", upRig.m.chat.selActive(), upRig.m.chat.selDir)
	}
	if _, ln := underlineLine(upRig.view()); !strings.Contains(ln, "┘") {
		t.Fatalf("shift+up muss den Cursor am ENDE (Blockfuss) parken: %q", ln)
	}

	// Neutraler Einstieg (tab + shift+down): Cursor am ANFANG der Einheit —
	// Leserichtung; shift+down startet KEINE Auswahl mehr aus der Eingabe
	// (User-Report: "im Cursor-Mode gefangen"), der Weg fuehrt ueber tab in
	// den Antwort-Modus — und shift+enter ist der Zeichen-Cursor (FR-42).
	neuRig := newRig(t).seed("```go\na := 1\nb := 2\n```")
	neuRig.key("tab") // Antwort-Modus: Fokus auf dem (einzigen) Codeblock
	neuRig.key("shift+down")
	if !neuRig.m.chat.selActive() || neuRig.m.chat.selDir != 1 {
		t.Fatalf("shift+down: sel=%v dir=%d", neuRig.m.chat.selActive(), neuRig.m.chat.selDir)
	}
	if _, ln := underlineLine(neuRig.view()); !strings.Contains(ln, "┌") {
		t.Fatalf("shift+down muss den Cursor am ANFANG parken: %q", ln)
	}
}
