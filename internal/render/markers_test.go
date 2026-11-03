package render

import (
	"strings"
	"testing"
)

// textOf joins the text of parsed nodes (for readable failure messages).
func textOf(nodes []Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(n.Text)
	}
	return b.String()
}

// TestInlineMarkers rendert alle Textmarker mit den richtigen ANSI-Sequenzen:
// **/__ fett (Level 2), */_ kursiv (Level 1), ~~ durchgestrichen — und deren
// Kombinationen/Verschachtelungen (***, ___, **…*…**, ~~**…**~~). Geschlossene
// Marker muessen sauber geklammert sein (keine offenen Codes, kein \x1b[0m, das
// die Basis-Farbe des Blocks loeschen wuerde).
func TestInlineMarkers(t *testing.T) {
	cases := []struct{ src, want string }{
		{"**fett**", "\x1b[1mfett\x1b[22m"},
		{"__fett__", "\x1b[1mfett\x1b[22m"},
		{"*kursiv*", "\x1b[3mkursiv\x1b[23m"},
		{"_kursiv_", "\x1b[3mkursiv\x1b[23m"},
		{"~~strike~~", "\x1b[9mstrike\x1b[29m"},
		{"***fett kursiv***", "\x1b[3m\x1b[1mfett kursiv\x1b[22m\x1b[23m"},
		{"___fett kursiv___", "\x1b[3m\x1b[1mfett kursiv\x1b[22m\x1b[23m"},
		{"**fett und *kursiv***", "\x1b[1mfett und \x1b[3mkursiv\x1b[23m\x1b[22m"},
		{"__a und ~~b~~__", "\x1b[1ma und \x1b[9mb\x1b[29m\x1b[22m"},
		{"~~**fett und durchgestrichen**~~", "\x1b[9m\x1b[1mfett und durchgestrichen\x1b[22m\x1b[29m"},
	}
	for _, c := range cases {
		nodes, err := Parse(c.src)
		if err != nil {
			t.Fatalf("parse %q: %v", c.src, err)
		}
		if len(nodes) != 1 || nodes[0].Text != c.want {
			t.Fatalf("marker %q => %q, want %q", c.src, textOf(nodes), c.want)
		}
	}
}

// TestInlineMarkersEdgeCases: Escaping, Satzzeichen, Marker in Woertern sowie
// leer/offen/mehrdeutige/direkt aufeinanderfolgende Marker muessen robust
// bleiben — kein Textverlust, keine falsche Formatierung, keine offenen ANSI-
// Runs.
func TestInlineMarkersEdgeCases(t *testing.T) {
	bs := string([]byte{0x5c}) // Backslash
	// Backslash-Escapes: \\* verbraucht den Backslash, der Marker bleibt
	// literal stehen (kein Emphase-Styling).
	nodes, err := Parse(bs + "*keine Emphase" + bs + "*")
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].Text != "*keine Emphase*" {
		t.Fatalf("escape: %q", nodes[0].Text)
	}
	// Escaped Backslash (a\\b -> a\b); Backslash vor Nicht-Interpunktion
	// bleibt literal (a\b).
	nodes, _ = Parse("a" + bs + bs + "b")
	if nodes[0].Text != "a"+bs+"b" {
		t.Fatalf("escaped backslash: %q", nodes[0].Text)
	}
	nodes, _ = Parse("a" + bs + "b")
	if nodes[0].Text != "a"+bs+"b" {
		t.Fatalf("non-punct escape must stay literal: %q", nodes[0].Text)
	}
	// In Code-Spans sind Backslashes literal (CommonMark) — das \\* im Span
	// bleibt erhalten.
	nodes, _ = Parse("`" + bs + "*" + "`")
	if nodes[0].Text != bs+"*" {
		t.Fatalf("code span must keep the backslash: %q", nodes[0].Text)
	}
	// Unterstrich in einem Wort ist keine Emphase (CommonMark).
	nodes, _ = Parse("foo_bar_baz")
	if nodes[0].Text != "foo_bar_baz" {
		t.Fatalf("intraword underscore: %q", nodes[0].Text)
	}
	// Intraword-Asterisk ist gueltige Emphase (CommonMark).
	nodes, _ = Parse("foo*bar*baz")
	if nodes[0].Text != "foo\x1b[3mbar\x1b[23mbaz" {
		t.Fatalf("intraword asterisk: %q", nodes[0].Text)
	}
	// Satzzeichen direkt am Marker stoeren die Emphase nicht.
	nodes, _ = Parse("50% *kursiv*,")
	if nodes[0].Text != "50% \x1b[3mkursiv\x1b[23m," {
		t.Fatalf("punctuation: %q", nodes[0].Text)
	}
	// Leere/offene/mehrdeutige Marker: kein Styling, Marker bleiben da, kein
	// Textverlust.
	for _, src := range []string{"** unclosed", "*not closed", "~~~~", "**", "*", "~~ in word~~x"} {
		nodes, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		got := textOf(nodes)
		if strings.Contains(got, "\x1b[") {
			t.Fatalf("lone/ambiguous marker %q styled: %q", src, got)
		}
	}
	// Direkt aufeinanderfolgende Marker: goldmark entscheidet pro CommonMark —
	// die ANSI-Klammerung muss balanciert bleiben (Close == Open je Sequenz).
	nodes, _ = Parse("**a****b**")
	if strings.Count(nodes[0].Text, "\x1b[1m") != strings.Count(nodes[0].Text, "\x1b[22m") {
		t.Fatalf("unbalanced bold: %q", nodes[0].Text)
	}
}
