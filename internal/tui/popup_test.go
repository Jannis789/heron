package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pickerRig(t *testing.T) (*rig, *[]string) {
	r := newRig(t)
	var calls []string
	r.m = r.m.WithConfigSet(func(id, value string) error {
		calls = append(calls, id+"="+value)
		return nil
	})
	r.m.modelOptions = []string{"glm-5.2", "glm-5.3"}
	r.m.modelLabel = "glm-5.3"
	return r, &calls
}

// TestModelListOpensViaSlashCommand (FR-20): enter auf /model öffnet die
// Modellliste im Eingabefeld, das aktive Modell ist markiert.
func TestModelListOpensViaSlashCommand(t *testing.T) {
	r, _ := pickerRig(t)
	typeRunes(r, "/model")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if !r.m.modelListOpen {
		t.Fatal("/model did not open the model list")
	}
	r.contains("✓ glm-5.3")
	r.contains("›")
}

// TestModelListSearchFilters (FR-20): tippen filtert die Liste.
func TestModelListSearchFilters(t *testing.T) {
	r, _ := pickerRig(t)
	r.m.openModelList()
	r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})

	view := stripANSI(r.m.View())
	if !strings.Contains(view, "glm-5.2") {
		t.Fatal("glm-5.2 missing after filtering")
	}
	if strings.Contains(view, "✓ glm-5.3") {
		t.Fatal("filtered-out model is still marked active")
	}
}

// TestModelListEnterAppliesAndCloses (FR-20): enter setzt das markierte
// Modell live und schließt die Liste.
func TestModelListEnterAppliesAndCloses(t *testing.T) {
	r, calls := pickerRig(t)
	r.m.openModelList()
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // cursor 0 → glm-5.2

	if len(*calls) != 1 || (*calls)[0] != "model=glm-5.2" {
		t.Fatalf("config calls = %v", *calls)
	}
	if r.m.modelLabel != "glm-5.2" {
		t.Fatalf("model label = %q", r.m.modelLabel)
	}
	if r.m.modelListOpen {
		t.Fatal("list stayed open after applying")
	}
}

// TestModelListEscCloses (FR-20).
func TestModelListEscCloses(t *testing.T) {
	r, _ := pickerRig(t)
	r.m.openModelList()
	r.do(tea.KeyMsg{Type: tea.KeyEsc})
	if r.m.modelListOpen {
		t.Fatal("esc did not close the list")
	}
}

// TestFooterSegmentClickOpensModelList (FR-20): Klick aufs Modell-Segment in
// der Footer-Bar öffnet die Liste.
func TestFooterSegmentClickOpensModelList(t *testing.T) {
	r, _ := pickerRig(t)
	lo, _ := r.m.modelSegmentRange()
	r.do(tea.MouseMsg{X: lo + 2, Y: 29, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.modelListOpen {
		t.Fatal("segment click did not open the model list")
	}
}

// TestTopLabelHasNixIcon: the workspace path carries the icon prefix.
func TestTopLabelHasNixIcon(t *testing.T) {
	r := newRig(t)
	r.m.cwd = "~/projekte/heron"
	if !strings.HasPrefix(r.m.topLabel(), nixIcon+" ") {
		t.Fatalf("top label = %q, want icon prefix", r.m.topLabel())
	}
}
