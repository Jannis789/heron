package tui

import (
	"strings"

	"heron/internal/render"
)

// streamIDRange is the ID window reserved for transient streaming blocks.
const streamIDRange = 64

// streamSegment is one piece of the streaming preview: raw text (prose and
// not-yet-parsed markdown) or an in-progress table rendered live.
type streamSegment struct {
	table bool
	text  string
}

// splitStreamSegments groups the streamed text into prose and in-progress
// table segments. A table starts at a line beginning with "|" and grows with
// every completed row (FR-2).
func splitStreamSegments(text string) []streamSegment {
	var segs []streamSegment
	var textLines, tableLines []string
	flushText := func() {
		s := strings.TrimRight(strings.Join(textLines, "\n"), "\n")
		if strings.TrimSpace(s) != "" {
			segs = append(segs, streamSegment{text: s})
		}
		textLines = nil
	}
	flushTable := func() {
		if len(tableLines) > 0 {
			segs = append(segs, streamSegment{table: true, text: strings.Join(tableLines, "\n")})
			tableLines = nil
		}
	}
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "|") {
			flushText()
			tableLines = append(tableLines, ln)
		} else {
			flushTable()
			textLines = append(textLines, ln)
		}
	}
	flushTable()
	flushText()
	return segs
}

// isAlignRow reports whether a table row is the markdown separator row
// (dashes and colons only).
func isAlignRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		t := strings.TrimSpace(c)
		if t == "" || strings.Trim(t, "-: ") != "" {
			return false
		}
	}
	return true
}

// parsePipeTable builds cells and default alignments from the raw pipe rows
// of an in-progress markdown table. The separator row carries the alignments
// and is dropped from the cells; rows are padded to a common width.
func parsePipeTable(text string) ([][]string, []render.Align) {
	var rows [][]string
	aligns := []render.Align{}
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		t = strings.Trim(t, "|")
		parts := strings.Split(t, "|")
		cells := make([]string, len(parts))
		for j, p := range parts {
			cells[j] = strings.TrimSpace(p)
		}
		if isAlignRow(cells) {
			for range cells {
				aligns = append(aligns, render.AlignDefault)
			}
			continue
		}
		rows = append(rows, cells)
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	for i, r := range rows {
		for len(r) < cols {
			r = append(r, "")
		}
		rows[i] = r
	}
	for len(aligns) < cols {
		aligns = append(aligns, render.AlignDefault)
	}
	return rows, aligns
}

// buildStreamWidgets turns the streamed text into the live preview blocks:
// prose as raw text, in-progress tables as table blocks that re-layout with
// every completed row (FR-2).
func buildStreamWidgets(text, color string, th *render.Theme, copyFn CopyFunc) []Widget {
	var out []Widget
	id := StreamBlockID
	for _, seg := range splitStreamSegments(text) {
		if seg.table {
			cells, aligns := parsePipeTable(seg.text)
			if len(cells) == 0 {
				continue
			}
			tb := NewTableBlock(cells, aligns, copyFn)
			tb.th = th
			out = append(out, tb.SetID(id))
		} else {
			out = append(out, NewTextBlock(seg.text, color).SetID(id))
		}
		id--
	}
	return out
}
