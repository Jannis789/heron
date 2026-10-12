package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestThinkBlockCollapsesLongThinking (FR-30): a long thinking stream shows
// three lines plus a "more" marker until unfolded.
func TestThinkBlockCollapsesLongThinking(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "eins\nzwei\ndrei\nvier\nfünf"})

	r.contains("thinking")
	// The collapsed block shows the END of the thinking ("drei/vier/fünf");
	// the beginning ("eins") stays hidden until unfolded. "denken" appears
	// exactly once: in the block header.
	if n := strings.Count(stripANSI(r.m.View()), "thinking"); n != 1 {
		t.Fatalf("'thinking' appears %d times, want exactly 1 (header only)", n)
	}
	if strings.Contains(stripANSI(r.m.View()), "eins") {
		t.Fatal("collapsed block shows the beginning instead of the end")
	}
	r.contains("fünf")

	// One long streamed line wraps at word boundaries instead of being cut
	// with an ellipsis — the collapsed block still shows the tail.
	r2 := newRig(t)
	r2.m.startTurn()
	r2.do(thoughtMsg{text: strings.Repeat("Denkzeile ", 40) + "Schlusswort"})
	r2.contains("Schlusswort")
	// The block itself wraps — no ellipsis in the block body (the status
	// line above may still abbreviate its one-liner).
	b := NewThinkBlock(strings.Repeat("Denkzeile ", 40)+"Schlusswort", r2.m.theme)
	if strings.Contains(stripANSI(b.View(r2.m.width)), "…") {
		t.Fatal("long thinking line is truncated instead of wrapped")
	}
	r2.do(tea.KeyMsg{Type: tea.KeyTab})
	r2.do(tea.KeyMsg{Type: tea.KeyEnter})
	r2.contains("Denkzeile")

	// Unfold: focus the block and press enter.
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.contains("fünf")
	r.contains("show less")
}

// TestThinkActionHoverMatchesCopy (FR-30): hovering AUFKLAPPEN highlights it
// — the hit area must match the rendered footer exactly like COPY, and the
// hover treatment must be the same as the copy action's.
func TestThinkActionHoverMatchesCopy(t *testing.T) {
	r := newRig(t)
	w := r.m.width
	if w == 0 {
		w = 80
	}
	b := NewThinkBlock("eins\nzwei\ndrei\nvier\nfünf", r.m.theme)
	lines := strings.Split(stripANSI(b.View(w)), "\n")
	footer := lines[len(lines)-1]
	// Byte offsets are not cell offsets: box-drawing chars are 3 bytes but
	// one cell — measure the prefix in cells.
	cellX := len([]rune(footer[:strings.Index(footer, "show more")]))
	if cellX < 0 {
		t.Fatal("footer does not show the fold action")
	}
	y := len(lines) - 1 // footer row: header + body rows, 0-based
	if !b.HitAction(w, cellX+2, y) {
		t.Fatalf("HitAction misses the fold action at cell x=%d y=%d", cellX+2, y)
	}
	// ... and does not fire outside the action box.
	if b.HitAction(w, 10, y) {
		t.Fatal("HitAction hits outside the action box")
	}
	// Hover/focus restyle the fold action exactly like COPY (white text on
	// hover, reverse on focus). go test runs with an Ascii color profile, so
	// force TrueColor for this check and restore it afterwards.
	rp := lipgloss.DefaultRenderer()
	old := rp.ColorProfile()
	rp.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { rp.SetColorProfile(old) })

	hb := b.SetHover(true).(ThinkBlock)
	hoverLabel, hoverStyled := hb.action()
	if hoverLabel != "+ show more" || stripANSI(hoverStyled) != "+ show more" {
		t.Fatal("hover styling changed the label")
	}
	// One single style wraps the WHOLE label — white from the first glyph
	// to the last (nested renders used to leave the first one lavender).
	// lipgloss v1.1 re-applies the style per run, so require white on
	// EVERY run, not one big sequence.
	for i, run := range strings.Split(hoverStyled, "\x1b[0m") {
		if run == "" {
			continue
		}
		if !strings.Contains(run, "38;2;255;255;255") {
			t.Fatalf("hover run %d is not white: %q", i, run)
		}
	}
	fb := b.SetFocused(true).(ThinkBlock)
	focusLabel, focusedStyled := fb.action()
	if focusLabel != "+ show more" || stripANSI(focusedStyled) != "+ show more" {
		t.Fatal("focus styling changed the label")
	}
	// Reverse now merges into the single style's sequence ("\x1b[7;38;…").
	if !strings.Contains(focusedStyled, "\x1b[7;") {
		t.Fatalf("focus does not reverse the fold action: %q", focusedStyled)
	}
}

// TestThinkBlockUnfoldsOnClick (FR-30): clicking AUFKLAPPEN unfolds the
// block — the mouse path dispatches the block's own action, never Copy.
func TestThinkBlockUnfoldsOnClick(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "eins\nzwei\ndrei\nvier\nfünf"})

	l := r.m.chat
	w := l.Width()
	b, ok := l.items[0].(ThinkBlock)
	if !ok {
		t.Fatal("thought did not produce a think block")
	}
	h := b.Height(w)
	footer := strings.Split(stripANSI(b.View(w)), "\n")[h-1]
	i := strings.Index(footer, "show more")
	if i < 0 {
		t.Fatal("footer does not show the fold action")
	}
	x := len([]rune(footer[:i])) + 2 // cell offset, +2 lands inside the label
	y := chatTop + h - 1             // only block → list top = chatTop

	r.click(x, y)
	nb, ok := r.m.chat.items[0].(ThinkBlock)
	if !ok || !nb.expanded {
		t.Fatal("clicking the fold action did not unfold the block")
	}
	r.contains("eins") // the beginning appears only when unfolded
	r.contains("show less")

	// Clicking the fold action folds it back. The expanded block is taller —
	// recompute footer row and screen y, they moved with the height.
	h2 := nb.Height(w)
	footer2 := strings.Split(stripANSI(nb.View(w)), "\n")[h2-1]
	i2 := strings.Index(footer2, "show less")
	if i2 < 0 {
		t.Fatal("footer does not show the fold action")
	}
	x2 := len([]rune(footer2[:i2])) + 2
	r.click(x2, chatTop+h2-1)
	if nb2, _ := r.m.chat.items[0].(ThinkBlock); nb2.expanded {
		t.Fatal("clicking the fold action did not fold the block")
	}
	r.notContains("eins")

	// Clicking the header caption folds/unfolds as well — and must NOT set
	// the selection mark (the ✓ that did nothing).
	r.click(5, chatTop)
	nb3, ok := r.m.chat.items[0].(ThinkBlock)
	if !ok || !nb3.expanded {
		t.Fatal("clicking the header caption did not unfold the block")
	}
	if nb3.Marked() {
		t.Fatal("clicking the header caption set the selection mark")
	}
	r.click(5, chatTop)
	nb4, _ := r.m.chat.items[0].(ThinkBlock)
	if nb4.expanded {
		t.Fatal("second header click did not fold the block")
	}
	if nb4.Marked() {
		t.Fatal("header clicks set the selection mark")
	}
}

// TestThinkBlockNoFoldForShort (FR-30): a short thinking shows everything —
// no fold action anywhere (the footer is a plain closing border).
func TestThinkBlockNoFoldForShort(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "nur\neine\nZeile"})
	if strings.Contains(stripANSI(r.m.View()), "show more") {
		t.Fatal("short thinking shows a fold action")
	}
	l := r.m.chat
	w := l.Width()
	b, ok := l.items[0].(ThinkBlock)
	if !ok {
		t.Fatal("thought did not produce a think block")
	}
	if b.HitAction(w, 5, b.Height(w)-1) {
		t.Fatal("footer offers a fold hit area without hidden content")
	}
	if b.HitAction(w, 5, 0) {
		t.Fatal("header offers a fold hit area without hidden content")
	}
}

// TestThinkBlockSurvivesStreaming (FR-30): refreshes keep the fold state.
func TestThinkBlockSurvivesStreaming(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "schritt eins\nschritt zwei"})
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // unfolded

	r.do(thoughtMsg{text: "\nschritt drei"})
	if !strings.Contains(stripANSI(r.m.View()), "schritt drei") {
		t.Fatal("refresh collapsed the unfolded block")
	}
	if !strings.Contains(stripANSI(r.m.View()), "thinking ▾") {
		t.Fatal("unfolded block lost its header state")
	}
}

// TestStartTurnDropsThinkBlock (FR-30): a new turn starts clean.
func TestStartTurnDropsThinkBlock(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "alt"})
	r.contains("thinking")

	r.m.startTurn()
	r.notContains("denken ▸")
}
