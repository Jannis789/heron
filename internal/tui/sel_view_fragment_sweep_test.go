package tui

import (
	"regexp"
	"strings"
	"testing"
)

// View-Sweep: ALLE Selektions-/Zustands-Paintings (Block-Selektion Reverse,
// residual stale, focused underline, char range+caret, marked per Klick)
// ueber den VOLLEN Model-View (inkl. fillScreen) auf sichtbare SGR-Fragmente.
func TestViewStateFragmentSweep(t *testing.T) {
	withTrueColor(t)
	frag := regexp.MustCompile(`\[[0-9]+m`)
	check := func(tag string, w int, v string) {
		plain := stripANSI(v)
		if frag.MatchString(plain) {
			for i, ln := range strings.Split(v, "\n") {
				p := stripANSI(ln)
				if frag.MatchString(p) {
					t.Fatalf("%s w=%d: Zeile %d Fragment: %q", tag, w, i, p)
				}
			}
		}
	}
	text := "# **Sonstiges:** Überschrift mit fett\n\n**Sonstiges:**\n- `diagram.puml` – PlantUML\n\nUnd **fett im Text** am Ende."
	for w := 8; w <= 160; w++ {
		// 1) Block-Selektion (tab + shift+down) — Reverse-Markierung im View
		r := newRig(t).seed(text)
		r.size(w, 30)
		r.key("tab")
		for s := 0; s < 8; s++ {
			r.key("shift+down")
			check("blocksel", w, r.m.View())
		}
		// c danach (Copy-Checks separat), esc zurueck
		r.key("esc")

		// 2) residual (Nach esc bleibt die Auswahl sichtbar — stale)
		r2 := newRig(t).seed(text)
		r2.size(w, 30)
		r2.key("tab")
		for s := 0; s < 3; s++ {
			r2.key("shift+down")
		}
		r2.key("esc") // residual selection shown
		check("residual", w, r2.m.View())
		r2.key("c")
		check("residual-copy-view", w, r2.m.View())

		// 3) char mode: caret move + shift up/down + left/right Kombis
		r3 := newRig(t).seed(text)
		r3.size(w, 30)
		r3.key("shift+tab")
		if !r3.m.chat.charActive() {
			continue
		}
		for s := 0; s < 30; s++ {
			switch s % 4 {
			case 0:
				r3.key("shift+down")
			case 1:
				r3.key("shift+up")
			case 2:
				r3.key("shift+right")
			case 3:
				r3.key("shift+left")
			}
			check("charsel", w, r3.m.View())
		}
		// enter kopiert im char mode
		r3.key("enter")
		check("charsel-after-enter", w, r3.m.View())

		// 4) Maus-Klick-Markierung + c
		r4 := newRig(t).seed(text)
		r4.size(w, 30)
		// Klick mitten in den ersten Textblock (x irgendwo im Text, y Zeile 2)
		r4.click(w / 2, 2)
		check("marked", w, r4.m.View())
		r4.key("tab")
		r4.key("c")
		check("marked-copy-view", w, r4.m.View())
	}
}
