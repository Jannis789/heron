package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestCtrlJKeepsFirstLine (FR-7): nach ctrl+j darf die erste Zeile nicht
// abgeschnitten sein. Die bubbles-Textarea scrollt ihren internen Viewport,
// wenn der Cursor eine zu kleine Box verlaesst, und scrollt nach dem
// spaeteren SetHeight nicht zurueck — deshalb waechst die Box vor dem
// Einfuegen (ctrl+j Pre-Grow in Update).
func TestCtrlJKeepsFirstLine(t *testing.T) {
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	r.do(runes("erste zeile"))
	r.do(tea.KeyMsg{Type: tea.KeyCtrlJ})
	r.do(runes("zweite zeile"))

	view := stripANSI(r.m.inputView())
	if !strings.Contains(view, "erste zeile") {
		t.Errorf("first line cut after ctrl+j:\n%s", view)
	}
	if !strings.Contains(view, "zweite zeile") {
		t.Errorf("second line cut after ctrl+j:\n%s", view)
	}
}

// TestTypingPastFullRowKeepsVisible (FR-7): tippen ueber eine volle Zeile
// hinaus (weicher Umbruch mitten im Schreiben) — Anfang und Ende muessen
// sichtbar bleiben. EINDEUTIGE Marker (Regression 2026-10-31): Der alte
// Test pruefte "wort"/"mehr" — beides stand auch in der ZWEITEN Wrap-Zeile,
// der abgeschnittene Anfang fiel durch. Erst der Zeilenanfang (anfang99)
// beweist Sichtbarkeit der ersten Zeile.
func TestTypingPastFullRowKeepsVisible(t *testing.T) {
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	r.do(runes("anfang99 " + strings.Repeat("wort ", 20)))
	r.do(runes(" mehr"))
	view := stripANSI(r.m.inputView())
	if !strings.Contains(view, "anfang99") {
		t.Errorf("beginning cut after soft wrap:\n%s", view)
	}
	if !strings.Contains(view, "mehr") {
		t.Errorf("tail cut after soft wrap:\n%s", view)
	}
}

// TestSingleLongLineKeepsStart (FR-7, Regression 2026-10-31): EINE logische
// Zeile, die weich umbriecht — genau der Fall, in dem der alte atEnd-Check
// (CharOffset innerhalb der Wrap-Zeile vs. Laenge der GANZEN Zeile) nie
// griff: die Box wuchs, der Viewport blieb nach unten gescrollt, nur die
// letzte Wrap-Zeile war sichtbar. Der Zeilenanfang muss sichtbar bleiben.
func TestSingleLongLineKeepsStart(t *testing.T) {
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	r.do(runes("startmarker1 " + strings.Repeat("fuelltext ", 20)))
	r.do(runes(" endmarker2"))
	view := stripANSI(r.m.inputView())
	if !strings.Contains(view, "startmarker1") {
		t.Errorf("start of the single wrapped line cut:\n%s", view)
	}
	if !strings.Contains(view, "endmarker2") {
		t.Errorf("tail cut:\n%s", view)
	}
}

// TestMultipleBreaksKeepEveryLine (FR-7): mehrfache Umbrueche mit langen
// Zeilen — jede Zeile bleibt sichtbar, der Cursor folgt der Hoehe.
func TestMultipleBreaksKeepEveryLine(t *testing.T) {
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	r.do(runes("eins " + strings.Repeat("wort ", 20)))
	r.do(tea.KeyMsg{Type: tea.KeyCtrlJ})
	r.do(runes("zwei " + strings.Repeat("text ", 18)))
	r.do(tea.KeyMsg{Type: tea.KeyCtrlJ})
	r.do(runes("drei schluss"))

	view := stripANSI(r.m.inputView())
	for _, want := range []string{"eins", "zwei", "drei schluss"} {
		if !strings.Contains(view, want) {
			t.Errorf("line %q cut:\n%s", want, view)
		}
	}
}

// TestSmallTerminalNoOverflow (FR-7): selbst mit grosser Box laeuft der
// Screen nie ueber — der Footer bleibt sichtbar.
func TestSmallTerminalNoOverflow(t *testing.T) {
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 12})
	for i := 0; i < 10; i++ {
		r.do(runes("zeile mit inhalt damit die box waechst\n"))
	}
	screen := stripANSI(r.m.View())
	rows := strings.Split(screen, "\n")
	if len(rows) > 12 {
		t.Errorf("screen overflows: %d rows on a 12-row terminal", len(rows))
	}
	if !strings.Contains(screen, "goose · medium") {
		t.Errorf("footer pushed off screen:\n%s", screen[len(screen)-300:])
	}
}
