package tui

import (
	"strings"
	"testing"
	"time"
)

// TestWorkPodCapsuleClosed (User-Prototyp 21.11.): Jede Working-on-Kapsel
// schliesst sich SELBST — Kopf `╰─┬──── / …`, Inhalt `   ┆ …`,
// Abschluss `   ╰────`. Nach einem ┬-Kopf folgen IMMER Inhalt und
// Abschluss (nichts zeigt zeitversetzt ins Leere); ein Pod ohne Inhalt
// hat gar keinen Stamm (┬→─). Leerlauf ist stammblos.
func TestWorkPodCapsuleClosed(t *testing.T) {
	a := NewActivityTree(nil).
		Tool(ToolUpdate{CallID: "r", Label: "bash", Kind: "execute", Command: "go test ./..."})
	a = a.Tool(ToolUpdate{CallID: "x", Label: "read_file", Text: "docs/spec.md"})
	a = a.Thought("Hallo im Terminal!\nAlles sauber eingerahmt.")
	a.liveStart = time.Now().Add(-800 * time.Millisecond)

	for _, w := range []int{60, 40, 24} {
		for _, budget := range []int{6, 9, 12, 20} {
			rows := plain(a.View(w, budget))
			joined := strings.Join(rows, "\n")
			if !strings.Contains(joined, "Working on") && budget >= 4 {
				t.Fatalf("w%d b%d: Working on fehlt:\n%s", w, budget, joined)
			}
			// Jeder ┬-Kopf wird von ┆-Inhalt und ╰──── abgeschlossen.
			open := false
			for _, r := range rows {
				switch {
				case strings.Contains(r, "┬"):
					if open {
						t.Fatalf("w%d b%d: offener Kopf ohne Abschluss:\n%s", w, budget, joined)
					}
					open = true
				case strings.HasPrefix(r, " ┆") || strings.Contains(r, "┆"):
					if !open {
						t.Fatalf("w%d b%d: Inhalt ohne Kopf:\n%s", w, budget, joined)
					}
				case strings.Contains(r, "╰────") && open:
					open = false
				}
			}
			if open {
				t.Fatalf("w%d b%d: Kapsel ungeschlossen:\n%s", w, budget, joined)
			}
			// Keine Zeile breiter als das Terminal.
			for i, r := range rows {
				if cw := runewidth0(r); cw > w {
					t.Fatalf("w%d b%d: Zeile %d zu breit (%d): %q", w, budget, i, cw, r)
				}
			}
		}
	}
	// Leerlauf: stammbloser Spinner-Kopf, kein ┬, kein hängender Stamm.
	idle := NewActivityTree(nil).Idle(time.Now())
	idle.idleFrame = 3
	irows := plain(idle.View(60, 20))
	ij := strings.Join(irows, "\n")
	if strings.Contains(ij, "┬") || strings.Contains(ij, "┆") {
		t.Fatalf("Leerlauf hat keinen Stamm:\n%s", ij)
	}
	if !strings.Contains(ij, "╰──────") || !strings.Contains(ij, "⠸") {
		t.Fatalf("Leerlauf-Spinner fehlt:\n%s", ij)
	}
}
