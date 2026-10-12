package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// The prompt box hugs one row and grows as the user types, without ever
// eating more than a share of the screen.
const (
	inputMinHeight = 1
	inputMaxHeight = 8
	// inputChromeRows are the two frame rows of the prompt box.
	inputChromeRows = 2
)

// syncInputSize grows the prompt box with its content and keeps the chat
// viewport sized to whatever is left over. Height follows the SOFT-WRAPPED
// row count: the textarea already word-wraps long lines at its width, so
// the box must grow as soon as a line is full — not only after explicit
// ctrl+j newlines.
func (m *Model) syncInputSize() {
	w := m.width
	if w < 24 {
		w = 80
	}
	inner := w - 4
	var rows []string
	for _, ln := range strings.Split(m.input.Value(), "\n") {
		rows = append(rows, render.WrapWords(ln, inner)...)
	}
	lines := len(rows)
	if lines < inputMinHeight {
		lines = inputMinHeight
	}
	if lines > inputMaxHeight {
		lines = inputMaxHeight
	}
	if lines != m.input.Height() {
		m.input.SetHeight(lines)
	}
	// The textarea viewport follows the cursor only downward: an insert
	// that pushes the cursor past a too-small box scrolls it and it never
	// scrolls back (repositionView has no upward correction for a grown
	// window) — the first rows stayed hidden. When the whole value fits the
	// box and the cursor is at the end (typing/pasting flow), reset the
	// viewport via SetValue; the cursor lands back at the end. Mid-text
	// cursors are left alone, an over-height box scrolls intentionally.
	if v := m.input.Value(); v != "" && len(rows) <= lines {
		hard := strings.Split(v, "\n")
		last := hard[len(hard)-1]
		li := m.input.LineInfo()
		atEnd := li.RowOffset == li.Height-1 &&
			li.CharOffset >= len([]rune(last))
		if atEnd {
			m.input.SetValue(v)
		}
	}
	m.relayout()
}

// relayout recomputes the chat viewport so the whole screen fits exactly:
// title, status line, the prompt box as it renders right now, a gap, and the
// chat takes whatever is left. Using the rendered height — not the configured
// one — keeps the bottom border of the box from being clipped.
func (m *Model) relayout() {
	inputRows := len(strings.Split(m.inputView(), "\n"))
	used := 2 + inputRows + 2 // title + status, box, gap, footer
	h := m.height - used
	if h < 1 {
		h = 1
	}
	m.chat = m.chat.SetSize(m.width, h)
}

// inputView renders the prompt area: a framed box that hugs the typed text,
// with the send hint embedded in the bottom border — the same chrome the
// tables and code blocks use.
// rewritePromptRow swaps the one background code the focused textarea style
// paints its cursor line with. bubbles v1 keeps that style unexported, so it
// cannot be themed directly — and plain ANSI black glares against the theme
// background. Everything else (grey placeholder, lavender frame) is left as
// the textarea produced it.
func rewritePromptRow(ln string, th *render.Theme) string {
	if th == nil {
		return ln
	}
	return strings.ReplaceAll(ln, "\x1b[40m", th.BackgroundSeq())
}

// nixIcon prefixes the workspace path — the Nerd Font NixOS glyph.
const nixIcon = "󱄅"

// topLabel is the workspace the session runs in — nothing else. The app name
// already lives in the title row.
func (m Model) topLabel() string {
	if m.cwd == "" {
		return ""
	}
	return nixIcon + " " + m.cwd
}

// stateLabel is the bottom-left footer: model and effort — the same in every
// state (FR-23, minimal: no token/price dump, no elapsed/abort chrome).
func (m Model) stateLabel() string {
	model := m.modelLabel
	if model == "" {
		model = "goose"
	}
	effort := m.effort
	if effort == "" {
		effort = "medium"
	}
	return model + " · " + effort
}

func (m Model) inputView() string {
	w := m.width
	if w < 24 {
		w = 80
	}
	th := m.theme
	// Monochrome lavender chrome: one hue for frame, label and hints. The
	// former accent colour and the inner background fill made the box read as
	// two colours and left a visible patch around the placeholder.
	bs := th.BorderStyle()
	// The prompt row itself gets the theme background, so the box interior is
	// filled and does not show the terminal's default background.
	fill := th.TableFill()
	inner := w - 4

	label := m.topLabel()
	lines := make([]string, 0, 8)
	if label == "" {
		lines = append(lines, bs.Render("┌"+strings.Repeat("─", w-2)+"┐"))
	} else {
		dashes := w - 5 - runewidth.StringWidth(label)
		if dashes < 0 {
			dashes = 0
		}
		lines = append(lines, bs.Render("┌─ "+label+" "+strings.Repeat("─", dashes)+"┐"))
	}

	// Model/effort picker (FR-20): the box expands into the option list.
	if th != nil && m.pickerOpen {
		ls := th.LabelStyle()
		active := m.modelLabel
		if m.pickerKind == "effort" {
			active = m.effort
		}
		row := func(s string) string {
			gap := w - 4 - lipgloss.Width(s)
			if gap < 0 {
				gap = 0
			}
			return bs.Render("│") + " " + s + strings.Repeat(" ", gap) + " " + bs.Render("│")
		}
		// /model carries a search line above the list; /effort is short
		// enough to browse without one (FR-20).
		if m.pickerKind == "model" {
			if m.pickerQuery == "" {
				lines = append(lines, row("> "+ls.Render("search")))
			} else {
				lines = append(lines, row("> "+runewidth.Truncate(m.pickerQuery, inner-3, "…")))
			}
		}
		options := m.pickerOptions()
		if len(options) == 0 {
			lines = append(lines, row("  " + ls.Render("no options")))
		}
		rows := m.pickerVisibleRows()
		end := min(m.pickerScroll+rows, len(options))
		for idx := m.pickerScroll; idx < end; idx++ {
			name := options[idx]
			prefix := "    "
			if idx == m.pickerCursor {
				prefix = "  ▸ "
			}
			// The active option keeps its ✓ even while the cursor is
			// somewhere else — scrolling must not hide what is live (FR-20).
			suffix := ""
			if name == active {
				suffix = " ✓"
			}
			lines = append(lines, row(prefix+name+suffix))
		}
		lines = append(lines, bs.Render("└"+strings.Repeat("─", w-2)+"╯"))
		return strings.Join(lines, "\n")
	}

	for i, raw := range strings.Split(m.input.View(), "\n") {
		ln := rewritePromptRow(raw, th)
		// Slash mode (FR-20) renders its own row: typed text + dim ghost —
		// shell-style autocomplete on the same line. The textarea row stays
		// out of the way, so no cursor cell or styled padding can clash.
		if i == 0 && th != nil && m.slashActive() {
			if rem := m.slashGhostRemainder(); rem != "" {
				val := m.input.Value()
				budget := inner - runewidth.StringWidth(val)
				if budget > 0 {
					ln = rewritePromptRow(val, th) + th.LabelStyle().Faint(true).Render(runewidth.Truncate(rem, budget, ""))
				}
			}
		}
		gap := inner - lipgloss.Width(ln)
		if gap < 0 {
			gap = 0
		}
		// One fill across content + padding keeps the background continuous
		// instead of a seam between the text and the empty cells.
		lines = append(lines, bs.Render("│")+" "+
			fill.Render(ln+strings.Repeat(" ", gap))+" "+bs.Render("│"))
	}

	// The box border stays clean — the model/effort label lives in the
	// footer bar below (crush-style).
	lines = append(lines, bs.Render("└"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(lines, "\n")
}
