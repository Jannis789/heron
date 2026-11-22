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
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"runtime/pprof"
	"sync"
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
	// streamID: Block-ID der wachsenden Stream-Vorschau im Chat (0 = keine).
	// Der Block wird pro Chunk in-place ersetzt; endTurn nimmt ihn heraus
	// und die fertig gerenderte Antwort rueckt nach (FR-2).
	streamID int

	// inputHistory: zuletzt eingegebene Prompts (aeltester → neuester);
	// histIdx -1 = Live-Draft, sonst Index in inputHistory. ↑/↓ im
	// Eingabefeld laufen die Historie ab, ↓ am neuesten Eintrag stellt
	// den noch nicht abgeschickten Draft wieder her.
	inputHistory []string
	histIdx      int
	histDraft    string

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
	// activity ist der Prozess-Baum des Turns (Activity-Tree-Spec):
	// Thinking (N) · Tool Calls (N) · Working on. Sie ersetzt die
	// ehemaligen Think-/Tool-Karten und die Statuszeile — Agent-Text ist
	// KEIN Prozess und streamt als wachsender Block in den Chat.
	activity ActivityTree
	// detail: offene Detail-Alertbox — ein Element (Tool-Call ODER
	// Thought-Sektion) als schwebendes Fenster ueber der TUI (nil = zu).
	detail *Detail
	// detailScroll: Zeilen-Offset im Box-Inhalt (pgup/pgdown); das Oeffnen
	// und Schliessen laesst die Baum-Auswahl unberuehrt.
	detailScroll int
	spinner      int // frame index of the running animation
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
	pickerKind   string   // "model" | "effort" | "resume" | "provider" | "skills" | "mcp"
	pickerOpen   bool
	pickerCursor int
	pickerScroll int
	pickerQuery  string // /model filters the list as you type (FR-20)

	// Skills (/skills): Quelle ist `goose skills list` (kein ACP-Pendant),
	// deshalb ein Callback. skillSel ist die Mehrfachauswahl: Leertaste
	// hakt ab, enter übernimmt ALLE Haken in den Prompt.
	skillsList func() ([]SkillRow, error)
	skillRows  []SkillRow
	skillSel   map[string]bool
	// MCP-Extensions (/mcp): Liste aus gooses config.yaml, Umschalten
	// wirkt sofort in der Session UND persistent in der config.yaml.
	extList   func() ([]ExtRow, error)
	extToggle func(key string, on bool) error
	extRows   []ExtRow

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
	// sie gehen automatisch raus, sobald der Turn endet (FR-46); ctrl+c
	// drengt den ersten wartenden Prompt sofort nach (Abbruch + Versand),
	// der Doppeldruck beendet heron. lastCtrlC erkennt den Doppeldruck,
	// flushQueued armt den Sofort-Versand fuer den Abbruch-Reply.
	queued      []string
	lastCtrlC   time.Time
	flushQueued bool

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
	return Model{input: ta, chat: NewBlockList().SetCopy(copyFn).SetTheme(th), activity: NewActivityTree(th), send: send, theme: th, copyFn: copyFn, inline: inlineEnabled(), inputFocus: true, charBlinkInterval: caretBlinkInterval, histIdx: -1}
}

// recordHistory merkt sich einen abgeschickten (oder queued) Prompt und
// setzt die Historien-Navigation auf den Live-Draft zurueck.
func (m *Model) recordHistory(prompt string) {
	if prompt == "" {
		return
	}
	m.inputHistory = append(m.inputHistory, prompt)
	m.histIdx = -1
	m.histDraft = ""
}

// recallHistory laeuft die Eingabe-Historie ab: dir<0 = ↑ (aelter),
// dir>0 = ↓ (neuer); ↓ am neuesten Eintrag stellt den Live-Draft wieder her.
func (m *Model) recallHistory(dir int) {
	if len(m.inputHistory) == 0 {
		return
	}
	if dir < 0 {
		if m.histIdx == -1 {
			m.histDraft = m.input.Value()
			m.histIdx = len(m.inputHistory) - 1
		} else if m.histIdx > 0 {
			m.histIdx--
		} else {
			return // aeltester Eintrag erreicht
		}
	} else {
		if m.histIdx == -1 {
			return // schon im Live-Draft
		}
		if m.histIdx < len(m.inputHistory)-1 {
			m.histIdx++
		} else {
			m.histIdx = -1
			m.input.SetValue(m.histDraft)
			m.input.CursorEnd()
			m.syncInputSize()
			return
		}
	}
	m.input.SetValue(m.inputHistory[m.histIdx])
	m.input.CursorEnd()
	m.syncInputSize()
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

// configResultMsg reports a finished model change. prevModel/prevProvider
// halten den Stand VOR dem Wechsel — die Silent-no-op-Erkennung vergleicht
// damit den echten Nachher-Stand (FR-45-Nachfass).
type configResultMsg struct {
	kind         string
	value        string
	err          error
	prevModel    string
	prevProvider string
}

// pickerOptions returns the announced options for the open picker kind —
// the rows of the list.
// pickerOptions returns the options of the open picker: effort levels or
// models — models filtered by the search query while one is typed (FR-20).
// pickerMatch filtert Picker-Zeilen nach der Picker-Suche (FR-20): ohne
// Suche unverändert, sonst Groß-/Kleinschreibung ignorieren — ein Treffer
// im von hay gebauten Suchfeld genügt. Ein generischer Helfer statt fünf
// typspezifischer Duplikate (Modelle, Sessions, Provider, Skills, MCP).
func pickerMatch[T any](rows []T, query string, hay func(T) string) []T {
	if query == "" {
		return rows
	}
	q := strings.ToLower(query)
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		if strings.Contains(strings.ToLower(hay(r)), q) {
			out = append(out, r)
		}
	}
	return out
}

// pickerRowAt liefert die Zeile unter dem Picker-Cursor, falls sie existiert.
func pickerRowAt[T any](rows []T, cursor int) (T, bool) {
	if cursor < 0 || cursor >= len(rows) {
		var zero T
		return zero, false
	}
	return rows[cursor], true
}

// favSet baut die Mengen-Sicht der Provider-Favoriten.
func favSet(values []string) map[string]bool {
	fav := make(map[string]bool, len(values))
	for _, v := range values {
		fav[v] = true
	}
	return fav
}

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
	if m.pickerKind == "skills" {
		rows := m.filteredSkillRows()
		labels := make([]string, len(rows))
		for i, r := range rows {
			labels[i] = r.Name
		}
		return labels
	}
	if m.pickerKind == "mcp" {
		rows := m.filteredExtRows()
		labels := make([]string, len(rows))
		for i, r := range rows {
			labels[i] = r.Label
		}
		return labels
	}
	return pickerMatch(m.modelOptions, m.pickerQuery, func(o string) string { return o })
}

// filteredResumeRows wendet die Picker-Suche auf die Session-Zeilen an —
// dieselbe Logik wie die Modell-Suche (FR-20), über Label UND Detail.
func (m Model) filteredResumeRows() []SessionRow {
	return pickerMatch(m.resumeRows, m.pickerQuery, func(r SessionRow) string { return r.Label + " " + r.Detail })
}

// providerRows sortiert die Provider-Liste fuer den Picker in drei Zonen,
// jede in der Reihenfolge, die goose annonciert hat: der AKTIVE Provider
// steht zuerst (ein Druck auf enter haelt ihn), dann die in goose
// EINGERICHTETEN Provider (Keys des providers:-Blocks, die goose via
// config/read selbst nennt), dann der Rest.
func (m Model) providerRows() []OptionRow {
	rows := make([]OptionRow, len(m.providerOptions))
	copy(rows, m.providerOptions)
	fav := favSet(m.providerFavorites)
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
		return pickerMatch(rows, m.pickerQuery, func(r OptionRow) string { return r.Name + " " + r.Value })
	}
	if len(m.providerFavorites) == 0 {
		return rows
	}
	fav := favSet(m.providerFavorites)
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
	if kind == "skills" || kind == "mcp" {
		// Eigene Quellen (goose skills list / config.yaml): kein
		// "aktiver" Eintrag, also ohne Cursor-Vorpositionierung.
		m.pickerKind = kind
		if len(m.pickerOptions()) == 0 {
			m.pickerKind = ""
			return
		}
		m.pickerOpen = true
		m.pickerCursor, m.pickerScroll, m.pickerQuery = 0, 0, ""
		m.clampPickerScroll()
		return
	}
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
	case " ":
		// /skills: Leertaste hakt die Zeile ab (Mehrfachauswahl). /mcp:
		// Leertaste schaltet den Server um. Alle anderen Picker nutzen
		// die Leertaste als Suchzeichen.
		if m.pickerKind == "skills" {
			m.toggleSkill()
			return m, nil
		}
		if m.pickerKind == "mcp" {
			return m.mcpKey()
		}
		m.pickerQuery += " "
		return m, nil
	case "enter":
		// /skills: enter übernimmt ALLE Häkchen (ohne Häkchen die Zeile
		// unter dem Cursor) als Prompt-Prefix. /mcp: enter schaltet wie
		// die Leertaste um — der Picker bleibt offen.
		if m.pickerKind == "skills" {
			return m.applySkills()
		}
		if m.pickerKind == "mcp" {
			return m.mcpKey()
		}
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
			return configResultMsg{kind: kind, value: value, err: fn(id, value),
				prevModel: m.modelLabel, prevProvider: m.provider}
		}
	case "esc":
		m.closePicker()
		return m, nil
	}
	// The model and resume lists filter as you type; the effort list is
	// short enough to browse without a query (FR-20/FR-44). A changed
	// query restarts at the top.
	if m.pickerKind == "model" || m.pickerKind == "resume" || m.pickerKind == "provider" ||
		m.pickerKind == "skills" || m.pickerKind == "mcp" {
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
	hints := ""
	if m.running {
		hints = "ctrl+c abort"
		if len(m.queued) > 0 {
			hints = "ctrl+c abort · warteschlange geht sofort raus · 2× quit"
		}
	}
	// Zentrale Aktions-/Hint-Leiste (ganz unten): shift+enter SPAWNT den
	// Zeichen-Cursor — das steht IMMER da, nicht erst nach einer Auswahl
	// (der Cursor ist der Haupteingang, blockweise Auswahl ist der Spezial-
	// fall per shift+up/down). Die Spezial-Modi (Pan, Bild, Cursor aktiv)
	// verdraengen ihn nur, solange sie gelten.
	var h string
	switch {
	case m.activity.Focused():
		// Activity-Baum (Auswahlmodus): ↑/↓ laufen die Knoten ab, enter
		// klappt den Knoten unter dem Cursor auf und zu, esc kehrt zur
		// Eingabe zurueck.
		h = "↑↓ knoten · ⏎ auf-/zuklappen · bild scrollen · esc fertig"
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
		// EXKLUSIVITAET (User-Report "Eingabefeld UND Cursor gleichzeitig"):
		// Eingabefokus und Zeichen-Cursor/Rest-Auswahl schliessen sich aus —
		// der Rueckweg ins Feld raeumt sie mit esc-Semantik ab (Anker und
		// Caret-Rest weg, kein Residual beim Tippen).
		m.activity = m.activity.Blur()
		m.chat = m.chat.clearCharSelEsc()
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
	if m.streamID > 0 {
		// Vorsichtshalber: endTurn hat die Vorschau normalerweise schon
		// weggeraeumt — ein Turn, der sie hinterliess, putzt hier nach.
		m.chat = m.chat.removeBlock(m.streamID)
		m.streamID = 0
	}
	m.spinner = 0
	// Frischer Turn, frischer Prozess-Baum (Activity-Tree-Spec): Der
	// vorherige Stand weicht der neuen Aktivitaet; fertige Bloecke
	// (inklusive der gestreamten Antwort) bleiben im Chat.
	// Idle armiert die Leerlauf-Zeile (User-Report): Bevor das erste
	// Event eintrifft, zeigt Working on einen Ladeindikator statt gar
	// nicht zu erscheinen.
	m.activity = NewActivityTree(m.theme).Idle(time.Now())
	// Der Baum aendert seine Zeilenzahl (leer <-> sichtbar): Der Chat-
	// Viewport muss sofort Platz abgeben — ohne relayout behaelt er die
	// alte Hoehe, View() laeuft ueber und schneidet Header und erste
	// Chatzeilen oben ab (User-Report: "TUI verschiebt sich nach oben").
	m.relayout()
}

// endTurn clears the live state once the turn settles; the activity tree
// bleibt als Zusammenfassung des Turns stehen, bis der naechste beginnt.
// Die Stream-Vorschau weicht hier: replyMsg haengt danach die fertig
// gerenderten Widgets an (FR-2).
func (m *Model) endTurn() {
	m.running = false
	m.cancelling = false
	m.streaming = ""
	if m.streamID > 0 {
		// Die Vorschau trug die zuletzt vergebene Block-Nummer — sie ist
		// mit dem Block weg und wird an den ersten finalen Block wieder
		// vergeben (FR-3 bleibt lueckenlos 1-basiert).
		if m.streamID == m.chat.nextID {
			m.chat.nextID--
		}
		m.chat = m.chat.removeBlock(m.streamID)
		m.streamID = 0
	}
	m.activity = m.activity.Settle()
	// Settle schrumpft den Baum (Working-on faellt weg): Der Chat bekommt
	// die Zeilen zurueck — ohne relayout bliebe er zu klein.
	m.relayout()
}

// dispatchNextQueued (FR-46): nimmt den ERSTEN wartenden Prompt und startet
// ihn als naechsten Turn — User-Block, Versand parallel, Spinner bis zur
// Antwort (gleiche Kette wie der Enter-Versand). Nur im Ruhestand rufen
// (replyMsg-Drainage bzw. ctrl+c im Leerlauf), nie neben einem laufenden
// Turn.
func (m *Model) dispatchNextQueued() tea.Cmd {
	next := m.queued[0]
	m.queued = m.queued[1:]
	m.chat = m.chat.Append(NewUserBlock(next))
	m.startTurn()
	send := m.send
	sendCmd := func() tea.Msg {
		text, err := send(next)
		return replyMsg{text: text, err: err, prompt: next}
	}
	return tea.Batch(sendCmd, m.flushPrinted(), nextSpinner())
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
		if pending[id] || id == m.streamID {
			break // Stream-Vorschau mutiert noch — druckt erst nach dem Settle
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

// activityBudget begrenzt den Activity-Baum auf (hoechstens) die Haelfte
// der Terminalhoehe (§ 9: die Historie beansprucht nie den ganzen Bereich;
// Chat, Eingabe und Footer behalten ihren Platz).
func (m Model) activityBudget() int {
	if m.height <= 0 {
		return 12
	}
	b := m.height / 2
	if b < 3 {
		b = 3
	}
	return b
}

// activityRows liefert die Zeilenzahl des gerenderten Activity-Baums —
// eine Quelle fuer View, syncInputSize und die Maus-Geometrie, damit
// Layout und Klickziele nicht driften.
func (m Model) activityRows() int {
	return len(m.activityRowsView())
}

// activityVisible: der Baum zeigt sich mit dem ersten Inhalt — ein
// laufender Turn ohne jedes Event bleibt unsichtbar (keine Null-Zahlen,
// § 10); ein abgeschlossener Turn hinterlaesst seine Zusammenfassung, der
// naechste Start setzt zurueck.
func (m Model) activityVisible() bool {
	return !m.activity.Empty()
}

// wrapDetailLines bricht Detail-Zeilen ANSI-frei auf die Innenbreite um;
// Leerzeilen bleiben als Gliederung erhalten.
func wrapDetailLines(src []string, width int) []string {
	var out []string
	for _, ln := range src {
		if strings.TrimSpace(ln) == "" {
			out = append(out, "")
			continue
		}
		ws := render.WrapWords(ln, width)
		if len(ws) == 0 {
			ws = []string{""}
		}
		out = append(out, ws...)
	}
	return out
}

// detailBox baut die Zeilen der schwebenden Detail-Alertbox: runde
// Rahmen, Kopf (Kategorie/Titel/Status), Metazeile, Volltext und
// Fusszeile. Groesse: 80 % des Terminals, gedeckelt auf ~100x30. Zu
// langer Inhalt wird gefenstert (pgup/pgdown) und als gekuerzt markiert.
func (m Model) detailBox(d Detail) []string {
	termW, termH := m.width, m.height
	if termW <= 0 {
		termW = 80
	}
	if termH <= 0 {
		termH = 24
	}
	boxW := termW * 8 / 10
	if boxW > 100 {
		boxW = 100
	}
	if boxW < 20 {
		boxW = 20
	}
	if boxW > termW {
		boxW = termW
	}
	// Das Fenster ist SO GROSS WIE SEIN INHALT (hoechstens 80 % / 30
	// Zeilen): eine kurze Ausgabe ergibt eine kompakte Alertbox, statt
	// 20 leere Zeilen unter den Text zu haengen.
	maxInnerH := termH * 8 / 10
	if maxInnerH > 30 {
		maxInnerH = 30
	}
	maxInnerH -= 2 // obere/untere Rahmenlinie
	if maxInnerH < 2 {
		maxInnerH = 2
	}
	innerW := boxW - 4
	if innerW < 8 {
		innerW = 8
	}

	headSrc := []string{d.Kind + " · " + d.Title + "  [" + d.Status + "]"}
	if d.Duration > 0 {
		headSrc = append(headSrc, "Dauer "+d.Duration.Round(time.Millisecond).String())
	}
	if len(d.Meta) > 0 {
		headSrc = append(headSrc, strings.Join(d.Meta, " · "))
	}
	header := wrapDetailLines(headSrc, innerW)
	body := wrapDetailLines(strings.Split(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\n"), innerW)
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}

	// 2 Zeilen: Leerzeile + Fusszeile. Reicht der Platz fuer den ganzen
	// Inhalt, zeigt die Box alles; sonst fenstert pgup/pgdn.
	avail := maxInnerH - len(header) - 2
	if avail < 1 {
		avail = 1
	}
	if avail > len(body) {
		avail = len(body)
	}
	innerH := len(header) + avail + 2
	if innerH > maxInnerH {
		innerH = maxInnerH
	}
	maxScroll := len(body) - avail
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.detailScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + avail
	if end > len(body) {
		end = len(body)
	}
	footer := "enter/esc schliesst · pgup/pgdn scrollt"
	if maxScroll > 0 {
		footer = fmt.Sprintf("%d–%d/%d · pgup/pgdn · enter/esc schliesst", scroll+1, end, len(body))
	}
	content := append([]string{}, header...)
	content = append(content, body[scroll:end]...)
	content = append(content, "", footer)
	if len(content) > innerH {
		content = content[:innerH]
	}
	for len(content) < innerH {
		content = append(content, "")
	}
	for i, ln := range content {
		if w := lipgloss.Width(ln); w < innerW {
			content[i] = ln + strings.Repeat(" ", innerW-w)
		}
	}
	// Rahmen: Vordergrund Valida (Theme-Action), HINTERGRUND ein dunkler
	// Violett-Ton aus derselben Farbe — nur die Rahmenzeichen (User-Wunsch
	// 22.11.: "nur für den Alert die Rahmenzeichen bg einfärben"), der
	// Inhalt behaelt den Terminal-Grund. lipgloss faerbt BorderBackground
	// ausschliesslich auf die Rahmenzellen, nicht auf Padding/Inhalt.
	valida := m.theme.ValidaColor()
	frameBG := render.LerpHex("#000000", valida, 0.35)
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(valida)).
		BorderBackground(lipgloss.Color(frameBG)).Padding(0, 1)
	return strings.Split(st.Render(strings.Join(content, "\n")), "\n")
}

// overlayDetail komponiert die Detailbox MITTIG ueber die Basis-TUI: links
// und rechts der Box bleibt die normale Ansicht sichtbar (Header, Chat,
// Eingabe, Footer). Kein Vollbild-Ersatz.
func (m Model) overlayDetail(base string, d Detail) string {
	box := m.detailBox(d)
	if m.width <= 0 || m.height <= 0 || len(box) == 0 {
		return strings.Join(box, "\n")
	}
	lines := strings.Split(base, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	boxW := 0
	for _, ln := range box {
		if w := lipgloss.Width(ln); w > boxW {
			boxW = w
		}
	}
	x := (m.width - boxW) / 2
	if x < 0 {
		x = 0
	}
	y := (m.height - len(box)) / 2
	if y < 0 {
		y = 0
	}
	for i, bl := range box {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		left := ansi.Truncate(lines[row], x, "")
		right := ansi.TruncateLeft(lines[row], x+boxW, "")
		lines[row] = left + bl + right
	}
	return strings.Join(lines, "\n")
}

// activityView rendert den Activity-Baum als Block ueber dem Eingabefeld
// (Nachfolger der einzeiligen Working-Line). Leer, wenn nichts zu zeigen
// ist — dann bleibt die Zeile wie bisher ganz weg.
func (m Model) activityView() string {
	rows := m.activityRowsView()
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n")
}

// activityRowsView rendert die Zeilen (Maus-Geometrie teilt activityRows).
func (m Model) activityRowsView() []string {
	if !m.activityVisible() {
		return nil
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	return m.activity.View(w, m.activityBudget())
}

// activityRect liefert die Bildschirm-Geometrie des Activity-Bereichs:
// yTop ist die Zeile der Thinking-Kopfzeile, rows die Zeilenzahl. Die
// Kopfzeilen stehen IMMER an den ersten beiden Positionen (Klickziele
// fuer das Auf-/Zuklappen, § 5).
func (m Model) activityRect() (yTop, rows int) {
	n := m.activityRows()
	if n == 0 {
		return 0, 0
	}
	inputRows := len(strings.Split(m.inputView(), "\n"))
	yTop = m.height - 1 - inputRows - n // -1: Footer-Zeile
	return yTop, n
}

// View rendert die TUI. Ist die Detail-Alertbox offen, wird sie als
// schwebendes Fenster MITTIG ueber die normale Ansicht komponiert — die
// Basis-TUI (Header, Chat, Eingabe, Footer) bleibt ringsum sichtbar.
func (m Model) View() string {
	base := m.baseView()
	if m.detail == nil {
		return base
	}
	return m.overlayDetail(base, *m.detail)
}

// baseView: die normale TUI ohne Detail-Overlay.
func (m Model) baseView() string {
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
		// haelt NUR die feste Live-Region — Header + Activity + Eingabe +
		// Footer. Sie passt garantiert ins Fenster, also kann der Renderer
		// nichts verwerfen, und die Eingabe bleibt bedienbar (gemessen:
		// Input ohne Renderer ist tot — Nur-Druck scheidet als Endzustand
		// aus).
		var live strings.Builder
		live.WriteString(lipgloss.NewStyle().Bold(true).Render("heron · goose acp"))
		live.WriteString("\n")
		if av := m.activityView(); av != "" {
			live.WriteString(av)
			live.WriteString("\n")
		}
		live.WriteString(prompt)
		live.WriteString("\n")
		live.WriteString(m.footerView())
		out := live.String()
		perfBytes(len(out))
		return out
	}
	// Der Activity-Baum haengt unten: direkt ueber der Box, zu der er
	// gehoert, nie oben neben dem Titel.
	bottom := prompt
	if av := m.activityView(); av != "" {
		bottom = av + "\n" + bottom
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
		// Defensive bei Layout-Drift (z.B. Baum ohne relayout gewachsen):
		// Nur der CHAT kuerzt von oben — die TITELZEILE bleibt IMMER
		// Zeile 1. Der alte Zuschnitt topLines[len-avail:] frass den
		// Header und die ersten Chatzeilen: Das ganze TUI schien sich
		// "nach oben zu verschieben" (User-Report 19.11.).
		head := topLines[0]
		chat := topLines[1:]
		if keep := avail - 1; len(chat) > keep {
			chat = chat[len(chat)-keep:]
		}
		topLines = append([]string{head}, chat...)
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

// mouseResidueFrag matcht SGR-Maus-Reports, deren ESC-Byte der Parser
// verloren hat — der Rest ("[<35;42;55M") landet sonst als Text im
// Prompt, klassisch beim Scrollen/Bewegen, wenn Maus-Reporting noch
// aus einem frueheren Lauf an ist (Quellen-Reset: ProbeTerminal).
var mouseResidueFrag = regexp.MustCompile(`^(alt+\+)?\[?<\d+;\d+;\d+[MHm]`)

// kittyResponseFilter verwirft Tasten, die wie Kitty-Grafik-Antworten
// aussehen (z. B. ESC_Gi=1,p=1;OK ESC): q=2 unterdrueckt die Antworten der
// Bildbefehle bereits an der Quelle, aber die PROBE (a=q) WILL Antworten —
// treffen die verspaetet bei bubbletea ein, zerschneidet der Parser sie in
// Runen-Muell (sichtbare Fragmente "i=31", "[<65;..."). Der Filter dropt
// beide Formen (FR-27).
//
// Der Filter ist ZUSTANDBEHAFtet (bracketBurst): SGR-Maus-Muell trifft
// FRAGMENTIERT ein — jede Message 1-2 Runen ("[", "[[") — und verkettet
// sich erst IM Eingabefeld zum "[[[[[[...-Stapel (User-Report beim
// Scrollen). Ein statischer Schwellwert greift da nie. Die Erkennung ist
// eine RATE: bracketBurst zaehlt reine '['-Messages im 600ms-Fenster —
// ab der 3. gilt Schwall (Scrollen liefert Reports im 5ms-Takt, Dutzende
// je Fenster; menschliches Tippen schafft keine 3 NUR-Klammern-Messages
// in 600ms, weil zwischen echten Klammern Text kommt — "[[x]]" liefert
// hoechstens 2, bevor nicht-Klammer-Runen den Stapel brechen).
var bracketMu sync.Mutex

// bracketTimes sind die Zeitstempel der letzten reinen Klammer-Messages
// (Ring via Filterung im bracketBurst).
var bracketTimes []time.Time

// bracketWindow und bracketBurstLimit definieren den Schwall: >=3 reine
// Klammer-Messages innerhalb von 600ms.
const (
	bracketWindow     = 600 * time.Millisecond
	bracketBurstLimit = 3
)

// resetBracketTrap setzt die burst-Historie zurueck (Test-Isolation).
func resetBracketTrap() {
	bracketMu.Lock()
	bracketTimes = nil
	bracketMu.Unlock()
}

// bracketBurst meldet, ob die Message einen Schwall fortsetzt, und merkt
// ihren Zeitstempel.
func bracketBurst(now time.Time) bool {
	bracketMu.Lock()
	defer bracketMu.Unlock()
	cutoff := now.Add(-bracketWindow)
	kept := bracketTimes[:0]
	for _, t := range bracketTimes {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	bracketTimes = append(kept, now)
	return len(bracketTimes) >= bracketBurstLimit
}

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
	if len(k.Runes) > 1 && mouseResidueFrag.MatchString(s) {
		return nil
	}
	// Reine Klammer-Laeufe (User-Report "[[[[[[ beim Scrollen"): SGR-Maus-
	// Reports, deren ESC UND Parameter verloren gingen — der Rest ist ein
	// Rune-Stapel aus '['. Statisch ab 3 Klammern je Message (Merge-Muell,
	// Markdown-Sicherheit: echte Eingabe hat zwischen Klammern anderen
	// Text), dynamisch per Schwall-Rate fuer die fragmentierten 1-2er.
	allBrackets := len(k.Runes) > 0
	for _, r := range k.Runes {
		if r != '[' {
			allBrackets = false
			break
		}
	}
	if allBrackets {
		if len(k.Runes) >= 3 || bracketBurst(time.Now()) {
			return nil
		}
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
