package tui

import (
	"strconv"
	"strings"
	"testing"
)

// TestViewRendersOnlyViewport: windowed rendering must stay byte-identical to
// the old full render + slice, and must not touch blocks outside the
// viewport (that is what made long chats lag).
func TestViewRendersOnlyViewport(t *testing.T) {
	base := NewBlockList()
	for i := 0; i < 40; i++ {
		base = base.Append(NewTextBlock(strings.Repeat("wort"+strconv.Itoa(i)+" ", 12), ""))
	}
	base = base.SetSize(80, 12)
	full, total := base.all()

	for _, off := range []int{0, 3, 57, total - 12, total - 1, total} {
		l := base
		l.offset, l.follow = off, false
		left := max(off, 0)
		right := min(left+l.height, total)
		want := strings.Join(full[left:right], "\n")
		if got := l.View(); got != want {
			t.Fatalf("offset %d: windowed view differs from the full render\n got %q\nwant %q", off, got, want)
		}
	}

	// Counting widget proves the off-screen blocks are never rendered.
	var rendered int
	var items []Widget
	for i := 0; i < 40; i++ {
		items = append(items, countingBlock{TextBlock: NewTextBlock("zeile "+strconv.Itoa(i), ""), calls: &rendered})
	}
	// SetSize zuerst: Resize gibt den eingebetteten TextBlock zurueck und
	// wuerde die Zaehler-Wrapper ersetzen.
	l := NewBlockList().SetSize(80, 5)
	l.items = items
	l = l.ScrollEnd()
	_ = l.View()
	if rendered > 8 {
		t.Fatalf("View rendered %d of 40 blocks for a 5-row window", rendered)
	}
	if rendered == 0 {
		t.Fatal("View rendered nothing at all")
	}

	// Inline mode (unknown/full height) still renders everything.
	l.inline = true
	rendered = 0
	_ = l.View()
	if rendered != 40 {
		t.Fatalf("inline view rendered %d of 40 blocks", rendered)
	}
}

// countingBlock wraps a TextBlock and counts View() calls.
type countingBlock struct {
	TextBlock
	calls *int
}

func (c countingBlock) View(width int) string {
	*c.calls++
	return c.TextBlock.View(width)
}
