package render

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"golang.org/x/term"
)

// maxTableWidth caps the total rendered table width for the string-mode
// renderer (cells wrap to fit). The TUI passes its live viewport width
// instead (dynamic re-flow).
var maxTableWidth = detectWidth()

func detectWidth() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(c)); err == nil && n > 0 {
			return n
		}
	}
	if w, _, err := term.GetSize(0); err == nil && w > 0 {
		return w
	}
	return 100
}

// nodeText collects the plain (unstyled) text under a node.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := c.(*ast.Text); ok && !t.SoftLineBreak() {
				b.Write(t.Text(src))
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// WrapDisplay splits s into lines of at most w display cells (rune/emoji-aware).
// Hard break — used for code; table cells use wrapWords.
func WrapDisplay(s string, w int) []string {
	if w <= 0 || s == "" {
		return []string{s}
	}
	var lines []string
	var cur []rune
	curW := 0
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, string(cur))
			cur, curW = []rune{}, 0
			continue
		}
		ww := runewidth.RuneWidth(r)
		if len(cur) > 0 && curW+ww > w {
			lines = append(lines, string(cur))
			cur, curW = []rune{}, 0
		}
		cur = append(cur, r)
		curW += ww
	}
	lines = append(lines, string(cur))
	return lines
}

// WrapWords wraps s to w display cells at word boundaries; a single token
// wider than w falls back to a hard break. Prose must never split mid-word.
func WrapWords(s string, w int) []string {
	if w <= 0 || s == "" {
		return []string{s}
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		var cur string
		var curW int
		flush := func() {
			if cur != "" {
				lines = append(lines, cur)
				cur, curW = "", 0
			}
		}
		for _, word := range words {
			ww := runewidth.StringWidth(word)
			if ww > w {
				flush()
				lines = append(lines, WrapDisplay(word, w)...)
				continue
			}
			if cur == "" {
				cur, curW = word, ww
				continue
			}
			if curW+1+ww <= w {
				cur += " " + word
				curW += 1 + ww
			} else {
				flush()
				cur, curW = word, ww
			}
		}
		flush()
	}
	return lines
}

func padDisp(s string, w int, a Align) string {
	d := w - runewidth.StringWidth(s)
	if d <= 0 {
		return s
	}
	switch a {
	case AlignRight:
		return strings.Repeat(" ", d) + s
	case AlignCenter:
		l := d / 2
		return strings.Repeat(" ", l) + s + strings.Repeat(" ", d-l)
	default:
		return s + strings.Repeat(" ", d)
	}
}

// wrapCells wraps every cell of a row to its per-column budget (word wrap).
func wrapCells(row []string, budgets []int) [][]string {
	out := make([][]string, len(budgets))
	for c := range budgets {
		s := ""
		if c < len(row) {
			s = row[c]
		}
		out[c] = WrapWords(s, budgets[c])
	}
	return out
}

// DimsLabel builds the compact "R × C" table size label.
func DimsLabel(rows, cols int) string {
	return fmt.Sprintf("%d × %d", rows, cols)
}

// boxWidth returns the total rendered width of a box with the given column
// widths (outer borders, per-column padding, column junctions).
func boxWidth(widths []int) int {
	total := 2
	for _, w := range widths {
		total += w + 2
	}
	total += len(widths) - 1 // junctions between columns
	return total
}

// FooterLine renders the closing bottom border with label and action embedded
// as boxes: ╰─┤ label ├────────────┤ action ├─╯
// actionStyled may carry SGR sequences (focus feedback); actionW is the plain
// display width of the action. st colors the chrome (dim); the action keeps
// its own styling.
func FooterLine(totalW int, label, action, actionStyled string, actionW int, st, labelStyle lipgloss.Style, junctions []int) string {
	// No label: the dashes run straight into the action box — no empty
	// bracket chrome (FR-30 think block footer).
	if strings.TrimSpace(label) == "" {
		n := max(totalW-actionW-8, 0)
		return st.Render("└─"+strings.Repeat("─", n)) +
			st.Render("[ ") + actionStyled + st.Render(" ]─┘")
	}
	labelW := runewidth.StringWidth(label)
	spaced := totalW-labelW-actionW-12 >= 0
	pad := ""
	if spaced {
		pad = " "
	}
	labelBox := "[" + pad + label + pad + "]"
	extra := 8
	if spaced {
		extra = 12
	}
	n := totalW - labelW - actionW - extra
	if n < 0 {
		n = 0
	}
	dashStart := 2 + len([]rune(labelBox))
	inJunction := func(x int) bool {
		for _, j := range junctions {
			if j == x {
				return true
			}
		}
		return false
	}
	var dash strings.Builder
	for x := dashStart; x < dashStart+n; x++ {
		if inJunction(x) {
			dash.WriteString("┴") // column border meets the footer line
		} else {
			dash.WriteString("─")
		}
	}
	prefix := st.Render("└─") + labelStyle.Render(labelBox) + st.Render(dash.String()+"["+pad)
	suffix := st.Render(pad + "]─┘")
	return prefix + actionStyled + suffix
}

// junctionPositions returns the x positions (in cells) of the column
// junctions inside a row rendered with the given widths.
func junctionPositions(widths []int) []int {
	var js []int
	x := 1
	for k := 1; k < len(widths); k++ {
		x += widths[k-1] + 2
		js = append(js, x)
		x++
	}
	return js
}

// tableBody renders the table box down to the last data row; the bottom
// border (with embedded footer) closes it. Columns are separated by dim
// junctions, the header by a double line, data rows by separators — all in
// the theme border color; cells stay plain and wrap at word boundaries.
// Column budgets are proportional to natural content width (sqrt-weighted)
// so wide columns dominate without crushing narrow ones. width is the wrap
// budget (dynamic: pass the live viewport width).
func tableBody(cells [][]string, aligns []Align, th *Theme, width int) (lines []string, widths []int) {
	if len(cells) == 0 {
		return nil, nil
	}
	header := cells[0]
	rows := cells[1:]
	cols := len(header)
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return nil, nil
	}

	// Natural (unwrapped) content width per column.
	nat := make([]int, cols)
	for c := 0; c < cols; c++ {
		for _, row := range cells {
			s := ""
			if c < len(row) {
				s = row[c]
			}
			if w := runewidth.StringWidth(s); w > nat[c] {
				nat[c] = w
			}
		}
	}

	// Proportional budgets: natural widths when they fit, otherwise
	// sqrt-weighted shares of the available space (pi-style proportions).
	avail := width - 2 - 2*cols - (cols - 1)
	if avail < cols {
		avail = cols
	}
	budgets := make([]int, cols)
	natSum := 0
	for _, n := range nat {
		natSum += n
	}
	if natSum <= avail {
		copy(budgets, nat)
	} else {
		wSum := 0.0
		for _, n := range nat {
			wSum += math.Sqrt(float64(n))
		}
		assigned := 0
		widest := 0
		for c, n := range nat {
			b := int(math.Round(math.Sqrt(float64(n)) / wSum * float64(avail)))
			if b < 1 {
				b = 1
			}
			budgets[c] = b
			assigned += b
			if n > nat[widest] {
				widest = c
			}
		}
		if rem := avail - assigned; rem != 0 {
			budgets[widest] += rem
		}
	}

	headerW := wrapCells(header, budgets)
	rowsW := make([][][]string, 0, len(rows))
	for _, r := range rows {
		rowsW = append(rowsW, wrapCells(r, budgets))
	}

	widths = make([]int, cols)
	for c := 0; c < cols; c++ {
		wmax := 0
		for _, ln := range headerW[c] {
			if w := runewidth.StringWidth(ln); w > wmax {
				wmax = w
			}
		}
		for _, row := range rowsW {
			for _, ln := range row[c] {
				if w := runewidth.StringWidth(ln); w > wmax {
					wmax = w
				}
			}
		}
		widths[c] = wmax
	}

	// Grow the last column if the footer line would not fit.
	footerMin := runewidth.StringWidth(DimsLabel(len(rows), cols)) + len("COPY") + 12
	if footerMin > width {
		footerMin -= 4 // drop the inner box padding instead of exceeding the cap
	}
	if total := boxWidth(widths); total < footerMin {
		widths[len(widths)-1] += footerMin - total
	}

	// Fill the available width: distribute surplus evenly across columns so
	// tables span the viewport like pi renders them.
	if total := boxWidth(widths); width > total {
		extra := (width - total) / len(widths)
		for c := range widths {
			widths[c] += extra
		}
		if rem := (width - total) % len(widths); rem > 0 {
			widths[len(widths)-1] += rem
		}
	}

	bs := th.BorderStyle()
	fill := th.TableFill()
	alignOf := func(c int) Align {
		if c < len(aligns) {
			return aligns[c]
		}
		return AlignDefault
	}
	sep := func(left, mid, right, hor string) string {
		var b strings.Builder
		b.WriteString(bs.Render(left))
		for c, w := range widths {
			b.WriteString(bs.Render(strings.Repeat(hor, w+2)))
			if c < len(widths)-1 {
				b.WriteString(bs.Render(mid))
			}
		}
		b.WriteString(bs.Render(right))
		return b.String()
	}
	rowLines := func(cells [][]string, header bool) []string {
		maxLines := 0
		for _, ls := range cells {
			if len(ls) > maxLines {
				maxLines = len(ls)
			}
		}
		var out []string
		for i := 0; i < maxLines; i++ {
			var b strings.Builder
			b.WriteString(bs.Render("│"))
			for c := 0; c < cols; c++ {
				ln := ""
				if i < len(cells[c]) {
					ln = cells[c][i]
				}
				cellStyle := fill
				if header {
					cellStyle = th.HeaderStyle()
				}
				b.WriteString(cellStyle.Render(" " + padDisp(ln, widths[c], alignOf(c)) + " "))
				b.WriteString(bs.Render("│"))
			}
			out = append(out, b.String())
		}
		return out
	}

	lines = append(lines, sep("┌", "┬", "┐", "─"))
	if len(header) > 0 {
		lines = append(lines, rowLines(headerW, true)...)
		// Double-line header separation so the column header stays recognizable.
		lines = append(lines, sep("╞", "╪", "╡", "═"))
	}
	for i, row := range rowsW {
		lines = append(lines, rowLines(row, false)...)
		// Dim separator between data rows so line ends stay visible.
		if i < len(rowsW)-1 {
			lines = append(lines, sep("├", "┼", "┤", "─"))
		}
	}
	return lines, widths
}

// BoxWidth returns the total rendered width of a box with the given column
// widths (outer borders, per-column padding, column junctions).
func BoxWidth(widths []int) int { return boxWidth(widths) }

// ActionRange returns the [start,end) cell positions of the action box in a
// footer line of totalW cells (for mouse hit-testing).
func ActionRange(totalW int, label string, actionW int) (int, int) {
	// Labelless footer (FR-30 think block): "└─" + dashes + "[ action ]─┘".
	// The action box spans actionW+4 cells and ends two cells before the
	// corner (" ]─┘").
	if strings.TrimSpace(label) == "" {
		return totalW - actionW - 6, totalW - 2
	}
	labelW := runewidth.StringWidth(label)
	spaced := totalW-labelW-actionW-12 >= 0
	if spaced {
		return totalW - 8 - actionW, totalW - 4
	}
	return totalW - 5 - actionW, totalW - 3
}

// TableBodyLines renders the table body (no footer line) plus column widths,
// wrapping cells to fit width (dynamic re-flow: pass the live width).
func TableBodyLines(cells [][]string, aligns []Align, th *Theme, width int) (lines []string, widths []int) {
	return tableBody(cells, aligns, th, width)
}

// CenteredActionLine renders a closing border with the action box centered:
// ╰─────[ + mehr anzeigen ]─────╯ — dashes on both sides, so the box never
// touches the corners (FR-30 think block).
func CenteredActionLine(totalW int, action, actionStyled string, actionW int, st lipgloss.Style) string {
	box := actionW + 4 // "[ " + action + " ]"
	n := totalW - box - 2
	if n < 0 {
		n = 0
	}
	left := n / 2
	return st.Render("└"+strings.Repeat("─", left)) +
		st.Render("[ ") + actionStyled + st.Render(" ]") +
		st.Render(strings.Repeat("─", n-left)+"┘")
}

// CenteredActionRange returns the [start,end) cell positions of the centered
// action box (mouse hit-testing, FR-30).
func CenteredActionRange(totalW, actionW int) (int, int) {
	box := actionW + 4
	n := totalW - box - 2
	if n < 0 {
		n = 0
	}
	left := n / 2
	return 1 + left, 1 + left + box
}

// TableFooterLine renders the closing bottom border of a table with the size
// label and the copy action embedded; chrome colored via the theme border.
func TableFooterLine(widths []int, rows, cols int, th *Theme, action, actionStyled string, actionW int) string {
	return FooterLine(boxWidth(widths), DimsLabel(rows, cols), action, th.ActionStyle().Render(actionStyled), actionW, th.BorderStyle(), th.LabelStyle(), junctionPositions(widths))
}

// renderTable renders a goldmark table as a bordered box whose bottom border
// embeds the size label and copy action. String mode: the width cap is
// maxTableWidth (COLUMNS/terminal at startup).
func renderTable(t *extast.Table, src []byte, th *Theme) string {
	cells, aligns := tableData(t, src)
	body, widths := tableBody(cells, aligns, th, maxTableWidth)
	if len(body) == 0 {
		return ""
	}
	rows := len(cells) - 1
	if rows < 0 {
		rows = 0
	}
	lines := append(body, TableFooterLine(widths, rows, len(widths), th, "COPY", "COPY", len("COPY")))
	return strings.Join(lines, "\n") + "\n"
}
