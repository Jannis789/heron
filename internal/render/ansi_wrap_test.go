package render

import (
	"regexp"
	"strings"
	"testing"
)

// TestWrapDisplayKeepsAnsiSequencesWhole: ein harter Umbruch darf eine
// SGR-Sequenz nie zerschneiden — ein halber Code erscheint im Terminal als
// sichtbarer Muell ("[1msonstiges:[22m", User-Report). Hier fuellt exakt
// w=5 die Zeile an der Escape-Kante: frueher landete "\x1b" allein am
// Zeilenende und "[1m" am Anfang der Folgeseite.
func TestWrapDisplayKeepsAnsiSequencesWhole(t *testing.T) {
	s := "aaaa\x1b[1mbbbb\x1b[22mcccc"
	for w := 1; w <= 12; w++ {
		lines := WrapDisplay(s, w)
		for _, ln := range lines {
			rest := ansiRe.ReplaceAllString(ln, "")
			if strings.Contains(rest, "[") && regexp.MustCompile(`\[[0-9]+m`).MatchString(rest) {
				t.Errorf("w=%d zerschnittene Sequenz: %q", w, ln)
			}
			// Eine Sequenz muss komplett in einer Zeile liegen: nach dem
			// Strippen duerfen keine ESC-Bytes uebrig bleiben.
			if strings.Contains(rest, "\x1b") || strings.Contains(ln, "\x1b\x1b") {
				t.Errorf("w=%d verwaistes ESC: %q", w, ln)
			}
		}
	}
}

// TestWrapDisplayReemitsActiveStyle: bricht der Umbruch MITTEN in einem
// formatierten Token, setzt die Folgeseite den aktiven SGR fort — der Stil
// endet nicht an der Zeilenkante.
func TestWrapDisplayReemitsActiveStyle(t *testing.T) {
	lines := WrapDisplay("aaaa\x1b[1mbbbbbb", 5)
	if len(lines) < 2 {
		t.Fatalf("zu wenig Zeilen: %q", lines)
	}
	if !strings.HasPrefix(lines[1], "\x1b[1m") {
		t.Errorf("Folgeseite verliert den Stil: %q", lines[1])
	}
}

// TestTextWrapNoVisibleAnsi (Integration ueber Parse->BuildWidgets): Prosa
// mit Code-Spans/Fett muss bei JEDER Terminalbreite ohne sichtbare
// Sequenz-Fragmente umbrechen (User-Report "[1msonstiges:[22m").
func TestTextWrapNoVisibleAnsi(t *testing.T) {
	md := "Einleitung, **Sonstiges:** und `/nix/store/nq93ihn17r25cg08kkl300ix4dk6b54f-heron-0.1.0` als langes Token, ***fett kursiv*** Ende."
	nodes, err := Parse(md)
	if err != nil {
		t.Fatal(err)
	}
	frag := regexp.MustCompile(`\[[0-9]+m`)
	for _, n := range nodes {
		if n.Kind != NodeText {
			continue
		}
		for w := 8; w <= 120; w++ {
			for _, ln := range WrapWords(n.Text, w) {
				if frag.MatchString(ansiRe.ReplaceAllString(ln, "")) {
					t.Errorf("w=%d sichtbares Fragment: %q", w, ln)
				}
			}
		}
	}
}
