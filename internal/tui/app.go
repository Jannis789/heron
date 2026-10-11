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

	// Slash-command autocomplete (FR-20).
	cmdCursor    int
	cmdDismissed bool
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

// modelSegmentRange returns the screen columns of the model segment in the
// footer bar (last row).
func (m Model) modelSegmentRange() (int, int) {
	return 1, 3 + runewidth.StringWidth(m.stateLabel())
}

// footerView is the status bar under the prompt box, crush-style: the model
// segment on the left (click opens the selection dialog), contextual hints on
// the right — only what applies right now.
func (m Model) footerView() string {
	left := m.stateLabel()
	hints := ""
	if m.running {
		hints = "ctrl+c abbrechen"
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - runewidth.StringWidth(left) - 2 - runewidth.StringWidth(hints) - 3
	if gap < 1 {
		gap = 1
	}
	if m.theme == nil {
		return strings.TrimRight(" "+left+strings.Repeat(" ", gap+3+runewidth.StringWidth(hints)), " ")
	}
	fill := m.theme.TableFill()
	line := " " + fill.Render(" "+left+" ") + strings.Repeat(" ", gap)
	if hints != "" {
		line += m.theme.LabelStyle().Render(hints) + " "
	}
	return strings.TrimRight(line, " ")
}

// dialogGeometry returns position and size of the centered selection dialog.
func (m Model) dialogGeometry() (int, int, int, int) {
	w := popupWidth(m.popupEntries) + 4
	if w > m.width-2 {
		w = m.width - 2
	}
	h := len(m.popupEntries) + 5 // borders, padding rows, hint row
	x := max((m.width-w)/2, 0)
	y := max((m.height-h)/2, 0)
	return x, y, w, h
}

// popupRowAt maps a click to a dialog entry row; ok is false outside the
// entry area of the centered dialog.
func (m Model) popupRowAt(x, y int) (int, bool) {
	dx, dy, w, h := m.dialogGeometry()
	if y < dy+2 || y >= dy+h-2 { // skip border and padding rows
		return 0, false
	}
	row := y - dy - 2
	if row < 0 || row >= len(m.popupEntries) || x < dx+2 || x > dx+w-2 {
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

// renderDialog paints the open model/effort dialog centered over the frame —
// a solid panel with padding and a hint row, like crush's dialogs (FR-20-lite).
func (m Model) renderDialog(frame string) string {
	if !m.popupOpen || m.height <= 0 || m.width < 24 {
		return frame
	}
	dx, dy, w, h := m.dialogGeometry()
	inner := w - 2
	ls := m.theme.LabelStyle()

	rows := make([]string, 0, h)
	rows = append(rows, "┌─ modell / effort "+strings.Repeat("─", max(w-20, 0))+"┐")
	rows = append(rows, "│"+strings.Repeat(" ", inner)+"│")
	for i, e := range m.popupEntries {
		cursor := "  "
		if i == m.popupCursor {
			cursor = "▸ "
		}
		check := "   "
		if e.active {
			check = " ✓ "
		}
		txt := cursor + check + e.value
		if e.header {
			txt = "  " + strings.ToUpper(e.kind)
		}
		gap := inner - 2 - runewidth.StringWidth(txt)
		if gap < 0 {
			gap = 0
		}
		if i == m.popupCursor && !e.header {
			// The selected row inverts, like crush's dialog selection.
			rows = append(rows, "│  "+ls.Reverse(true).Render(txt+strings.Repeat(" ", gap))+"  │")
			continue
		}
		rows = append(rows, "│  "+ls.Render(txt)+strings.Repeat(" ", gap)+"  │")
	}
	rows = append(rows, "│"+strings.Repeat(" ", inner)+"│")
	hint := "↑↓ · enter · esc"
	hw := runewidth.StringWidth(hint)
	lp := max((inner-hw)/2, 0)
	rows = append(rows, "│"+strings.Repeat(" ", lp)+ls.Render(hint)+strings.Repeat(" ", max(inner-hw-lp, 0))+"│")
	rows = append(rows, "└"+strings.Repeat("─", inner)+"┘")

	lines := strings.Split(frame, "\n")
	for r, row := range rows {
		y := dy + r
		if y < 0 || y >= len(lines) {
			continue
		}
		pad := max(m.width-dx-runewidth.StringWidth(row), 0)
		lines[y] = strings.Repeat(" ", dx) + row + strings.Repeat(" ", pad)
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
	prevValue := m.input.Value()
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
			if lo, hi := m.modelSegmentRange(); msg.Y == m.height-1 && msg.X >= lo && msg.X < hi {
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
		// Slash-command navigation (FR-20): while the prompt starts with
		// "/", up/down move the selection, tab completes, esc dismisses.
		if m.slashActive() && len(m.slashMatches()) > 0 {
			switch msg.String() {
			case "down":
				m.cmdCursor = m.nextCmd(1)
				return m, nil
			case "up":
				m.cmdCursor = m.nextCmd(-1)
				return m, nil
			case "tab", "right":
				// Accept the ghost into the input (right does what tab does
				// in shells).
				if full := m.slashGhostFull(); full != "" {
					m.input.SetValue(full + " ")
					m.cmdCursor = 0
					m.syncInputSize()
				}
				return m, nil
			case "esc":
				m.cmdDismissed = true
				return m, nil
			case "enter":
				if matches := m.slashMatches(); m.cmdCursor < len(matches) {
					return m.execCommand("/" + matches[m.cmdCursor].name)
				}
			}
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
			if strings.HasPrefix(prompt, "/") {
				// Slash commands (FR-20): /model opens the selection dialog,
				// unknown commands report an error instead of sending.
				name := strings.TrimPrefix(prompt, "/")
				for _, c := range commands {
					if c.name == name {
						return m.execCommand("/" + name)
					}
				}
				m.chat = m.chat.Append(NewTextBlock("Unbekannter Befehl: "+prompt, "1"))
				m.input.SetValue("")
				m.syncInputSize()
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
		m.chat = m.chat.upsertThinkBlock(m.thinking, m.theme)
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
	if m.input.Value() != prevValue {
		// Typing resets the slash-command state (FR-20).
		m.cmdDismissed = false
		m.cmdCursor = 0
	}
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

// statusLine is the fixed line under the title: tool activity (FR-2). Keeping
// it to exactly one row keeps chatTop at 2, so mouse hit-testing against the
// chat area stays correct. Thinking lives in its own collapsible block (FR-30).
func (m Model) statusLine() string {
	if m.toolStatus == "" {
		return ""
	}
	line := "🔧 " + m.toolStatus
	if m.width > 0 {
		if w := render.WrapDisplay(line, m.width); len(w) > 0 {
			line = w[0]
		}
	}
	return lipgloss.NewStyle().Faint(true).Render(line)
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
		return m.renderDialog(m.fillScreen(top.String() + "\n\n" + prompt + "\n" + m.footerView()))
	}
	promptRows := len(strings.Split(prompt, "\n"))
	avail := m.height - promptRows - 1 // the footer row
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
	return m.renderDialog(m.fillScreen(strings.Join(append(append(topLines, prompt), m.footerView()), "\n")))
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
