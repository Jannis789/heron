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
	r.m.effortOptions = []string{"low", "high", "max"}
	r.m.modelLabel = "glm-5.3"
	r.m.effort = "max"
	return r, &calls
}

// TestModelListOpensViaSlashCommand (FR-20): /model öffnet die Modellliste,
// das aktive Modell ist markiert.
func TestModelListOpensViaSlashCommand(t *testing.T) {
	r, _ := pickerRig(t)
	typeRunes(r, "/model")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if !r.m.pickerOpen {
		t.Fatal("/model did not open the model list")
	}
	r.contains("✓ glm-5.3")
}

// TestEffortListOpensViaSlashCommand (FR-20): /effort öffnet die Stufenliste,
// die aktive Stufe ist markiert.
func TestEffortListOpensViaSlashCommand(t *testing.T) {
	r, _ := pickerRig(t)
	typeRunes(r, "/effort")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if !r.m.pickerOpen {
		t.Fatal("/effort did not open the effort list")
	}
	r.contains("✓ max")
}

// TestEffortEnterAppliesAndCloses (FR-20): enter setzt die markierte Stufe
// live und schließt die Liste.
func TestEffortEnterAppliesAndCloses(t *testing.T) {
	r, calls := pickerRig(t)
	typeRunes(r, "/effort")
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // executes /effort → opens the list
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // applies the highlighted option ("low")

	if len(*calls) != 1 || (*calls)[0] != "thinking_effort=low" {
		t.Fatalf("config calls = %v", *calls)
	}
	if r.m.effort != "low" {
		t.Fatalf("effort = %q", r.m.effort)
	}
	if r.m.pickerOpen {
		t.Fatal("list stayed open after applying")
	}
}

// TestModelPickerSearchFilters (FR-20): /model zeigt die Suchzeile, Tippen
// filtert (glm-5.3-flash ist in goose' Liste und wird auffindbar), enter
// setzt das gefilterte Modell.
func TestModelPickerSearchFilters(t *testing.T) {
	r, calls := pickerRig(t)
	r.m.modelOptions = []string{
		"glm-4.5", "glm-4.5-air", "glm-4.6", "glm-5",
		"glm-5.2", "glm-5.3", "glm-5.3-flash", "glm-5.3-flashx",
	}
	typeRunes(r, "/model")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.contains("Sucheingabe")

	typeRunes(r, "flash")
	r.contains("glm-5.3-flash")
	r.contains("glm-5.3-flashx")
	if strings.Contains(stripANSI(r.m.View()), "glm-4.6") {
		t.Fatal("query did not filter the model list")
	}
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if len(*calls) != 1 || (*calls)[0] != "model=glm-5.3-flash" {
		t.Fatalf("config calls = %v", *calls)
	}
}

// TestEffortPickerHasNoSearch (FR-20): die kurze Stufenliste bleibt ohne
// Suchzeile.
func TestEffortPickerHasNoSearch(t *testing.T) {
	r, _ := pickerRig(t)
	typeRunes(r, "/effort")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if strings.Contains(stripANSI(r.m.View()), "Sucheingabe") {
		t.Fatal("effort picker shows a search line")
	}
}

// TestPickerEscCloses (FR-20).
func TestPickerEscCloses(t *testing.T) {
	r, _ := pickerRig(t)
	r.m.openPicker("model")
	r.do(tea.KeyMsg{Type: tea.KeyEsc})
	if r.m.pickerOpen {
		t.Fatal("esc did not close the picker")
	}
}

// TestFooterSegmentClickOpensPicker (FR-20): Klick aufs Modell-Segment in der
// Footer-Bar öffnet die Modellliste.
func TestFooterSegmentClickOpensPicker(t *testing.T) {
	r, _ := pickerRig(t)
	lo, _ := r.m.modelSegmentRange()
	r.do(tea.MouseMsg{X: lo + 2, Y: 29, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.pickerOpen {
		t.Fatal("segment click did not open the picker")
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
