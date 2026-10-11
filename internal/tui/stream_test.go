package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestStreamingPreviewGrowsBeforeFinalReply (FR-2): chunks reach the UI as
// they arrive; the final text then replaces the preview.
func TestStreamingPreviewGrowsBeforeFinalReply(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()

	r.do(chunkMsg{text: "Hallo "})
	r.contains("Hallo")
	r.do(chunkMsg{text: "Welt, dies streamt."})
	r.contains("Hallo Welt, dies streamt.")
	if !r.m.running {
		t.Fatal("turn should still be running while chunks arrive")
	}

	r.do(replyMsg{text: "Finaler Block mit **Formatierung**."})
	r.contains("Finaler Block")
	if r.m.running {
		t.Fatal("turn should be settled after the reply")
	}
	if _, ok := r.m.chat.UpdateBlock(StreamBlockID, NewTextBlock("probe", "")); ok {
		t.Fatal("stream preview must be retired when the final text arrives")
	}
}

// TestStreamBlockDoesNotConsumeNumber (FR-2/FR-3): the transient preview must
// not burn a user-visible block number.
func TestStreamBlockDoesNotConsumeNumber(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(chunkMsg{text: "Vorschau"})
	r.m = r.m.SeedMarkdown("```go\nx := 1\n```")
	r.contains("#1")
}

// TestStreamingKeepsFollowAtBottom (FR-8): new content keeps the pager pinned
// to the bottom; scrolling pauses follow.
func TestStreamingKeepsFollowAtBottom(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	for i := 0; i < 40; i++ {
		r.do(chunkMsg{text: fmt.Sprintf("Zeile %d mit etwas Text\n", i)})
	}
	if !r.m.chat.follow {
		t.Fatal("follow should stay active while streaming")
	}
	r.key("pgup")
	if r.m.chat.follow {
		t.Fatal("scrolling must pause follow")
	}
}

// TestThinkingStreamsIntoStatusLine (FR-2): thinking streams into the status
// line — the 💭 marker plus the current last line.
func TestThinkingStreamsIntoStatusLine(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "überlege kurz\nfast fertig"})

	r.contains("💭")
	r.contains("fast fertig")
}

// TestToolStatusShowsAndClears (FR-2): tool activity is visible while the
// tool runs and disappears when it finishes.
func TestToolStatusShowsAndClears(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(toolMsg{name: "read_file.go", done: false})
	r.contains("read_file.go")
	r.do(toolMsg{name: "read_file.go", done: true})
	r.notContains("read_file.go")
}

// TestCtrlCAbortsTurnInsteadOfQuitting (FR-7): the first Ctrl+C cancels the
// running turn, a second one force-quits the app.
func TestCtrlCAbortsTurnInsteadOfQuitting(t *testing.T) {
	cancelled := false
	r := newRig(t)
	r.m.cancel = func() { cancelled = true }
	r.m.startTurn()

	r.key("ctrl+c")
	if !cancelled {
		t.Fatal("cancel must be invoked during a running turn")
	}
	if r.quit {
		t.Fatal("ctrl+c during a turn must not quit the app")
	}
	if !r.m.cancelling {
		t.Fatal("expected cancelling state")
	}

	r.key("ctrl+c")
	if !r.quit {
		t.Fatal("a second ctrl+c should force-quit")
	}

	// With no turn in flight, ctrl+c quits right away.
	r2 := newRig(t)
	r2.key("ctrl+c")
	if !r2.quit {
		t.Fatal("ctrl+c outside a turn should quit")
	}
}

// TestShiftEnterInsertsNewline (FR-7): the prompt box is a textarea and
// Ctrl+J (what terminals send for Shift+Enter) breaks the line, while Enter
// keeps sending.
func TestShiftEnterInsertsNewline(t *testing.T) {
	r := newRig(t)
	r.typeLine("erste Zeile")
	r.do(tea.KeyMsg{Type: tea.KeyCtrlJ})
	r.typeLine("zweite Zeile")

	v := r.m.input.Value()
	if !strings.Contains(v, "\n") {
		t.Fatalf("expected a newline in the prompt, got %q", v)
	}
	if !strings.Contains(v, "erste Zeile") || !strings.Contains(v, "zweite Zeile") {
		t.Fatalf("prompt lost text: %q", v)
	}
}

// TestEnterSendsMultilinePrompt (FR-7): the assembled multi-line prompt is
// sent as one prompt and the box is cleared.
func TestEnterSendsMultilinePrompt(t *testing.T) {
	var got string
	r := newRig(t).withSender(func(p string) (string, error) { got = p; return "", nil })
	r.typeLine("Zeile eins")
	r.do(tea.KeyMsg{Type: tea.KeyCtrlJ})
	r.typeLine("Zeile zwei")
	r.key("enter")

	if !strings.Contains(got, "Zeile eins") || !strings.Contains(got, "Zeile zwei") {
		t.Fatalf("prompt=%q", got)
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("prompt lost its newline: %q", got)
	}
	if r.m.input.Value() != "" {
		t.Fatalf("prompt box should be empty after sending, got %q", r.m.input.Value())
	}
}

// TestStreamPublishWithoutProgramIsNoop (FR-2): publishing before the runtime
// attaches — or on a nil stream — must never panic.
func TestStreamPublishWithoutProgramIsNoop(t *testing.T) {
	var nilStream *Stream
	nilStream.Chunk("x")
	nilStream.Thought("y")
	nilStream.Tool("z", false)

	empty := &Stream{}
	empty.Chunk("x")
	empty.Thought("y")
	empty.Tool("z", true)
	empty.attach(nil)
	empty.Chunk("still safe")
}
