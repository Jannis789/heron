package render

import (
	"strings"
	"testing"
)

func TestParseSplitsBlocks(t *testing.T) {
	src := "hello\n```go\nx := 1\n```\n| a | b |\n|---|---|\n| 1 | 2 |\n"
	nodes, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	if nodes[0].Kind != NodeText || strings.TrimSpace(nodes[0].Text) != "hello" {
		t.Fatalf("node0: %+v", nodes[0])
	}
	if nodes[1].Kind != NodeCode || nodes[1].Lang != "go" || !strings.Contains(nodes[1].Code, "x := 1") {
		t.Fatalf("node1: %+v", nodes[1])
	}
	if nodes[2].Kind != NodeTable || len(nodes[2].Cells) != 2 || nodes[2].Cells[0][1] != "b" {
		t.Fatalf("node2: %+v", nodes[2])
	}
}

func TestParseFlushesAdjoiningText(t *testing.T) {
	nodes, err := Parse("a\n\n**b**\n\n|c|\n|-|\n|1|\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var got []NodeKind
	for _, n := range nodes {
		got = append(got, n.Kind)
	}
	// text merges the two paragraphs, then a table.
	if len(got) != 2 || got[0] != NodeText || got[1] != NodeTable {
		t.Fatalf("kinds: %v", got)
	}
	if !strings.Contains(nodes[0].Text, "b") {
		t.Fatalf("text should merge paragraphs: %q", nodes[0].Text)
	}
}

func TestTableMarkdownRoundTrip(t *testing.T) {
	cells := [][]string{{"name", "qty"}, {"apples", "3"}}
	md := TableMarkdown(cells)
	for _, want := range []string{"| name | qty |", "| apples | 3 |", "---"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q: %q", want, md)
		}
	}
}
