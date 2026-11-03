package render

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

type NodeKind int

const (
	NodeText NodeKind = iota
	NodeCode
	NodeTable
	// NodeHeading is a markdown section title; it renders on its own row so
	// titles never run into the paragraph that follows.
	NodeHeading
)

// unescapeBackslash loest goldmark-Backslash-Escapes auf: ein Backslash vor
// ASCII-Interpunktion ist ein Escape und gehoert nicht in den gerenderten
// Text (\X -> X). Goldmark legt die Sequenz roh ins Text-Segment, der
// Renderer loest sie selbst auf. Backslash vor Nicht-Interpunktion bleibt
// literal (a\b). Innerhalb von Code-Spans wird die Funktion nicht
// aufgerufen — dort sind Backslashes literal.
func unescapeBackslash(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' && i+1 < len(b) && isAsciiPunct(b[i+1]) {
			out = append(out, b[i+1])
			i++
			continue
		}
		out = append(out, b[i])
	}
	return out
}

// isAsciiPunct ist die ASCII-Interpunktionsmenge von CommonMark
// (!"#$%&'()*+,-./:;<=>?@[\]^_`{|}~) — nur davor escapet ein Backslash.
func isAsciiPunct(c byte) bool {
	return strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(c))
}

// emOpen/emClose liefern die ANSI-SGR-Sequenzen fuer Emphasis (Level 2 =
// fett, sonst kursiv). '\x1b[0m' zum Schliessen wuerde die Basis-Farbe des
// Blocks loeschen — deshalb die gezielten Attribute-Off-Codes.
func emOpen(level int) string {
	if level == 2 {
		return "\x1b[1m"
	}
	return "\x1b[3m"
}
func emClose(level int) string {
	if level == 2 {
		return "\x1b[22m"
	}
	return "\x1b[23m"
}

// strikeOpen/strikeClose liefern die ANSI-SGR-Sequenzen fuer Strikethrough
// (GFM ~~…~~). Geschlossen wird mit dem Attribut-ZWEI-Code (\x1b[29m), nicht
// \x1b[0m — das wuerde die Basis-Farbe des Blocks loeschen.
const (
	strikeOpen  = "\x1b[9m"
	strikeClose = "\x1b[29m"
)

// Align mirrors table cell alignment for the widget layer.
type Align int

const (
	AlignDefault Align = iota
	AlignLeft
	AlignRight
	AlignCenter
)

// Node is a structured top-level block of a markdown document.
type Node struct {
	Kind   NodeKind
	Text   string     // NodeText: plain text
	Level  int        // NodeHeading: markdown level 1..6 (Anzahl #)
	List   bool       // NodeText: aus einer Markdown-Liste (Items je Zeile)
	Lang   string     // NodeCode: language tag
	Code   string     // NodeCode: fenced code content
	Cells  [][]string // NodeTable: cells, header first
	Aligns []Align    // NodeTable: per-column alignment
}

// Parse splits src into structured top-level blocks via goldmark (GFM).
func Parse(src string) ([]Node, error) {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	doc := md.Parser().Parse(text.NewReader([]byte(src)))
	srcB := []byte(src)
	var out []Node
	var txt strings.Builder
	var inCode bool // Escape-Aufloesung pausieren, solange ein CodeSpan aktiv ist
	flush := func() {
		if txt.Len() > 0 {
			out = append(out, Node{Kind: NodeText, Text: txt.String()})
			txt.Reset()
		}
	}
	if err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Beim Verlassen eines Emphasis-/Strikethrough-Knotens die passende
			// ANSI-Close-Sequenz schreiben. NICHT \x1b[0m — das wuerde die
			// Basis-Farbe des Blocks loeschen.
			switch t := n.(type) {
			case *ast.Emphasis:
				txt.WriteString(emClose(t.Level))
			case *extast.Strikethrough:
				txt.WriteString(strikeClose)
			case *ast.CodeSpan:
				inCode = false
			}
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Heading:
			flush()
			out = append(out, Node{Kind: NodeHeading, Level: t.Level, Text: strings.TrimSpace(nodeText(t, srcB))})
			return ast.WalkSkipChildren, nil
		case *ast.Paragraph:
			// End the current text block so paragraphs keep their gap.
			flush()
			return ast.WalkContinue, nil
		case *ast.List:
			// Lists become one row per item with the marker kept — without
			// this every bullet collapsed into a single run-on line.
			flush()
			appendList(&out, t, srcB, 0)
			return ast.WalkSkipChildren, nil
		case *extast.Table:
			flush()
			cells, aligns := tableData(t, srcB)
			out = append(out, Node{Kind: NodeTable, Cells: cells, Aligns: aligns})
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock:
			flush()
			out = append(out, Node{
				Kind: NodeCode,
				Lang: string(t.Language(srcB)),
				Code: string(t.Lines().Value(srcB)),
			})
			return ast.WalkSkipChildren, nil
		case *ast.Emphasis:
			// *kursiv*/_em_ (Level 1) und **fett**/__fett__ (Level 2) als
			// ANSI-Styling direkt in den Text — goldmark loest die Marker auf,
			// ohne diesen Schritt gaebe es nur nackten Text. Der Wortumbruch
			// (WrapWords->lipgloss.Width) misst ANSI-breitensicher.
			txt.WriteString(emOpen(t.Level))
			return ast.WalkContinue, nil
		case *extast.Strikethrough:
			// ~~durchgestrichen~~ (GFM) analog.
			txt.WriteString(strikeOpen)
			return ast.WalkContinue, nil
		case *ast.CodeSpan:
			// Inline-Code: Backslash-Escapes sind darin literal (CommonMark),
			// die Aufloesung wird bis zum Ende des Spans pausiert.
			inCode = true
			return ast.WalkContinue, nil
		case *ast.Text:
			// Der Text KOMMT IMMER ZUERST — Soft/Hard-Break haengen am
			// letzten Text-Knoten der Zeile. Frueher schrieb der Walker bei
			// SoftLineBreak NUR den Umbruch und warf den Knotentext weg:
			// das letzte Inline-Stueck jeder Soft-Break-Zeile verschwand
			// (Gedankenstriche, Klammern, ganze Woerter). Hard-Breaks
			// klebten zudem Zeile A und B ohne Trennung zusammen.
			seg := t.Text(srcB)
			if !inCode {
				seg = unescapeBackslash(seg)
			}
			txt.Write(seg)
			if t.SoftLineBreak() || t.HardLineBreak() {
				txt.WriteByte('\n')
			}
		case *ast.AutoLink:
			// AutoLinks (<https://…>, GFM-Linkify, E-Mail) tragen ihre URL
			// in einem PRIVATEN Feld — der Walk sieht kein Text-Kind, die
			// URL fiel voellig weg. Label ist der getippte Klartext (FR-16).
			txt.Write(t.Label(srcB))
		case *ast.CodeBlock:
			// Vier-Spaces-Code (eingerueckter Block, ast.CodeBlock) ist ein
			// eigener AST-Knoten — ohne diesen Fall verschluckte der Walker
			// den Block komplett (Datenverlust: der Inhalt verschwand still).
			flush()
			out = append(out, Node{Kind: NodeCode, Code: string(t.Lines().Value(srcB))})
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	}); err != nil {
		return nil, err
	}
	flush()
	return out, nil
}

// appendList flattens a markdown list into one text row per item, keeping the
// marker, numbering ordered lists and indenting nested lists by two spaces per
// level.
func appendList(out *[]Node, list *ast.List, src []byte, depth int) {
	var lines []string
	collectList(&lines, list, src, depth)
	if len(lines) > 0 {
		*out = append(*out, Node{Kind: NodeText, Text: strings.Join(lines, "\n"), List: true})
	}
}

// collectList sammelt die Zeilen EINER Liste (inklusive verschachtelter) —
// eine ganze Liste wird so ein einziger Block (mit List:true). Die Dar-
// stellung zieht dadurch keine Leerzeile zwischen die Items, sondern nur
// rund um die gesamte Liste ("bei Nummerierung eine Zeile frei").
func collectList(lines *[]string, list *ast.List, src []byte, depth int) {
	indent := strings.Repeat("  ", depth)
	for i, c := 0, list.FirstChild(); c != nil; i, c = i+1, c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		// Start is 0 for a bullet list; an ordered list carries its first number.
		mark := "- "
		if list.Start != 0 {
			mark = strconv.Itoa(list.Start+i) + ". "
		}
		var (
			texts  []string
			nested []*ast.List
		)
		for sc := item.FirstChild(); sc != nil; sc = sc.NextSibling() {
			if nl, isList := sc.(*ast.List); isList {
				nested = append(nested, nl)
				continue
			}
			if t := strings.TrimSpace(nodeText(sc, src)); t != "" {
				texts = append(texts, t)
			}
		}
		if len(texts) > 0 {
			*lines = append(*lines, indent+mark+strings.Join(texts, " "))
		}
		for _, nl := range nested {
			collectList(lines, nl, src, depth+1)
		}
	}
}

// tableData extracts header+rows (cells) and per-column alignment from a table.
func tableData(t *extast.Table, src []byte) (cells [][]string, aligns []Align) {
	aligns = make([]Align, len(t.Alignments))
	for i, a := range t.Alignments {
		aligns[i] = renderAlign(a)
	}
	header := []string{}
	rows := [][]string{}
	for rowEl := t.FirstChild(); rowEl != nil; rowEl = rowEl.NextSibling() {
		c := []string{}
		for cellEl := rowEl.FirstChild(); cellEl != nil; cellEl = cellEl.NextSibling() {
			c = append(c, strings.TrimSpace(nodeText(cellEl, src)))
		}
		if _, ok := rowEl.(*extast.TableHeader); ok || len(c) == 0 {
			if len(c) > 0 {
				header = c
			}
			continue
		}
		rows = append(rows, c)
	}
	cells = append([][]string{}, header)
	cells = append(cells, rows...)
	return cells, aligns
}

func renderAlign(a extast.Alignment) Align {
	switch a {
	case extast.AlignRight:
		return AlignRight
	case extast.AlignCenter:
		return AlignCenter
	case extast.AlignLeft:
		return AlignLeft
	}
	return AlignDefault
}

// TableMarkdown renders cells back to a markdown table (for the copy action).
func TableMarkdown(cells [][]string) string {
	if len(cells) == 0 {
		return ""
	}
	var b strings.Builder
	writeRow := func(r []string) {
		b.WriteString("| ")
		for i, c := range r {
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(c)
		}
		b.WriteString(" |")
	}
	writeRow(cells[0])
	n := len(cells[0])
	if n == 0 {
		n = 1
	}
	b.WriteString("\n| ")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString("---")
	}
	b.WriteString(" |")
	for _, r := range cells[1:] {
		b.WriteString("\n")
		writeRow(r)
	}
	return b.String()
}
