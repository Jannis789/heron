package render

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// stripANSI removes SGR escape sequences so visual display width can be measured.
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case in:
			if (r >= '0' && r <= '9') || r == ';' {
				continue
			}
			if r == 'm' {
				in = false
			}
			continue
		case r == 0x1b:
			in = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func visW(ln string) int { return runewidth.StringWidth(stripANSI(ln)) }

func TestTableWrapsAndFitsWidth(t *testing.T) {
	old := maxTableWidth
	maxTableWidth = 18
	defer func() { maxTableWidth = old }()

	th, _ := DefaultTheme()
	md := "| Col |\n|-----|\n| a very long text that must wrap |\n"
	out, err := RenderText(md, th)
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	for i, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := visW(ln); w > 18 {
			t.Fatalf("line %d exceeds 18 (w=%d): %q", i, w, stripANSI(ln))
		}
	}
	if strings.Count(out, "┌") != 1 || strings.Count(out, "┘") != 1 {
		t.Fatalf("expected a single closed table:%s", "\n"+stripANSI(out))
	}
}

func TestTableColumnsAlignByDisplayWidth(t *testing.T) {
	th, _ := DefaultTheme()
	md := "| Änd | Name |\n|-----|------|\n| ü   | ß    |\n| 🧇  | äöü  |\n"
	out, err := RenderText(md, th)
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	first := -1
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		w := visW(ln)
		if first < 0 {
			first = w
		} else if w != first {
			t.Fatalf("columns misaligned (first=%d, got=%d): %q", first, w, stripANSI(ln))
		}
	}
	t.Logf("visual table:%s", "\n"+stripANSI(out))
}

func TestTableHeaderIsBold(t *testing.T) {
	th, _ := DefaultTheme()
	out, err := RenderText("| A |\n|---|\n| b |\n", th)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("short render: %q", out)
	}
	// The cell is the segment between the two bold border glyphs.
	cellOf := func(ln string) string {
		parts := strings.Split(ln, "\x1b[0m")
		if len(parts) < 2 {
			return ""
		}
		return parts[1]
	}
	if !strings.Contains(cellOf(lines[1]), "\x1b[1") {
		t.Fatalf("header not bold: %q", cellOf(lines[1]))
	}
	// lines[2] is the bold ╞═══╡ header separation; the data row follows it.
	if strings.Contains(cellOf(lines[3]), "\x1b[1") {
		t.Fatalf("data cell must not be bold: %q", cellOf(lines[3]))
	}
}

func TestTableFooterShowsRowCountAndCopyIcon(t *testing.T) {
	th, _ := DefaultTheme()
	md := "| A  | B  |\n|----|----|\n| 1  | 2  |\n| 3  | 4  |\n| 5  | 6  |\n"
	out, err := RenderText(md, th)
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	vis := stripANSI(out)
	if !strings.Contains(vis, "3 × 2") {
		t.Fatalf("size label missing:\n%s", vis)
	}
	if !strings.Contains(vis, "COPY") {
		t.Fatalf("copy action missing:\n%s", vis)
	}
	if !strings.Contains(vis, "┴") {
		t.Fatalf("footer junction missing:\n%s", vis)
	}
	for _, want := range []string{"┬", "┼", "╞", "╪", "╡"} {
		if !strings.Contains(vis, want) {
			t.Fatalf("junction %q fehlt:\n%s", want, vis)
		}
	}
	first := -1
	for _, ln := range strings.Split(strings.TrimRight(vis, "\n"), "\n") {
		w := runewidth.StringWidth(ln)
		if first < 0 {
			first = w
		} else if w != first {
			t.Fatalf("footer/box width mismatch (first=%d got=%d): %q", first, w, ln)
		}
	}
}

func TestTableFooterSingularRowLabel(t *testing.T) {
	th, _ := DefaultTheme()
	md := "| A  | B  |\n|----|----|\n| 1  | 2  |\n"
	out, err := RenderText(md, th)
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	if !strings.Contains(stripANSI(out), "1 × 2") {
		t.Fatalf("singular row label missing:\n%s", stripANSI(out))
	}
}
