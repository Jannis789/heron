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

// Reine Klammer-Laeufe (User-Report: "[[[[[[[[... beim Scrollen"): SGR-
// Maus-Reports, deren ESC UND Parameter verloren gingen — der Rest ist
// ein Rune-Stapel aus '['. STATISCH dropt jede Message mit >=3 Klammern
// (ein Merge-Stapel; echte Eingabe hat zwischen Klammern anderen Text).
// DYNAMISCH gilt die Schwall-Rate: Die Fragmente kommen 1-2 Runen je
// Message im 5ms-Takt — ab der 3. reinen Klammer-Message im 600ms-Fenster
// ist es Maus-Muell, menschliches Tippen erreicht diese Rate nie (zwischen
// echten Klammern kommt Text, der den Stapel bricht).
func TestKittyResponseFilterDropsBracketRuns(t *testing.T) {
	// Statisch: Merge-Stapel in EINER Message.
	for _, s := range []string{"[[[[[[[[[[", "[[[", "[[[[[[[[[[[[[[[[["} {
		resetBracketTrap()
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		if got := kittyResponseFilter(nil, msg); got != nil {
			t.Errorf("bracket run %q must be dropped, got %+v", s, got)
		}
	}
	// Dynamisch: 1-2er-Fragmente im schnellen Nachlauf (Schwall).
	resetBracketTrap()
	one := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")}
	for i := 0; i < 2; i++ {
		if got := kittyResponseFilter(nil, one); got == nil {
			t.Errorf("Schwall-Fragment %d darf vor dem Limit NICHT gedroppt werden", i+1)
		}
	}
	if got := kittyResponseFilter(nil, one); got != nil {
		t.Error("3. reine Klammer-Message im Fenster muss als Schwall gedroppt werden")
	}
	// Markdown-Klammern-Tippen bleibt IMMER durch: Einzelne '[' unter der
	// Rate und Meldungen MIT Text (keine reinen Stapel).
	resetBracketTrap()
	keep := []string{"[", "[[", "[so](link)", "hallo[world"}
	for _, s := range keep {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		if got := kittyResponseFilter(nil, msg); got == nil {
			t.Errorf("input %q must pass through, got dropped", s)
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
