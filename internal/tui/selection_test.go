package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestSpaceMarksFocusedBlock (FR-29): space toggles the mark of the focused
// block and the chip announces it.
func TestSpaceMarksFocusedBlock(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	if !r.m.chat.items[r.m.chat.focus].Marked() {
		t.Fatal("space did not mark the focused block")
	}
	r.contains("MARKED")
	r.do(runes(" "))
	if r.m.chat.items[r.m.chat.focus].Marked() {
		t.Fatal("second space did not unmark")
	}
}

// TestYankCopiesPlainMarkdown (FR-29): y copies the marked blocks' raw
// markdown — joined, no ANSI — and clears the selection.
func TestYankCopiesPlainMarkdown(t *testing.T) {
	r := newRig(t).seed("vor\n\n```go\nx := 1\n```\n\n```go\ny := 2\n```")
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	r.do(runes("y"))

	if len(r.copied) != 1 {
		t.Fatalf("copied %d payloads, want 1: %q", len(r.copied), r.copied)
	}
	got := r.copied[0]
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("selection copy contains ANSI codes: %q", got)
	}
	want := "```go\nx := 1\n```\n\n```go\ny := 2\n```"
	if got != want {
		t.Fatalf("selection copy = %q, want %q", got, want)
	}
	if r.m.chat.markedCount() != 0 {
		t.Fatal("marks survived the copy")
	}
}

// TestClickTogglesMark (FR-29): a left click on a block body toggles its
// mark; clicking the COPY chip still copies that block (FR-5 bleibt).
func TestClickTogglesMark(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	// The first block starts at the top of the chat area; its body row is
	// one line below the header.
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.chat.items[0].Marked() {
		t.Fatal("click did not mark the block")
	}
	r.do(tea.MouseMsg{X: 5, Y: chatTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if r.m.chat.items[0].Marked() {
		t.Fatal("second click did not unmark")
	}
}

// TestEscClearsMarksFirst (FR-29): esc clears the selection before it
// unfocuses.
func TestEscClearsMarksFirst(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	r.do(tea.KeyMsg{Type: tea.KeyEsc})
	if r.m.chat.markedCount() != 0 {
		t.Fatal("esc did not clear the marks")
	}
	if !r.m.chat.Focused() {
		t.Fatal("esc consumed the focus too — the second esc belongs to focus")
	}
}
