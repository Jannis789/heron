package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

func TestTableBlockHitActionAndCopy(t *testing.T) {
	var copied string
	b := NewTableBlock(
		[][]string{{"name", "qty"}, {"apples", "3"}},
		[]render.Align{render.AlignDefault, render.AlignDefault},
		func(s string) tea.Cmd { return func() tea.Msg { copied = s; return nil } },
	)
	w := 60
	// Kein klickbarer COPY-Kasten mehr im Block-Footer (Copy = Eingabefeld-
	// Chip): HitAction bleibt ueberall false — Klicks markieren den Block.
	for _, pt := range [][2]int{{w - 6, b.Height(w) - 1}, {2, 0}, {4, 1}} {
		if b.HitAction(w, pt[0], pt[1]) {
			t.Fatalf("HitAction(%v) must be false — kein Block-Copy mehr", pt)
		}
	}
	_, cmd := b.Copy()
	if cmd == nil {
		t.Fatal("expected copy cmd")
	}
	cmd()
	if !strings.Contains(copied, "| name | qty |") {
		t.Fatalf("copied=%q", copied)
	}
}

func TestBlockListClickMarksBlock(t *testing.T) {
	var copied string
	l := NewBlockList()
	l = l.Append(NewTableBlock(
		[][]string{{"a", "b"}, {"1", "2"}},
		[]render.Align{render.AlignDefault, render.AlignDefault},
		func(s string) tea.Cmd { return func() tea.Msg { copied = s; return nil } },
	))
	w := 60
	l = l.SetSize(w, 30)
	fy := l.items[0].Height(w) - 1
	// Klick (auch auf die ehemalige COPY-Zeile) MARKIERT den Block — das
	// Kopieren uebernimmt der Chip im Eingabefeld-Footer.
	l2, _, handled := l.Update(tea.MouseMsg{
		X:      w - 6,
		Y:      fy,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if !handled {
		t.Fatal("click should mark")
	}
	if !l2.items[0].Marked() {
		t.Fatal("block not marked after click")
	}
	m := New(nil, nil, nil)
	m.chat = l2
	nm, cmd := m.copySelection()
	if cmd == nil {
		t.Fatal("chip copy should copy the marked block")
	}
	cmd()
	if !strings.Contains(copied, "| a | b |") {
		t.Fatalf("copied=%q", copied)
	}
	if !strings.Contains(nm.View(), "✓ kopiert") {
		t.Fatal("chip flash missing")
	}
}

func TestHoverSetsAndClears(t *testing.T) {
	b := NewTableBlock(
		[][]string{{"a"}, {"1"}},
		[]render.Align{render.AlignDefault},
		nil,
	)
	w := 60
	l := NewBlockList()
	l = l.Append(b)
	l = l.SetSize(w, 30)
	fy := l.items[0].Height(w) - 1
	l = l.setHoverAt(w-6, fy)
	if !l.items[0].(TableBlock).hover {
		t.Fatal("hover flag missing")
	}
	// Hover bleibt, solange die Maus ueber IRGENDEINE Zeile des Blocks
	// liegt (Progressive Disclosure haelt den Footer offen)…
	l = l.setHoverAt(2, 0)
	if !l.items[0].(TableBlock).hover {
		t.Fatal("hover must persist while over the block body")
	}
	// …und loescht erst, wenn der Block verlassen wird.
	l = l.setHoverAt(w-6, l.items[0].Height(w))
	if l.items[0].(TableBlock).hover {
		t.Fatal("hover should clear when leaving the block")
	}
}

func TestBlockListClickAfterScrollMarks(t *testing.T) {
	l := NewBlockList()
	l = l.Append(
		NewTableBlock([][]string{{"a"}, {"1"}}, []render.Align{render.AlignDefault}, nil),
		NewTableBlock([][]string{{"b"}, {"2"}}, []render.Align{render.AlignDefault}, nil),
	)
	w := 60
	h0 := l.items[0].Height(w)
	l = l.SetSize(w, 2) // viewport smaller than the first block → scrolled
	l = l.ScrollEnd()
	if l.offset == 0 {
		t.Fatalf("expected scroll offset > 0, got %d", l.offset)
	}
	// Click the footer row of the second (visible) table in visible
	// coordinates — markiert den richtigen Block trotz Scroll-Offset.
	fyAbs := h0 + l.items[1].Height(w) - 1
	visY := fyAbs - l.offset
	l2, _, handled := l.Update(tea.MouseMsg{
		X:      w - 6,
		Y:      visY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if !handled {
		t.Fatalf("scrolled click should mark: handled=%v", handled)
	}
	if !l2.items[1].Marked() {
		t.Fatal("scrolled click marked the wrong block")
	}
}
