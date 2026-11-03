package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestSpaceMarksFocusedBlock (FR-29): space toggles the mark of the focused
// block and the chip announces it.
func TestSpaceMarksFocusedBlock(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	if !r.m.chat.items[r.m.chat.focus].Marked() {
		t.Fatal("space did not mark the focused block")
	}
	// Der MARKED-Chip ist entfallen — die Auswahl zeigt der Neon-Rahmen
	// (TestMarkColorsWholeFrame) und der Copy-Hint in der zentralen Leiste.
	r.contains("c copy")
	r.do(runes(" "))
	if r.m.chat.items[r.m.chat.focus].Marked() {
		t.Fatal("second space did not unmark")
	}
}

// TestYankCopiesPlainMarkdown (FR-29): y copies the marked block's raw
// markdown — no ANSI — and clears the selection.
func TestYankCopiesPlainMarkdown(t *testing.T) {
	r := newRig(t).seed("vor\n\n```go\nx := 1\n```\n\n```go\ny := 2\n```")
	r.do(tea.KeyMsg{Type: tea.KeyTab}) // Chat: letzter Block (code 2)
	r.do(runes(" "))
	r.do(tea.KeyMsg{Type: tea.KeyUp}) // code 1: ERSETZT die Auswahl
	r.do(runes(" "))
	r.do(runes("y"))

	if len(r.copied) != 1 {
		t.Fatalf("copied %d payloads, want 1: %q", len(r.copied), r.copied)
	}
	got := r.copied[0]
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("selection copy contains ANSI codes: %q", got)
	}
	want := "```go\nx := 1\n```"
	if got != want {
		t.Fatalf("selection copy = %q, want %q (Einfach-Auswahl)", got, want)
	}
	if r.m.chat.markedCount() != 0 {
		t.Fatal("marks survived the copy")
	}
}

// TestMarkIsSingleSelection (FR-29): Es gibt zu jedem Zeitpunkt HOECHSTENS
// EINEN markierten Block — eine neue Markierung ersetzt die alte (Radio,
// nicht Checkbox), egal ob per Tastatur (space) oder Maus (Klick). Der
// Nutzer konnte vorher mehrere Bloecke gleichzeitig markieren.
func TestMarkIsSingleSelection(t *testing.T) {
	r := newRig(t).seed("```go\na\n```\n\n```go\nb\n```\n\n```go\nc\n```")
	l := func() []bool {
		m := []bool{}
		for _, it := range r.m.chat.items {
			m = append(m, it.Marked())
		}
		return m
	}
	eq := func(want ...bool) {
		t.Helper()
		got := l()
		if len(got) != len(want) {
			t.Fatalf("bloecke=%d want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("marks=%v want %v", got, want)
			}
		}
		if n := r.m.chat.markedCount(); n > 1 {
			t.Fatalf("markedCount=%d: mehr als eine Auswahl", n)
		}
	}

	r.do(tea.KeyMsg{Type: tea.KeyTab}) // letzter Block (c)
	r.do(runes(" "))
	eq(false, false, true)
	r.do(tea.KeyMsg{Type: tea.KeyUp}) // b
	r.do(runes(" "))                  // ersetzt c
	eq(false, true, false)
	// Mauspfad: Klick auf den ersten Blockkoerper ersetzt b — der erste
	// Block beginnt oben im Chatbereich (Dokument passt ins Fenster,
	// kein Scroll), Body-Zeile = Kopf + 1.
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	eq(true, false, false)
	// Erneutes space auf DEMSELBEN (nun fokussierten) Block hebt die
	// Auswahl auf — die Auswahl darf leer sein.
	r.do(tea.KeyMsg{Type: tea.KeyUp}) // Fokus b -> a (der Markierte)
	r.do(runes(" "))
	eq(false, false, false)
}

// TestClickTogglesMark (FR-29): a left click on a block body toggles its
// mark; clicking the COPY chip still copies that block (FR-5 bleibt).
func TestClickTogglesMark(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	// The first block starts at the top of the chat area; its body row is
	// one line below the header.
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.chat.items[0].Marked() {
		t.Fatal("click did not mark the block")
	}
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if r.m.chat.items[0].Marked() {
		t.Fatal("second click did not unmark")
	}
}

// TestEscClearsMarksFirst (FR-29): esc clears the selection before it
// unfocuses.
func TestEscClearsMarksFirst(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	r.do(tea.KeyMsg{Type: tea.KeyEsc})
	if r.m.chat.markedCount() != 0 {
		t.Fatal("esc did not clear the marks")
	}
	if !r.m.chat.Focused() {
		t.Fatal("esc consumed the focus too — the second esc belongs to focus")
	}
}

// TestMarkColorsWholeFrame: Die Auswahl faerbt den GESAMTEN Block-Rahmen
// wie der Fokus — nicht nur die Aktionszeile ("nur die letzte Zeile
// gefaerbt"). Kopf- und Body-Zeilen des markierten Blocks muessen byte-
// identisch zum fokussierten Block sein (gleiche Rahmen-Farbe); nur die
// Aktionszeile darf abweichen (MARKED statt COPY).
func TestMarkColorsWholeFrame(t *testing.T) {
	withTrueColor(t) // sonst rendert go test farblos: Fokus == Ruhefarbe
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatalf("default theme: %v", err)
	}
	cmp := func(name string, focus, mark Widget, skipHead, skipFooter int) {
		t.Helper()
		fl := strings.Split(focus.View(80), "\n")
		ml := strings.Split(mark.View(80), "\n")
		if len(fl) != len(ml) {
			t.Fatalf("%s: Zeilenzahl Fokus=%d Markiert=%d", name, len(fl), len(ml))
		}
		for i := skipHead; i < len(fl)-skipFooter; i++ {
			if fl[i] != ml[i] {
				t.Fatalf("%s Zeile %d weicht ab:\nfokus    %q\nmarkiert %q", name, i, fl[i], ml[i])
			}
		}
	}
	cb := NewCodeBlock("go", "aaa\nbbb", nil)
	cb.th = th
	cmp("CodeBlock", cb.SetFocused(true), cb.SetMarked(true), 0, 1)

	tb := NewTableBlock([][]string{{"a", "b"}, {"1", "2"}}, nil, nil)
	tb.th = th
	cmp("TableBlock", tb.SetFocused(true), tb.SetMarked(true), 0, 0)
	// Regression (letzte Reihe in Ruhefarbe statt Auswahl-Farbe): Die
	// markierte Abschlusszeile muss sich von der UNMARKIERTEN unterscheiden
	// — der Fokus/Markiert-Vergleich allein sieht den Bug nicht, weil beide
	// falsch waren.
	last := func(v Widget) string { return strings.Split(v.View(80), "\n")[len(strings.Split(v.View(80), "\n"))-1] }
	if last(tb.SetMarked(true)) == last(tb) {
		t.Fatal("markierte Tabellen-Abschlusszeile hat NICHT die Auswahl-Farbe")
	}

	kb := NewThinkBlock("denken\ndenken2", th)
	cmp("ThinkBlock", kb.SetFocused(true), kb.SetMarked(true), 1, 1)
}

// TestUpdateBlockKeepsMark: In-Place-Aktualisierung (ThinkBlock-Refresh im
// Stream) erhaelt die Markierung — sonst Chip weg, Auswahl unsichtbar in
// der marks-Map.
func TestUpdateBlockKeepsMark(t *testing.T) {
	l := NewBlockList()
	tb := NewThinkBlock("alt", nil).SetID(ThinkBlockID)
	l = l.Append(tb)
	l = l.toggleMarkIdx(len(l.items) - 1)
	if !l.items[0].Marked() {
		t.Fatal("Vorbedingung: ThinkBlock markiert")
	}
	fresh := NewThinkBlock("neu im Stream", nil)
	nl, ok := l.UpdateBlock(ThinkBlockID, fresh)
	if !ok {
		t.Fatal("UpdateBlock fand den Block nicht")
	}
	if !nl.items[0].Marked() {
		t.Fatal("Markierung ging beim Refresh verloren")
	}
	if nl.markedCount() != 1 {
		t.Fatalf("markedCount=%d want 1", nl.markedCount())
	}
}

// TestSelectionFrameUnique (FR-39-Invariante inkl. Auswahl): Fokus-Rahmen
// und MARKierungs-Rahmen sind dieselbe "Auswahl" — zu jedem Zeitpunkt
// leuchtet hoechstens EIN Neon-Rahmen. Repro: tab fokussiert den letzten
// Block, Linksklick markiert einen anderen — vorher leuchteten ZWEI.
func TestSelectionFrameUnique(t *testing.T) {
	r := newRig(t).seed("```go\na\n```\n\n```go\nb\n```\n\n```go\nc\n```")
	frames := func() int {
		n := 0
		for _, it := range r.m.chat.items {
			if it.Focused() || it.Marked() {
				n++
			}
		}
		return n
	}

	r.do(tea.KeyMsg{Type: tea.KeyTab}) // Antwort-Modus: letzter Block fokussiert
	if frames() != 1 {
		t.Fatalf("nach tab %d Neon-Rahmen, want 1", frames())
	}
	// Linksklick auf den Body des ERSTEN Blocks (Kopfzeile + 1).
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if frames() != 1 {
		t.Fatalf("nach Klick auf anderen Block %d Neon-Rahmen, want 1", frames())
	}
	if r.m.chat.Focused() {
		t.Fatal("Fokus-Rahmen haette der Klick-Markierung weichen muessen")
	}
	if !r.m.chat.items[0].Marked() {
		t.Fatal("geklickter Block ist nicht markiert")
	}
	// Gegenrichtung: Fokuswechsel auf einen ANDEREN Block loest die
	// Markierung (erster down holt den Fokus zum markierten Block, der
	// zweite bewegt ihn weiter).
	r.do(tea.KeyMsg{Type: tea.KeyDown})
	r.do(tea.KeyMsg{Type: tea.KeyDown})
	if frames() != 1 {
		t.Fatalf("nach Fokuswechsel %d Neon-Rahmen, want 1", frames())
	}
	if r.m.chat.items[0].Marked() {
		t.Fatal("Markierung haette dem Fokus weichen muessen")
	}
	// Am selben Block bleiben Fokus UND Markierung zusammen (space am
	// fokussierten Block): ein Rahmen, MARKED-Chip dazu.
	r.do(runes(" "))
	if frames() != 1 || !r.m.chat.items[r.m.chat.focus].Marked() {
		t.Fatalf("space am fokussierten Block: Rahmen=%d markiert=%v", frames(), r.m.chat.items[r.m.chat.focus].Marked())
	}
}

// TestClickMarkBlursInput (FR-39, Maus-Pfad): Ein Linksklick, der einen Block
// markiert, muss das Eingabefeld weichen lassen — die Klick-Markierung IST
// die Auswahl und haelt den EINEN Neon-Rahmen. Vorher leuchteten ZWEI: das
// fokussierte Eingabefeld UND der markierte Block. Der Aufhebe-Klick kehrt
// auf den STANDARD zurueck (Eingabe-Modus), tab aus der Auswahl ebenfalls.
func TestClickMarkBlursInput(t *testing.T) {
	r := newRig(t).seed("```go\na\n```\n\n```go\nb\n```")
	neon := func() int {
		n := 0
		if r.m.inputFocus {
			n++
		}
		for _, it := range r.m.chat.items {
			if it.Focused() || it.Marked() {
				n++
			}
		}
		return n
	}
	if !r.m.inputFocus || neon() != 1 {
		t.Fatalf("Startzustand: inputFocus=%v Neon=%d, want Eingabe-Modus mit 1 Rahmen", r.m.inputFocus, neon())
	}

	// Linksklick auf den Body des ersten Blocks: Auswahl uebernimmt den
	// Rahmen, das Eingabefeld blurrt.
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.chat.items[0].Marked() {
		t.Fatal("Klick hat den Block nicht markiert")
	}
	if r.m.inputFocus {
		t.Fatal("Eingabefeld haelt den Fokus — die Auswahl haette weichen muessen (2 Neon-Rahmen)")
	}
	if neon() != 1 {
		t.Fatalf("nach Klick %d Neon-Rahmen, want 1", neon())
	}

	// tab aus der Auswahl zurueck ins Eingabefeld: Die Markierung weicht
	// MIT dem Fokus (Heimweg loest die Auswahl auf — nie zwei Rahmen).
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	if !r.m.inputFocus || r.m.chat.markedCount() != 0 {
		t.Fatalf("nach tab: inputFocus=%v marks=%d — die Auswahl haette aufgeloest werden muessen", r.m.inputFocus, r.m.chat.markedCount())
	}
	if neon() != 1 {
		t.Fatalf("nach tab %d Neon-Rahmen, want 1", neon())
	}

	// Gegenprobe Aufhebe-Klick: Markierung setzen und wieder loesen — ohne
	// Chat-Fokus kehrt die Kette heim ins Eingabefeld.
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if r.m.inputFocus || r.m.chat.markedCount() != 1 {
		t.Fatal("Zwenschritt: Klick haette markieren und blurren muessen")
	}
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.inputFocus || r.m.chat.markedCount() != 0 {
		t.Fatalf("Aufhebe-Klick: inputFocus=%v marks=%d — die Kette haette heimkehren muessen", r.m.inputFocus, r.m.chat.markedCount())
	}
	if neon() != 1 {
		t.Fatalf("nach Aufheben %d Neon-Rahmen, want 1", neon())
	}
}

// TestNeonBandYieldsToSelection (FR-39): Das magenta Neon-Band am Eingabe-
// rahmen (Turn-Signal) ruht, solange der Chat den Rahmen haelt — die magenta
// Rahmenzeichen sind unique, nie gleichzeitig am Eingabefeld UND an der
// Auswahl. Ohne Auswahl laeuft das Band weiter (Turn-Signal bleibt).
func TestNeonBandYieldsToSelection(t *testing.T) {
	withTrueColor(t)
	r := newRig(t).seed("```go\na\n```")
	r.m.startTurn()
	r.m.neonLevel = 1 // volles Band (der Fade-In startet bei 0)
	magenta := "38;2;255;0;204"
	if !strings.Contains(r.m.inputView(), magenta) {
		t.Fatal("Vorbedingung: Band leuchtet im laufenden Turn")
	}

	// Klick-Markierung: das Band ruht am geblurrlen Eingabefeld.
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if hot := r.m.inputView(); strings.Contains(hot, magenta) {
		t.Fatal("Neon-Band leuchtet trotz Auswahl — die magenta Zeichen muessen unique sein")
	}

	// Auswahl loesen: die Kette kehrt heim, das Band laeuft weiter.
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.inputFocus {
		t.Fatal("Aufhebe-Klick haette heimkehren muessen")
	}
	if hot := r.m.inputView(); !strings.Contains(hot, magenta) {
		t.Fatal("Band laeuft nach dem Loesen der Auswahl nicht weiter")
	}
}
