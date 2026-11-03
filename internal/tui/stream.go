package tui

import (
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// StreamBlockID addresses the transient block that shows an answer while it
// is still streaming. It lives far above the ordinary block numbers so a turn
// never burns a user-visible number (and alt+<n> cannot reach it).
const StreamBlockID = 999999

// Stream is the handle a backend uses to push live turn updates into the UI
// (FR-2). The tea runtime is attached once the program starts; publishing
// before that (or after it exited) is a no-op, never a panic.
type Stream struct {
	prog    atomic.Pointer[tea.Program]
	mu      sync.Mutex
	pending []tea.Msg // published before attach; flushed by attach
}

// attach binds the running program; called by RunModel. Messages published
// before this point are flushed now, so early senders (session prewarm)
// cannot lose their message.
func (s *Stream) attach(p *tea.Program) {
	if s == nil {
		return
	}
	s.prog.Store(p)
	if p == nil {
		return
	}
	for _, msg := range s.takePending() {
		p.Send(msg)
	}
}

// takePending drains the pre-attach buffer.
func (s *Stream) takePending() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending
	s.pending = nil
	return p
}

// publish injects a message into the runtime. Safe from any goroutine: the
// ACP reader and the prompt cmd both call into this. Before the program is
// attached the message is buffered.
func (s *Stream) publish(msg tea.Msg) {
	if s == nil {
		return
	}
	if p := s.prog.Load(); p != nil {
		p.Send(msg)
		return
	}
	s.mu.Lock()
	s.pending = append(s.pending, msg)
	s.mu.Unlock()
}

// Chunk publishes a piece of the streamed answer (FR-2).
func (s *Stream) Chunk(text string) {
	if text != "" {
		s.publish(chunkMsg{text: text})
	}
}

// Thought publishes a piece of the thinking stream (FR-2/FR-30).
func (s *Stream) Thought(text string) {
	if text != "" {
		s.publish(thoughtMsg{text: text})
	}
}

// ToolCall veroeffentlicht eine Tool-Aktivitaet (FR-2/FR-31): Die Karte im
// Chat (callID identifiziert sie fuer In-place-Updates) UND die Statuszeile
// (done raeumt sie). label ist der lesbare Titel (ToolEvent.Label), kind die
// ACP-Kategorie (execute/fetch/… — sie waehlt das Icon der Karte), text der
// bisher gestreamte Inhalt; failed markiert das Call als gescheitert
// (Kopf zeigt ✗ statt ✓). Legacy-/Demo-Pfad: ohne Struktur (rawInput/
// rawOutput) — dafuer gibt es Stream.Tool.
func (s *Stream) ToolCall(callID, label, kind, text string, done, failed bool) {
	s.Tool(ToolUpdate{CallID: callID, Label: label, Kind: kind, Text: text,
		Done: done, Failed: failed})
}

// Tool veroeffentlicht eine Tool-Aktivitaet MIT Struktur (echte goose-
// Frames): Kommando, aufgeteilte Ausgabe, Exit-Code und Live-Chunk —
// die Karte rendert daraus $-Zeile, Output, ⚠-Stderr und exit-N.
func (s *Stream) Tool(u ToolUpdate) {
	s.publish(toolMsg{callID: u.CallID, name: u.Label, kind: u.Kind, text: u.Text,
		done: u.Done, failed: u.Failed, cmd: u.Command, out: u.Stdout,
		errOut: u.Stderr, exit: u.Exit, hasOut: u.HasOutput, live: u.Live})
}

// Usage publishes the token accounting of a finished turn (FR-23).
func (s *Stream) Usage(in, out int) {
	s.publish(usageMsg{in: in, out: out})
}

// usageMsg carries the token usage of the finished turn.
type usageMsg struct{ in, out int }

// Info publishes the model/effort/provider the backend announced (ACP
// configOptions) once the session exists, plus the selectable option lists
// — auch ERNEUT nach einem set_config_option (Provider-Wechsel aendert die
// Modellliste).
func (s *Stream) Info(model, effort, provider string, modelOptions, effortOptions []string, providerOptions []OptionRow) {
	s.publish(infoMsg{model: model, effort: effort, provider: provider,
		modelOptions: modelOptions, effortOptions: effortOptions,
		providerOptions: providerOptions})
}

// ProviderFavorites published die IDs der in goose EINGERICHTETEN
// Provider (Quelle: goose _goose/unstable/config/read, key "providers"
// — der providers:-Block nennt genau die eingerichteten) — der
// /provider-Picker zeigt nur sie plus den aktiven Provider.
func (s *Stream) ProviderFavorites(ids []string) {
	s.publish(providerFavoritesMsg{ids: ids})
}

// providerFavoritesMsg traegt die eigenen Provider-IDs in das Modell.
type providerFavoritesMsg struct{ ids []string }

// infoMsg carries the session's real model/effort labels and the selectable
// option lists behind them.
type infoMsg struct {
	model           string
	effort          string
	provider        string
	modelOptions    []string
	effortOptions   []string
	providerOptions []OptionRow
}

// ReplayUser publishes one replayed user message (session/load, FR-44): the
// loaded history arrives as whole blocks, never as a live stream.
func (s *Stream) ReplayUser(text string) {
	if text != "" {
		s.publish(replayMsg{kind: replayUser, text: text})
	}
}

// ReplayAgent publishes one replayed agent answer (session/load, FR-44):
// whole text, rendered as final blocks like a settled reply.
func (s *Stream) ReplayAgent(text string) {
	if text != "" {
		s.publish(replayMsg{kind: replayAgent, text: text})
	}
}

// Notice publishes a backend/system hint as a plain text block (e.g. the
// -c resume fallback in main.go).
func (s *Stream) Notice(text string) {
	if text != "" {
		s.publish(noticeMsg{text: text})
	}
}

// replayKind classifies a replayed history element (FR-44).
type replayKind int

const (
	replayUser replayKind = iota
	replayAgent
)

// replayMsg carries one whole element of the loaded conversation.
type replayMsg struct {
	kind replayKind
	text string
}

// noticeMsg reports a backend hint that lands as a text block.
type noticeMsg struct{ text string }

// chunkMsg carries one piece of the in-flight answer.
type chunkMsg struct{ text string }

// thoughtMsg carries one piece of the thinking stream.
type thoughtMsg struct{ text string }

// toolMsg reports tool activity while the turn runs: name speist die
// Statuszeile, callID/title/text die Tool-Karte im Chat; kind waehlt deren
// Icon, failed den Fehler-Zustand.
type toolMsg struct {
	callID string
	name   string
	kind   string
	text   string
	done   bool
	failed bool
	// Strukturierte Felder aus den echten goose-Frames (rawInput/rawOutput/
	// live_output) — leer im Legacy-/Demo-Pfad, dann gilt text.
	cmd    string // rawInput.command → $-Zeile der Karte
	out    string // rawOutput.stdout
	errOut string // rawOutput.stderr (⚠-Zeilen)
	exit   int    // rawOutput.exit_code
	hasOut bool   // finales Update mit rawOutput
	live   string // live_output-Chunk DIESES Updates
}

// ToolUpdate bündelt alle Anzeige-Daten eines Tool-Call-Stands für
// Stream.Tool — die strukturierte Variante von ToolCall.
type ToolUpdate struct {
	CallID, Label, Kind string
	Text                string
	Command             string
	Stdout, Stderr      string
	Exit                int
	HasOutput           bool
	Live                string
	Done, Failed        bool
}

// removeStreamBlocks drops every transient streaming preview block (the
// final parsed blocks replace them when the reply settles).
func (l BlockList) removeStreamBlocks() BlockList {
	for i := len(l.items) - 1; i >= 0; i-- {
		id := l.items[i].ID()
		if id <= StreamBlockID && id > StreamBlockID-streamIDRange {
			l.items = append(l.items[:i], l.items[i+1:]...)
		}
	}
	return l
}

// upsertStreamWidgets replaces the transient streaming preview blocks with
// the fresh set and keeps the pager pinned while follow is active (FR-2/FR-8).
func (l BlockList) upsertStreamWidgets(widgets []Widget) BlockList {
	l = l.removeStreamBlocks()
	if len(widgets) > 0 {
		l = l.Append(widgets...)
	}
	if l.follow {
		l = l.ScrollEnd()
	}
	return l
}

// ThinkBlockID addresses the collapsible thinking block (FR-30). Like the
// stream preview it lives outside the user-visible numbering — far enough
// below the streaming range to never collide with it.
const ThinkBlockID = 999900

// upsertThinkBlock creates or refreshes the collapsible thinking block and
// keeps the pager pinned while follow is active (FR-30). The fold state
// survives the refresh so streaming does not collapse an unfolded block.
func (l BlockList) upsertThinkBlock(text string, th *render.Theme) BlockList {
	expanded := false
	for _, it := range l.items {
		if it.ID() == ThinkBlockID {
			if tb, ok := it.(ThinkBlock); ok {
				expanded = tb.expanded
			}
		}
	}
	tb := NewThinkBlock(text, th)
	tb.expanded = expanded
	nb := tb.SetID(ThinkBlockID)
	if _, ok := l.UpdateBlock(ThinkBlockID, nb); ok {
		if l.follow {
			l = l.ScrollEnd()
		}
		return l
	}
	l = l.Append(nb)
	if l.follow {
		l = l.ScrollEnd()
	}
	return l
}
