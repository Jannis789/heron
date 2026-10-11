package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeRunes feeds a string into the model as individual rune keys, so the
// value-change resets (cursor/dismiss) behave like real typing.
func typeRunes(r *rig, s string) {
	for _, ru := range s {
		r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ru}})
	}
}

// TestSlashInlineGhost (FR-20): typing a prefix completes the command inline
// on the same line — dim ghost, no list below, no box growth.
func TestSlashInlineGhost(t *testing.T) {
	r := newRig(t)
	typeRunes(r, "/mo")

	view := stripANSI(r.m.View())
	if !strings.Contains(view, "/model") {
		t.Fatalf("inline ghost missing in view: %q", view)
	}
	if strings.Contains(view, "├") {
		t.Fatal("suggestion list below is gone — autocomplete is inline now")
	}
}

// TestSlashFilterAndExecuteOpensDialog (FR-20): the list filters while
// typing; enter runs /model, which opens the selection dialog.
func TestSlashFilterAndExecuteOpensDialog(t *testing.T) {
	r, _ := pickerRig(t)
	typeRunes(r, "/mo")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if !r.m.modelListOpen {
		t.Fatal("/model did not open the model list")
	}
	if r.m.input.Value() != "" {
		t.Fatalf("input not cleared: %q", r.m.input.Value())
	}
}

// TestSlashTabCompletes (FR-20): tab completes the highlighted suggestion.
func TestSlashTabCompletes(t *testing.T) {
	r := newRig(t)
	typeRunes(r, "/mo")
	r.do(tea.KeyMsg{Type: tea.KeyTab})

	if got := r.m.input.Value(); got != "/model " {
		t.Fatalf("input = %q, want %q", got, "/model ")
	}
}

// TestSlashRightAcceptsGhost (FR-20): the right arrow accepts the ghost into
// the input, like tab.
func TestSlashRightAcceptsGhost(t *testing.T) {
	r := newRig(t)
	typeRunes(r, "/mo")
	r.do(tea.KeyMsg{Type: tea.KeyRight})

	if got := r.m.input.Value(); got != "/model " {
		t.Fatalf("input = %q, want %q", got, "/model ")
	}
}

// TestSlashUnknownCommandErrors (FR-20): an unknown command reports an error
// instead of sending the prompt.
func TestSlashUnknownCommandErrors(t *testing.T) {
	r := newRig(t)
	typeRunes(r, "/xyz")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	r.contains("Unbekannter Befehl: /xyz")
}

// TestSlashEscDismissesUntilTyping (FR-20): esc hides the list; typing
// brings it back.
func TestSlashEscDismissesUntilTyping(t *testing.T) {
	r := newRig(t)
	typeRunes(r, "/")
	r.do(tea.KeyMsg{Type: tea.KeyEsc})

	if strings.Contains(stripANSI(r.m.View()), "/model") {
		t.Fatal("esc did not dismiss the ghost")
	}
	typeRunes(r, "m")
	if !strings.Contains(stripANSI(r.m.View()), "/model") {
		t.Fatal("typing did not bring the ghost back")
	}
}
