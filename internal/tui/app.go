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
	text   string
	err    error
	prompt string // der gescheiterte Prompt — Fach fuer den Retry (FR-33)
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

	// inputFocus: der Zustand der Zwei-Modus-Kette. STANDARD ist der
	// Eingabe-Modus — das Eingabefeld ist selektiert (Neon-Rahmen,
	// blinkender Cursor) und Getippe landet dort. tab schaltet in den
	// Antwort-Modus: der letzte interaktive Block der KI-Antwort (z. B.
	// ein Codeblock) ist selektiert, ↑/↓ laufen blockweise, Getippe
	// erreicht das Eingabefeld NICHT (die Textarea ist geblurrt — nur ein
	// selektierter Abschnitt ist interaktionsfaehig). ↑/↓ im Eingabe-Modus
	// tun nichts: sie sind fuer die Eingabe-History reserviert.
	inputFocus bool

	seedDiagrams bool // Seed enthaelt plantuml/mermaid: einmalige Konvertierung

	// Caret-Blink (FR-42): charBlinkArmed verhindert eine zweite Tick-Kette
	// beim schnellen esc/Wiedereintritt (zwei Takte wuerden sich pro Periode
	// zweimal schlagen — das Caret stuende still); charBlinkInterval ist im
	// rig auf Nanosekunden kuerzbar (wie input.Cursor.BlinkSpeed), sonst
	// wuerde jeder Test-Eintritt 530 ms echt warten.
	charBlinkArmed    bool
	charBlinkInterval time.Duration

	// Live turn state (FR-2, FR-7).
	stream     *Stream // may be nil: no live updates
	cancel     func()  // aborts the running turn, may be nil
	running    bool    // a turn is in flight
	cancelling bool    // cancel already requested
	streaming  string  // raw answer received so far
	thinking   string  // thinking received so far
	toolStatus string  // last tool label while the turn runs
	spinner    int     // frame index of the running animation
	// neonPhase schiebt das Neon-Band am Eingaberahmen pro Spinner-Takt eine
	// Zelle weiter (crushs Regenbogen-Spinner zyklisiert ebenso); unbounded,
	// der Render nimmt es modulo Umfang.
	neonPhase int
	// neonLevel 0..1: Fade des Neon-Bands — beim Turnstart blendet es ein,
	// nach der Antwort wieder aus; der Ticker laeuft solange nach.
	neonLevel float32

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
	// Resume (FR-44): Liste + Start als Callbacks — dieselbe Naht wie
	// /new. resumeRows sind die Zeilen des offene Resume-Pickers.
	resumeList   func() ([]SessionRow, error)
	resumeStart  func(id string) error
	resumeRows   []SessionRow
	caps         TermCaps // Term-Probe (FR-27): Platzhalter + Zellpixel
	pickerKind   string   // "model" or "effort" while open
	pickerOpen   bool
	pickerCursor int
	pickerScroll int
	pickerQuery  string // /model filters the list as you type (FR-20)

	// provider/providerOptions: aktueller ACP-Provider und die waehlbaren
	// Alternativen (/provider, FR-45) — Name ist das lesbare Label, Value
	// der Konfigurationswert fuer set_config_option.
	provider        string
	providerOptions []OptionRow
	// providerFavorites: die IDs der in goose EINGERICHTETEN Provider —
	// goose nennt sie selbst via _goose/unstable/config/read (key
	// "providers": der providers:-Block, nicht das ~90-Eintraege-
	// Inventar, das keyless Provider wie lmstudio als "configured"
	// katalogisiert). filteredProviderRows() zeigt nur sie plus den
	// aktiven; die Suche im Picker oeffnet den Vollkatalog.
	providerFavorites []string

	// queued: Prompts, die waehrend eines laufenden Turns getippt wurden —
	// sie gehen automatisch raus, sobald der Turn endet (FR-46); doppeltes
	// ctrl+c verwirft die Warteschlange. lastCtrlC erkennt den Doppeldruck.
	queued    []string
	lastCtrlC time.Time

	// turnTools zaehlt die Tool-Calls des laufenden/letzten Turns als
	// distinct callIDs (FR-31: In-place-Updates einer Karte zaehlen nicht
	// doppelt). startTurn setzt zurueck, endTurn bewahrt den Stand.
	turnTools map[string]bool
	// toolLive akkumuliert die live_output-Chunks je CallID (goose streamt
	// Shell-Ausgabe Stueckweise; das finale Update traegt das Gesamtergebnis
	// als rawOutput und loescht den Eintrag wieder).
	toolLive map[string]string

	// Token usage (FR-23): goose reports per-turn usage in the prompt
	// response. Totals accumulate the session; lastIn/lastOut feed the
	// context bar (last input ≈ context used).
	totalIn, totalOut int
	lastIn, lastOut   int
	ctxWin            int // context window tokens; 0 = default 200k

	// Slash-command autocomplete (FR-20).
	cmdCursor    int
	cmdDismissed bool

	// copiedFlash: kurzzeitiges Feedback des Copy-Chips im Eingabefeld-
	// Footer ("✓ kopiert") — verfaellt mit der naechsten Eingabe. Seit das
	// Copy-UI aus den Block-Footern gewandert ist (s. inputView), lebt das
	// COPIED-Signal HIER.
	copiedFlash bool
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
	// inputFocus ist der ZUSTAND der Zwei-Modus-Kette, kein Extra-Modus:
	// Standard sitzt sie auf dem Eingabefeld (dort wird getippt), tab
	// toggelt zu den Chat-Segmenten und zurueck, ↑/↓ laufen blockweise —
	// aber NUR im Antwort-Modus.
	return Model{input: ta, chat: NewBlockList().SetCopy(copyFn).SetTheme(th), send: send, theme: th, copyFn: copyFn, inline: inlineEnabled(), inputFocus: true, charBlinkInterval: caretBlinkInterval}
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

// WithProviderFavorites prioisiert die eigenen Provider im /provider-
// Picker direkt hinter dem aktiven (FR-45). Die IDs stammen aus GOOSE
// (_goose/unstable/providers/list, Typ "Custom"/"Declarative") und
// treffen im Regelbetrieb via providerFavoritesMsg ein; der Setter ist
// die Naht fuer Demo/Tests.
func (m Model) WithProviderFavorites(values []string) Model {
	m.providerFavorites = values
	return m
}

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
	if m.pickerKind == "resume" {
		rows := m.filteredResumeRows()
		labels := make([]string, len(rows))
		for i, r := range rows {
			labels[i] = r.Label
		}
		return labels
	}
	if m.pickerKind == "provider" {
		rows := m.filteredProviderRows()
		labels := make([]string, len(rows))
		for i, r := range rows {
			labels[i] = r.Name
		}
		return labels
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

// filteredResumeRows wendet die Picker-Suche auf die Session-Zeilen an —
// dieselbe Logik wie die Modell-Suche (FR-20), über Label UND Detail.
func (m Model) filteredResumeRows() []SessionRow {
	if m.pickerQuery == "" {
		return m.resumeRows
	}
	q := strings.ToLower(m.pickerQuery)
	var out []SessionRow
	for _, r := range m.resumeRows {
		if strings.Contains(strings.ToLower(r.Label+" "+r.Detail), q) {
			out = append(out, r)
		}
	}
	return out
}

// providerRows sortiert die Provider-Liste fuer den Picker in drei Zonen,
// jede in der Reihenfolge, die goose annonciert hat: der AKTIVE Provider
// steht zuerst (ein Druck auf enter haelt ihn), dann die in goose
// EINGERICHTETEN Provider (Keys des providers:-Blocks, die goose via
// config/read selbst nennt), dann der Rest.
func (m Model) providerRows() []OptionRow {
	rows := make([]OptionRow, len(m.providerOptions))
	copy(rows, m.providerOptions)
	fav := make(map[string]bool, len(m.providerFavorites))
	for _, v := range m.providerFavorites {
		fav[v] = true
	}
	var active, own, rest []OptionRow
	for _, r := range rows {
		switch {
		case r.Value == m.provider:
			active = append(active, r)
		case fav[r.Value]:
			own = append(own, r)
		default:
			rest = append(rest, r)
		}
	}
	out := append(active, own...)
	return append(out, rest...)
}

// filteredProviderRows ist die sichtbare Picker-Liste: OHNE Suche nur
// der aktive Provider plus die in goose EINGERICHTETEN (goose katalogisiert
// ~90 Provider, eingerichtet sind wenige — die Ruhe-Liste bleibt klein).
// MIT Suche der volle Katalog: auch ein nicht eingerichteter Provider
// bleibt so erreichbar. Ohne bekannte Favoriten (Demo-Modus, goose ohne
// config/read) zeigt die Ruhe-Liste wie bisher den vollen Katalog.
func (m Model) filteredProviderRows() []OptionRow {
	rows := m.providerRows()
	if m.pickerQuery != "" {
		q := strings.ToLower(m.pickerQuery)
		var out []OptionRow
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.Name+" "+r.Value), q) {
				out = append(out, r)
			}
		}
		return out
	}
	if len(m.providerFavorites) == 0 {
		return rows
	}
	fav := make(map[string]bool, len(m.providerFavorites))
	for _, v := range m.providerFavorites {
		fav[v] = true
	}
	var out []OptionRow
	for _, r := range rows {
		if r.Value == m.provider || fav[r.Value] {
			out = append(out, r)
		}
	}
	return out
}

// openPicker expands the prompt box into the option list for the given kind
// ("model", "effort" or "resume"). No-op without announced options.
func (m *Model) openPicker(kind string) {
	if kind == "resume" {
		if len(m.resumeRows) == 0 {
			return
		}
		m.pickerKind = kind
		m.pickerOpen = true
		m.pickerCursor, m.pickerScroll, m.pickerQuery = 0, 0, ""
		m.clampPickerScroll()
		return
	}
	var options []string
	if kind == "provider" {
		if len(m.providerOptions) == 0 {
			return
		}
		m.pickerKind = kind
		m.pickerOpen = true
		m.pickerCursor, m.pickerScroll, m.pickerQuery = 0, 0, ""
		m.clampPickerScroll()
		return
	}
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

// WithResumeList wires the /resume listing: goose's stored sessions mapped
// to display rows (nil = demo mode, /resume reports that).
func (m Model) WithResumeList(fn func() ([]SessionRow, error)) Model {
	m.resumeList = fn
	return m
}

// WithResumeStart wires the /resume start: loads the chosen session via
// session/load and replays its history into the chat.
func (m Model) WithResumeStart(fn func(id string) error) Model {
	m.resumeStart = fn
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
		// Resume (FR-44): enter startet die markierte Session — Chat und
		// Token-Zähler werden vorher geleert, der Replay füllt beides neu.
		if m.pickerKind == "resume" {
			rows := m.filteredResumeRows()
			if m.pickerCursor >= len(rows) {
				return m, nil
			}
			id := rows[m.pickerCursor].ID
			m.closePicker()
			m.chat = m.chat.Clear()
			m.printedUpTo = 0 // Inline: von vorn drucken (dieselbe Regel wie /new)
			m.totalIn, m.totalOut, m.lastIn, m.lastOut = 0, 0, 0, 0
			if m.resumeStart == nil {
				m.chat = m.chat.Append(NewTextBlock("Kein Backend für Resume (Demo-Modus).", "1"))
				return m, m.flushPrinted()
			}
			fn := m.resumeStart
			return m, func() tea.Msg { return resumeStartMsg{err: fn(id)} }
		}
		options := m.pickerOptions()
		if m.pickerCursor >= len(options) {
			return m, nil
		}
		value := options[m.pickerCursor]
		// Provider-Zeilen: Der Picker zeigt das Label, gesendet wird der
		// Value ("Z.AI (GLM)" -> "zai_coding_plan").
		if m.pickerKind == "provider" {
			rows := m.filteredProviderRows()
			if m.pickerCursor >= len(rows) {
				return m, nil
			}
			value = rows[m.pickerCursor].Value
		}
		if m.configSet == nil {
			m.applyPicker(value)
			m.closePicker()
			return m, nil
		}
		id := "model"
		if m.pickerKind == "effort" {
			id = "thinking_effort"
		}
		if m.pickerKind == "provider" {
			id = "provider"
		}
		kind, fn := m.pickerKind, m.configSet
		return m, func() tea.Msg {
			return configResultMsg{kind: kind, value: value, err: fn(id, value)}
		}
	case "esc":
		m.closePicker()
		return m, nil
	}
	// The model and resume lists filter as you type; the effort list is
	// short enough to browse without a query (FR-20/FR-44). A changed
	// query restarts at the top.
	if m.pickerKind == "model" || m.pickerKind == "resume" || m.pickerKind == "provider" {
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
	if m.pickerKind == "provider" {
		m.provider = value
		return
	}
	m.modelLabel = value
}

// OptionRow ist eine waehlbare Konfigurationsoption mit Label — der
// Provider-Picker zeigt das Name-Feld und sendet das Value-Feld (FR-45).
type OptionRow struct {
	Value string
	Name  string
}

// retryPromptMsg: "r" auf einem fokussierten Fehler-Block (FR-33) sendet
// den gescheiterten Prompt erneut — der Fehler-Block wird davor entfernt.
type retryPromptMsg struct {
	prompt  string
	blockID int
}

// spinnerKick returns the next animation frame while a turn runs (FR-2):
// the status line shows a rotating braille spinner so the user can see the
// AI is working even before the first chunk/thought/tool event arrives.
const (
	spinnerInterval = 90 * time.Millisecond
)

// spinnerFrames are the sequential frames of the spinner. They must stay
// array literals (NOT one multi-byte string): braille glyphs are 3 bytes in
// UTF-8, so indexing a string with a frame counter would slice continuation
// bytes and render garbage.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerTickMsg advances the running animation by one frame.
type spinnerTickMsg struct{}

// nextSpinner re-arms the animation timer for the next frame.
func nextSpinner() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// caretBlinkInterval ist der Standard-Takt des Zeichen-Carets (FR-42):
// ~2 Hz wie der Terminal-Cursor — sichtbar/aufblendend per Bewegung,
// abblendend im Leerlauf. Das rig kuerzt ihn auf Nanosekunden.
const caretBlinkInterval = 530 * time.Millisecond

// charBlinkMsg toggelt die Blink-Phase des Zeichen-Carets.
type charBlinkMsg struct{}

// nextCharBlink re-arms the caret blink timer.
func (m Model) nextCharBlink() tea.Cmd {
	return tea.Tick(m.charBlinkInterval, func(time.Time) tea.Msg { return charBlinkMsg{} })
}

// spinnerGlyph returns the current animation glyph, or "" when idle.
func (m Model) spinnerGlyph() string {
	if !m.running {
		return ""
	}
	return spinnerFrames[m.spinner%len(spinnerFrames)]
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

// trimHints kuerzt die zentrale Hint-Leiste auf maxW Zellen (Punkt→Eclipse),
// damit die Footer-Zeile nie umbricht und Zeilenzahl/Layout stabil bleiben.
func trimHints(hints string, maxW int) string {
	if maxW < 1 || runewidth.StringWidth(hints) <= maxW {
		return hints
	}
	var out strings.Builder
	w := 0
	for _, r := range hints {
		rw := runewidth.StringWidth(string(r))
		if w+rw > maxW-1 {
			break
		}
		out.WriteRune(r)
		w += rw
	}
	return strings.TrimRight(out.String(), " ") + "…"
}

func (m Model) footerView() string {
	left := m.stateLabel()
	use := m.usageLabel()
	// FR-31: Anzahl der Tool-Calls des (letzten) Turns als eigenes Segment
	// vor der Token-Anzeige — Updates derselben Karte zaehlen einfach, ein
	// neuer Turn startet bei 0. Ohne Tool-Calls bleibt das Segment weg.
	if n := len(m.turnTools); n > 0 {
		seg := fmt.Sprintf("🔧 %d tool call", n)
		if n != 1 {
			seg += "s"
		}
		if use != "" {
			seg += " · " + use
		}
		use = seg
	}
	hints := ""
	if m.running {
		hints = "ctrl+c abort"
		if len(m.queued) > 0 {
			hints = "ctrl+c abort · 2× verwirft warteschlange"
		}
	}
	// Zentrale Aktions-/Hint-Leiste (ganz unten): shift+enter SPAWNT den
	// Zeichen-Cursor — das steht IMMER da, nicht erst nach einer Auswahl
	// (der Cursor ist der Haupteingang, blockweise Auswahl ist der Spezial-
	// fall per shift+up/down). Die Spezial-Modi (Pan, Bild, Cursor aktiv)
	// verdraengen ihn nur, solange sie gelten.
	var h string
	switch {
	case m.chat.charActive():
		// Zeichen-Cursor (FR-42): Editor-Caret ueber dem Chat — Pfeile
		// bewegen (alle Richtungen), shift+Pfeile waehlen, c/enter
		// kopieren, esc loest erst die Auswahl und verlaesst dann.
		h = "←↑↓→ cursor · shift+pfeile auswählen · c copy · esc fertig"
	case m.chat.FocusedError():
		// Fehler-Block (FR-33): r sendet den gescheiterten Prompt erneut.
		h = "r retry · c copy · esc lösen"
	case m.chat.FocusedPanning():
		h = "+/- zoom · ←↑↓→ bewegen · enter/esc fertig"
	case m.chat.FocusedImageToggle():
		h = "⏎ pan & zoom · t text·bild"
	case m.chat.selActive():
		// Blockweise Auswahl (FR-42/43): c kopiert den Bereich, shift+enter
		// spawnt den Cursor an der Auswahl-Kante.
		h = "c copy · shift+enter cursor · esc lösen"
	case m.chat.Focused() || m.chat.markedCount() > 0:
		h = "c copy · shift+enter cursor · space auswählen · y yank · esc lösen"
	case m.chat.Empty():
		// Der Cursor braucht Text: Bei leerem Chat (frische Session) waere
		// shift+enter stumm — die Leiste sagt, warum.
		h = "shift+enter cursor (ab der ersten Antwort)"
	default:
		h = "shift+enter cursor · tab chat"
	}
	if m.copiedFlash {
		// Copy-Feedback: der Flash steht kurzzeitig VOR den Hints — so
		// ueberlebt er das Breiten-Kappen der Leiste (trimHints schneidet
		// das Ende, nicht den Anfang).
		h = "✓ kopiert · " + h
	}
	if hints != "" {
		hints += " · "
	}
	hints += h
	w := m.width
	if w <= 0 {
		w = 80
	}
	// Hints nie die Zeile wachsen lassen: hart auf die Breite kappen, damit
	// die finale Zeile nicht umbricht und die Zeilenzahl/Layout stabil bleibt.
	if budget := w - runewidth.StringWidth(left) - 6; budget > 0 && runewidth.StringWidth(hints) > budget {
		hints = trimHints(hints, budget)
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
		// Theme-loser Fallback (Tests/PrintOnly): die Hints duerfen NICHT
		// verloren gehen — vorher stand hier nur Links-Label + Padding.
		line := " " + left + strings.Repeat(" ", gap)
		if hints != "" {
			line += hints + " "
		}
		return strings.TrimRight(line, " ")
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

// setInputFocus schaltet die Zwei-Modus-Kette: Eingabe-Modus (on) selektiert
// das Eingabefeld — die Textarea ist fokussiert, der Cursor blinkt, der
// Rahmen leuchtet Neon. Antwort-Modus (off) blurrt die Textarea: bubbles
// verarbeitet Tasten nur im fokussierten Zustand, Getippe kann also NICHT
// im Eingabefeld landen, solange ein Chat-Segment selektiert ist — ein
// nicht selektierter Abschnitt ist nicht interaktionsfaehig.
func (m *Model) setInputFocus(on bool) tea.Cmd {
	if on == m.inputFocus {
		return nil
	}
	m.inputFocus = on
	if on {
		// Ketten-Heimat: Der Fokus kehrt ins Eingabefeld zurueck — die
		// Klick-Markierung IST Teil der Auswahl und weicht MIT ihm (FR-39:
		// zu jedem Zeitpunkt leuchtet hoechstens EIN Neon-Rahmen; tab/esc
		// duerfen nie zwei Rahmen stehen lassen).
		m.chat = m.chat.clearMarks()
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

// startTurn resets the live state for a fresh prompt.
func (m *Model) startTurn() {
	m.running = true
	m.cancelling = false
	m.streaming = ""
	m.thinking = ""
	m.toolStatus = ""
	m.spinner = 0
	m.turnTools = nil // FR-31: der Zaehler gehoert zum EINEN Turn
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

// chatTop is the screen row where the chat area starts (title only — the
// "AI is working" row sits above the input box, not up here).
const chatTop = 1

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
	// Der Copy-Flash des Eingabefeld-Chips verfaellt mit der naechsten
	// Eingabe — Tasten und Maus-PRESS. Ein RELEASE (folgt jedem Klick
	// unmittelbar) loescht ihn nicht: sonst waere der Klick-Flash nie
	// sichtbar (Press setzt, Release loescht, bevor gerendert wird).
	switch mm := msg.(type) {
	case tea.KeyMsg:
		m.copiedFlash = false
	case tea.MouseMsg:
		if mm.Action == tea.MouseActionPress {
			m.copiedFlash = false
		}
	}
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
		marksBefore := m.chat.markedCount()
		m.chat, cmd, handled = m.chat.Update(mm)
		if handled {
			// FR-39-Invariante "hoechstens EIN Neon-Rahmen", Maus-Pfad: Die
			// Klick-Markierung IST die Auswahl und haelt DEN Rahmen — das
			// Eingabefeld weicht (blur, Rahmen zur Ruhefarbe), genauso wie
			// die Shift-Auswahl die Eingabe weichen laesst. Loeist der Klick
			// die letzte Markierung und haelt die Kette keinen Chat-Fokus,
			// kehrt der Fokus auf seinen STANDARD zurueck: Eingabe-Modus.
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				switch {
				case m.chat.markedCount() > 0 && m.inputFocus:
					cmd = tea.Batch(cmd, m.setInputFocus(false))
				case marksBefore > 0 && m.chat.markedCount() == 0 && !m.chat.Focused():
					cmd = tea.Batch(cmd, m.setInputFocus(true))
				}
			}
			return m, cmd
		}
	case tea.KeyMsg:
		// The open picker swallows keys first (FR-20).
		if m.pickerOpen {
			return m.pickerKey(msg)
		}
		// Slash-command navigation (FR-20): while the prompt starts with
		// "/", up/down move the selection, tab completes, esc dismisses.
		// Nur im Eingabe-Modus: im Antwort-Modus sind ↑/↓ Block-Tasten.
		if m.inputFocus && m.slashActive() && len(m.slashMatches()) > 0 {
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
		// Antwort-Modus: kein Zeilenumbruch — das Eingabefeld ist nicht
		// selektiert und damit nicht interaktionsfaehig.
		if msg.String() == "ctrl+j" && m.inputFocus {
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
		case "shift+enter", "shift+tab":
			// Zeichen-Cursor (FR-42): shift+enter IST das Editor-Caret —
			// frei bewegend, shift+Pfeile auswaehlend (nur lesend). handleKey
			// entscheidet: Eintritt an der Leseposition (bzw. an laufender
			// Block-Auswahl), Austritt per Toggle. Aus beiden Modi (Eingabe
			// weicht, Antwort bleibt). shift+enter kommt in ghostty/kitty
			// als eigene Sequenz an (Kitty-Protokoll, kbd.go uebersetzt sie
			// auf den selben Trigger); alle anderen Terminals senden fuer
			// shift+enter schlicht \r — dort ist shift+tab der Ausloeser.
			if !m.chat.Empty() {
				var cmd tea.Cmd
				var handled bool
				m.chat, cmd, handled = m.chat.Update(msg)
				if handled {
					focus := m.setInputFocus(false)
					cmds := []tea.Cmd{cmd, focus}
					if m.chat.charActive() && !m.charBlinkArmed {
						// Modus-Eintritt: GENAU EINE Blink-Kette starten
						// (Armed-Marker gegen Doppel-Takte, s. Model).
						m.charBlinkArmed = true
						cmds = append(cmds, m.nextCharBlink())
					}
					return m, tea.Batch(cmds...)
				}
			}
		case "ctrl+c":
			// FR-7: during a turn Ctrl+C aborts the turn (session/cancel)
			// instead of killing the app; a second Ctrl+C force-quits.
			// FR-46: existiert eine Warteschlange, QUITTIERT der Doppel-
			// druck sie (verwirft alles Wartende) — Quit greift erst, wenn
			// nichts mehr wartet.
			now := time.Now()
			double := now.Sub(m.lastCtrlC) < 750*time.Millisecond
			m.lastCtrlC = now
			if double && len(m.queued) > 0 {
				n := len(m.queued)
				m.queued = nil
				m.chat = m.chat.Append(NewTextBlock(
					fmt.Sprintf("Warteschlange verworfen (%d).", n), "1"))
				if !m.running || m.cancel == nil || m.cancelling {
					return m, m.flushPrinted()
				}
			}
			if m.running && m.cancel != nil && !m.cancelling {
				m.cancelling = true
				cancel := m.cancel
				return m, func() tea.Msg { cancel(); return nil }
			}
			return m, tea.Quit
		case "enter":
			// Zeichen-Modus (FR-42): enter kopiert den markierten Buchstaben-
			// Bereich (wie c) — die Cursor-Sektion hat keinen Block-Fokus.
			// Erhaltene Auswahl (22:18): auch von aussen kopierbar.
			if (m.chat.charActive() || m.chat.residualSelShown()) && m.copyFn != nil {
				m.copiedFlash = true
				return m, m.copyFn(m.chat.selMarkdown())
			}
			if m.chat.Focused() {
				var cmd tea.Cmd
				m.chat, cmd, _ = m.chat.Update(msg)
				return m, cmd
			}
			if !m.inputFocus {
				return m, nil // Antwort-Modus ohne Block-Fokus: schlucken
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
			// Queue (FR-46): Laeuft der Turn noch, wartet der Prompt in der
			// Warteschlange und geht automatisch raus, sobald er endet —
			// doppeltes ctrl+c verwirft alles.
			if m.running {
				m.queued = append(m.queued, prompt)
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
				return replyMsg{text: text, err: err, prompt: prompt}
			}
			// User-Block append-only echoen (timg-Modell), Versand parallel;
			// der Status-Spinner laeuft, bis die Antwort eintrifft (FR-2).
			return m, tea.Batch(sendCmd, m.flushPrinted(), nextSpinner())
		case "up", "down", "pgup", "pgdown", "shift+up", "shift+down":
			// Eingabe-Modus: ↑/↓ tun NICHTS — sie sind fuer die Eingabe-
			// History reserviert (spaeter) und springen keinen Fokus. Nur
			// Bild ↓/↑ scrollen seitenweise (Lesen ohne Fokus-Wechsel).
			// Antwort-Modus: ↑/↓ laufen blockweise und STOPPEN an beiden
			// Kettenenden — der Rueckweg ins Eingabefeld fuehrt hier
			// AUSSCHLIESSLICH tab (esc bleibt der Reset auf den Standard).
			if m.inputFocus {
				s := msg.String()
				if s == "pgup" || s == "pgdown" {
					// Bild-Seitenwechsel aus der Eingabe: reines Scrollen,
					// KEIN Fokus-Wechsel (die Kette bleibt in der Eingabe).
					var cmd tea.Cmd
					m.chat, cmd, _ = m.chat.Update(msg)
					return m, cmd
				}
				// shift+up/down starten KEINE Auswahl mehr aus der Eingabe
				// (User-Report: unfreiwilliger Modus-Wechsel — "Man ist im
				// Cursor-Mode gefangen", Getippte erreichte das Feld nicht
				// mehr). Der Weg in die Auswahl fuehrt bewusst ueber tab
				// (Antwort-Modus) bzw. shift+enter (Zeichen-Cursor).
				return m, nil // ↑/↓/shift+↑/shift+↓: History-Reserve, no-op
			}
			var cmd tea.Cmd
			m.chat, cmd, _ = m.chat.Update(msg)
			return m, cmd
		case "left", "right", "shift+left", "shift+right", "home", "end":
			// Antwort-Modus: links/rechts steuern das pannende Bild (die
			// BlockList leitet an den Block weiter); im Zeichen-Cursor
			// (FR-42) bewegen sie das Caret bzw. waehlen mit shift. Ohne
			// beides sind es KEINE Block-Tasten und werden unten verschluckt.
			// Im Eingabe-Modus fallen sie durch zur Textarea (Cursor bewegt
			// sich im Text) — Slash-Completion fängt right oben ab.
			if !m.inputFocus {
				var cmd tea.Cmd
				m.chat, cmd, _ = m.chat.Update(msg)
				return m, cmd
			}
		case "tab":
			// tab TOGGELT Eingabe-Modus <-> Antwort-Modus: vom Eingabefeld
			// direkt ins letzte Chat-Segment der KI-Antwort (z. B. ein
			// Codeblock) und mit EINEM tab zurueck zum Tippen (nicht 10x ↓).
			// Die Slash-Completion oben faengt tab ab, solange ein Befehl-
			// Praefix getippt ist.
			if m.chat.Focused() || !m.inputFocus {
				m.chat = m.chat.Unfocus()
				return m, m.setInputFocus(true)
			}
			nl := m.chat.FocusVisible()
			if !nl.Focused() {
				// Kein fokussierbares Segment im Chat: im Eingabe-Modus
				// bleiben — sonst hinge die Kette ohne Ziel fest.
				return m, nil
			}
			m.chat = nl
			return m, m.setInputFocus(false)
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
		case "c":
			// Copy-Chip im Eingabefeld-Footer: c kopiert die Auswahl (oder
			// den fokussierten Block) — das Copy-UI lebt nicht mehr in den
			// Block-Footern. Shift-Auswahl UND Zeichen-Cursor haben Vorrang:
			// c kopiert NUR sie.
			if (m.chat.selActive() || m.chat.charActive() || m.chat.residualSelShown()) && m.copyFn != nil {
				m.copiedFlash = true
				return m, m.copyFn(m.chat.selMarkdown())
			}
			if m.chat.markedCount() > 0 || m.chat.Focused() {
				return m.copySelection()
			}
		case "esc", "t", "+", "=", "-", "_", "r":
			// Block-Aktionen am fokussierten Block: t toggelt EIN-TASTIG
			// zwischen Bild und Quelltext (FR-38 — i ist entfallen), +/-
			// klappen den Think-Block. Im Zeichen-Cursor kollabiert esc
			// zunaechst die Auswahl. Ohne Fokus faellt alles durch und
			// tippt normal ins Eingabefeld.
			if m.chat.Focused() || m.chat.selActive() || m.chat.charActive() || m.chat.prevSel || m.chat.residualSelShown() {
				var cmd tea.Cmd
				var handled bool
				m.chat, cmd, handled = m.chat.Update(msg)
				if handled {
					// Hat esc den BLOCK-Fokus weggeraeumt, faellt die Kette
					// auf ihren Standard zurueck: das Eingabefeld. esc raeumt
					// aber in STUFEN ab: Laeuft noch der Zeichen-Cursor (esc
					// hat NUR die Auswahl aufs Caret kollabiert) oder wartet
					// unter ihm eine Block-Auswahl (esc kehrt in sie zurueck),
					// bleibt die Kette IM Chat — das Eingabefeld darf den
					// Nutzer nicht mitten im Modus uebernehmen (User-Report:
					// esc warf aus dem Schnipsel-Modus in die Eingabe).
					if msg.String() == "esc" && !m.chat.Focused() && !m.chat.charActive() && !m.chat.selActive() {
						return m, tea.Batch(cmd, m.setInputFocus(true))
					}
					return m, cmd
				}
			}
			if msg.String() == "esc" {
				// Esc raeumt der Reihe nach weg: Auswahl, Block-Fokus — und
				// kehrt auf den STANDARD zurueck: Eingabefeld selektiert.
				m.setInputFocus(true)
			}
		}
	case spinnerTickMsg:
		// Advance the running animation one frame; re-arm while the turn
		// is still in flight (FR-2) ODER der Rahmen-Fade noch auslaeuft:
		// Das Neon-Band blendet beim Start ein, nach dem Turn wieder aus —
		// der Ticker verstopft erst, wenn beides Ruhe hat.
		m.neonPhase += neonPhaseStep
		if m.running {
			m.spinner = (m.spinner + 1) % len(spinnerFrames)
			m.neonLevel = min(1, m.neonLevel+neonFadeStep)
			return m, nextSpinner()
		}
		if m.neonLevel > 0 {
			m.neonLevel = max(0, m.neonLevel-neonFadeStep)
			return m, nextSpinner()
		}
		return m, nil
	case charBlinkMsg:
		// Caret-Blink (FR-42): Die Phase toggelt NUR, solange der Zeichen-
		// Cursor laeuft — sonst endet die Kette hier (und der Arm-Marker
		// wird fuer den naechsten Eintritt frei). Eine laufende Auswahl
		// blinkt nicht mit: charDecorate blendet nur das alleinstehende
		// Caret ab, der Bereich steht als Farbe.
		if m.chat.charActive() {
			m.chat = m.chat.toggleCharBlink()
			return m, m.nextCharBlink()
		}
		m.charBlinkArmed = false
		return m, nil
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
		// FR-31: distinct callIDs zaehlen — Updates derselben Karte
		// (gleiche callID) erhoehen den Turn-Zaehler nicht.
		if m.turnTools == nil {
			m.turnTools = map[string]bool{}
		}
		m.turnTools[msg.callID] = true
		// Live-Output akkumulieren (live_output-Chunks kommen Stueckweise,
		// jedes Update traegt nur seinen Chunk); das finale Update liefert
		// das komplette rawOutput und raeumt den Akkumulator.
		if m.toolLive == nil {
			m.toolLive = map[string]string{}
		}
		if msg.live != "" {
			m.toolLive[msg.callID] += msg.live
		}
		live := m.toolLive[msg.callID]
		if msg.done {
			delete(m.toolLive, msg.callID)
			live = ""
		}
		// Tool-Karte im Chat (FR-2/FR-31): tool_call startet die laufende
		// Karte, tool_call_update aktualisiert dieselbe in Place (callID);
		// kind waehlt das Icon, failed den ✗-Zustand. Sie ist ein normales
		// Ketten-Segment — Shift-Auswahl inklusive.
		m.chat = m.chat.upsertToolBlock(ToolData{
			CallID: msg.callID, Title: msg.name, Kind: msg.kind, Text: msg.text,
			Command: msg.cmd, Stdout: msg.out, Stderr: msg.errOut,
			Exit: msg.exit, HasOutput: msg.hasOut, Live: live,
			Running: !msg.done, Failed: msg.failed,
		}, m.theme, m.copyFn)
		return m, nil
	case infoMsg:
		// The backend announced the real model/effort/provider (ACP
		// configOptions); empty parts keep the current labels (flag
		// overrides). Auch ERNEUT nach einem Config-Wechsel — der Provider-
		// Wechsel meldet die NEUE Modellliste.
		if msg.model != "" {
			m.modelLabel = msg.model
		}
		if msg.effort != "" {
			m.effort = msg.effort
		}
		if msg.provider != "" {
			m.provider = msg.provider
		}
		m.modelOptions = msg.modelOptions
		m.effortOptions = msg.effortOptions
		m.providerOptions = msg.providerOptions
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
		m.printedUpTo = 0 // Inline: von vorn drucken, sonst druckt /new nie wieder
		m.totalIn, m.totalOut = 0, 0
		m.lastIn, m.lastOut = 0, 0
		return m, nil
	case resumeListMsg:
		if msg.err != nil {
			m.chat = m.chat.Append(NewTextBlock("Resume-Liste fehlgeschlagen: "+msg.err.Error(), "1"))
			return m, m.flushPrinted()
		}
		if len(msg.rows) == 0 {
			m.chat = m.chat.Append(NewTextBlock("goose meldet keine früheren Sessions.", "1"))
			return m, m.flushPrinted()
		}
		m.resumeRows = msg.rows
		m.openPicker("resume")
		return m, nil
	case resumeStartMsg:
		if msg.err != nil {
			m.chat = m.chat.Append(NewTextBlock("Resume fehlgeschlagen: "+msg.err.Error(), "1"))
			return m, m.flushPrinted()
		}
		// Der Replay ist schon da (er lief WÄHREND des Loads) — Inline
		// druckt Reste nach (endet der Verlauf z.B. auf Tool-Karten).
		return m, m.flushPrinted()
	case replayMsg:
		// session/load-Replay (FR-44): der geladene Verlauf landet als
		// fertige Blöcke — Nutzerzeile bzw. final gerenderte Antwort,
		// inkl. Diagramm-Konvertierung wie in replyMsg.
		switch msg.kind {
		case replayUser:
			m.chat = m.chat.Append(NewUserBlock(msg.text))
			return m, m.flushPrinted()
		case replayAgent:
			nodes, err := render.Parse(msg.text)
			if err != nil {
				m.chat = m.chat.Append(NewTextBlock("Parse-Fehler: "+err.Error(), "1"))
				return m, m.flushPrinted()
			}
			m.chat = m.chat.Append(BuildWidgets(nodes, m.theme, m.copyFn)...)
			return m, tea.Batch(m.pendingDiagrams(), m.flushPrinted())
		}
		return m, nil
	case noticeMsg:
		m.chat = m.chat.Append(NewTextBlock(msg.text, "1"))
		return m, m.flushPrinted()
	case providerFavoritesMsg:
		// Eigene Provider, wie GOOSE sie klassifiziert
		// (_goose/unstable/providers/list: "Custom"/"Declarative") —
		// providerRows() sortiert sie hinter den aktiven (FR-45).
		m.providerFavorites = msg.ids
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
			// API-/RPC-Fehler als eigener Block mit Retry-Aktion (FR-33):
			// "r" auf dem fokussierten Block sendet den Prompt erneut.
			m.chat = m.chat.Append(NewErrorBlock(msg.err.Error(), msg.prompt, m.theme, m.copyFn))
		case msg.text == "":
		case true:
			nodes, err := render.Parse(msg.text)
			if err != nil {
				m.chat = m.chat.Append(NewTextBlock("Parse-Fehler: "+err.Error(), "1"))
			} else {
				m.chat = m.chat.Append(BuildWidgets(nodes, m.theme, m.copyFn)...)
			}
		}
		// Queue-Drainage (FR-46): Nur nach einem sauberen Turn-Ende geht der
		// naechste wartende Prompt raus — nach einem Fehler bleibt die
		// Warteschlange stehen (Retry entscheidet, 2x ctrl+c verwirft).
		if msg.err == nil && len(m.queued) > 0 && m.send != nil {
			next := m.queued[0]
			m.queued = m.queued[1:]
			m.chat = m.chat.Append(NewUserBlock(next))
			m.startTurn()
			send := m.send
			sendCmd := func() tea.Msg {
				text, err := send(next)
				return replyMsg{text: text, err: err, prompt: next}
			}
			return m, tea.Batch(sendCmd, m.flushPrinted(), nextSpinner())
		}
		// PlantUML blocks in the fresh reply convert in the background
		// (FR-6); die finalen Bloecke druckt der Flush append-only.
		return m, tea.Batch(m.pendingDiagrams(), m.flushPrinted())
	case retryPromptMsg:
		// Retry (FR-33): Fehler-Block weg, derselbe Prompt erneut — nur im
		// Ruhestand; laeuft ein Turn (oder fehlt das Backend), passiert nichts.
		if m.running || m.send == nil {
			return m, nil
		}
		m.chat = m.chat.removeBlock(msg.blockID)
		m.startTurn()
		send := m.send
		p := msg.prompt
		sendCmd := func() tea.Msg {
			text, err := send(p)
			return replyMsg{text: text, err: err, prompt: p}
		}
		return m, tea.Batch(sendCmd, m.flushPrinted(), nextSpinner())
	}
	// Eingabe-Modus: nur die SELEKTIERTE Textarea bekommt Tasten — im
	// Antwort-Modus faellt hier nichts mehr durch, Getippe kann das
	// Eingabefeld nicht erreichen (nicht selektiert = nicht interaktiv).
	var cmd tea.Cmd
	if m.inputFocus {
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != prevValue {
			// Typing resets the slash-command state (FR-20).
			m.cmdDismissed = false
			m.cmdCursor = 0
		}
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

// workingLine is the slim "AI is working" row rendered DIRECTLY ABOVE the
// input box (not under the title): a rotating spinner while the turn runs,
// plus the current tool label when one is active. It collapses to nothing
// when idle, so a finished turn leaves no blank row and the input box keeps
// its place. Keeping it to exactly one row preserves the chat hit-testing.
func (m Model) workingLine() string {
	if !m.running && m.toolStatus == "" {
		return ""
	}
	var line string
	if m.running {
		line = m.spinnerGlyph() + " "
	}
	if m.toolStatus != "" {
		line += "🔧 " + m.toolStatus
	} else if m.running {
		line += "antwort läuft …"
	}
	if m.running && len(m.queued) > 0 {
		line += fmt.Sprintf(" · ⏳ %d wartend (2× ctrl+c verwirft)", len(m.queued))
	}
	if m.width > 0 {
		if w := render.WrapDisplay(line, m.width); len(w) > 0 {
			line = w[0]
		}
	}
	styled := lipgloss.NewStyle().Faint(true).Render(line)
	// Mittig in der Zeile: Der Status gehoert zur Box DARUNTER — zentriert
	// liest er sich als deren Ueberschrift, nicht als Fliesstext-Rest. Der
	// Farbstil zaehlt nicht in die Breite (ANSI wird ignoriert).
	if m.width > 0 {
		if pad := (m.width - lipgloss.Width(styled)) / 2; pad > 0 {
			return strings.Repeat(" ", pad) + styled
		}
	}
	return styled
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
		if wl := m.workingLine(); wl != "" {
			live.WriteString(wl)
			live.WriteString("\n")
		}
		live.WriteString(prompt)
		live.WriteString("\n")
		live.WriteString(m.footerView())
		out := live.String()
		perfBytes(len(out))
		return out
	}
	// The "AI is working" row hangs on to the bottom: directly above the box
	// it belongs to, never at the top next to the title.
	bottom := prompt
	if wl := m.workingLine(); wl != "" {
		bottom = wl + "\n" + bottom
	}
	if m.height <= 0 {
		// No terminal size known yet (tests, first paint): render as-is
		// instead of squeezing the frame into an unknown height, which cut
		// the header and the newest chat rows.
		return m.fillScreen(top.String() + "\n\n" + bottom + "\n" + m.footerView())
	}
	bottomRows := len(strings.Split(bottom, "\n"))
	avail := m.height - bottomRows - 1 // the footer row
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
	return m.fillScreen(strings.Join(append(append(topLines, bottom), m.footerView()), "\n"))
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
	// Shift+Enter (ghostty/kitty): Das Kitty-Tastaturprotokoll liefert die
	// Taste als \x1b[13;2u — der uebersetzende Reader macht daraus den
	// shift+tab-Trigger (beide Opening-Cases matchen gemeinsam), Esc
	// (\x1b[27u) faellt auf \x1b zurueck. Ohne Kitty-Terminal bleibt es
	// beim shift+tab-Fallback (kbd.go).
	if kbd := newKittyKeyboard(os.Stdin, os.Stdout); kbd != nil {
		opts = append(opts, tea.WithInput(kbd))
		defer kbd.Restore()
		// SICHTBARE Bestaetigung: Der Placeholder meldet, dass shift+enter
		// wirklich scharf ist (Terminal hat das Protokoll angenommen) —
		// ohne Raetseln, ob "nichts passiert" Tastatur oder Feature ist.
		m.input.Placeholder = m.input.Placeholder + " · shift+enter cursor"
	}
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
