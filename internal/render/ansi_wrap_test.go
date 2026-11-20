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

// TestWrapWordsContinuesOpenStyleAcrossLines (User-Report: "fettgedruckter
// text wird nicht richtig angezeigt, wenn ich im auswahlmode mit up/down
// bin"): Ein umgebrochener fett-Absatz verliert ab Zeile 2 sein \x1b[1m —
// der Prosa-Wrap trug die aktive Formatierung nicht über die Zeilenkante.
// Jede Zeile INNERHALB des fetten Segments muss den Opener tragen, die
// Zeile NACH dem Zwei-Code-Closer muss plain sein (kein wiedergeöffnetes
// Bold, kein leeres "\x1b[1m\x1b[22m"-Paar).
func TestWrapWordsContinuesOpenStyleAcrossLines(t *testing.T) {
	md := "**Ganze Absatz Fett der auch umbricht damit der Wrap über zwei Zeilen geht und beim Selektieren etwas passiert.**"
	nodes, err := Parse(md)
	if err != nil {
		t.Fatal(err)
	}
	text := nodes[0].Text
	found := 0
	for w := 20; w <= 120; w++ {
		lines := WrapWords(text, w)
		if len(lines) < 2 {
			continue
		}
		found++
		prevOpen := false
		for _, ln := range lines {
			open, close := strings.Contains(ln, "\x1b[1m"), strings.Contains(ln, "\x1b[22m")
			if strings.Contains(ln, "\x1b[1m\x1b[22m") {
				t.Errorf("w=%d leeres Stil-Paar auf Folgeline: %q", w, ln)
			}
			if prevOpen && !open {
				t.Errorf("w=%d Folgeline verliert offenes Bold: %q", w, ln)
			}
			prevOpen = open && !close
		}
	}
	if found == 0 {
		t.Fatal("Testtext bricht bei keiner Breite um — Aussagekraft null")
	}
}

// TestWrapWordsClosesStyleOnContinuation: endet ein Inline-Stil MITTEN im
// umgebrochenen Absatz, dürfen die Zeilen nach dem Closer NICHT wieder
// fett öffnen (SGRChain entfernt geschlossene Opener statt sie mitzu-
// schleppen — sonst lebte der Stil auf Folgelines unsterblich weiter).
func TestWrapWordsClosesStyleOnContinuation(t *testing.T) {
	md := "Normaler Text davor und **fett mit genug Worten dass der Umbruch mitten im fetten Segment passiert und noch mehr Worte hier folgen** sowie Text danach."
	nodes, err := Parse(md)
	if err != nil {
		t.Fatal(err)
	}
	text := nodes[0].Text
	found := 0
	for w := 24; w <= 120; w++ {
		lines := WrapWords(text, w)
		if len(lines) < 3 {
			continue
		}
		found++
		lastClose := -1
		for i, ln := range lines {
			if strings.Contains(ln, "\x1b[22m") {
				lastClose = i
			}
		}
		for i, ln := range lines {
			if i > lastClose && strings.Contains(ln, "\x1b[1m") {
				t.Errorf("w=%d Zeile nach dem Closer oeffnet Bold wieder: %q", w, ln)
			}
		}
	}
	if found == 0 {
		t.Fatal("Testtext bricht bei keiner Breite über 3 Zeilen — Aussagekraft null")
	}
}

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
