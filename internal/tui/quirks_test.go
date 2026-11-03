package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"heron/internal/render"
)

// TestQuirksFileRendersAllWidths: Die Quirks-Referenz (docs/quirks-test.md)
// muss durch die komplette Widget-Pipeline ohne Panic durchgehen — bei
// jeder Breite, auch bei der Minimalbreite 16 (COLUMNS-Stress). Ein neuer
// Sonderfall in der Datei deckt damit automatisch Layout-Crashes auf.
func TestQuirksFileRendersAllWidths(t *testing.T) {
	src, err := os.ReadFile("../../docs/quirks-test.md")
	if err != nil {
		t.Fatalf("quirks file missing: %v", err)
	}
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := render.Parse(string(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	widgets := BuildWidgets(nodes, th, nil)
	if len(widgets) < 40 {
		t.Fatalf("expected a rich block mix, got %d widgets", len(widgets))
	}
	for _, w := range []int{16, 20, 30, 60, 90, 200} {
		var out strings.Builder
		for _, it := range widgets {
			view := it.View(w)
			out.WriteString(view)
			out.WriteString("\n")
			// Jede View-Zeile muss die Zielbreite einhalten — Ueberlauf
			// zerstoert die Blockliste (Geometrie == Anzeige).
			for _, ln := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
				if got := lipgloss.Width(ln); got > w {
					t.Fatalf("width %d: line overflows (%d cells): %q", w, got, ln)
				}
			}
		}
		if len(out.String()) == 0 {
			t.Fatalf("width %d: empty render", w)
		}
	}
}
