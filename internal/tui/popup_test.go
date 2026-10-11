package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func popupRig(t *testing.T) (*rig, *[]string) {
	t.Helper()
	var calls []string
	r := newRig(t)
	r.m = r.m.WithConfigSet(func(id, value string) error {
		calls = append(calls, id+"="+value)
		return nil
	})
	r.m.modelOptions = []string{"glm-5.2", "glm-5.3"}
	r.m.effortOptions = []string{"low", "high", "max"}
	// The labels mirror what goose announced so the popup can mark the
	// active entries.
	r.m.modelLabel = "glm-5.3"
	r.m.effort = "max"
	return r, &calls
}

// TestPopupOpensAndAppliesEffort (FR-20-lite): ctrl+o opens the popup with
// the active entries marked; enter applies the effort live.
func TestPopupOpensAndAppliesEffort(t *testing.T) {
	r, calls := popupRig(t)
	r.key("ctrl+o")
	if !r.m.popupOpen {
		t.Fatal("ctrl+o did not open the popup")
	}
	r.contains("modell / effort")
	r.contains("✓ glm-5.3")

	// The cursor starts on the first model row; two downs land on "low"
	// (the section caption is skipped).
	r.key("down")
	r.key("down")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if len(*calls) != 1 || (*calls)[0] != "effort=low" {
		t.Fatalf("config calls = %v", *calls)
	}
	if !strings.Contains(stripANSI(r.m.View()), "✓ low") {
		t.Fatal("active effort did not move")
	}
}

// TestPopupEnterAppliesModelAndEscCloses (FR-20-lite).
func TestPopupEnterAppliesModelAndEscCloses(t *testing.T) {
	r, calls := popupRig(t)
	r.key("ctrl+o")
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // cursor sits on the first model row

	if len(*calls) != 1 || (*calls)[0] != "modell=glm-5.2" {
		t.Fatalf("config calls = %v", *calls)
	}
	if r.m.modelLabel != "glm-5.2" {
		t.Fatalf("model label = %q", r.m.modelLabel)
	}
	r.key("esc")
	if r.m.popupOpen {
		t.Fatal("esc did not close the popup")
	}
}

// TestFooterChipClickOpensPopup (FR-20-lite): clicking the [ modell · effort ]
// chip opens the popup.
func TestFooterChipClickOpensPopup(t *testing.T) {
	r, _ := popupRig(t)
	lo, _ := r.m.promptChipRange()
	r.do(tea.MouseMsg{X: lo + 2, Y: 29, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !r.m.popupOpen {
		t.Fatal("chip click did not open the popup")
	}
	r.contains("modell / effort")
}

// TestTopLabelHasNixIcon: the workspace path carries the icon prefix.
func TestTopLabelHasNixIcon(t *testing.T) {
	r := newRig(t)
	r.m.cwd = "~/projekte/heron"
	if !strings.HasPrefix(r.m.topLabel(), nixIcon+" ") {
		t.Fatalf("top label = %q, want icon prefix", r.m.topLabel())
	}
}
