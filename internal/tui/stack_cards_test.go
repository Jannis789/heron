package tui

import (
	"strings"
	"testing"

	"heron/internal/render"
)

// FR stack: think- und tool-Karten direkt hintereinander teilen EINEN
// Rail-Lauf (User-Skizze). Seit dem User-Feedback "horizontale
// Rahmenzeichen entfernt" sind das KEINE ┌┐/├┤/└┘-Kaesten mehr: Es gibt
// NUR NOCH linke Rails "│" — der Stack zeigt eine Kopfzeile oben und
// getrennte Inhalte durch reine Leerzeilen (Rail-Abstand).
func TestStackJoinAdjacentCards(t *testing.T) {
	th := &render.Theme{}
	l := BlockList{}.Append(
		NewThinkBlock("ich denke nach", th),
		NewToolBlock(ToolData{CallID: "c1", Title: "fetch_mooncheese", Kind: "fetch", Text: "flurb"}, th, nil))

	view := l.View()
	if strings.Contains(view, "┌") || strings.Contains(view, "└") ||
		strings.Contains(view, "├") || strings.Contains(view, "┐") ||
		strings.Contains(view, "┘") || strings.Contains(view, "┤") {
		t.Fatalf("horizontale Rahmenzeichen sind verboten:\n%s", view)
	}
	if strings.Count(view, "thinking") != 1 || strings.Count(view, "↯ fetch_mooncheese") != 1 {
		t.Fatalf("jede Karte traegt ihren Kopf:\n%s", view)
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

// Drei Karten hintereinander: nur die erste traegt eine Kopfzeile.
func TestStackJoinThreeCards(t *testing.T) {
	th := &render.Theme{}
	l := BlockList{}.Append(
		NewThinkBlock("schritt 1", th),
		NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "execute", Text: "x"}, th, nil),
		NewToolBlock(ToolData{CallID: "c2", Title: "t2", Kind: "edit", Text: "y"}, th, nil))
	view := l.View()
	if strings.Count(view, "thinking") != 1 || strings.Count(view, "$ t1") != 1 || strings.Count(view, "✎ t2") != 1 {
		t.Fatalf("jede Karte traegt ihren Kopf:\n%s", view)
	}
}

// Text zwischen den Karten beendet den Stack — zwei Rail-Laeufe mit je
// einer Kopfzeile.
func TestStackBrokenByText(t *testing.T) {
	th := &render.Theme{}
	l := BlockList{}.Append(
		NewThinkBlock("denke", th),
		NewTextBlock("Antwort des Agents", "1"),
		NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "fetch", Text: "x"}, th, nil))
	view := l.View()
	if strings.Count(view, "thinking") != 1 || strings.Count(view, "↯ t1") != 1 {
		t.Fatalf("want one header per rail run:\n%s", view)
	}
}

// Der User-Fall: DENKEN → TOOL → DENKEN → TOOL → DENKEN mit jeweils
// verstecktem Inhalt — ALLE Karten laufen in EINEM Rail-Lauf (1 Kopfzeile).
// Die Falt-Marke lebt in DEREN Kopfzeile, es gibt keine show-more-Zeile.
func TestStackConnectsFoldedCards(t *testing.T) {
	th := &render.Theme{}
	long := func(seed string) string {
		return seed + "\naaa\nbbb\nccc\nddd\neee\nfff" // > collapsed-Cap → ▸
	}
	l := BlockList{}.Append(
		NewThinkBlock(long("gedanke 1"), th),
		NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "execute", Text: long("out 1")}, th, nil),
		NewThinkBlock(long("gedanke 2"), th),
		NewToolBlock(ToolData{CallID: "c2", Title: "t2", Kind: "fetch", Text: long("out 2")}, th, nil),
		NewThinkBlock(long("gedanke 3"), th))

	view := l.View()
	if strings.Contains(view, "┌") || strings.Contains(view, "└") || strings.Contains(view, "├─") {
		t.Fatalf("horizontale Rahmenzeichen sind verboten:\n%s", view)
	}
	if strings.Contains(view, "show more") || strings.Contains(view, "show less") {
		t.Fatalf("fold action line must be gone:\n%s", view)
	}
	// Jede Karte traegt Kopf UND Falt-Marke (▸) im Rail-Lauf: 5 Karten.
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

// Auswahl highlightet NUR die Kopfzeile: Der Neon lebt im Caption-Text
// (fg), die Kartenflaeche bleibt grau wie der Text (User-Feedback).
func TestStackSelectionHighlightsHeaderOnly(t *testing.T) {
	withTrueColor(t)
	th, _ := render.DefaultTheme()
	tool := NewToolBlock(ToolData{CallID: "c1", Title: "t1", Kind: "execute", Text: "x"}, th, nil).
		SetFocused(true).(ToolBlock)
	l := BlockList{}.Append(NewThinkBlock("gedanke", th), tool)

	view := l.View()
	rows := strings.Split(tool.View(l.Width()), "\n")
	if !strings.Contains(rows[0], "38;2;191;0;255") {
		t.Fatalf("selected tool header is not neon: %q", rows[0])
	}
	// Kein Fuell-Neon: NIRGENDWO traegt eine Zeile den Akzent als
	// Hintergrund — die Auswahl zeigt sich allein im Kopf.
	for i, ln := range rows {
		if strings.Contains(ln, "48;2;191;0;255") {
			t.Fatalf("selected tool row %d still filled: %q", i, ln)
		}
	}
	if !strings.Contains(view, "38;2;191;0;255") {
		t.Fatal("selection neon missing from the stack view")
	}
	// Der Nachbarkartenzustand bleibt unangetastet: die ruhige Karte zeigt
	// kein Neon.
	if rest := NewThinkBlock("gedanke", th).View(l.Width()); strings.Contains(rest, "191;0;255") {
		t.Fatalf("unselected card carries neon: %q", rest)
	}
}

// Rails sind GRAU wie der Text in den Karten (User-Feedback): Der
// Ruhe-Rahmen traegt NICHT mehr die Pink-Purple-Akzentfarbe.
func TestCardRailsAreGreyLikeText(t *testing.T) {
	withTrueColor(t)
	th, _ := render.DefaultTheme()
	b := NewThinkBlock("eins", th)
	rows := strings.Split(b.View(60), "\n")
	for i, ln := range rows {
		if strings.Contains(ln, "205;116;243") {
			t.Fatalf("row %d still pink-purple, want grey rails: %q", i, ln)
		}
	}
}
