package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestRigRendersSeededChat proves the harness can assert on the rendered
// screen: a seeded code block shows its language header and COPY action.
func TestRigRendersSeededChat(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	r.contains("go").contains("COPY")
	r.contains("heron") // header
}

// TestRigTypesAndReplies drives the input field end-to-end through Update:
// typed prompt is echoed, Enter sends, the reply is parsed into the chat.
func TestRigTypesAndReplies(t *testing.T) {
	r := newRig(t).withSender(func(p string) (string, error) {
		return "Antwort zu: " + p, nil
	})
	r.typeLine("hallo welt").key("enter")
	r.contains("hallo welt").contains("Antwort zu: hallo welt")
}

// TestRigCopyIsDeferredToRuntime checks the Elm rule: Update must not perform
// the clipboard write; only running the returned Cmd may.
func TestRigCopyIsDeferredToRuntime(t *testing.T) {
	r := newRig(t).seed("```go\ny := 2\n```")
	r.key("tab") // focus the code block
	if !r.m.chat.Focused() {
		t.Fatal("tab should focus the code block")
	}
	nm, cmd := r.m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	r.m = nm.(Model)
	if len(r.copied) != 0 {
		t.Fatalf("copy ran during Update: %v", r.copied)
	}
	r.pending(cmd)
	if got := r.copiedAll(); len(got) != 1 || !strings.Contains(got[0], "y := 2") {
		t.Fatalf("copied=%v want code body", got)
	}
	r.contains("COPIED")
}

// TestRigCtrlCQuits verifies quit handling through the harness.
func TestRigCtrlCQuits(t *testing.T) {
	r := newRig(t)
	r.key("ctrl+c")
	if !r.quit {
		t.Fatal("ctrl+c should produce a QuitMsg")
	}
}

// TestRigScreenHasNoEscapes guards stripANSI: assertions run on plain text.
func TestRigScreenHasNoEscapes(t *testing.T) {
	r := newRig(t).seed("| a | b |\n|---|---|\n| 1 | 2 |")
	if strings.Contains(r.screen(), "\x1b") {
		t.Fatalf("screen still contains ESC: %q", r.screen())
	}
	if r.view() == r.screen() && !strings.Contains(r.view(), "\x1b") {
		t.Log("theme produced no ANSI (acceptable)")
	}
	if len(r.lines()) < 3 {
		t.Fatalf("expected a multi-line table, got %d lines", len(r.lines()))
	}
}
