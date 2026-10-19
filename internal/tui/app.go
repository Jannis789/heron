// Package tui is the Bubble Tea frontend for heron.
package tui

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"runtime/pprof"
	"sync/atomic"

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
	input       textarea.Model
	chat        BlockList
	send        Sender
	theme       *render.Theme
	copyFn      CopyFunc
	width       int
	height      int
	inline      bool // timg-Modell: Primärscreen statt Alt-Screen (HERON_INLINE)
	printedUpTo int  // Inline: Index des ersten noch nicht append-only gedruckten Blocks

	seedDiagrams bool // Seed enthaelt plantuml/mermaid: einmalige Konvertierung

	// Live turn state (FR-2, FR-7).
	stream     *Stream // may be nil: no live updates
	cancel     func()  // aborts the running turn, may be nil
	running    bool    // a turn is in flight
	cancelling bool    // cancel already requested
	streaming  string  // raw answer received so far
	thinking   string  // thinking received so far
	toolStatus string  // last tool label while the turn runs

	// Prompt box chrome (FR-23, minimal).
	cwd        string // workspace shown in the top border
	modelLabel string // model shown in the bottom border
	effort     string // effort level shown next to the model

	// Model/effort picker (FR-20): the input box expands into a searchable
	// list. pickerKind selects what the list edits.
	modelOptions  []string
	effortOptions []string
	configSet     func(id, value string) error
	newSession    func() error // /new: fresh goose session (nil = demo)
	caps          TermCaps     // Term-Probe (FR-27): Platzhalter + Zellpixel
	pickerKind    string       // "model" or "effort" while open
	pickerOpen    bool
	pickerCursor  int
	pickerScroll  int
	pickerQuery   string // /model filters the list as you type (FR-20)

	// Token usage (FR-23): goose reports per-turn usage in the prompt
	// response. Totals accumulate the session; lastIn/lastOut feed the
	// context bar (last input ≈ context used).
	totalIn, totalOut int
	lastIn, lastOut   int
	ctxWin            int // context window tokens; 0 = default 200k

	// Slash-command autocomplete (FR-20).
	cmdCursor    int
	cmdDismissed bool
}

// inlineEnabled schaltet das timg-Modell (HERON_INLINE=1): kein Alt-Screen,
// kein eigener Pager-Viewport — das Terminal-Scrollback uebernimmt das
// Scrollen. Grund: im Alt-Screen ist der Viewport fix (52 Zeilen), und fuenf
// Diagramme a 30 Zeilen verdraengen jeden Text aus dem sichtbaren Bereich
// (gemessen: 50 Bildzeilen gegen 7 Textzeilen bei 337x59). timg kennt kein
// Alt-Screen (grep ueber den kompletten Quellcode: null Treffer) und hat
// dieses Problem deshalb nicht — Text und Bilder stehen im selben Fluss.
func inlineEnabled() bool {
	v := strings.ToLower(os.Getenv("HERON_INLINE"))
	return v == "1" || v == "true"
}

// inlineActive waehrend eines Inline-Programms gesetzt: KittyCleanup loescht
// dann keine Bilder — die sollen im Scrollback bleiben (timg tut es auch
// nicht), ein a=d,d=a wuerde den Scrollback leeren.
var inlineActive atomic.Bool

func New(send Sender, th *render.Theme, copyFn CopyFunc) Model {
	ta := textarea.New()
	// Hints live in the placeholder: they are only useful before the first
	// keystroke, and the borders carry the live session info instead.
	ta.Placeholder = "Message goose …  (enter sends · ctrl+j = new line)"
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
	return Model{input: ta, chat: NewBlockList().SetCopy(copyFn), send: send, theme: th, copyFn: copyFn, inline: inlineEnabled()}
}

// WithStream wires the live-update bridge used by the backend to push chunk,
// thought and tool events into the runtime (FR-2). A nil stream is allowed.
func (m Model) WithStream(s *Stream) Model { m.stream = s; return m }

// WithCancel wires the turn abort invoked by Ctrl+C while a turn runs (FR-7).
func (m Model) WithCancel(fn func()) Model { m.cancel = fn; return m }

// WithTermCaps uebernimmt das Probe-Ergebnis des Terminals (FR-27).
func (m Model) WithTermCaps(c TermCaps) Model { m.caps = c; return m }

// WithWorkspace sets the directory shown in the prompt box top border.
func (m Model) WithWorkspace(dir string) Model { m.cwd = dir; return m }

// WithModel sets the model label shown in the bottom border.
func (m Model) WithModel(name string) Model { m.modelLabel = name; return m }

// WithEffort sets the effort level shown next to the model.
func (m Model) WithEffort(e string) Model { m.effort = e; return m }

// WithConfigSet wires the backend call that changes the session model for
// the picker (FR-20).
func (m Model) WithConfigSet(fn func(id, value string) error) Model { m.configSet = fn; return m }

// configResultMsg reports a finished model change.
type configResultMsg struct {
	kind  string
	value string
	err   error
}

// pickerOptions returns the announced options for the open picker kind —
// the rows of the list.
// pickerOptions returns the options of the open picker: effort levels or
// models — models filtered by the search query while one is typed (FR-20).
func (m Model) pickerOptions() []string {
	if m.pickerKind == "effort" {
		return m.effortOptions
	}
	opts := m.modelOptions
	if m.pickerQuery == "" {
		return opts
	}
	q := strings.ToLower(m.pickerQuery)
	var out []string
	for _, o := range opts {
		if strings.Contains(strings.ToLower(o), q) {
			out = append(out, o)
		}
	}
	return out
}

// openPicker expands the prompt box into the option list for the given kind
// ("model" or "effort"). No-op without announced options.
func (m *Model) openPicker(kind string) {
	var options []string
	if kind == "effort" {
		options = m.effortOptions
	} else {
		options = m.modelOptions
	}
	if len(options) == 0 {
		return
	}
	m.pickerKind = kind
	m.pickerOpen = true
	m.pickerCursor = 0
	m.pickerScroll = 0
	m.pickerQuery = ""
	// Start with the cursor on the active option: switching back (e.g.
	// glm-5.3) is one enter, and the selection is visible immediately
	// instead of below the fold (FR-20).
	active := m.modelLabel
	if kind == "effort" {
		active = m.effort
	}
	for i, o := range m.pickerOptions() {
		if o == active {
			m.pickerCursor = i
			break
		}
	}
	m.clampPickerScroll()
}

// closePicker collapses the picker.
func (m *Model) closePicker() {
	m.pickerOpen = false
	m.pickerCursor = 0
	m.pickerScroll = 0
	m.pickerQuery = ""
}

// WithNewSession wires the /new callback: starts a fresh goose session.
func (m Model) WithNewSession(fn func() error) Model {
	m.newSession = fn
	return m
}

// WithContextWindow sets the context window behind the footer bar
// (flag -context-window; default 200000).
func (m Model) WithContextWindow(n int) Model {
	m.ctxWin = n
	return m
}

// pickerRows caps the option rows visible in the expanded input box — wide
// enough for goose's full model list (11 announced), clamped by the terminal
// height at render time.
const pickerRows = 12

// pickerVisibleRows is the list window: the whole list when it fits, never
// more than the terminal affords, at least one row (FR-20).
func (m Model) pickerVisibleRows() int {
	rows := pickerRows
	if n := len(m.pickerOptions()); n < rows {
		rows = n
	}
	if room := m.height - 9; room < rows {
		rows = room
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// clampPickerScroll keeps the cursor inside the visible list window.
func (m *Model) clampPickerScroll() {
	n := len(m.pickerOptions())
	if m.pickerCursor >= n {
		m.pickerCursor = max(n-1, 0)
	}
	rows := m.pickerVisibleRows()
	if m.pickerCursor < m.pickerScroll {
		m.pickerScroll = m.pickerCursor
	}
	if m.pickerCursor >= m.pickerScroll+rows {
		m.pickerScroll = m.pickerCursor - rows + 1
	}
	if m.pickerScroll < 0 {
		m.pickerScroll = 0
	}
}

// pickerKey handles keys while the picker is open: ↑↓ navigate, enter
// applies the highlighted option live, esc closes.
func (m Model) pickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
	case "down":
		if m.pickerCursor < len(m.pickerOptions())-1 {
			m.pickerCursor++
		}
	case "enter":
		options := m.pickerOptions()
		if m.pickerCursor >= len(options) {
			return m, nil
		}
		value := options[m.pickerCursor]
		if m.configSet == nil {
			m.applyPicker(value)
			m.closePicker()
			return m, nil
		}
		id := "model"
		if m.pickerKind == "effort" {
			id = "thinking_effort"
		}
		kind, fn := m.pickerKind, m.configSet
		return m, func() tea.Msg {
			return configResultMsg{kind: kind, value: value, err: fn(id, value)}
		}
	case "esc":
		m.closePicker()
		return m, nil
	}
	// The model list filters as you type; the effort list is short enough
	// to browse without a query (FR-20). A changed query restarts at the top.
	if m.pickerKind == "model" {
		changed := false
		switch msg.String() {
		case "backspace":
			if r := []rune(m.pickerQuery); len(r) > 0 {
				m.pickerQuery = string(r[:len(r)-1])
				changed = true
			}
		default:
			if msg.Type == tea.KeySpace {
				m.pickerQuery += " "
				changed = true
			} else if len(msg.Runes) > 0 {
				m.pickerQuery += string(msg.Runes)
				changed = true
			}
		}
		if changed {
			m.pickerCursor, m.pickerScroll = 0, 0
		}
	}
	m.clampPickerScroll()
	return m, nil
}

// applyPicker updates the label of the open picker kind (demo path or
// confirmed result).
func (m *Model) applyPicker(value string) {
	if m.pickerKind == "effort" {
		m.effort = value
		return
	}
	m.modelLabel = value
}

// modelSegmentRange returns the screen columns of the model segment in the
// footer bar (last row).
func (m Model) modelSegmentRange() (int, int) {
	return 1, 3 + runewidth.StringWidth(m.stateLabel())
}

// footerView is the status bar under the prompt box, crush-style: the model
// segment on the left (click opens the selection dialog), contextual hints on
// the right — only what applies right now.
// defaultContextWindow backs the context bar when no window is configured.
const defaultContextWindow = 200000

// usageLabel renders the token segment: session totals up/down plus the
// context bar (last turn vs. window). Hidden until the first turn (FR-23).
func (m Model) usageLabel() string {
	if m.totalIn == 0 && m.totalOut == 0 {
		return ""
	}
	use := "↑" + humanTokens(m.totalIn) + " ↓" + humanTokens(m.totalOut)
	win := m.ctxWin
	if win <= 0 {
		win = defaultContextWindow
	}
	if m.lastIn > 0 && win > 0 {
		used := m.lastIn + m.lastOut
		if used > win {
			used = win
		}
		pct := used * 100 / win
		filled := used * 10 / win
		bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
		use += " · " + bar + " " + fmt.Sprintf("%d%%", pct)
	}
	return use
}

// humanTokens formats a token count compactly: 980, 4.5k, 1.2M.
func humanTokens(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func (m Model) footerView() string {
	left := m.stateLabel()
	use := m.usageLabel()
	hints := ""
	if m.running {
		hints = "ctrl+c abort"
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - runewidth.StringWidth(left) - 2 - runewidth.StringWidth(hints) - 3
	if use != "" {
		gap -= runewidth.StringWidth(use) + 2
	}
	if gap < 1 {
		// Too narrow for the numbers: drop the usage segment before the
		// hints lose their place.
		use = ""
		gap = w - runewidth.StringWidth(left) - 2 - runewidth.StringWidth(hints) - 3
		if gap < 1 {
			gap = 1
		}
	}
	if m.theme == nil {
		return strings.TrimRight(" "+left+strings.Repeat(" ", gap+3+runewidth.StringWidth(hints)), " ")
	}
	fill := m.theme.TableFill()
	line := " " + fill.Render(" "+left+" ")
	if use != "" {
		line += " " + m.theme.LabelStyle().Render(use)
	}
	line += strings.Repeat(" ", gap)
	if hints != "" {
		line += m.theme.LabelStyle().Render(hints) + " "
	}
	return strings.TrimRight(line, " ")
}

// startTurn resets the live state for a fresh prompt.
func (m *Model) startTurn() {
	m.running = true
	m.cancelling = false
	m.streaming = ""
	m.thinking = ""
	m.toolStatus = ""
	// A fresh turn starts without the previous thinking block (FR-30) and
	// without stale streaming preview blocks.
	m.chat = m.chat.removeBlock(ThinkBlockID)
	m.chat = m.chat.removeStreamBlocks()
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
	m.seedDiagrams = true
	return m
}

func (m Model) Init() tea.Cmd {
	imageLogf("app open: term=%s images=%v caps=%+v", os.Getenv("TERM"), m.imagesEnabled(), m.caps)
	// Seeded Diagramme konvertiert der WindowSizeMsg-Handler (seedDiagrams,
	// FR-6/27/28) — hier NICHT nochmal triggern: doppelter Start hat jede
	// Konvertierung doppelt ausgefuehrt (doppelte Wartezeit, s. Debug-Log).
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevValue := m.input.Value()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		imageLogf("viewport %dx%d Zellen", msg.Width, msg.Height)
		m.input.SetWidth(msg.Width - 4)
		m.syncInputSize()
		// Der Seed haengt Diagramm-Fences an; die Konvertierung braucht das
		// laufende Programm — der erste Frame loest sie aus (FR-6/27/28).
		if m.seedDiagrams {
			m.seedDiagrams = false
			// Der Flush druckt alle non-pending Seed-Bloecke sofort; die
			// Diagramm-Bloecke druckt diagramsDoneMsg nach der Konvertierung.
			return m, tea.Batch(m.pendingDiagrams(), m.flushPrinted())
		}
		return m, m.flushPrinted()
	case tea.MouseMsg:
		// The [ modell · effort ] footer chip toggles the popup; clicks
		// inside the open popup select, outside close it (FR-20-lite).
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if lo, hi := m.modelSegmentRange(); msg.Y == m.height-1 && msg.X >= lo && msg.X < hi {
				m.openPicker("model")
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
		// The open picker swallows keys first (FR-20).
		if m.pickerOpen {
			return m.pickerKey(msg)
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
		// ctrl+j is the line break (FR-7). Grow the box BEFORE the textarea
		// moves the cursor to the new line: its internal viewport scrolls
		// when the cursor leaves a too-small window and never scrolls back
		// after SetHeight — the first line was cut off above. Growing first
		// keeps every row visible; syncInputSize normalises afterwards.
		if msg.String() == "ctrl+j" {
			if h := m.input.Height(); h < inputMaxHeight {
				m.input.SetHeight(h + 1)
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
				return m, m.flushPrinted()
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
			// User-Block append-only echoen (timg-Modell), Versand parallel.
			return m, tea.Batch(sendCmd, m.flushPrinted())
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
		case "c", "esc", "t", "+", "=", "-", "_":
			// Block-Aktionen am fokussierten Block: c/esc wie gehabt; t
			// schaltet einen Diagramm-Block zwischen Bild (Standard) und
			// rohem Quelltext um, +/- sind fuer Zoom reserviert (Bonus-FR).
			// Ohne Fokus faellt alles durch und tippt normal ins Eingabefeld.
			if m.chat.Focused() {
				var cmd tea.Cmd
				var handled bool
				m.chat, cmd, handled = m.chat.Update(msg)
				if handled {
					return m, cmd
				}
			}		}
	case chunkMsg:
		// The answer grows in place under the chat while it streams (FR-2):
		// prose as raw preview, in-progress tables re-layout per row.
		m.streaming += msg.text
		m.chat = m.chat.upsertStreamWidgets(buildStreamWidgets(
			m.streaming, textColorFor(m.theme), m.theme, m.copyFn))
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
	case usageMsg:
		m.totalIn += msg.in
		m.totalOut += msg.out
		m.lastIn, m.lastOut = msg.in, msg.out
		return m, nil
	case newSessionMsg:
		if msg.err != nil {
			m.chat = m.chat.Append(NewTextBlock("Neue Session fehlgeschlagen: "+msg.err.Error(), "1"))
			return m, nil
		}
		// Fresh session: fresh chat, fresh counters (FR-20).
		m.chat = m.chat.Clear()
		m.totalIn, m.totalOut = 0, 0
		m.lastIn, m.lastOut = 0, 0
		return m, nil
	case configResultMsg:
		// The backend confirmed (or failed) the model/effort change.
		if msg.err != nil {
			m.chat = m.chat.Append(NewTextBlock("Fehler: "+msg.err.Error(), "1"))
			return m, nil
		}
		m.pickerKind = msg.kind
		m.applyPicker(msg.value)
		m.closePicker()
		return m, nil
	case diagramsDoneMsg:
		m.chat, _ = m.chat.applyDiagrams(msg.results, m.imagesEnabled(), m.caps)
		// Jetzt sind die Bloecke final (Bild oder Fallback-Text): drucken.
		return m, m.flushPrinted()
	case replyMsg:
		// The final text supersedes the live preview (FR-2).
		m.endTurn()
		m.chat = m.chat.removeStreamBlocks()
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
		// PlantUML blocks in the fresh reply convert in the background
		// (FR-6); die finalen Bloecke druckt der Flush append-only.
		return m, tea.Batch(m.pendingDiagrams(), m.flushPrinted())
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

// flushPrinted druckt im Inline-Modus alle abgeschlossenen Bloecke
// append-only ueber die Live-Region (tea.Println): Zeilen GENAU EINMAL
// als queued lines — der Renderer schreibt sie VOR den Frame und fasst
// sie nie wieder an (timg-Modell innerhalb bubbletea). Kein Vollbild-
// Rerender, kein Verwerfen ueber der Fensterhoehe. Der Druck stoppt an
// Blockern: Diagramm-Konvertierung anstehend (drucken erst nach
// applyDiagrams), Stream-Vorschau (mutiert noch) und ThinkBlock waehrend
// des Laufs.
func (m *Model) flushPrinted() tea.Cmd {
	if !m.inline {
		return nil
	}
	pending := make(map[int]bool, 4)
	for _, j := range m.diagramJobs() {
		pending[j.id] = true
	}
	var cmds []tea.Cmd
	for m.printedUpTo < len(m.chat.items) {
		it := m.chat.items[m.printedUpTo]
		id := it.ID()
		if pending[id] {
			break
		}
		if id <= StreamBlockID && id > StreamBlockID-streamIDRange {
			break
		}
		if _, ok := it.(ThinkBlock); ok && m.running {
			break
		}
		for _, line := range strings.Split(it.View(m.width), "\n") {
			cmds = append(cmds, tea.Println(line))
		}
		m.printedUpTo++
	}
	if len(cmds) == 0 {
		return nil
	}
	// Sequence haelt die Reihenfolge: Bild-Escape und seine Leerzeilen
	// gehoeren zusammen und duerfen nicht vertauscht werden.
	return tea.Sequence(cmds...)
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
	vt0 := time.Now()
	var tChat time.Duration
	defer func() {
		d := time.Since(vt0)
		if d > 20*time.Millisecond {
			imageLogf("view slow: total=%.1fms chat=%.1fms fill=0.0ms blocks=%d win=%dx%d",
				float64(d.Microseconds())/1000, float64(tChat.Microseconds())/1000, len(m.chat.items), m.width, m.height)
		}
		perfView(d, m.width, m.height)
	}()
	var top strings.Builder
	top.WriteString(lipgloss.NewStyle().Bold(true).Render("heron · goose acp"))
	top.WriteString("\n")
	top.WriteString(m.statusLine())
	top.WriteString("\n")
	if m.chat.Empty() {
		top.WriteString("(no messages yet — type below)\n")
	}
	var chatView string
	if !m.inline {
		// Inline: der Chat ist append-only gedruckt, nicht Teil des Views —
		// hier wuerde er nur Render-Kosten ohne sichtbaren Effekt machen.
		tc0 := time.Now()
		chatView = m.chat.View()
		tChat = time.Since(tc0)
	}
	top.WriteString(chatView)

	// The prompt box belongs to the bottom of the screen. Padding the gap
	// after it — what a plain full-screen fill does — left the box floating
	// near the top whenever the chat was short, i.e. always on an empty chat.
	// So the spare rows go between chat and prompt, never below it.
	prompt := m.inputView()
	if m.inline {
		// timg-Modell mit sichtbarer Basis (FR-27): der Verlauf wird
		// append-only gedruckt (flushPrinted -> tea.Println), der View
		// haelt NUR die feste Live-Region — Header + Status + Eingabe +
		// Footer, immer 6 Zeilen. Sie passt garantiert ins Fenster, also
		// kann der Renderer nichts verwerfen, und die Eingabe bleibt
		// bedienbar (gemessen: Input ohne Renderer ist tot — Nur-Druck
		// scheidet als Endzustand aus).
		var live strings.Builder
		live.WriteString(lipgloss.NewStyle().Bold(true).Render("heron · goose acp"))
		live.WriteString("\n")
		sl := m.statusLine()
		if sl == "" && m.running {
			sl = lipgloss.NewStyle().Faint(true).Render("▸ Antwort läuft …")
		}
		live.WriteString(sl)
		live.WriteString("\n")
		live.WriteString(prompt)
		live.WriteString("\n")
		live.WriteString(m.footerView())
		out := live.String()
		perfBytes(len(out))
		return out
	}
	if m.height <= 0 {
		// No terminal size known yet (tests, first paint): render as-is
		// instead of squeezing the frame into an unknown height, which cut
		// the header and the newest chat rows.
		return m.fillScreen(top.String() + "\n\n" + prompt + "\n" + m.footerView())
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
	return m.fillScreen(strings.Join(append(append(topLines, prompt), m.footerView()), "\n"))
}

// fillScreen pads every line to the terminal width and paints the theme
// background across the whole screen, re-establishing it after inner resets.
func (m Model) fillScreen(s string) string {
	seq := m.theme.BackgroundSeq()
	if m.width <= 0 || seq == "" {
		perfBytes(len(s))
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
	out := strings.Join(lines, "\n")
	perfBytes(len(out))
	return out
}

// kittyRespFrag matcht Runen-Buendel, in die bubbletea verspaetete Kitty-
// Antworten zerschneidet: ESC_Gi=31;OK ESC\ wird zu KeyMsgs wie "_Gi=31",
// ";OK", "i=32" — echte Tastatur liefert JEDES Zeichen als eigene KeyMsg,
// Runen-Buendel dieser Form entstehen nur aus Sequenzmuell (PTY-Beweis).
var kittyRespFrag = regexp.MustCompile(`^(alt+)?_?G?i=d+(,p=d+)?(;(OK|0K|E[A-Z]+))?$`)

// kittyResponseFilter verwirft Tasten, die wie Kitty-Grafik-Antworten
// aussehen (z. B. ESC_Gi=1,p=1;OK ESC): q=2 unterdrueckt die Antworten der
// Bildbefehle bereits an der Quelle, aber die PROBE (a=q) WILL Antworten —
// treffen die verspaetet bei bubbletea ein, zerschneidet der Parser sie in
// Runen-Muell (sichtbare Fragmente "i=31", "[<65;..."). Der Filter dropt
// beide Formen (FR-27).
func kittyResponseFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return msg
	}
	s := k.String()
	for _, frag := range []string{";OK", ";0K", ";ENOENT", ";EINVAL"} {
		if strings.Contains(s, frag) {
			return nil
		}
	}
	if len(k.Runes) > 1 && kittyRespFrag.MatchString(s) {
		return nil
	}
	return msg
}

// RunModel starts the TUI with a prepared model (all-motion mouse: hover).
func RunModel(m Model) error {
	// CPU-Profil bei HERON_CPU_PROF=1 (Diagnose, s. perflog.go): zeigt,
	// wo Render-Zeit wirklich verbracht wird, statt zu raten.
	if strings.EqualFold(os.Getenv("HERON_CPU_PROF"), "1") {
		if f, err := os.Create("/tmp/heron.prof"); err == nil {
			defer f.Close()
			if err := pprof.StartCPUProfile(f); err == nil {
				defer pprof.StopCPUProfile()
				imageLogf("cpu-profil: /tmp/heron.prof")
			}
		}
	}
	opts := []tea.ProgramOption{tea.WithFilter(kittyResponseFilter)}
	if m.inline {
		// Kein Alt-Screen, kein Mouse-Reporting: der Terminal scrollt
		// nativ — damit entfaellt auch die Klasse der SGR-Maus-Leaks,
		// weil keine Reports mehr erzeugt werden.
		inlineActive.Store(true)
		defer inlineActive.Store(false)
		// Inline (FR-27): KEIN Alt-Screen, KEIN Mouse-Reporting — der
		// Terminal scrollt nativ, der Verlauf lebt im Scrollback. Der
		// Default-Renderer unten wird gebraucht: ohne ihn ist die
		// Tastatur tot (PTY-Beweis: Ctrl+C ohne Renderer beendet nicht).
	} else {
		opts = append(opts, tea.WithAltScreen(), tea.WithMouseAllMotion())
	}
	p := tea.NewProgram(m, opts...)
	// Hand the runtime to the stream so the backend can publish live updates
	// from its own goroutine (FR-2). A nil stream is a no-op.
	m.stream.attach(p)
	_, err := p.Run()
	// Alle Terminal-Bilder loeschen und Temp-PNGs entfernen (FR-27): nach
	// dem Exit bleibt kein Bildmuell im Ghostty/Kitty-Speicher.
	KittyCleanup()
	return err
}

// PrintOnly rendert einen Seed als REINEN Text direkt auf stdout — kein
// bubbletea, kein Renderer, kein TUI, keine Tastatur (FR-27 Diagnose):
// die haerteste Bisektionsstufe fuer "kommt Text ueberhaupt an?". Wie cat
// docs/render-test.md, nur mit den echten Block-Views. Bild-Bloecke
// erscheinen als ihre ASCII-Fallback-Zeilen + Marker.
func PrintOnly(src string, w int) error {
	m := New(nil, nil, nil).SeedMarkdown(src)
	m.width = w
	var out strings.Builder
	for i, it := range m.chat.items {
		if i > 0 {
			out.WriteString("\n")
		}
		if img, ok := it.(ImageBlock); ok {
			out.WriteString("⟦Diagramm " + strconv.Itoa(img.ID()) + " — in der TUI als Kitty-Bild (HERON_INLINE)⟧\n")
			if img.ascii != "" {
				out.WriteString(img.ascii)
				out.WriteString("\n")
			}
			continue
		}
		out.WriteString(it.View(w))
	}
	out.WriteString("\n")
	_, err := os.Stdout.WriteString(out.String())
	return err
}

// Run starts the TUI.
func Run(send Sender, th *render.Theme, copyFn CopyFunc) error {
	return RunModel(New(send, th, copyFn))
}
