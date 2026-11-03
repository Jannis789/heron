package tui

import "testing"

// TestUnderCursorTrueColorSGR: Der Underscore-Cursor darf eine SGR-Sequenz
// mit Truecolor (38;2;R;G;B) NICHT verkuerzen — Regression: Esc-Parser
// stoppte am '[' und liess "7;38;2;217;211;236m" als Gibberish zurueck.
func TestUnderCursorTrueColorSGR(t *testing.T) {
	in := "\x1b[7;38;2;217;211;236mHallo"
	got := undercursor(in)
	want := "\x1b[7;38;2;217;211;236m\x1b[4mH\x1b[0m\x1b[7;38;2;217;211;236mallo"
	if got != want {
		t.Fatalf("undercursor = %q\nwant        %q", got, want)
	}
}
