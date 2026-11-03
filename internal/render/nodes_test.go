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

func TestParseKeepsParagraphsApart(t *testing.T) {
	nodes, err := Parse("a\n\n**b**\n\n|c|\n|-|\n|1|\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var got []NodeKind
	for _, n := range nodes {
		got = append(got, n.Kind)
	}
	// Paragraphs stay separate blocks; merging them is what glued a heading
	// to its body when rendering. The table still follows.
	if len(got) != 3 || got[0] != NodeText || got[1] != NodeText || got[2] != NodeTable {
		t.Fatalf("kinds: %v", got)
	}
	if !strings.Contains(nodes[0].Text, "a") || !strings.Contains(nodes[1].Text, "b") {
		t.Fatalf("paragraphs not separated: %q / %q", nodes[0].Text, nodes[1].Text)
	}
}

// TestParseSplitsHeadingsOut: a section title becomes its own block so it
// never gets glued to the paragraph below it.
func TestParseSplitsHeadingsOut(t *testing.T) {
	nodes, err := Parse("# Titel\n\nFließtext danach.\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected heading + paragraph, got %d nodes: %+v", len(nodes), nodes)
	}
	if nodes[0].Kind != NodeHeading || nodes[0].Text != "Titel" {
		t.Fatalf("node0: %+v", nodes[0])
	}
	if nodes[0].Level != 1 {
		t.Fatalf("heading level = %d, want 1 (Anzahl #)", nodes[0].Level)
	}
	if nodes[1].Kind != NodeText || !strings.Contains(nodes[1].Text, "Fließtext") {
		t.Fatalf("node1: %+v", nodes[1])
	}
}

// TestParseCapturesHeadingLevels: jedes # liefert die Stufe 1..6 mit,
// damit die Darstellung eine (subtile) Groessen-Hierarchie bauen kann.
func TestParseCapturesHeadingLevels(t *testing.T) {
	nodes, err := Parse("# a\n## b\n#### d\n###### f\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []int{1, 2, 4, 6}
	if len(nodes) != len(want) {
		t.Fatalf("nodes=%d want %d", len(nodes), len(want))
	}
	for i, w := range want {
		if nodes[i].Kind != NodeHeading || nodes[i].Level != w {
			t.Fatalf("node%d: %+v (level want %d)", i, nodes[i], w)
		}
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
