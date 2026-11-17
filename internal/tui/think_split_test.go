package tui

import (
	"strings"
	"testing"
)

// TestThinkSectionsSplitAtToolCalls (User-Report): a tool call INTERRUPTS the
// thinking. The finished section stays where it was, later thinking opens a
// NEW block BELOW the tool card — it must not keep growing the old block
// above the interruption.
func TestThinkSectionsSplitAtToolCalls(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "erste zeile\nzweite zeile\ndritte\nvierte"})
	r.do(toolMsg{callID: "c1", name: "shell", kind: "execute", cmd: "ls"})
	r.do(thoughtMsg{text: "nach dem tool"})

	var got []string
	for _, it := range r.m.chat.items {
		switch v := it.(type) {
		case ThinkBlock:
			got = append(got, "think:"+v.text)
		case ToolBlock:
			got = append(got, "tool:"+v.callID)
		}
	}
	want := []string{
		"think:erste zeile\nzweite zeile\ndritte\nvierte",
		"tool:c1",
		"think:nach dem tool",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chat order = %q, want %q", got, want)
	}

	// Updates of the SAME call (live output, done) are no new interruption —
	// they must not slice the running section again.
	r.do(thoughtMsg{text: "\nmehr"})
	thinks := 0
	for _, it := range r.m.chat.items {
		if _, ok := it.(ThinkBlock); ok {
			thinks++
		}
	}
	r.do(toolMsg{callID: "c1", name: "shell", kind: "execute", done: true, hasOut: true, out: "x"})
	for _, it := range r.m.chat.items {
		if _, ok := it.(ThinkBlock); ok {
			thinks++
		}
	}
	if thinks != 4 { // 2 vor den Updates + 2 beim erneuten Zaehlen
		t.Fatalf("same-call updates changed the thinking sections (saw %d entries)", thinks)
	}

	// A fresh turn starts without any of the previous thinking blocks (FR-30).
	r.m.startTurn()
	for _, it := range r.m.chat.items {
		if _, ok := it.(ThinkBlock); ok {
			t.Fatal("startTurn left a thinking block behind")
		}
	}
}
