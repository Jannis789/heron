package tui

import (
	"regexp"
	"strings"
	"testing"
)

// Sweep: Original-User-Text + meine ls-Antwort durch BlockList mit gemalter
// Zeichen-Selektion bei JEDER Breite — Fragment-Regex wie TestTextWrapNoVisibleAnsi.
func TestSweepSelectionPainting(t *testing.T) {
	withTrueColor(t)
	frag := regexp.MustCompile(`\[[0-9]+m`)
	mds := []string{
		"Einleitung, **Sonstiges:** und `/nix/store/nq93ihn17r25cg08kkl300ix4dk6b54f-heron-0.1.0` als langes Token, ***fett kursiv*** Ende.",
		reproMD,
		"**Sonstiges:**\n- `diagram.puml` – PlantUML\n- `tui.test` – 15 MB Binary\n\n**Das ist ein Go-Projekt namens \"heron\":**\n\n- **`main.go`** (15 KB) – Einstiegspunkt",
	}
	for mi, md := range mds {
		for w := 8; w <= 120; w++ {
			r := newRig(t).seed(md)
			r.size(w, 40)
			r.key("shift+tab")
			if !r.m.chat.charActive() {
				continue
			}
			for s := 0; s < 400; s++ {
				r.key("shift+right")
				if !r.m.chat.charActive() {
					break
				}
				v := r.m.chat.View()
				plain := renderStrip(v)
				if frag.MatchString(plain) {
					// Fundstelle lokalisieren
					for i, ln := range strings.Split(v, "\n") {
						p := renderStrip(ln)
						if frag.MatchString(p) {
							t.Fatalf("md=%d w=%d step=%d: Zeile %d Fragment: %q", mi, w, s, i, p)
						}
					}
				}
			}
		}
	}
}

func renderStrip(s string) string { return stripANSI(s) }
