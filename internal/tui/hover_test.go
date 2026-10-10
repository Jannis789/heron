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
	fy := b.Height(w) - 1
	if !b.HitAction(w, w-6, fy) {
		t.Fatalf("expected COPY hit at x=%d y=%d", w-6, fy)
	}
	if b.HitAction(w, 2, 0) {
		t.Fatal("header is not the action")
	}
	w2, cmd := b.Copy()
	if cmd == nil {
		t.Fatal("expected copy cmd")
	}
	cmd()
	if !strings.Contains(copied, "| name | qty |") {
		t.Fatalf("copied=%q", copied)
	}
	if !w2.(TableBlock).copied {
		t.Fatal("copied flag missing")
	}
}

func TestBlockListClickCopiesMarkdown(t *testing.T) {
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
	l2, cmd, handled := l.Update(tea.MouseMsg{
		X:      w - 6,
		Y:      fy,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if !handled || cmd == nil {
		t.Fatalf("click should copy: handled=%v cmd=%v", handled, cmd != nil)
	}
	cmd()
	if !strings.Contains(copied, "| a | b |") {
		t.Fatalf("copied=%q", copied)
	}
	if !l2.items[0].(TableBlock).copied {
		t.Fatal("widget should be marked copied")
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
	l = l.setHoverAt(2, 0)
	if l.items[0].(TableBlock).hover {
		t.Fatal("hover should clear")
	}
}

func TestBlockListClickAfterScrollCopies(t *testing.T) {
	var copied string
	l := NewBlockList()
	l = l.Append(
		NewTableBlock([][]string{{"a"}, {"1"}}, []render.Align{render.AlignDefault}, nil),
		NewTableBlock([][]string{{"b"}, {"2"}}, []render.Align{render.AlignDefault},
			func(s string) tea.Cmd { return func() tea.Msg { copied = s; return nil } }),
	)
	w := 60
	h0 := l.items[0].Height(w)
	l = l.SetSize(w, 2) // viewport smaller than the first block → scrolled
	l = l.ScrollEnd()
	if l.offset == 0 {
		t.Fatalf("expected scroll offset > 0, got %d", l.offset)
	}
	// Click the footer of the second (visible) table in visible coordinates.
	fyAbs := h0 + l.items[1].Height(w) - 1
	visY := fyAbs - l.offset
	l2, cmd, handled := l.Update(tea.MouseMsg{
		X:      w - 6,
		Y:      visY,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if !handled || cmd == nil {
		t.Fatalf("scrolled click should copy: handled=%v cmd=%v", handled, cmd != nil)
	}
	cmd()
	if !strings.Contains(copied, "| b |") {
		t.Fatalf("copied=%q", copied)
	}
	if !l2.items[1].(TableBlock).copied {
		t.Fatal("widget should be marked copied")
	}
}
