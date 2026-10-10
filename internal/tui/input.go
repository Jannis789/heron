package tui

import (
	"fmt"
	"strings"
	"time"

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

// syncInputSize grows the prompt box with its content (one row per line,
// clamped) and keeps the chat viewport sized to whatever is left over.
func (m *Model) syncInputSize() {
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines < inputMinHeight {
		lines = inputMinHeight
	}
	if lines > inputMaxHeight {
		lines = inputMaxHeight
	}
	if lines != m.input.Height() {
		m.input.SetHeight(lines)
	}
	m.relayout()
}

// relayout recomputes the chat viewport so the whole screen fits exactly:
// title, status line, the prompt box as it renders right now, a gap, and the
// chat takes whatever is left. Using the rendered height — not the configured
// one — keeps the bottom border of the box from being clipped.
func (m *Model) relayout() {
	inputRows := len(strings.Split(m.inputView(), "\n"))
	used := 2 + inputRows + 1 // title + status, box, gap
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

// topLabel is the workspace the session runs in — nothing else. The app name
// already lives in the title row.
func (m Model) topLabel() string {
	return m.cwd
}

// stateLabel is the bottom-left footer: model and effort, plus what the
// session is doing right now (FR-23, minimal — no token/price dump).
func (m Model) stateLabel() string {
	model := m.modelLabel
	if model == "" {
		model = "goose"
	}
	effort := m.effort
	if effort == "" {
		effort = "medium"
	}
	label := model + " * " + effort
	if m.running {
		label += " · antwortet …"
		if !m.startedAt.IsZero() {
			label += fmt.Sprintf(" %ds", int(time.Since(m.startedAt).Seconds()))
		}
	}
	return label
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
	lt := th.LabelStyle()
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

	for _, raw := range strings.Split(m.input.View(), "\n") {
		ln := rewritePromptRow(raw, th)
		gap := inner - lipgloss.Width(ln)
		if gap < 0 {
			gap = 0
		}
		// One fill across content + padding keeps the background continuous
		// instead of a seam between the text and the empty cells.
		lines = append(lines, bs.Render("│")+" "+
			fill.Render(ln+strings.Repeat(" ", gap))+" "+bs.Render("│"))
	}

	// The abort hint appears only while it is actually possible.
	action := ""
	if m.running {
		action = "ctrl+c bricht ab"
	}
	if action == "" {
		// Without an action there is no point in an empty action box.
		labelBox := "[ " + m.stateLabel() + " ]"
		n := w - 2 - runewidth.StringWidth(labelBox) - 1
		if n < 0 {
			n = 0
		}
		lines = append(lines, bs.Render("└─"+labelBox+strings.Repeat("─", n)+"╯"))
		return strings.Join(lines, "\n")
	}
	lines = append(lines, render.FooterLine(w, m.stateLabel(), action, lt.Render(action),
		runewidth.StringWidth(action), bs, lt, nil))
	return strings.Join(lines, "\n")
}
