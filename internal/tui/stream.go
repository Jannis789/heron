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

// Tool publishes tool activity (FR-2). done clears the busy label.
func (s *Stream) Tool(name string, done bool) {
	s.publish(toolMsg{name: name, done: done})
}

// Info publishes the model/effort the backend announced (ACP configOptions)
// once the session exists, plus the selectable model list.
func (s *Stream) Info(model, effort string, modelOptions []string) {
	s.publish(infoMsg{model: model, effort: effort, modelOptions: modelOptions})
}

// infoMsg carries the session's real model/effort labels and the selectable
// model list behind them.
type infoMsg struct {
	model        string
	effort       string
	modelOptions []string
}

// chunkMsg carries one piece of the in-flight answer.
type chunkMsg struct{ text string }

// thoughtMsg carries one piece of the thinking stream.
type thoughtMsg struct{ text string }

// toolMsg reports tool activity while the turn runs.
type toolMsg struct {
	name string
	done bool
}

// upsertStreamBlock creates or refreshes the streaming preview block and
// keeps the pager pinned to the bottom while follow is active (FR-2/FR-8).
func (l BlockList) upsertStreamBlock(text, color string) BlockList {
	nb := NewTextBlock(text, color).SetID(StreamBlockID)
	if _, ok := l.UpdateBlock(StreamBlockID, nb); ok {
		if l.follow {
			l = l.ScrollEnd()
		}
		return l
	}
	return l.Append(nb)
}

// ThinkBlockID addresses the collapsible thinking block (FR-30). Like the
// stream preview it lives outside the user-visible numbering.
const ThinkBlockID = 999998

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
