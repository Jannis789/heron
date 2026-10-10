package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestTextBlockWrapsWithinWidth (FR-16): a prose answer is wrapped to the
// terminal width — no line may overflow.
func TestTextBlockWrapsWithinWidth(t *testing.T) {
	const w = 40
	text := strings.Repeat("Der schnelle braune Fuchs springt über den faulen Hund. ", 8)
	b := NewTextBlock(text, "")
	lines := strings.Split(b.View(w), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected wrapping, got %d lines", len(lines))
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got > w {
			t.Fatalf("line %d is %d cells > %d: %q", i, got, w, ln)
		}
	}
	// Wrapping must not lose content.
	if joined := strings.Join(lines, ""); !strings.Contains(joined, "Fuchs") || !strings.Contains(joined, "Hund") {
		t.Fatalf("content lost while wrapping: %q", joined)
	}
}

// TestTextBlockUnicodeWidth (FR-16): umlauts, emoji and box drawing must not
// break the width arithmetic.
func TestTextBlockUnicodeWidth(t *testing.T) {
	const w = 30
	text := "Übung größer Straße Köln 🎉 äöü ß ┌─┐ │X│ └─┘"
	b := NewTextBlock(text, "")
	for i, ln := range strings.Split(b.View(w), "\n") {
		if got := lipgloss.Width(ln); got > w {
			t.Fatalf("line %d is %d cells > %d: %q", i, got, w, ln)
		}
	}
	if !strings.Contains(b.View(w), "🎉") {
		t.Fatal("emoji dropped from output")
	}
}

// TestTextBlockDoesNotConfigureFont (FR-16): default rendering uses the
// terminal font — heron must not emit font/title control (OSC) sequences.
func TestTextBlockDoesNotConfigureFont(t *testing.T) {
	if strings.Contains(NewTextBlock("nur text", "").View(80), "\x1b]") {
		t.Fatal("text block emits an OSC sequence (font handling?)")
	}
}

// TestScreenFitsTerminalWidth (FR-16): the whole composed frame stays inside
// the terminal width, not just individual blocks.
func TestScreenFitsTerminalWidth(t *testing.T) {
	const w = 72
	r := newRig(t).size(w, 30).
		seed("Ein langer Absatz der über mehrere Zeilen laufen muss und dabei " +
			"die Terminalbreite nicht überschreiten darf.\n\n```go\nx := 1\n```")
	for i, ln := range r.lines() {
		if got := lipgloss.Width(strings.TrimRight(ln, " ")); got > w {
			t.Fatalf("screen line %d is %d cells > %d: %q", i, got, w, ln)
		}
	}
}
