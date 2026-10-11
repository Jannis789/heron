package tui

import (
	"strings"
	"testing"
)

// TestSplitStreamSegments (FR-2): prose and in-progress tables split into
// separate preview segments.
func TestSplitStreamSegments(t *testing.T) {
	segs := splitStreamSegments("Vor\n\n| A | B |\n|---|---|\n| 1 | 2 |\n")
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2", len(segs))
	}
	if segs[0].table || !strings.Contains(segs[0].text, "Vor") {
		t.Fatalf("seg0 = %+v, want prose", segs[0])
	}
	if !segs[1].table {
		t.Fatal("seg1 should be a table segment")
	}
}

// TestParsePipeTable (FR-2): pipe rows become cells; the separator row turns
// into alignments and disappears; rows are padded to a common width.
func TestParsePipeTable(t *testing.T) {
	cells, aligns := parsePipeTable("| A | B |\n|---|---|\n| 1 | 2 |")
	if len(cells) != 2 {
		t.Fatalf("rows = %d, want 2", len(cells))
	}
	if cells[0][0] != "A" || cells[0][1] != "B" || cells[1][0] != "1" || cells[1][1] != "2" {
		t.Fatalf("cells = %v", cells)
	}
	if len(aligns) != 2 {
		t.Fatalf("aligns = %d entries, want 2", len(aligns))
	}
}

// TestParsePipeTablePartialRow (FR-2): an unfinished streaming row becomes a
// padded last row instead of breaking the layout.
func TestParsePipeTablePartialRow(t *testing.T) {
	cells, _ := parsePipeTable("| A | B |\n|---|---|\n| 1")
	if len(cells) != 2 {
		t.Fatalf("rows = %d, want 2", len(cells))
	}
	if len(cells[1]) != 2 || cells[1][0] != "1" || cells[1][1] != "" {
		t.Fatalf("partial row = %v", cells[1])
	}
}

// TestStreamTableGrowsRowByRow (FR-2): the live preview renders the table
// as soon as the header streams in and grows with every completed row —
// no raw pipe markdown visible, and the reply replaces the preview.
func TestStreamTableGrowsRowByRow(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(chunkMsg{text: "Vor\n\n| A | B |\n|---|---|\n"})

	view := stripANSI(r.m.View())
	if !strings.Contains(view, "A") || !strings.Contains(view, "B") {
		t.Fatalf("table header missing: %q", view)
	}
	if strings.Contains(view, "|---") || strings.Contains(view, "| A | B |") {
		t.Fatal("raw markdown table leaked into the preview")
	}

	r.do(chunkMsg{text: "| 1 | 2 |\n"})
	view = stripANSI(r.m.View())
	if !strings.Contains(view, "1") || !strings.Contains(view, "2") {
		t.Fatalf("table row missing after growth: %q", view)
	}

	// The reply replaces the whole preview with the final blocks.
	r.do(replyMsg{text: "fertig"})
	view = stripANSI(r.m.View())
	if !strings.Contains(view, "fertig") {
		t.Fatal("reply text missing")
	}
	if strings.Contains(view, "A") || strings.Contains(view, "|---") {
		t.Fatal("stream preview survived the reply")
	}
}
