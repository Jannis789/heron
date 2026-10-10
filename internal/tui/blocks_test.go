package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

func TestCodeBlockCopiesOnEnter(t *testing.T) {
	var copied string
	b := NewCodeBlock("go", "x := 1\n", func(s string) tea.Cmd {
		return func() tea.Msg { copied = s; return nil }
	})
	b = b.SetFocused(true).(CodeBlock)
	w, cmd := b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a deferred copy cmd")
	}
	if copied != "" {
		t.Fatalf("copy must not run at Update time, got %q", copied)
	}
	cmd() // the runtime executes the effect
	if copied != "x := 1" {
		t.Fatalf("copied=%q", copied)
	}
	cb := w.(CodeBlock)
	if !cb.copied {
		t.Fatalf("block should be marked copied")
	}
	if !strings.Contains(cb.View(40), "COPIED") {
		t.Fatalf("view should show COPIED: %q", cb.View(40))
	}
}

func TestCodeBlockExpandsTabsForAlignment(t *testing.T) {
	b := NewCodeBlock("go", "func f() {\n\tfmt.Println(1)\n}\n", nil)
	for i, ln := range strings.Split(b.View(46), "\n") {
		if !strings.HasSuffix(ln, "│") && !strings.HasSuffix(ln, "┐") && !strings.HasSuffix(ln, "┘") {
			t.Fatalf("line %d not box-aligned: %q", i, ln)
		}
		if w := runewidth.StringWidth(ln); w > 46 {
			t.Fatalf("line %d too wide (%d): %q", i, w, ln)
		}
	}
}

func TestCodeBlockDoesNotReCopy(t *testing.T) {
	var calls int
	b := NewCodeBlock("sh", "echo hi", func(string) tea.Cmd {
		return func() tea.Msg { calls++; return nil }
	})
	b = b.SetFocused(true).(CodeBlock)
	w, cmd := b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if _, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	if calls != 1 {
		t.Fatalf("expected 1 copy, got %d", calls)
	}
}

func TestTableBlockCopiesMarkdown(t *testing.T) {
	var copied string
	b := NewTableBlock(
		[][]string{{"name", "qty"}, {"apples", "3"}, {"pears", "5"}},
		[]render.Align{render.AlignDefault, render.AlignDefault},
		func(s string) tea.Cmd {
			return func() tea.Msg { copied = s; return nil }
		},
	)
	b = b.SetFocused(true).(TableBlock)
	_, cmd := b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a copy cmd")
	}
	cmd()
	if !strings.Contains(copied, "| name | qty |") || !strings.Contains(copied, "| apples | 3 |") {
		t.Fatalf("markdown=%q", copied)
	}
	if !strings.Contains(b.View(60), "2 × 2") {
		t.Fatalf("footer should show size label: %q", b.View(60))
	}
}

func TestTableBlockReflowsOnResize(t *testing.T) {
	b := NewTableBlock(
		[][]string{{"col"}, {"a very long cell that must wrap to fit"}},
		[]render.Align{render.AlignDefault},
		nil,
	)
	narrow := b.View(20)
	wide := b.View(100)
	if narrow == wide {
		t.Fatal("table should reflow with width")
	}
	for i, ln := range strings.Split(narrow, "\n") {
		if w := runewidth.StringWidth(ln); w > 20 {
			t.Fatalf("narrow line %d too wide (%d): %q", i, w, ln)
		}
	}
	// Resize caches the layout for that width; Height uses the cache.
	r := b.Resize(60).(TableBlock)
	if r.cacheW != 60 || r.body == nil {
		t.Fatalf("resize should cache layout for 60, cacheW=%d body=%v", r.cacheW, r.body != nil)
	}
	if r.Height(60) != len(r.body)+1 {
		t.Fatalf("height=%d want %d (cache)", r.Height(60), len(r.body)+1)
	}
	// A different width ignores the cache and lays out dynamically.
	if r.Height(100) == 0 {
		t.Fatal("fallback layout missing")
	}
}

func TestBlockListResizeRebuildsWidgets(t *testing.T) {
	l := NewBlockList()
	l = l.Append(NewTableBlock([][]string{{"a"}, {"b"}}, []render.Align{render.AlignDefault}, nil))
	l = l.SetSize(42, 10)
	tb := l.items[0].(TableBlock)
	if tb.cacheW != 42 || tb.body == nil {
		t.Fatalf("SetSize should rebuild layout cache, cacheW=%d", tb.cacheW)
	}
}

func TestBlockListHeightAndScroll(t *testing.T) {
	l := NewBlockList()
	l = l.Append(NewTextBlock("line one", ""), NewTextBlock("line two", ""))
	if l.totalLines() != 2 {
		t.Fatalf("total=%d want 2", l.totalLines())
	}
	l = l.SetSize(40, 1)
	l = l.ScrollEnd()
	if l.offset != 1 {
		t.Fatalf("offset=%d want 1", l.offset)
	}
	v := l.View()
	if strings.Contains(v, "line one") {
		t.Fatalf("view(1 line) should not show the first line: %q", v)
	}
}

func TestBlockListFocusCycles(t *testing.T) {
	l := NewBlockList()
	l = l.Append(
		NewTextBlock("text", ""),
		NewCodeBlock("go", "a", nil),
		NewCodeBlock("go", "b", nil),
	)
	var handled bool
	l, _, handled = l.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !handled || l.focus != 1 {
		t.Fatalf("first tab focus=%d want 1", l.focus)
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyTab})
	if l.focus != 2 {
		t.Fatalf("second tab focus=%d want 2", l.focus)
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyTab})
	if l.focus != 1 {
		t.Fatalf("wrap focus=%d want 1", l.focus)
	}
}
