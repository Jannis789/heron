package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestStreamingPreviewGrowsBeforeFinalReply (FR-2, Activity-Tree): Chunks
// leben in den LETZTEN DREI Textzeilen des Activity-Baums — der Chat bleibt
// waehrend des Turns stabil; die fertige Antwort ersetzt sie im Chat.
func TestStreamingPreviewGrowsBeforeFinalReply(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()

	r.do(chunkMsg{text: "Hallo "})
	r.contains("Hallo")
	r.do(chunkMsg{text: "Welt, dies streamt."})
	r.contains("Hallo Welt, dies streamt.")
	if n := len(r.m.chat.items); n != 0 {
		t.Fatalf("Chat hat %d Bloecke waehrend des Streams, want 0 (stabil)", n)
	}
	if !r.m.running {
		t.Fatal("turn should still be running while chunks arrive")
	}

	r.do(replyMsg{text: "Finaler Block mit **Formatierung**."})
	r.contains("Finaler Block")
	if r.m.running {
		t.Fatal("turn should be settled after the reply")
	}
}

// TestStreamDoesNotConsumeNumber (FR-3): der Stream brennt keine Block-
// nummer — er legt nichts im Chat an.
func TestStreamDoesNotConsumeNumber(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(chunkMsg{text: "Vorschau"})
	r.m = r.m.SeedMarkdown("```go\nx := 1\n```")
	r.contains("#1")
}

// TestStreamingKeepsFollowAtBottom (FR-8): der stabile Chat erlaubt
// Weiterlesen waehrend des Streams — Follow bleibt gesetzt, Scrollen
// pausiert es weiter.
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

// TestThinkingStreamsIntoActivity (FR-2/FR-30, Activity-Tree): Thinking
// landet im eingeklappten Thinking-Knoten; Working on zeigt den Text.
func TestThinkingStreamsIntoActivity(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "überlege kurz\nfast fertig"})

	r.contains("Thinking (1)")
	r.contains("Working on")
	r.contains("überlege kurz")
	r.notContains("💭")
}

// TestRunningSpinnerDrivesInputBorder (FR-2): der Spinner-Takt treibt den
// Neon-Verlauf am Eingaberahmen; im View selbst gibt es KEIN Spinner-
// Glyph mehr (Activity-Tree-Spec § 10: keine Ladeanimation).
func TestRunningSpinnerDrivesInputBorder(t *testing.T) {
	r := newRig(t)

	if g := r.m.spinnerGlyph(); g != "" {
		t.Fatalf("idle spinner glyph = %q, want empty", g)
	}
	r.m.startTurn()
	a := r.m.spinnerGlyph()
	if a == "" {
		t.Fatal("spinner glyph must exist while the turn runs (border clock)")
	}
	r.do(spinnerTickMsg{})
	if b := r.m.spinnerGlyph(); b == a {
		t.Fatalf("spinner did not advance: both frames %q", a)
	}
	if r.m.running && strings.Contains(stripANSI(r.m.View()), a) {
		t.Fatalf("spinner glyph %q leaked into the view", a)
	}
	r.m.endTurn()
	if g := r.m.spinnerGlyph(); g != "" {
		t.Fatalf("spinner glyph survived endTurn: %q", g)
	}
}

// TestToolCallLivesInActivityTree (FR-2/FR-31, Activity-Tree): Tool-
// Aktivitaet zaehlt im Tool-Call-Knoten und in Working on — laufend ●,
// abgeschlossen ✓; der Chat bleibt frei von Karten.
func TestToolCallLivesInActivityTree(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(toolMsg{callID: "c1", name: "read_file.go", done: false})
	r.contains("Tool Calls (1)")
	r.contains("Working on")
	r.contains("read_file.go")
	// Auswahlmodus (tab) statt Modifier-Taste: ↓ auf Tool Calls, enter
	// klappt auf — derselbe Druck klappt auch wieder zu.
	r.key("tab")
	r.key("down")
	r.key("enter")
	r.contains("● read_file.go")
	r.do(toolMsg{callID: "c1", name: "read_file.go", done: true})
	r.contains("Tool Calls (1)") // Update zaehlt nicht doppelt (FR-31)
	r.contains("✓ read_file.go")
	if n := len(r.m.chat.items); n != 0 {
		t.Fatalf("Chat hat %d Bloecke, want 0 — der Prozess lebt im Baum", n)
	}
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
	nilStream.ToolCall("c0", "z", "execute", "text", false, false)

	empty := &Stream{}
	empty.Chunk("x")
	empty.Thought("y")
	empty.ToolCall("c1", "z", "fetch", "text", true, false)
	empty.attach(nil)
	empty.Chunk("still safe")
}
