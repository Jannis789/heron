package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

func TestCodeBlockCopiesOnEnter(t *testing.T) {
	var copied string
	b := NewCodeBlock("go", "x := 1\n", func(s string) tea.Cmd {
		return func() tea.Msg { copied = s; return nil }
	})
	// Copy laeuft nicht mehr ueber enter/c am Block (Eingabefeld-Chip):
	// Update bleibt zustandslos, Copy() liefert nur noch das Cmd.
	w, cmd := b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter am Block darf nicht mehr kopieren: %T", cmd)
	}
	if wc, ok := w.(CodeBlock); !ok || wc.code != b.code || wc.focused != b.focused {
		t.Fatal("Update sollte den Block unangetastet lassen")
	}
	_, ccmd := b.Copy()
	if ccmd == nil {
		t.Fatal("expected a deferred copy cmd")
	}
	ccmd() // the runtime executes the effect
	if copied != "x := 1" {
		t.Fatalf("copied=%q", copied)
	}
	if strings.Contains(b.View(40), "COPY") {
		t.Fatalf("Block-Footer darf keinen COPY-Kasten mehr zeigen: %q", b.View(40))
	}
}

func TestCodeBlockExpandsTabsForAlignment(t *testing.T) {
	b := NewCodeBlock("go", "func f() {\n\tfmt.Println(1)\n}\n", nil)
	for i, ln := range strings.Split(b.View(46), "\n") {
		if !strings.HasSuffix(ln, "│") && !strings.HasSuffix(ln, "┐") && !strings.HasSuffix(ln, "┘") {
			t.Fatalf("line %d not box-aligned: %q", i, ln)
		}
		// lipgloss.Width ignores ANSI; runewidth would count escape bytes.
		if w := lipgloss.Width(ln); w > 46 {
			t.Fatalf("line %d too wide (%d): %q", i, w, ln)
		}
	}
}

func TestUserBlockRailAndArrowPrefix(t *testing.T) {
	// Die eigene Frage traegt die DICKE Schiene "▌" vor jeder Zeile und den
	// Pfeil "→ " nur vor der ersten; Folgezeilen ruecken ein, der Umbruch
	// steht bündig unter dem Textanfang — nicht unter dem Pfeil.
	b := NewUserBlock("langer prompt der ueber mehrere zeilen umbricht damit der einzug sichtbar wird")
	lines := strings.Split(b.View(40), "\n")
	// Eine Leerzeile davor/danach (Platzverteilung): Zeile 0 ist leer …
	if lines[0] != "" {
		t.Fatalf("erwartet Leerzeile VOR der Frage, got %q", lines[0])
	}
	if lines[len(lines)-1] != "" {
		t.Fatalf("erwartet Leerzeile NACH der Frage, got %q", lines[len(lines)-1])
	}
	body := lines[1 : len(lines)-1]
	if len(body) < 2 {
		t.Fatalf("erwartet Umbruch, got %d Zeilen", len(body))
	}
	if !strings.HasPrefix(body[0], "▌ → ") {
		t.Fatalf("erste Zeile ohne Schiene+Pfeil: %q", body[0])
	}
	for i, ln := range body[1:] {
		if !strings.HasPrefix(ln, "▌   ") {
			t.Fatalf("Folgezeile %d ohne Schiene/Einzug: %q", i+1, ln)
		}
	}
	// KEINE Box um die Frage, keine dünne Einzellinie "│".
	for i, ln := range body {
		if strings.Contains(ln, "┌") || strings.Contains(ln, "└") || strings.HasPrefix(ln, "│") {
			t.Fatalf("Zeile %d sieht nach Box/Schiene-duenn aus: %q", i, ln)
		}
	}
	// Markdown bleibt der ROHE Text ohne Schmuck (FR-29: Copy-Payload).
	if b.Markdown() != "langer prompt der ueber mehrere zeilen umbricht damit der einzug sichtbar wird" {
		t.Fatalf("Markdown=%q", b.Markdown())
	}
	if b.Height(40) != len(lines) {
		t.Fatalf("Height=%d, View-Zeilen=%d — Scroll/Hit-Testing driften", b.Height(40), len(lines))
	}
}

func TestUserRailHeightMatchesViewAcrossWidths(t *testing.T) {
	// Height und View muessen fuer JEDE Breite gleich zaehlen — sonst
	// springt der Scroll beim Resize. Keine Zeile darf breiter als das
	// Terminal werden.
	b := NewUserBlock("langer prompt der ueber mehrere zeilen umbricht damit wrap und schiene zusammenwirken")
	for _, w := range []int{10, 16, 20, 32, 48, 64, 100} {
		lines := strings.Split(b.View(w), "\n")
		if got := b.Height(w); got != len(lines) {
			t.Fatalf("width %d: Height=%d View=%d", w, got, len(lines))
		}
		for i, ln := range lines {
			if x := lipgloss.Width(ln); x > w {
				t.Fatalf("width %d: Zeile %d zu breit (%d)", w, i, x)
			}
		}
	}
}

func TestCodeBlockDoesNotReCopy(t *testing.T) {
	var calls int
	b := NewCodeBlock("sh", "echo hi", func(string) tea.Cmd {
		return func() tea.Msg { calls++; return nil }
	})
	b = b.SetFocused(true).(CodeBlock)
	// enter loest am Block NICHTS mehr aus (Copy = Eingabefeld-Chip) —
	// auch nicht mehrfach.
	for i := 0; i < 2; i++ {
		if _, cmd := b.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
			t.Fatal("enter am Block darf nicht mehr kopieren")
		}
	}
	if calls != 0 {
		t.Fatalf("expected 0 copies, got %d", calls)
	}
}

func TestTableBlockCopiesMarkdown(t *testing.T) {
	var copied string
	b := NewTableBlock(
		[][]string{{"name", "qty"}, {"apples", "3"}, {"pears", "5"}},
		[]render.Align{render.AlignDefault, render.AlignDefault},
		func(s string) tea.Cmd {
			return func() tea.Msg { copied = s; return nil }
		},
	)
	b = b.SetFocused(true).(TableBlock)
	_, cmd := b.Copy()
	if cmd == nil {
		t.Fatal("expected a copy cmd")
	}
	cmd()
	if !strings.Contains(copied, "| name | qty |") || !strings.Contains(copied, "| apples | 3 |") {
		t.Fatalf("markdown=%q", copied)
	}
	// Der Block-Footer ist info-only: keine COPY-Box mehr (der Chip im
	// Eingabefeld-Footer uebernimmt die Aktion).
	if strings.Contains(b.View(60), "COPY") {
		t.Fatalf("footer darf kein COPY mehr zeigen: %q", b.View(60))
	}
}

func TestTableBlockReflowsOnResize(t *testing.T) {
	b := NewTableBlock(
		[][]string{{"col"}, {"a very long cell that must wrap to fit"}},
		[]render.Align{render.AlignDefault},
		nil,
	)
	narrow := b.View(20)
	wide := b.View(100)
	if narrow == wide {
		t.Fatal("table should reflow with width")
	}
	for i, ln := range strings.Split(narrow, "\n") {
		if w := runewidth.StringWidth(ln); w > 20 {
			t.Fatalf("narrow line %d too wide (%d): %q", i, w, ln)
		}
	}
	// Resize caches the layout for that width; Height uses the cache.
	r := b.Resize(60).(TableBlock)
	if r.cacheW != 60 || r.body == nil {
		t.Fatalf("resize should cache layout for 60, cacheW=%d body=%v", r.cacheW, r.body != nil)
	}
	if r.Height(60) != len(r.body)+1 {
		t.Fatalf("height=%d want %d (cache)", r.Height(60), len(r.body)+1)
	}
	// A different width ignores the cache and lays out dynamically.
	if r.Height(100) == 0 {
		t.Fatal("fallback layout missing")
	}
}

func TestBlockListResizeRebuildsWidgets(t *testing.T) {
	l := NewBlockList()
	l = l.Append(NewTableBlock([][]string{{"a"}, {"b"}}, []render.Align{render.AlignDefault}, nil))
	l = l.SetSize(42, 10)
	tb := l.items[0].(TableBlock)
	if tb.cacheW != 42 || tb.body == nil {
		t.Fatalf("SetSize should rebuild layout cache, cacheW=%d", tb.cacheW)
	}
}

func TestBlockListHeightAndScroll(t *testing.T) {
	l := NewBlockList()
	l = l.Append(NewTextBlock("line one", ""), NewTextBlock("line two", ""))
	if l.totalLines() != 2 {
		t.Fatalf("total=%d want 2", l.totalLines())
	}
	l = l.SetSize(40, 1)
	l = l.ScrollEnd()
	if l.offset != 1 {
		t.Fatalf("offset=%d want 1", l.offset)
	}
	v := l.View()
	if strings.Contains(v, "line one") {
		t.Fatalf("view(1 line) should not show the first line: %q", v)
	}
}

func TestBlockListFocusSteps(t *testing.T) {
	l := NewBlockList()
	l = l.Append(
		NewTextBlock("text", ""), // FR-40: eigenes fokussierbares Segment
		NewCodeBlock("go", "a", nil),
		NewCodeBlock("go", "b", nil),
	)
	l = l.SetSize(40, 10) // alles sichtbar, Viewport-Anker = Block 0
	var handled bool
	l, _, handled = l.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !handled || l.focus != 0 {
		t.Fatalf("first down focus=%d want 0 (Text ist Fokusziel)", l.focus)
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyDown})
	if l.focus != 1 {
		t.Fatalf("second down focus=%d want 1", l.focus)
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyDown})
	if l.focus != 2 {
		t.Fatalf("third down focus=%d want 2", l.focus)
	}
	// down am letzten Block STOPPT — die Kette bleibt stehen; zurueck ins
	// Eingabefeld fuehrt ausschliesslich tab (kein Wrap, keine Fokus-Abgabe).
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !l.Focused() || l.focus != 2 {
		t.Fatalf("down past the last block must stay: focus=%d", l.focus)
	}
	// FocusLast: der Ruecksprung vom Eingabefeld in den Chat.
	l = l.FocusLast()
	if l.focus != 2 {
		t.Fatalf("FocusLast focus=%d want 2", l.focus)
	}
	// up vom letzten Block: ein Schritt zurueck, am Anfang stehenbleiben.
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyUp})
	if l.focus != 1 {
		t.Fatalf("up focus=%d want 1", l.focus)
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyUp})
	if l.focus != 0 {
		t.Fatalf("up focus=%d want 0 (Text wieder Fokusziel)", l.focus)
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyUp})
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyUp})
	if l.focus != 0 {
		t.Fatalf("up at the start must stay: focus=%d", l.focus)
	}
}

// TestBlockListFocusStartsAtViewport: Ohne Fokus startet tab am oberen
// Viewport-Rand — in einem langen Chat (Anfang weit weg gescrollt) darf
// tab NIEMALS zum Chat-Anfang springen.
func TestBlockListFocusStartsAtViewport(t *testing.T) {
	l := NewBlockList()
	for i := 0; i < 8; i++ {
		l = l.Append(NewCodeBlock("go", fmt.Sprintf("block %d", i), nil))
	}
	l = l.SetSize(40, 4) // winzig: nur ~1 Block sichtbar
	l = l.ScrollEnd()
	if l.offset == 0 {
		t.Fatal("precondition: chat scrolled down")
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyDown})
	if l.focus < 6 {
		t.Fatalf("tab must start at the viewport, not the chat start: focus=%d", l.focus)
	}
	if l.offset < l.focus {
		t.Fatalf("reveal failed: offset=%d focus=%d", l.offset, l.focus)
	}
}

// TestHeadingCopyGrabsSection: c/alt+<n>/y auf einer Ueberschrift kopieren den
// GESAMTEN Abschnitt bis zur naechsten Ueberschrift — nicht nur die Zeile.
func TestHeadingCopyGrabsSection(t *testing.T) {
	// Zwei Abschnitte: A (Text+Code) und B (Text) — A-Kopie enthaelt alles
	// ausser B.
	r := newRig(t)
	r.seed("# Alpha\n\ntext a\n\n```go\nx := 1\n```\n\n# Beta\n\ntext b\n")
	r.key("tab") // Antwort-Modus: Fokus auf dem letzten fokussierbaren Block
	// Fokus-Kette (FR-40): #Alpha(0) text a(1) code(2) #Beta(3) text b(4)
	// — Fokus steht auf text b, ↑ hebt ihn auf #Beta.
	r.key("up")
	r.key("c")
	if len(r.copied) == 0 {
		t.Fatal("c auf Beta hat nicht kopiert")
	}
	if got := r.copied[len(r.copied)-1]; !strings.Contains(got, "# Beta") || !strings.Contains(got, "text b") || strings.Contains(got, "x := 1") {
		t.Fatalf("Beta-Abschnitt falsch: %q", got)
	}
	// ↑↑↑ auf #Alpha (FR-40: text a ist eigenes Segment dazwischen):
	// c kopiert Titel+Text+Code aus A, nichts aus B.
	r.key("up") // Code-Block
	r.key("up") // text a
	r.key("up") // #Alpha
	r.key("c")
	if len(r.copied) < 2 {
		t.Fatal("c auf Alpha hat nicht kopiert")
	}
	got := r.copied[len(r.copied)-1]
	for _, want := range []string{"# Alpha", "text a", "```go", "x := 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Alpha-Abschnitt fehlt %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Beta") || strings.Contains(got, "text b") {
		t.Fatalf("Alpha-Abschnitt darf B nicht enthalten: %q", got)
	}
}

// TestCopyByIDOnHeadingCopiesSection: auch alt+<n> (Blocknummer) und y
// (Markierung) ziehen auf einer Ueberschrift den ganzen Abschnitt.
func TestCopyByIDOnHeadingCopiesSection(t *testing.T) {
	var got string
	l := NewBlockList().SetCopy(func(s string) tea.Cmd {
		got = s
		return nil
	})
	l = l.Append(NewHeadingBlock("A"), NewTextBlock("p", ""), NewCodeBlock("go", "x := 1", nil), NewHeadingBlock("B"), NewTextBlock("q", ""))
	// IDs: A=1, Code=2, B=3.
	if _, _, ok := l.copyByID(1); !ok {
		t.Fatal("alt+1 auf Heading hat nicht kopiert")
	}
	if !strings.Contains(got, "A") || !strings.Contains(got, "p") || !strings.Contains(got, "```go") || strings.Contains(got, "B") {
		t.Fatalf("alt+1 Abschnitt falsch: %q", got)
	}
	// y auf markierter Ueberschrift: Abschnitt statt Zeile.
	l = l.setFocus(3) // #Beta
	l = l.toggleMarkFocused()
	if md := l.markedMarkdown(); !strings.Contains(md, "B") || !strings.Contains(md, "q") || strings.Contains(md, "x := 1") {
		t.Fatalf("y-Abschnitt falsch: %q", md)
	}
}
