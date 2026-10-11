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

// TestSlashShowsSuggestions (FR-20): typing "/" opens the suggestion list
// below the input inside the box.
func TestSlashShowsSuggestions(t *testing.T) {
	r := newRig(t)
	typeRunes(r, "/")

	view := stripANSI(r.m.View())
	if !strings.Contains(view, "/model") {
		t.Fatalf("suggestion missing in view: %q", view)
	}
	if !strings.Contains(view, "Modell und Effort wählen") {
		t.Fatal("command help missing")
	}
	if !strings.Contains(view, "├") {
		t.Fatal("separator between input and suggestions missing")
	}
}

// TestSlashFilterAndExecuteOpensDialog (FR-20): the list filters while
// typing; enter runs /model, which opens the selection dialog.
func TestSlashFilterAndExecuteOpensDialog(t *testing.T) {
	r, _ := popupRig(t)
	typeRunes(r, "/mo")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if !r.m.popupOpen {
		t.Fatal("/model did not open the selection dialog")
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

	if strings.Contains(stripANSI(r.m.View()), "Modell und Effort wählen") {
		t.Fatal("esc did not dismiss the suggestions")
	}
	typeRunes(r, "m")
	if !strings.Contains(stripANSI(r.m.View()), "/model") {
		t.Fatal("typing did not bring the suggestions back")
	}
}
