package tui

import (
	"strings"
	"testing"

	"heron/internal/render"
)

// FR stack: think- und tool-Karten direkt hintereinander teilen EINEN
// Rahmen (User-Skizze) — ┌┐ nur oben, ├┤ je Übergang, └┘ nur unten.
func TestStackJoinAdjacentCards(t *testing.T) {
	th := &render.Theme{}
	mk := func(l BlockList, w ...Widget) BlockList { return l.Append(w...) }
	l := BlockList{}
	l = mk(l, NewThinkBlock("ich denke nach", th))
	l = mk(l, NewToolBlock(ToolData{CallID: "c1", Title: "fetch_mooncheese", Kind: "fetch", Text: "flurb"}, th, nil))

	view := l.View()
	if got := strings.Count(view, "┌"); got != 1 {
		t.Fatalf("want 1 top border, got %d:\n%s", got, view)
	}
	if got := strings.Count(view, "└"); got != 1 {
		t.Fatalf("want 1 bottom border, got %d:\n%s", got, view)
	}
	if !strings.Contains(view, "├─") {
		t.Fatalf("divider missing:\n%s", view)
	}
	// Höhe = Summe der gejointen Einzelhöhen — View und heightAt müssen
	// exakt übereinstimmen, sonst driften Offset/Clip/Fenster.
	w := l.Width()
	if want := l.heightAt(0, w) + l.heightAt(1, w); len(strings.Split(view, "\n")) != want {
		t.Fatalf("view lines != joined heights: %d vs %d\n%s", len(strings.Split(view, "\n")), want, view)
	}
	if l.totalLines() != len(strings.Split(view, "\n")) {
		t.Fatalf("totalLines %d != view lines %d", l.totalLines(), len(strings.Split(view, "\n")))
	}
}

// Drei Karten hintereinander: zwei Divider, ein Rahmen.
func TestStackJoinThreeCards(t *testing.T) {
	th := &render.Theme{}
	l := BlockList{}
	l = l.Append(NewThinkBlock("schritt 1", th))
	l = l.Append(NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "execute", Text: "x"}, th, nil))
	l = l.Append(NewToolBlock(ToolData{CallID: "c2", Title: "t2", Kind: "edit", Text: "y"}, th, nil))
	view := l.View()
	if got := strings.Count(view, "├─"); got != 2 {
		t.Fatalf("want 2 dividers, got %d:\n%s", got, view)
	}
	if got := strings.Count(view, "┌"); got != 1 {
		t.Fatalf("want 1 frame top, got %d:\n%s", got, view)
	}
}

// Text zwischen den Karten beendet den Stack — zwei eigene Rahmen.
func TestStackBrokenByText(t *testing.T) {
	th := &render.Theme{}
	l := BlockList{}
	l = l.Append(NewThinkBlock("denke", th))
	l = l.Append(NewTextBlock("Antwort des Agents", "1"))
	l = l.Append(NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "fetch", Text: "x"}, th, nil))
	view := l.View()
	if got := strings.Count(view, "┌"); got != 2 {
		t.Fatalf("text must break the stack, want 2 frames, got %d:\n%s", got, view)
	}
	if strings.Contains(view, "├─") {
		t.Fatalf("no divider expected across text:\n%s", view)
	}
}

// Der User-Fall: DENKEN → TOOL → DENKEN → TOOL → DENKEN mit jeweils
// verstecktem Inhalt — ALLE Karten verbinden sich zu EINEM Kasten (1 ┌,
// 1 └, 4 Divider). Zugeklappte Karten schliessen NICHT vorzeitig mit
// └[+ show more]┘ — die Falt-Marke lebt im Kopf, der Boden ist immer die
// Rahmenlinie (User-Report: "nur die ersten 2 verbinden" / "Boxen enden
// zu früh mit dem Border").
func TestStackConnectsFoldedCards(t *testing.T) {
	th := &render.Theme{}
	long := func(seed string) string {
		return seed + "\naaa\nbbb\nccc\nddd\neee\nfff" // > collapsed-Cap → ▸
	}
	l := BlockList{}
	l = l.Append(NewThinkBlock(long("gedanke 1"), th))
	l = l.Append(NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "execute", Text: long("out 1")}, th, nil))
	l = l.Append(NewThinkBlock(long("gedanke 2"), th))
	l = l.Append(NewToolBlock(ToolData{CallID: "c2", Title: "t2", Kind: "fetch", Text: long("out 2")}, th, nil))
	l = l.Append(NewThinkBlock(long("gedanke 3"), th))

	view := l.View()
	if got := strings.Count(view, "┌"); got != 1 {
		t.Fatalf("want exactly 1 top border, got %d:\n%s", got, view)
	}
	if got := strings.Count(view, "└"); got != 1 {
		t.Fatalf("want exactly 1 bottom border, got %d:\n%s", got, view)
	}
	if got := strings.Count(view, "├─"); got != 4 {
		t.Fatalf("want 4 dividers between the 5 cards, got %d:\n%s", got, view)
	}
	if strings.Contains(view, "show more") || strings.Contains(view, "show less") {
		t.Fatalf("fold action line must be gone:\n%s", view)
	}
	// Jede Karte traegt ihre Falt-Marke im Kopf (▸): 5 Cards, 5 Marks.
	if got := strings.Count(view, "▸"); got != 5 {
		t.Fatalf("want 5 fold marks (one per card header), got %d:\n%s", got, view)
	}
	// View-Höhe bleibt exakt die Summe der gejointen Einzelhöhen.
	w := l.Width()
	want := 0
	for i := 0; i < 5; i++ {
		want += l.heightAt(i, w)
	}
	if len(strings.Split(view, "\n")) != want {
		t.Fatalf("view lines != joined heights: %d vs %d\n%s", len(strings.Split(view, "\n")), want, view)
	}
}

// Auswahl faerbt die GESAMTE Kartenflaeche: Der fokussierten Karte wird
// jede Zeile mit der Akzentfarbe gefuellt (bg + Rahmen, symmetrisch) —
// der Nachbarkartenzustand bleibt unangetastet.
func TestStackSelectionFillsWholeCard(t *testing.T) {
	withTrueColor(t)
	th, _ := render.DefaultTheme()
	tool := NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "execute", Text: "x"}, th, nil).
		SetFocused(true).(ToolBlock)
	l := BlockList{}.Append(NewThinkBlock("gedanke", th), tool)

	view := l.View()
	for i, ln := range strings.Split(tool.View(l.Width()), "\n") {
		if !strings.Contains(ln, "48;2;191;0;255") {
			t.Fatalf("selected tool row %d is not filled: %q", i, ln)
		}
	}
	if !strings.Contains(view, "48;2;191;0;255") {
		t.Fatal("selection fill missing from the stack view")
	}
}
