package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// command is a slash command (FR-20).
type command struct {
	name string // without the leading slash
	help string
}

// commands lists the available slash commands; the suggestion list filters
// this by prefix while the user types.
var commands = []command{
	{"model", "choose model"},
	{"effort", "choose effort level"},
	{"new", "start a new session"},
	{"quit", "quit heron"},
}

// newSessionMsg reports the /new backend result — the session spawn runs as
// a tea.Cmd so the event loop never blocks (NFR-2).
type newSessionMsg struct{ err error }

// slashActive reports whether the prompt is currently typing a slash command
// and the suggestion list should show.
func (m Model) slashActive() bool {
	v := m.input.Value()
	return strings.HasPrefix(v, "/") && !strings.Contains(v, "\n") && !m.cmdDismissed
}

// slashMatches returns the commands matching the typed prefix.
func (m Model) slashMatches() []command {
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") {
		return nil
	}
	var out []command
	for _, c := range commands {
		if strings.HasPrefix("/"+c.name, v) {
			out = append(out, c)
		}
	}
	return out
}

// slashGhostRemainder returns the not-yet-typed rest of the currently
// suggested command (inline autocomplete, FR-20); empty when there is no
// match or the command is fully typed.
func (m Model) slashGhostRemainder() string {
	matches := m.slashMatches()
	if m.cmdCursor >= len(matches) {
		return ""
	}
	full := "/" + matches[m.cmdCursor].name
	v := m.input.Value()
	if full == v {
		return ""
	}
	return strings.TrimPrefix(full, v)
}

// slashGhostFull returns the complete command of the current suggestion.
func (m Model) slashGhostFull() string {
	matches := m.slashMatches()
	if m.cmdCursor >= len(matches) {
		return ""
	}
	return "/" + matches[m.cmdCursor].name
}

// nextCmd moves the suggestion cursor by step and clamps to the list.
func (m Model) nextCmd(step int) int {
	n := len(m.slashMatches())
	if n == 0 {
		return 0
	}
	c := m.cmdCursor + step
	if c < 0 {
		c = 0
	}
	if c > n-1 {
		c = n - 1
	}
	return c
}

// execCommand runs a slash command and clears the prompt.
func (m Model) execCommand(full string) (tea.Model, tea.Cmd) {
	m.input.SetValue("")
	m.cmdCursor = 0
	m.syncInputSize()
	switch full {
	case "/model":
		if len(m.modelOptions) == 0 {
			m.chat = m.chat.Append(NewTextBlock("goose reported no model options.", "1"))
			return m, nil
		}
		m.openPicker("model")
		return m, nil
	case "/effort":
		if len(m.effortOptions) == 0 {
			m.chat = m.chat.Append(NewTextBlock("goose reported no effort options.", "1"))
			return m, nil
		}
		m.openPicker("effort")
		return m, nil
	case "/new":
		// A fresh session while a turn is running would interleave two
		// streams into one chat — refuse until it settles.
		if m.running {
			m.chat = m.chat.Append(NewTextBlock("A turn is still running — abort it with ctrl+c first.", "1"))
			return m, nil
		}
		if m.newSession == nil {
			m.chat = m.chat.Append(NewTextBlock("No backend for a new session (demo mode).", "1"))
			return m, nil
		}
		fn := m.newSession
		return m, func() tea.Msg { return newSessionMsg{err: fn()} }
	case "/quit":
		return m, tea.Quit
	}
	m.chat = m.chat.Append(NewTextBlock("Unbekannter Befehl: "+full, "1"))
	return m, nil
}
