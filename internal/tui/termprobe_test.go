package tui

import "testing"

// TestParseTermCapsGhostty prueft die ECHTE Ghostty-Antwort auf die
// Probe-Fragen: XTWINOPS-16t-Zellpixel (ESC[6;h;wt — endet auf 't',
// nicht auf dem 'R' der CPR-Antwort!) plus die beiden a=q-OKs.
// Der alte Parser suchte 'R' als Trenner und Atoi("9t…") schlug fehl,
// weshalb CellW/CellH in echten Terminals NIE gesetzt wurden.
func TestParseTermCapsGhostty(t *testing.T) {
	esc := string(rune(0x1b))
	st := string(rune(0x5c)) // RÜCKSCHRÄGSTRICH (ST-Terminator)
	buf := esc + "[6;20;9t" +
		esc + "_Gi=31;OK" + esc + st +
		esc + "_Gi=32;OK" + esc + st +
		esc + "[41;109R"
	caps := parseTermCaps(buf)
	if !caps.Graphics {
		t.Fatal("graphics nicht erkannt")
	}
	if !caps.Placeholders {
		t.Fatal("placeholders nicht erkannt")
	}
	if caps.CellW != 9 || caps.CellH != 20 {
		t.Fatalf("zellpixel %dx%d will 9x20", caps.CellW, caps.CellH)
	}
}

func TestParseTermCapsNoCell(t *testing.T) {
	caps := parseTermCaps("muell ohne antworten")
	if caps.CellW != 0 || caps.CellH != 0 || caps.Graphics || caps.Placeholders {
		t.Fatalf("leerer puffer darf keine caps liefern: %+v", caps)
	}
}
