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
	r.key("ctrl+t")
	r.do(thoughtMsg{text: "eins\nzwei\ndrei\nvier\nfünf"})

	r.contains("denken")
	r.contains("… 2 weitere Zeilen")
	// The FR-2 status line deliberately shows the LAST thinking line ("fünf");
	// the collapsed block itself must stop after three ("vier" stays hidden).
	if strings.Contains(stripANSI(r.m.View()), "vier") {
		t.Fatal("collapsed block shows more than three lines")
	}

	// Unfold: focus the block and press enter.
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.contains("fünf")
	r.contains("ZUKLAPPEN")
}

// TestThinkBlockSurvivesStreamingAndToggle (FR-30/FR-2): refreshes keep the
// fold state, ctrl+t hides and re-shows the block.
func TestThinkBlockSurvivesStreamingAndToggle(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.key("ctrl+t")
	r.do(thoughtMsg{text: "schritt eins\nschritt zwei"})
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // unfolded

	r.do(thoughtMsg{text: "\nschritt drei"})
	if !strings.Contains(stripANSI(r.m.View()), "schritt drei") {
		t.Fatal("refresh collapsed the unfolded block")
	}

	r.key("ctrl+t")
	// The footer hint mentions "denken" too — assert on the block header.
	r.notContains("denken ▾")
	r.key("ctrl+t")
	r.contains("denken ▸")
}

// TestStartTurnDropsThinkBlock (FR-30): a new turn starts clean.
func TestStartTurnDropsThinkBlock(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.key("ctrl+t")
	r.do(thoughtMsg{text: "alt"})
	r.contains("denken")

	r.m.startTurn()
	r.notContains("denken ▸")
}
