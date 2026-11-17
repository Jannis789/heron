package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// SGR-Maus-Muell, wie er nach verlorenem ESC-Byte als Runen im Prompt
// landet (User-Report: "[<35;42;55M" beim Scrollen).
func TestKittyResponseFilterDropsMouseResidue(t *testing.T) {
	drop := []string{
		"[<35;42;55M",     // Motion, keine Taste (Scrollen/Bewegen)
		"[<64;10;20M",     // WheelUp
		"[<65;10;20M",     // WheelDown
		"[<0;3;4m",        // Release (kleines m)
		"<35;42;55M",      // '[' auch schon gefressen
		"alt+[<35;42;55M", // ESC als alt+ uebersetzt
	}
	for _, s := range drop {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		if got := kittyResponseFilter(nil, msg); got != nil {
			t.Errorf("mouse residue %q must be dropped, got %+v", s, got)
		}
	}
}

// Echte Nutzereingaben duerfen den Filter unberuehrt passieren —
// insbesondere die Retry-Aktion ("r"), gewoehnlicher Text und Tasten.
func TestKittyResponseFilterKeepsRealInput(t *testing.T) {
	keep := []tea.Msg{
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Hallo Welt")},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")},
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("35;42;55")}, // ohne SGR-Tail
		tea.WindowSizeMsg{Width: 80, Height: 24},
	}
	for _, msg := range keep {
		if got := kittyResponseFilter(nil, msg); got == nil {
			t.Errorf("msg %+v must pass through, got dropped", msg)
		}
	}
}

// Die Quellen-Reset-Sequenz muss alle ueblichen Maus-Reportings
// abschalten — sonst bleibt nach einem Crash irgendein Modus an und
// der naechste Inline-Lauf sammelt weiter Reports ein.
func TestDisableMouseReportingResetsAllModes(t *testing.T) {
	var buf bytes.Buffer
	disableMouseReporting(&buf)
	out := buf.String()
	for _, mode := range []string{"1000", "1002", "1003", "1006"} {
		want := "\x1b[?" + mode + "l"
		if !strings.Contains(out, want) {
			t.Errorf("reset missing %q (Any-Event 1003 ist der Täter beim Motion-Leak)", want)
		}
	}
}
