package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestThinkBlockCollapsesLongThinking (FR-30): a long thinking stream shows
// three lines plus a "more" marker until unfolded.
func TestThinkBlockCollapsesLongThinking(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "eins\nzwei\ndrei\nvier\nfünf"})

	r.contains("denken")
	// The collapsed block shows the END of the thinking ("drei/vier/fünf");
	// the beginning ("eins") stays hidden until unfolded. "denken" appears
	// exactly once: in the block header.
	if n := strings.Count(stripANSI(r.m.View()), "denken"); n != 1 {
		t.Fatalf("'denken' appears %d times, want exactly 1 (header only)", n)
	}
	if strings.Contains(stripANSI(r.m.View()), "eins") {
		t.Fatal("collapsed block shows the beginning instead of the end")
	}
	r.contains("fünf")

	// Unfold: focus the block and press enter.
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.contains("fünf")
	r.contains("ZUKLAPPEN")
}

// TestThinkBlockSurvivesStreaming (FR-30): refreshes keep the fold state.
func TestThinkBlockSurvivesStreaming(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "schritt eins\nschritt zwei"})
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // unfolded

	r.do(thoughtMsg{text: "\nschritt drei"})
	if !strings.Contains(stripANSI(r.m.View()), "schritt drei") {
		t.Fatal("refresh collapsed the unfolded block")
	}
	if !strings.Contains(stripANSI(r.m.View()), "denken ▾") {
		t.Fatal("unfolded block lost its header state")
	}
}

// TestStartTurnDropsThinkBlock (FR-30): a new turn starts clean.
func TestStartTurnDropsThinkBlock(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "alt"})
	r.contains("denken")

	r.m.startTurn()
	r.notContains("denken ▸")
}
