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

// Usage publishes the token accounting of a finished turn (FR-23).
func (s *Stream) Usage(in, out int) {
	s.publish(usageMsg{in: in, out: out})
}

// usageMsg carries the token usage of the finished turn.
type usageMsg struct{ in, out int }

// Info publishes the model/effort the backend announced (ACP configOptions)
// once the session exists, plus the selectable option lists.
func (s *Stream) Info(model, effort string, modelOptions, effortOptions []string) {
	s.publish(infoMsg{model: model, effort: effort,
		modelOptions: modelOptions, effortOptions: effortOptions})
}

// infoMsg carries the session's real model/effort labels and the selectable
// option lists behind them.
type infoMsg struct {
	model         string
	effort        string
	modelOptions  []string
	effortOptions []string
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
