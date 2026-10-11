// Package tui is the Bubble Tea frontend for heron.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// Sender sends a prompt and returns the (raw) response text.
type Sender func(prompt string) (string, error)

type replyMsg struct {
	text string
	err  error
}

// Model is the heron TUI state: a chat block list plus a prompt input.
// Everything flows through Update — state is never mutated in place.
type Model struct {
	input  textarea.Model
	chat   BlockList
	send   Sender
	theme  *render.Theme
	copyFn CopyFunc
	width  int
	height int

	// Live turn state (FR-2, FR-7).
	stream     *Stream // may be nil: no live updates
	cancel     func()  // aborts the running turn, may be nil
	running    bool    // a turn is in flight
	cancelling bool    // cancel already requested
	streaming  string  // raw answer received so far
	thinking   string  // thinking received so far
	toolStatus string  // last tool label while the turn runs
	showThink  bool    // thinking visible in the status line (FR-2 toggle)

	// Prompt box chrome (FR-23, minimal).
	cwd        string    // workspace shown in the top border
	modelLabel string    // model shown in the bottom border
	effort     string    // effort level shown next to the model

	// Model/effort popup (FR-20-lite).
	modelOptions  []string
	effortOptions []string
	configSet     func(id, value string) error
	popupOpen     bool
	popupCursor   int
	popupEntries  []configEntry
}

func New(send Sender, th *render.Theme, copyFn CopyFunc) Model {
	ta := textarea.New()
	// Hints live in the placeholder: they are only useful before the first
	// keystroke, and the borders carry the live session info instead.
	ta.Placeholder = "Nachricht an goose …  (enter sendet · ctrl+j = neue Zeile)"
	// The frame already marks the prompt area, so the textarea itself stays
	// free of a line prefix — one prompt character per row reads as noise
	// once the box grows.
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.SetWidth(80)
	ta.SetHeight(inputMinHeight)
	// Enter is captured by the model to send the prompt, so the newline needs
	// a distinct key. Terminals deliver Ctrl+J (LF) for Shift+Enter — the only
	// newline signal bubbletea v1 can tell apart from Enter (keyCR).
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))
	ta.Focus()
	return Model{input: ta, chat: NewBlockList().SetCopy(copyFn), send: send, theme: th, copyFn: copyFn}
}

// WithStream wires the live-update bridge used by the backend to push chunk,
// thought and tool events into the runtime (FR-2). A nil stream is allowed.
func (m Model) WithStream(s *Stream) Model { m.stream = s; return m }

// WithCancel wires the turn abort invoked by Ctrl+C while a turn runs (FR-7).
func (m Model) WithCancel(fn func()) Model { m.cancel = fn; return m }

// WithWorkspace sets the directory shown in the prompt box top border.
func (m Model) WithWorkspace(dir string) Model { m.cwd = dir; return m }

// WithModel sets the model label shown in the bottom border.
func (m Model) WithModel(name string) Model { m.modelLabel = name; return m }

// WithEffort sets the effort level shown next to the model.
func (m Model) WithEffort(e string) Model { m.effort = e; return m }

// WithConfigSet wires the backend call that changes a session config option
// (modell / effort) for the popup (FR-20-lite).
func (m Model) WithConfigSet(fn func(id, value string) error) Model { m.configSet = fn; return m }

// configEntry is one row of the model/effort popup (FR-20-lite).
type configEntry struct {
	kind   string // "modell" or "effort"
	value  string
	active bool
	header bool // section caption, not selectable
}

// configResultMsg reports a finished config option change.
type configResultMsg struct {
	kind  string
	value string
	err   error
}

// openPopup builds the model/effort selection rows from the announced config
// options (FR-20-lite). No-op without announced options.
func (m *Model) openPopup() {
	var entries []configEntry
	add := func(kind string, options []string, active string) {
		if len(options) == 0 {
			return
		}
		entries = append(entries, configEntry{kind: kind, header: true})
		for _, o := range options {
			entries = append(entries, configEntry{kind: kind, value: o, active: o == active})
		}
	}
	add("modell", m.modelOptions, m.modelLabel)
	add("effort", m.effortOptions, m.effort)
	if len(entries) == 0 {
		return
	}
	cursor := 0
	for i, e := range entries {
		if !e.header {
			cursor = i
			break
		}
	}
	m.popupOpen = true
	m.popupCursor = cursor
	m.popupEntries = entries
}

// popupKey handles keys while the popup is open; the bool reports whether
// the key was consumed.
func (m Model) popupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "up":
		m.popupCursor = m.prevSelectable()
		return m, nil, true
	case "down":
		m.popupCursor = m.nextSelectable()
		return m, nil, true
	case "esc":
		m.popupOpen = false
		return m, nil, true
	case "enter":
		e := m.popupEntries[m.popupCursor]
		if e.header {
			return m, nil, true
		}
		return m.applyConfigCmd(e)
	}
	return m, nil, false
}

// nextSelectable/prevSelectable move the cursor across section captions.
func (m Model) nextSelectable() int {
	for i := m.popupCursor + 1; i < len(m.popupEntries); i++ {
		if !m.popupEntries[i].header {
			return i
		}
	}
	return m.popupCursor
}

func (m Model) prevSelectable() int {
	for i := m.popupCursor - 1; i >= 0; i-- {
		if !m.popupEntries[i].header {
			return i
		}
	}
	return m.popupCursor
}

// applyConfigCmd applies the selection: without a backend (demo) locally,
// otherwise as a command so the event loop never blocks on I/O (NFR-2).
func (m Model) applyConfigCmd(e configEntry) (tea.Model, tea.Cmd, bool) {
	if m.configSet == nil {
		nm := m
		nm.applyConfig(e.kind, e.value)
		return nm, nil, true
	}
	kind, value, fn := e.kind, e.value, m.configSet
	return m, func() tea.Msg {
		return configResultMsg{kind: kind, value: value, err: fn(kind, value)}
	}, true
}

// applyConfig updates the labels locally (demo path or confirmed result) and
// keeps the open popup's active marks in sync.
func (m *Model) applyConfig(kind, value string) {
	switch kind {
	case "modell":
		m.modelLabel = value
	case "effort":
		m.effort = value
	}
	for i, e := range m.popupEntries {
		if e.kind == kind {
			m.popupEntries[i].active = e.value == value
		}
	}
}

// promptChipRange returns the screen columns of the [ modell · effort ] chip
// in the prompt box bottom border.
func (m Model) promptChipRange() (int, int) {
	labelBox := "[ " + m.stateLabel() + " ]"
	return 2, 2 + runewidth.StringWidth(labelBox)
}

// popupRowAt maps a click to a popup entry row; ok is false outside the box.
func (m Model) popupRowAt(x, y int) (int, bool) {
	if y < chatTop || x < 2 {
		return 0, false
	}
	row := y - chatTop - 1 // -1: box top border
	if row < 0 || row >= len(m.popupEntries) {
		return 0, false
	}
	if x > popupWidth(m.popupEntries)+1 {
		return 0, false
	}
	return row, true
}

// popupWidth is the box width for the current entries (capped).
func popupWidth(entries []configEntry) int {
	w := len("modell / effort")
	for _, e := range entries {
		n := len(e.kind)
		if !e.header {
			n = len(e.value)
		}
		if n+4 > w {
			w = n + 4
		}
	}
	if w < 20 {
		w = 20
	}
	if w > 42 {
		w = 42
	}
	return w
}

// overlayPopup paints the open model/effort popup over the chat rows
// (FR-20-lite).
func (m Model) overlayPopup(frame string) string {
	if !m.popupOpen || m.height <= 0 {
		return frame
	}
	w := popupWidth(m.popupEntries)
	inner := w - 2
	rows := make([]string, 0, len(m.popupEntries)+2)
	rows = append(rows, "┌─ modell / effort "+strings.Repeat("─", max(w-20, 0))+"┐")
	for i, e := range m.popupEntries {
		var txt string
		switch {
		case e.header:
			txt = e.kind
		default:
			mark := "  "
			if e.active {
				mark = "✓ "
			}
			cursor := " "
			if i == m.popupCursor {
				cursor = ">"
			}
			txt = cursor + " " + mark + e.value
		}
		gap := inner - runewidth.StringWidth(txt)
		if gap < 0 {
			gap = 0
		}
		rows = append(rows, "│"+txt+strings.Repeat(" ", gap)+"│")
	}
	rows = append(rows, "└"+strings.Repeat("─", inner)+"┘")

	lines := strings.Split(frame, "\n")
	for r, row := range rows {
		y := chatTop + r
		if y >= len(lines) || y >= m.height-1 {
			break
		}
		pad := max(m.width-runewidth.StringWidth(row), 0)
		lines[y] = row + strings.Repeat(" ", pad)
	}
	return strings.Join(lines, "\n")
}

// startTurn resets the live state for a fresh prompt.
func (m *Model) startTurn() {
	m.running = true
	m.cancelling = false
	m.streaming = ""
	m.thinking = ""
	m.toolStatus = ""
	// A fresh turn starts without the previous thinking block (FR-30).
	m.chat = m.chat.removeBlock(ThinkBlockID)
}

// endTurn clears the live state once the turn settles.
func (m *Model) endTurn() {
	m.running = false
	m.cancelling = false
	m.streaming = ""
	m.thinking = ""
	m.toolStatus = ""
}

// chatTop is the screen row where the chat area starts (title + status line).
const chatTop = 2

// SeedMarkdown appends the blocks of a markdown document to the chat — demo
// mode to exercise hover/copy actions without a goose session.
func (m Model) SeedMarkdown(src string) Model {
	nodes, err := render.Parse(src)
	if err != nil {
		return m
	}
	m.chat = m.chat.Append(BuildWidgets(nodes, m.theme, m.copyFn)...)
	return m
}

func (m Model) Init() tea.Cmd {
	// Seeded chats (-seed / demo) can already contain plantuml blocks.
	if cmd := m.pendingPlantUML(); cmd != nil {
		return tea.Batch(textarea.Blink, cmd)
	}
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(msg.Width - 4)
		m.syncInputSize()
		return m, nil
	case tea.MouseMsg:
		// The [ modell · effort ] footer chip toggles the popup; clicks
		// inside the open popup select, outside close it (FR-20-lite).
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if lo, hi := m.promptChipRange(); msg.Y == m.height-1 && msg.X >= lo && msg.X < hi {
				m.popupOpen = !m.popupOpen
				if m.popupOpen {
					m.openPopup()
				}
				return m, nil
			}
			if m.popupOpen {
				if idx, ok := m.popupRowAt(msg.X, msg.Y); ok && !m.popupEntries[idx].header {
					nm, cmd, _ := m.applyConfigCmd(m.popupEntries[idx])
					return nm, cmd
				}
				m.popupOpen = false
				return m, nil
			}
		}
		// Translate the screen row into chat-area coordinates, then let the
		// pager handle hover/click on COPY actions.
		mm := msg
		mm.Y -= chatTop
		var cmd tea.Cmd
		var handled bool
		m.chat, cmd, handled = m.chat.Update(mm)
		if handled {
			return m, cmd
		}
	case tea.KeyMsg:
		// The open popup swallows keys first (FR-20-lite).
		if m.popupOpen {
			nm, cmd, used := m.popupKey(msg)
			if used {
				return nm, cmd
			}
			return m, nil
		}
		// Numbered copy fallback (FR-5): alt+<n> / ctrl+<n> copy block n.
		if n := blockNumFromKey(msg.String()); n > 0 {
			chat, cmd, ok := m.chat.copyByID(n)
			if !ok {
				return m, nil
			}
			m.chat = chat
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c":
			// FR-7: during a turn Ctrl+C aborts the turn (session/cancel)
			// instead of killing the app; a second Ctrl+C force-quits.
			if m.running && m.cancel != nil && !m.cancelling {
				m.cancelling = true
				cancel := m.cancel
				return m, func() tea.Msg { cancel(); return nil }
			}
			return m, tea.Quit
		case "ctrl+t":
			// FR-2 toggle stays; FR-30 renders the thinking as a collapsible
			// block instead of dropping it after the turn.
			m.showThink = !m.showThink
			if m.showThink && m.thinking != "" {
				m.chat = m.chat.upsertThinkBlock(m.thinking, m.theme)
			}
			if !m.showThink {
				m.chat = m.chat.removeBlock(ThinkBlockID)
			}
			return m, nil
		case "ctrl+o":
			// Keyboard twin of the footer chip: open the popup (FR-20-lite).
			m.openPopup()
			return m, nil
		case "enter":
			if m.chat.Focused() {
				var cmd tea.Cmd
				m.chat, cmd, _ = m.chat.Update(msg)
				return m, cmd
			}
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			m.chat = m.chat.Append(NewUserBlock(prompt))
			m.input.SetValue("")
			if m.send == nil {
				return m, nil
			}
			m.startTurn()
			send := m.send
			sendCmd := func() tea.Msg {
				text, err := send(prompt)
				return replyMsg{text: text, err: err}
			}
			return m, sendCmd
		case "up", "down", "pgup", "pgdown", "tab":
			var cmd tea.Cmd
			var handled bool
			m.chat, cmd, handled = m.chat.Update(msg)
			if handled {
				return m, cmd
			}
		case " ", "y":
			// Selection keys (FR-29): space marks the focused block, y yanks
			// the marked range as plain markdown. Unhandled they fall
			// through to the input as normal typing.
			var cmd tea.Cmd
			var handled bool
			m.chat, cmd, handled = m.chat.Update(msg)
			if handled {
				return m, cmd
			}
		case "c", "esc":
			if m.chat.Focused() {
				var cmd tea.Cmd
				var handled bool
				m.chat, cmd, handled = m.chat.Update(msg)
				if handled {
					return m, cmd
				}
			}
		}
	case chunkMsg:
		// The answer grows in place under the chat while it streams (FR-2).
		m.streaming += msg.text
		m.chat = m.chat.upsertStreamBlock(m.streaming, textColorFor(m.theme))
		return m, nil
	case thoughtMsg:
		m.thinking += msg.text
		if m.showThink {
			m.chat = m.chat.upsertThinkBlock(m.thinking, m.theme)
		}
		return m, nil
	case toolMsg:
		if msg.done {
			m.toolStatus = ""
		} else {
			m.toolStatus = msg.name
		}
		return m, nil
	case infoMsg:
		// The backend announced the real model/effort (ACP configOptions);
		// empty parts keep the current labels (flag overrides).
		if msg.model != "" {
			m.modelLabel = msg.model
		}
		if msg.effort != "" {
			m.effort = msg.effort
		}
		m.modelOptions = msg.modelOptions
		m.effortOptions = msg.effortOptions
		return m, nil
	case configResultMsg:
		// The backend confirmed (or failed) a model/effort change.
		if msg.err != nil {
			m.chat = m.chat.Append(NewTextBlock("Fehler: "+msg.err.Error(), "1"))
			return m, nil
		}
		m.applyConfig(msg.kind, msg.value)
		return m, nil
	case plantumlDoneMsg:
		m.chat, _ = m.chat.applyPlantUML(msg.results)
		return m, nil
	case replyMsg:
		// The final text supersedes the live preview (FR-2).
		m.endTurn()
		m.chat = m.chat.removeBlock(StreamBlockID)
		switch {
		case msg.err != nil:
			m.chat = m.chat.Append(NewTextBlock("Fehler: "+msg.err.Error(), "1"))
		case msg.text == "":
		case true:
			nodes, err := render.Parse(msg.text)
			if err != nil {
				m.chat = m.chat.Append(NewTextBlock("Parse-Fehler: "+err.Error(), "1"))
			} else {
				m.chat = m.chat.Append(BuildWidgets(nodes, m.theme, m.copyFn)...)
			}
		}
		// PlantUML blocks in the fresh reply convert in the background (FR-6).
		return m, m.pendingPlantUML()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// The prompt box grows with the text, so the chat gives the rows back.
	m.syncInputSize()
	return m, cmd
}

// blockNumFromKey maps a numbered copy shortcut to its block number:
// "alt+3" -> 3, "ctrl+3" -> 3, anything else -> 0. Most terminals deliver
// alt+<digit> reliably; ctrl+<digit> is honoured when the terminal emits it
// (FR-5 keyboard fallback).
func blockNumFromKey(s string) int {
	for _, prefix := range []string{"alt+", "ctrl+"} {
		if len(s) != len(prefix)+1 || !strings.HasPrefix(s, prefix) {
			continue
		}
		d := s[len(prefix)]
		if d >= '1' && d <= '9' {
			return int(d - '0')
		}
	}
	return 0
}

// statusLine is the fixed line under the title: tool activity and the current
// thinking line (FR-2). Keeping it to exactly one row keeps chatTop at 2, so
// mouse hit-testing against the chat area stays correct.
func (m Model) statusLine() string {
	if m.toolStatus == "" && !(m.showThink && m.thinking != "") {
		return ""
	}
	var parts []string
	if m.toolStatus != "" {
		parts = append(parts, "🔧 "+m.toolStatus)
	}
	if m.showThink && m.thinking != "" {
		parts = append(parts, "💭 "+lastLine(m.thinking))
	}
	line := strings.Join(parts, "  ")
	if m.width > 0 {
		if w := render.WrapDisplay(line, m.width); len(w) > 0 {
			line = w[0]
		}
	}
	return lipgloss.NewStyle().Faint(true).Render(line)
}

// lastLine returns the final line of a possibly multi-line stream.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func (m Model) View() string {
	var top strings.Builder
	top.WriteString(lipgloss.NewStyle().Bold(true).Render("heron · goose acp"))
	top.WriteString("\n")
	top.WriteString(m.statusLine())
	top.WriteString("\n")
	if m.chat.Empty() {
		top.WriteString("(noch keine Nachricht — tippe unten)\n")
	}
	top.WriteString(m.chat.View())

	// The prompt box belongs to the bottom of the screen. Padding the gap
	// after it — what a plain full-screen fill does — left the box floating
	// near the top whenever the chat was short, i.e. always on an empty chat.
	// So the spare rows go between chat and prompt, never below it.
	prompt := m.inputView()
	if m.height <= 0 {
		// No terminal size known yet (tests, first paint): render as-is
		// instead of squeezing the frame into an unknown height, which cut
		// the header and the newest chat rows.
		return m.overlayPopup(m.fillScreen(top.String() + "\n\n" + prompt))
	}
	avail := m.height - len(strings.Split(prompt, "\n"))
	if avail < 1 {
		avail = 1
	}
	topLines := strings.Split(top.String(), "\n")
	if len(topLines) > avail {
		topLines = topLines[len(topLines)-avail:]
	}
	for len(topLines) < avail {
		topLines = append(topLines, "")
	}
	return m.overlayPopup(m.fillScreen(strings.Join(append(topLines, prompt), "\n")))
}

// fillScreen pads every line to the terminal width and paints the theme
// background across the whole screen, re-establishing it after inner resets.
func (m Model) fillScreen(s string) string {
	seq := m.theme.BackgroundSeq()
	if m.width <= 0 || seq == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if pad := m.width - lipgloss.Width(ln); pad > 0 {
			ln += strings.Repeat(" ", pad)
		}
		lines[i] = seq + strings.ReplaceAll(ln, "\x1b[0m", "\x1b[0m"+seq)
	}
	for len(lines) < m.height {
		lines = append(lines, seq+strings.Repeat(" ", m.width))
	}
	return strings.Join(lines, "\n")
}

// RunModel starts the TUI with a prepared model (all-motion mouse: hover).
func RunModel(m Model) error {
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	// Hand the runtime to the stream so the backend can publish live updates
	// from its own goroutine (FR-2). A nil stream is a no-op.
	m.stream.attach(p)
	_, err := p.Run()
	return err
}

// Run starts the TUI.
func Run(send Sender, th *render.Theme, copyFn CopyFunc) error {
	return RunModel(New(send, th, copyFn))
}
