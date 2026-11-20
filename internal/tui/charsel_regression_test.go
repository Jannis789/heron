package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// TestCursorModeSpawnFallsBackToReadingPos: Ohne Live-Karten im Chat (der
// Agent-Prozess lebt im Activity-Baum ueber der Eingabe) gilt die
// Leseposition (Viewport-Rand) — der Spawn darf nie stumm fehlschlagen.
func TestCursorModeSpawnFallsBackToReadingPos(t *testing.T) {
	r := newRig(t)
	r.m.chat = r.m.chat.Append(NewTextBlock("eins", ""), NewTextBlock("zwei", ""))
	r.m.chat = r.m.chat.SetSize(80, 24)
	r.m.chat = r.m.chat.ScrollEnd()
	r.do(tea.KeyMsg{Type: tea.KeyShiftTab})
	if !r.m.chat.char.active {
		t.Fatal("cursor mode did not activate")
	}
}

func mustTheme(t *testing.T) *render.Theme {
	t.Helper()
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatalf("default theme: %v", err)
	}
	return th
}
