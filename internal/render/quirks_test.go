package render

import (
	"os"
	"strings"
	"testing"
)

// quirksPath ist die Markdown-Quirks-Referenz (docs/quirks-test.md) — dieselbe
// Datei, die auch manuell gesichtet wird. Ein Test hier hält die Datei und
// den Parser zusammen: erweitern wir die Referenz, laufen die Assertions
// automatisch ueber die neuen Faelle.
const quirksPath = "../../docs/quirks-test.md"

// TestSoftBreakKeepsTrailingText: SoftLineBreak haengt am LETZTEN Text-Knoten
// der Zeile — der Walker musste frueher nur den Umbruch schreiben und warf
// genau dieses Stueck weg (Gedankenstriche, Klammern, ganze Woerter
// verschwanden still aus mehrzeiligen Absaetzen).
func TestSoftBreakKeepsTrailingText(t *testing.T) {
	nodes, err := Parse("erste Zeile mit Gedankenstrich —\nzweite Zeile")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Kind != NodeText {
		t.Fatalf("want one text node, got %+v", nodes)
	}
	want := "erste Zeile mit Gedankenstrich —\nzweite Zeile"
	if nodes[0].Text != want {
		t.Fatalf("soft break lost text: %q", nodes[0].Text)
	}
}

// TestHardBreakSplitsLines: Hard Breaks (zwei Leerzeichen oder Backslash am
// Zeilenende) muessen einen Umbruch erzeugen — frueher klebten die Zeilen
// ohne jedes Trennzeichen zusammen ("Zeile AZeile B").
func TestHardBreakSplitsLines(t *testing.T) {
	src := "Zeile A  \nZeile B\\\nZeile C"
	nodes, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "Zeile A\nZeile B\nZeile C"
	if nodes[0].Text != want {
		t.Fatalf("hard breaks glued: %q", nodes[0].Text)
	}
}

// TestAutoLinkTextSurvives: AutoLinks tragen die URL in einem privaten Feld
// (kein Text-Kind im Walk) — ohne Sonderbehandlung verschwanden <https://…>,
// GFM-Linkify-URLs und E-Mail-Adressen komplett aus dem Text.
func TestAutoLinkTextSurvives(t *testing.T) {
	src := "Siehe <https://example.invalid/x>, bare www.example.invalid/a und mail@example.invalid."
	nodes, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	txt := nodes[0].Text
	for _, want := range []string{"https://example.invalid/x", "www.example.invalid/a", "mail@example.invalid"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("autolink %q missing from %q", want, txt)
		}
	}
}

// TestTaskCheckBoxSurvives: Der GFM-TaskList-Parser frisst "[x]"/"[ ]" aus
// dem Quelltext — ohne Zurueckschreiben war der Haken-Stil weg.
func TestTaskCheckBoxSurvives(t *testing.T) {
	nodes, err := Parse("- [x] fertig\n- [ ] offen\n- [X] gross")
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, n := range nodes {
		joined += n.Text + "\n"
	}
	// goldmark traegt nur den Haken-Zustand (bool) — das Glyph normalisiert
	// auf [x]/[ ], die Gross-/Kleinschreibung des Quelltexts geht verloren.
	for _, want := range []string{"[x] fertig", "[ ] offen", "[x] gross"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("task %q missing from %q", want, joined)
		}
	}
}

// TestIndentedCodeBlockSurvives: Vier-Spaces-Code (ast.CodeBlock) ist ein
// eigener Knoten — der Walker warf den Block frueher komplett weg
// (Datenverlust: der Inhalt verschwand ohne jede Spur).
func TestIndentedCodeBlockSurvives(t *testing.T) {
	src := "    func eingerueckt() {\n        return 1\n    }"
	nodes, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Kind != NodeCode {
		t.Fatalf("want one code node, got %+v", nodes)
	}
	if !strings.Contains(nodes[0].Code, "func eingerueckt()") {
		t.Fatalf("code content lost: %q", nodes[0].Code)
	}
}

// TestQuirksFileParses: Die Quirks-Referenz muss fehlerfrei parsen und eine
// gesunde Blockmischung liefern (Text, Heading, Code, Tabelle) — erweitern
// wir die Datei, laeuft dieser Test automatisch mit.
func TestQuirksFileParses(t *testing.T) {
	src, err := os.ReadFile(quirksPath)
	if err != nil {
		t.Fatalf("quirks file missing: %v", err)
	}
	nodes, err := Parse(string(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	counts := map[NodeKind]int{}
	for _, n := range nodes {
		counts[n.Kind]++
		if n.Kind == NodeCode && strings.Count(n.Code, "\n") > 0 && !strings.HasSuffix(n.Code, "\n") {
			t.Fatalf("multi-line code block lost its trailing newline — fence detection broken: %q…", n.Code[:30])
		}
	}
	if counts[NodeText] < 20 {
		t.Fatalf("too few text blocks: %d", counts[NodeText])
	}
	if counts[NodeHeading] < 12 {
		t.Fatalf("too few headings: %d", counts[NodeHeading])
	}
	if counts[NodeCode] < 10 {
		t.Fatalf("too few code blocks: %d", counts[NodeCode])
	}
	if counts[NodeTable] < 7 {
		t.Fatalf("too few tables: %d", counts[NodeTable])
	}
}
