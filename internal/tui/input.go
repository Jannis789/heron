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
func (m Model) inputView() string {
	w := m.width
	if w < 24 {
		w = 80
	}
	th := m.theme
	bs := th.BorderStyle()
	fill := th.TableFill()
	inner := w - 4

	label := "nachricht"
	if m.running {
		label = "antwortet …"
	}
	labelW := runewidth.StringWidth(label)
	dashes := w - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	lines := make([]string, 0, 8)
	lines = append(lines, bs.Render("┌─ ")+th.ActionStyle().Render(label)+
		bs.Render(" "+strings.Repeat("─", dashes)+"┐"))

	for _, ln := range strings.Split(m.input.View(), "\n") {
		gap := inner - lipgloss.Width(ln)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+" "+
			fill.Render(ln)+fill.Render(strings.Repeat(" ", gap))+" "+
			bs.Render("│"))
	}

	hint := "enter sendet"
	action := "ctrl+j zeile"
	styled := th.ActionStyle().Render(action)
	lines = append(lines, render.FooterLine(w, hint, action, styled,
		runewidth.StringWidth(action), lipgloss.NewStyle(), th.LabelStyle(), nil))
	return strings.Join(lines, "\n")
}
