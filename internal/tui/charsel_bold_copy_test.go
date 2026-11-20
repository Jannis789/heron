package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSelectionCopyBoldIsPlain (User-Report: "fettgedruckter text ist im
// auswahl mode broken — Besonderheit wird zu [1mBesonderheit:[22m"): Der
// Markdown-Yank einer Block-Auswahl darf keine Inline-SGRs enthalten. Der
// Parser legt **fett** als \x1b[1m…\x1b[22m im Text ab; ohne Strippen
// erscheinen die Sequence-Reste beim Einfuegen als sichtbarer Muell.
func TestSelectionCopyBoldIsPlain(t *testing.T) {
	withTrueColor(t)
	r := newRig(t).seed("Hier ist **Besonderheit:** fett im Text.")
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	r.do(runes("c"))
	if len(r.copied) != 1 {
		t.Fatalf("copied %d payloads, want 1", len(r.copied))
	}
	got := r.copied[0]
	if strings.Contains(got, "\x1b") || strings.Contains(got, "[1m") || strings.Contains(got, "[22m") {
		t.Fatalf("selection copy carries ANSI: %q", got)
	}
	want := "Hier ist Besonderheit: fett im Text."
	if got != want {
		t.Fatalf("selection copy = %q, want %q", got, want)
	}
}

// TestCursorModeKeepsBoldInView: Der Zeichen-Cursor darf den Wrap-Cache
// nicht in-place entschoenen — sonst verliert die ANSICHT beim Eintritt in
// den Auswahlmodus ihr Fett (nur die Zellkarte fuer Offset-Rechnung muss
// plain sein).
func TestCursorModeKeepsBoldInView(t *testing.T) {
	withTrueColor(t)
	r := newRig(t).seed("Hier ist **Besonderheit:** fett im Text.")
	r.size(60, 20)
	before := r.m.chat.View()
	if !strings.Contains(before, "\x1b[1mBesonderheit:\x1b[22m") {
		t.Fatalf("bold missing before cursor mode: %q", before)
	}
	r.do(tea.KeyMsg{Type: tea.KeyShiftTab})
	if !r.m.chat.charActive() {
		t.Fatal("cursor mode did not activate")
	}
	after := r.m.chat.View()
	if !strings.Contains(after, "\x1b[1mBesonderheit:\x1b[22m") {
		t.Fatalf("cursor mode stripped bold from the view: %q", after)
	}
	_, cls, ok := r.m.chat.charView(0, r.m.chat.Width())
	if !ok {
		t.Fatal("block 0 not char-capable")
	}
	for i, s := range cls {
		if strings.Contains(s, "\x1b") {
			t.Fatalf("cell map carries ANSI at line %d: %q", i, s)
		}
	}
}
