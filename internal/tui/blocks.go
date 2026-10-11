package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// CopyFunc builds a tea.Cmd that copies text when the runtime executes it.
// The write must not happen at call time — effects live in Cmds (Elm).
type CopyFunc func(text string) tea.Cmd

// Widget is a single chat block as a nested Elm component: state, an Update
// returning the next state, and a pure View. SetFocused/SetHover return
// copies instead of mutating. HitAction reports whether a cell lies on the
// COPY action; Copy triggers it (mouse path).
type Widget interface {
	Height(width int) int
	View(width int) string
	Update(msg tea.Msg) (Widget, tea.Cmd)
	Focused() bool
	Focusable() bool
	SetFocused(bool) Widget
	SetHover(bool) Widget
	SetMarked(bool) Widget
	Marked() bool
	// Markdown returns the raw markdown source of the block — the copy
	// payload for selections (FR-29), never the ANSI-rendered view.
	Markdown() string
	Resize(width int) Widget
	HitAction(width, x, y int) bool
	Copy() (Widget, tea.Cmd)
	// ID is the stable block number (1-based, assigned to copyable blocks) or
	// -1 when the block is not addressable (FR-3).
	ID() int
	SetID(int) Widget
}

// actionWidget is a block whose footer action does something of its own:
// COPY copies (CodeBlock/TableBlock), AUFKLAPPEN/ZUKLAPPEN folds the think
// block (FR-30). Mouse clicks dispatch through Action — never through Copy,
// which is the copy-only keyboard path (alt+<i>, y).
type actionWidget interface {
	Action() (Widget, tea.Cmd)
}

func textColorFor(th *render.Theme) string {
	if th == nil {
		return ""
	}
	return th.Text.Color
}

// textWrapped wraps prose to w display cells at word boundaries, preserving
// line structure. Hard-wrapping would split words mid-syllable.
func textWrapped(s string, w int) []string {
	var lines []string
	if w <= 0 {
		w = 80
	}
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, render.WrapWords(l, w)...)
	}
	return lines
}

// NewHeadingBlock renders a markdown section title on its own row, bold so it
// separates from the body text around it.
func NewHeadingBlock(text string) TextBlock {
	b := NewTextBlock(text, "")
	b.style = lipgloss.NewStyle().Bold(true)
	return b
}

// actionStyle is the shared hover/focus treatment for footer actions —
// reversed when focused, underlined when hovered. COPY and AUFKLAPPEN must
// stay visually identical (FR-30).
func actionStyle(focused, hover bool) lipgloss.Style {
	st := lipgloss.NewStyle()
	if focused {
		st = st.Reverse(true)
	} else if hover {
		st = st.Underline(true)
	}
	return st
}

// copyAction returns the plain action label and a styled variant: MARKED
// while the block is in the selection (FR-29), COPIED after a copy, styled
// by actionStyle (reversed when focused, underlined when hovered).
func copyAction(copied, marked, focused, hover bool) (string, string) {
	label := "COPY"
	if marked {
		label = "MARKED"
	}
	if copied {
		label = "COPIED"
	}
	return label, actionStyle(focused, hover).Render(label)
}

// actionWidth is the display width of the current action label.
func actionWidth(copied, marked bool) int {
	if copied {
		return runewidth.StringWidth("COPIED")
	}
	if marked {
		return runewidth.StringWidth("MARKED")
	}
	return runewidth.StringWidth("COPY")
}

// ---- TextBlock -------------------------------------------------------------

// TextBlock is a plain, non-interactive chat message.
type TextBlock struct {
	id     int
	text   string
	style  lipgloss.Style
	marked bool
}

func NewTextBlock(text, color string) TextBlock {
	s := lipgloss.NewStyle()
	if color != "" {
		s = s.Foreground(lipgloss.Color(color))
	}
	return TextBlock{id: -1, text: text, style: s}
}

// NewUserBlock renders the user's own typed prompt (highlighted).
func NewUserBlock(text string) TextBlock {
	return TextBlock{id: -1, text: text, style: lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)}
}

// textGutter indents prose blocks so answers breathe next to the framed
// code/table blocks — the text starts flush with the code content inside
// the frames (border plus one padding cell), like an editor's margin.
const textGutter = 2

// wrapWidth is the prose wrap width for a terminal width: the gutter comes
// out of the wrap budget, except on very narrow terminals where it would
// squeeze the text below usability.
func (b TextBlock) wrapWidth(width int) int {
	if w := width - textGutter; w >= 8 {
		return w
	}
	return width
}

func (b TextBlock) Height(width int) int { return len(textWrapped(b.text, b.wrapWidth(width))) }
func (b TextBlock) View(width int) string {
	wrapW := b.wrapWidth(width)
	pad := strings.Repeat(" ", textGutter)
	if wrapW == width {
		pad = "" // too narrow for a gutter
	}
	lines := textWrapped(b.text, wrapW)
	st := b.style
	if b.marked {
		// Marked prose inverts instead of growing a frame, so the wrap
		// width stays untouched (FR-29).
		st = st.Reverse(true)
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = pad + st.Render(l)
	}
	return strings.Join(out, "\n")
}

func (b TextBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}

func (b TextBlock) Marked() bool { return b.marked }

// Markdown returns the stored text — plain, without ANSI (FR-29).
func (b TextBlock) Markdown() string { return b.text }
func (b TextBlock) Update(msg tea.Msg) (Widget, tea.Cmd) { return b, nil }
func (b TextBlock) Focused() bool                        { return false }
func (b TextBlock) Focusable() bool                      { return false }
func (b TextBlock) SetFocused(bool) Widget               { return b }
func (b TextBlock) SetHover(bool) Widget                 { return b }
func (b TextBlock) Resize(width int) Widget              { return b }
func (b TextBlock) HitAction(width, x, y int) bool       { return false }
func (b TextBlock) Copy() (Widget, tea.Cmd)              { return b, nil }
func (b TextBlock) ID() int                             { return b.id }
func (b TextBlock) SetID(i int) Widget                  { nb := b; nb.id = i; return nb }

// ---- CodeBlock -------------------------------------------------------------

type CodeBlock struct {
	id         int
	lang, code string
	th         *render.Theme
	copyFn     CopyFunc
	focused    bool
	copied     bool
	marked     bool
	hover      bool
	// converted marks a plantuml block whose ASCII conversion was attempted:
	// the ASCII replaced the source on success, the raw source stays on error
	// (FR-6 fallback). It prevents the block from being queued again.
	converted bool
}

func NewCodeBlock(lang, code string, copyFn CopyFunc) CodeBlock {
	// Tabs measure as width 0, which would break the box padding — expand them.
	return CodeBlock{id: -1, lang: lang, code: strings.ReplaceAll(strings.TrimSuffix(code, "\n"), "\t", "    "), copyFn: copyFn}
}

// codeInnerWidth is the room for code inside the frame: two border cells plus
// one padding cell on each side.
func codeInnerWidth(width int) int {
	w := width - 4
	if w < 8 {
		w = 8
	}
	return w
}

// bodyLines renders the code for a width: syntax highlighted, wrapped to the
// inner width, one entry per row. Height and View both use it, so the footer
// row and the mouse hit area can never disagree — they used to wrap at
// different widths, which drew the frame too short for long code.
func (b CodeBlock) bodyLines(width int) []string {
	return render.HighlightLines(b.lang, b.code, codeInnerWidth(width))
}

// linesAt returns the wrapped body line count for width.
func (b CodeBlock) linesAt(width int) int {
	return len(b.bodyLines(width))
}

// labelAt builds the footer label for a body line count.
func labelAt(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

func (b CodeBlock) Height(width int) int {
	return 1 + b.linesAt(width) + 1
}
func (b CodeBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	body := b.bodyLines(width)
	inner := codeInnerWidth(width)
	bs := b.th.BorderStyle()
	fill := b.th.TableFill()

	lang := b.lang
	if lang == "" {
		lang = "code"
	}
	// Stable block number in the header: the keyboard copy fallback (alt+<n>)
	// and the mouse both address this id (FR-3/FR-5).
	if b.id > 0 {
		lang = fmt.Sprintf("%s #%d", lang, b.id)
	}
	// Frame in the theme border colour, with the label padded away from the
	// corners and tinted with the action colour so it stays legible.
	labelW := runewidth.StringWidth(lang)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	lines := make([]string, 0, len(body)+2)
	lines = append(lines, bs.Render("┌─ ")+b.th.ActionStyle().Render(lang)+
		bs.Render(" "+strings.Repeat("─", dashes)+"┐"))

	for _, ln := range body {
		gap := inner - lipgloss.Width(ln)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+" "+
			fill.Render(ln)+fill.Render(strings.Repeat(" ", gap))+" "+
			bs.Render("│"))
	}

	action, styled := copyAction(b.copied, b.marked, b.focused, b.hover)
	styled = b.th.ActionStyle().Render(styled)
	lines = append(lines, render.FooterLine(width, labelAt(len(body)), action, styled,
		runewidth.StringWidth(action), lipgloss.NewStyle(), b.th.LabelStyle(), nil))
	return strings.Join(lines, "\n")
}
func (b CodeBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		key := km.String()
		if (key == "enter" || key == "c") && b.focused {
			return b.Copy()
		}
	}
	return b, nil
}
func (b CodeBlock) Focused() bool   { return b.focused }
func (b CodeBlock) Focusable() bool { return true }
func (b CodeBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	if !f {
		nb.copied = false
	}
	return nb
}
func (b CodeBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	return nb
}
func (b CodeBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b CodeBlock) Marked() bool { return b.marked }

// Markdown returns the block as a fenced markdown snippet (FR-29).
func (b CodeBlock) Markdown() string {
	return "```" + b.lang + "\n" + b.code + "\n```"
}
func (b CodeBlock) Resize(width int) Widget { return b } // lays out per View anyway
func (b CodeBlock) HitAction(width, x, y int) bool {
	n := b.linesAt(width)
	if y != 1+n {
		return false
	}
	start, end := render.ActionRange(width, labelAt(n), actionWidth(b.copied, b.marked))
	return x >= start && x < end
}
// Action: clicking COPY copies, identical to the keyboard copy path (FR-5).
func (b CodeBlock) Action() (Widget, tea.Cmd) { return b.Copy() }

func (b CodeBlock) Copy() (Widget, tea.Cmd) {
	if b.copied || b.copyFn == nil {
		return b, nil
	}
	nb := b
	nb.copied = true
	return nb, b.copyFn(b.code)
}

func (b CodeBlock) ID() int { return b.id }

func (b CodeBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- TableBlock ------------------------------------------------------------

// TableBlock renders a markdown table, laid out dynamically against the width
// it is given (re-flows on terminal resize). Chrome (borders, row separators)
// renders via the theme; cells stay plain.
type TableBlock struct {
	id      int
	cells   [][]string
	aligns  []render.Align
	th      *render.Theme
	copyFn  CopyFunc
	focused bool
	copied  bool
	marked  bool
	hover   bool

	cacheW int      // width the cache was built for (0 = none)
	body   []string // cached body lines (no footer)
	widths []int    // cached per-column widths
}

func NewTableBlock(cells [][]string, aligns []render.Align, copyFn CopyFunc) TableBlock {
	return TableBlock{id: -1, cells: cells, aligns: aligns, copyFn: copyFn}
}

// layout returns the body lines and column widths for width.
func (b TableBlock) layout(width int) ([]string, []int) {
	if b.body != nil && b.cacheW == width {
		return b.body, b.widths
	}
	return render.TableBodyLines(b.cells, b.aligns, b.th, width)
}

func (b TableBlock) Height(width int) int {
	body, _ := b.layout(width)
	if len(body) == 0 {
		return 0
	}
	return len(body) + 1
}
func (b TableBlock) View(width int) string {
	body, widths := b.layout(width)
	if len(body) == 0 {
		return ""
	}
	rows := len(b.cells) - 1
	if rows < 0 {
		rows = 0
	}
	action, styled := copyAction(b.copied, b.marked, b.focused, b.hover)
	lines := append(append([]string{}, body...),
		render.TableFooterLine(widths, rows, len(widths), b.th, action, styled, runewidth.StringWidth(action)))
	return strings.Join(lines, "\n")
}
func (b TableBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		key := km.String()
		if (key == "enter" || key == "c") && b.focused {
			return b.Copy()
		}
	}
	return b, nil
}
func (b TableBlock) Focused() bool   { return b.focused }
func (b TableBlock) Focusable() bool { return true }
func (b TableBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	if !f {
		nb.copied = false
	}
	return nb
}
func (b TableBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	return nb
}

// Resize rebuilds the layout cache for width (called on WindowSizeMsg).
func (b TableBlock) Resize(width int) Widget {
	if b.body != nil && b.cacheW == width {
		return b
	}
	nb := b
	nb.body, nb.widths = render.TableBodyLines(b.cells, b.aligns, b.th, width)
	nb.cacheW = width
	return nb
}
func (b TableBlock) HitAction(width, x, y int) bool {
	body, widths := b.layout(width)
	if len(body) == 0 || y != len(body) {
		return false
	}
	rows := len(b.cells) - 1
	if rows < 0 {
		rows = 0
	}
	start, end := render.ActionRange(render.BoxWidth(widths), render.DimsLabel(rows, len(widths)), actionWidth(b.copied, b.marked))
	return x >= start && x < end
}

// Action: clicking COPY copies, identical to the keyboard copy path (FR-5).
func (b TableBlock) Action() (Widget, tea.Cmd) { return b.Copy() }

// Copy copies the table as markdown source.
func (b TableBlock) Copy() (Widget, tea.Cmd) {
	if b.copied || b.copyFn == nil {
		return b, nil
	}
	nb := b
	nb.copied = true
	return nb, b.copyFn(render.TableMarkdown(b.cells))
}
func (b TableBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b TableBlock) Marked() bool { return b.marked }

// Markdown returns the table as markdown source (FR-29).
func (b TableBlock) Markdown() string { return render.TableMarkdown(b.cells) }

func (b TableBlock) ID() int { return b.id }

func (b TableBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- ThinkBlock ------------------------------------------------------------

// thinkCollapsed caps the visible body lines of a collapsed ThinkBlock (FR-30).
const thinkCollapsed = 3

// ThinkBlock shows the model's thinking as a collapsible chat block: long
// thinking is truncated to a few lines until unfolded (FR-30).
type ThinkBlock struct {
	id       int
	text     string
	expanded bool
	focused  bool
	hover    bool
	marked   bool
	th       *render.Theme
}

func NewThinkBlock(text string, th *render.Theme) ThinkBlock {
	return ThinkBlock{id: -1, text: text, th: th}
}

func (b ThinkBlock) bodyLines() []string {
	return strings.Split(strings.TrimRight(b.text, "\n"), "\n")
}

// wrappedBody wraps the thinking to the inner width. The model streams
// thoughts as one long line — truncating per row would hide everything but
// the start, so lines break at word boundaries instead (FR-30).
func (b ThinkBlock) wrappedBody(inner int) []string {
	if inner <= 0 {
		return b.bodyLines()
	}
	var out []string
	for _, ln := range b.bodyLines() {
		if ln == "" {
			out = append(out, "")
			continue
		}
		out = append(out, render.WrapWords(ln, inner)...)
	}
	return out
}

// bodyRows is the rendered body row count for the current fold state: all
// wrapped rows when expanded, otherwise the LAST thinkCollapsed rows (the
// conclusion of the thinking).
func (b ThinkBlock) bodyRows(width int) int {
	rows := b.wrappedBody(codeInnerWidth(width))
	if b.expanded || len(rows) <= thinkCollapsed {
		return len(rows)
	}
	return thinkCollapsed
}

func (b ThinkBlock) Height(width int) int { return 1 + b.bodyRows(width) + 1 }

// label is the header caption: fold state, line count, selection mark.
func (b ThinkBlock) label() string {
	state := "▸"
	if b.expanded {
		state = "▾"
	}
	l := fmt.Sprintf("denken %s %d Zeilen", state, len(b.bodyLines()))
	if b.marked {
		l += " ✓"
	}
	return l
}

func (b ThinkBlock) actionLabel() string {
	if b.expanded {
		return "ZUKLAPPEN"
	}
	return "AUFKLAPPEN"
}

// action returns the plain fold label and its styled variant — the same
// actionStyle treatment as COPY (reversed when focused, underlined when
// hovered), wrapped in the theme action colour.
func (b ThinkBlock) action() (string, string) {
	action := b.actionLabel()
	return action, b.th.ActionStyle().Render(actionStyle(b.focused, b.hover).Render(action))
}

// truncateCells cuts a line to w display cells, marking the cut with an
// ellipsis.
func truncateCells(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

func (b ThinkBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	inner := codeInnerWidth(width)
	bs := b.th.BorderStyle()
	ls := b.th.LabelStyle()

	label := b.label()
	labelW := runewidth.StringWidth(label)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	lines := make([]string, 0, b.Height(width))
	// The caption is a fold hit target (HitAction y==0) — same hover/focus
	// treatment as the footer action.
	lines = append(lines, bs.Render("┌─ ")+b.th.ActionStyle().Render(actionStyle(b.focused, b.hover).Render(label))+
		bs.Render(" "+strings.Repeat("─", dashes)+"┐"))

	rows := b.wrappedBody(inner)
	// Collapsed shows the END of the thinking — the conclusion lives there.
	start := 0
	if !b.expanded && len(rows) > thinkCollapsed {
		start = len(rows) - thinkCollapsed
	}
	for _, ln := range rows[start:] {
		txt := ls.Render(ln)
		gap := inner - lipgloss.Width(txt)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+" "+txt+strings.Repeat(" ", gap)+" "+bs.Render("│"))
	}

	action, styled := b.action()
	// No left label: the header above already names the block ("denken") —
	// the footer carries only the fold action.
	lines = append(lines, render.FooterLine(width, "", action, styled,
		runewidth.StringWidth(action), lipgloss.NewStyle(), b.th.LabelStyle(), nil))
	return strings.Join(lines, "\n")
}

func (b ThinkBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		key := km.String()
		if (key == "enter" || key == "c") && b.focused {
			nb := b
			nb.expanded = !nb.expanded
			return nb, nil
		}
	}
	return b, nil
}
func (b ThinkBlock) Focused() bool    { return b.focused }
func (b ThinkBlock) Focusable() bool  { return true }
func (b ThinkBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}
func (b ThinkBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	return nb
}
func (b ThinkBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b ThinkBlock) Marked() bool { return b.marked }

// Markdown returns the raw thinking text — plain, without ANSI (FR-29).
func (b ThinkBlock) Markdown() string { return b.text }
func (b ThinkBlock) Resize(width int) Widget {
	return b
}
func (b ThinkBlock) HitAction(width, x, y int) bool {
	// The header caption ("denken ▸ n Zeilen") folds too — clicking the
	// section title is the natural unfold, and it must NOT toggle the
	// selection mark (FR-30).
	if y == 0 {
		return x >= 3 && x < 3+runewidth.StringWidth(b.label())
	}
	if y != 1+b.bodyRows(width) {
		return false
	}
	// The think footer carries no left label — the header names the block.
	start, end := render.ActionRange(width, "", runewidth.StringWidth(b.actionLabel()))
	return x >= start && x < end
}
func (b ThinkBlock) Copy() (Widget, tea.Cmd) { return b, nil }

// Action: clicking the fold action toggles the block (FR-30) — no focus
// needed, same semantics as pressing enter on the focused block.
func (b ThinkBlock) Action() (Widget, tea.Cmd) {
	nb := b
	nb.expanded = !b.expanded
	return nb, nil
}
func (b ThinkBlock) ID() int                 { return b.id }
func (b ThinkBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- build -----------------------------------------------------------------

// BuildWidgets turns a structured block tree into interactive widgets.
func BuildWidgets(nodes []render.Node, th *render.Theme, copyFn CopyFunc) []Widget {
	tc := textColorFor(th)
	out := make([]Widget, 0, len(nodes))
	for _, n := range nodes {
		switch n.Kind {
		case render.NodeText:
			out = append(out, NewTextBlock(n.Text, tc))
		case render.NodeHeading:
			hb := NewHeadingBlock(n.Text)
			if th != nil && th.Action.Color != "" {
				hb.style = lipgloss.NewStyle().Bold(true).
					Foreground(lipgloss.Color(th.Action.Color))
			}
			out = append(out, hb)
		case render.NodeCode:
			cb := NewCodeBlock(n.Lang, n.Code, copyFn)
			cb.th = th
			out = append(out, cb)
		case render.NodeTable:
			tb := NewTableBlock(n.Cells, n.Aligns, copyFn)
			tb.th = th
			out = append(out, tb)
		}
	}
	return out
}
