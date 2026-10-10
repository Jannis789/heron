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
	flush := func() {
		if txt.Len() > 0 {
			out = append(out, Node{Kind: NodeText, Text: txt.String()})
			txt.Reset()
		}
	}
	if err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Heading:
			flush()
			out = append(out, Node{Kind: NodeHeading, Text: strings.TrimSpace(nodeText(t, srcB))})
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
		case *ast.Text:
			if t.SoftLineBreak() {
				txt.WriteByte('\n')
				return ast.WalkContinue, nil
			}
			txt.Write(t.Text(srcB))
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
			*out = append(*out, Node{Kind: NodeText, Text: indent + mark + strings.Join(texts, " ")})
		}
		for _, nl := range nested {
			appendList(out, nl, src, depth+1)
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
