package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
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
	r.contains("eins")
}

// TestThinkHeaderHoverAndSelectionFill (FR-30): hovering trifft nur die
// Kopfzeile (dort lebt die Falt-Marke), und die AKTUELLE Auswahl faerbt
// die GESAMTE Kastenflaeche — bg + Rahmen in der Akzentfarbe, symmetrisch
// ueber jede Zeile (User-Feedback: nur farbige Rahmenzeichen liessen die
// Horizontal-Linien an den ungestylten Mittelsegmenten ueberlaufen).
func TestThinkHeaderHoverAndSelectionFill(t *testing.T) {
	withTrueColor(t)
	th, _ := render.DefaultTheme()
	w := 60
	b := NewThinkBlock("eins\nzwei\ndrei\nvier\nfünf", th)

	// Hit-Geometrie: nur die Kopfzeile ist Falt-Ziel, sonst nichts.
	if !b.HitAction(w, 5, 0) {
		t.Fatal("HitAction misses the header caption")
	}
	if b.HitAction(w, 2, 0) || b.HitAction(w, 3+w, 0) {
		t.Fatal("HitAction fires outside the header label")
	}
	if b.HitAction(w, w/2, b.Height(w)-1) || b.HitAction(w, w/2, 1) {
		t.Fatal("HitAction offers a fold target on body or footer")
	}

	// Hover auf der Kopfzeile: Label weiß + unterstrichen (wie die alten
	// Footer-Aktionen), der Rest des Kastens bleibt im Ruhe-Stil.
	hb := b.SetHoverAt(w, 5, 0).(ThinkBlock)
	hv := hb.View(w)
	hl := strings.Split(hv, "\n")[0]
	if !strings.Contains(hl, "38;2;255;255;255") || !strings.Contains(hl, "\x1b[4;") {
		t.Fatalf("header hover is not white+underlined: %q", hl)
	}

	// Auswahl (Fokus ODER Markierung): NUR die Kopfzeile leuchtet Neon
	// (38;2;191;0;255 als Vordergrund) — der Kasten selbst bleibt in der
	// ruhigen Karten-Farbe, die Vollflaechen-Fuellung ist weg (User-
	// Feedback: "statt alles auszuwaehlen einfach nur die Ueberschrift
	// highlighten").
	for name, sb := range map[string]ThinkBlock{
		"focused": b.SetFocused(true).(ThinkBlock),
		"marked":  b.SetMarked(true).(ThinkBlock),
	} {
		rows := strings.Split(sb.View(w), "\n")
		if !strings.Contains(rows[0], "38;2;191;0;255") {
			t.Fatalf("%s header is not neon: %q", name, rows[0])
		}
		for i, ln := range rows {
			if strings.Contains(ln, "48;2;191;0;255") {
				t.Fatalf("%s row %d still filled with the focus color: %q", name, i, ln)
			}
		}
	}
	// Ruhe-Zustand: gar kein Neon-Werk.
	if u := b.View(w); strings.Contains(u, "191;0;255") {
		t.Fatalf("unfocused view must not carry neon: %q", u)
	}
}

// TestThinkBlockUnfoldsOnClick (FR-30): clicking the header caption unfolds
// the block — the mouse path dispatches the block's own action, never Copy.
// Die "+ show more"-Zeile ist weg: der Kopf (▸/▾) ist das einzige Klickziel.
func TestThinkBlockUnfoldsOnClick(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "eins\nzwei\ndrei\nvier\nfünf"})

	if strings.Contains(stripANSI(r.m.View()), "show more") {
		t.Fatal("collapsed block still shows a fold action line")
	}

	// Clicking the header caption unfolds — and must NOT set the selection
	// mark (the ✓ that did nothing).
	r.click(5, chatTop)
	nb, ok := r.m.chat.items[0].(ThinkBlock)
	if !ok || !nb.expanded {
		t.Fatal("clicking the header caption did not unfold the block")
	}
	if nb.Marked() {
		t.Fatal("clicking the header caption set the selection mark")
	}
	r.contains("eins") // the beginning appears only when unfolded

	// Clicking the header caption folds it back.
	r.click(5, chatTop)
	nb2, _ := r.m.chat.items[0].(ThinkBlock)
	if nb2.expanded {
		t.Fatal("second header click did not fold the block")
	}
	r.notContains("eins")
	if nb2.Marked() {
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
