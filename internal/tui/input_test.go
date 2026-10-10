package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestInputBoxIsFramedOnAllSides: the prompt area must render a closed box —
// top border, one row per text line, and a bottom border carrying the hint.
func TestInputBoxIsFramedOnAllSides(t *testing.T) {
	r := newRig(t)
	view := r.m.inputView()
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("prompt box has %d rows, want >=3 (top/body/bottom): %q", len(lines), view)
	}
	if !strings.HasPrefix(stripANSI(lines[0]), "┌") {
		t.Fatalf("top border missing: %q", lines[0])
	}
	bottom := stripANSI(lines[len(lines)-1])
	if !strings.HasPrefix(bottom, "└") || !(strings.HasSuffix(bottom, "┘") || strings.HasSuffix(bottom, "╯")) {
		t.Fatalf("bottom border missing: %q", bottom)
	}
	if !strings.Contains(bottom, "goose · medium") {
		t.Fatalf("model/effort missing: %q", bottom)
	}
}

// TestPromptBoxShowsWorkspaceTop: the top border names the workspace, the
// app name stays in the title row.
func TestPromptBoxShowsWorkspaceTop(t *testing.T) {
	r := newRig(t)
	r.m.cwd = "~/projekte/heron"
	r.contains("~/projekte/heron")
}

// TestPromptBoxStableWhileRunning: the bottom shows model · effort in every
// state — no elapsed seconds, no abort hint. Ctrl+C still aborts (FR-7),
// it just does not announce itself.
func TestPromptBoxStableWhileRunning(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.contains("goose · medium")
	if v := r.m.View(); strings.Contains(v, "antwortet") || strings.Contains(v, "bricht ab") {
		t.Fatalf("running state leaked into the box: %q", v)
	}
}

// TestPlaceholderCarriesTheHints: the static key hints live in the
// placeholder so the borders stay free for live information.
func TestPlaceholderCarriesTheHints(t *testing.T) {
	r := newRig(t)
	r.contains("enter sendet")
	r.contains("ctrl+j")
}

// TestPromptBoxGrowsWithText: a multi-line prompt widens the box and the chat
// viewport gives the rows back, so the screen still fits.
func TestPromptBoxGrowsWithText(t *testing.T) {
	r := newRig(t)
	before := len(strings.Split(r.m.inputView(), "\n"))

	r.typeLine("eine")
	r.do(tea.KeyMsg{Type: tea.KeyCtrlJ})
	r.typeLine("zweite")

	after := strings.Split(r.m.inputView(), "\n")
	if len(after) <= before {
		t.Fatalf("prompt box did not grow: %d rows before, %d after", before, len(after))
	}
	if !strings.HasPrefix(stripANSI(after[len(after)-1]), "└") {
		t.Fatalf("bottom border lost while growing: %q", after[len(after)-1])
	}
}

// TestScreenFitsWithPromptBox: the composed frame never exceeds the terminal
// height — the chat viewport has to give back the rows the box occupies.
func TestScreenFitsWithPromptBox(t *testing.T) {
	const rows = 20
	r := newRig(t).size(80, rows).seed("kurze antwort")
	if got := len(r.lines()); got > rows {
		t.Fatalf("frame is %d rows, terminal has %d — box would be clipped", got, rows)
	}
}
