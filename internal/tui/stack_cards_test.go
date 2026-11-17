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

// Einzelne Karte bleibt unverändert gerahmt (Regression gegen das
// bisherige Einkarten-Verhalten).
func TestStackSingleCardUntouched(t *testing.T) {
	th := &render.Theme{}
	l := BlockList{}
	l = l.Append(NewThinkBlock("nur ich", th))
	view := l.View()
	if !strings.Contains(view, "┌─") || !strings.Contains(view, "└") {
		t.Fatalf("single card must keep its frame:\n%s", view)
	}
	if strings.Contains(view, "├─") {
		t.Fatalf("single card must not have a divider:\n%s", view)
	}
}
