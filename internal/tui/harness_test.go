package tui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// ansiRE matches OSC sequences (incl. OSC52 clipboard writes) and CSI/other
// escapes so assertions can run against the printable screen content only.
var ansiRE = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-9;:?]*[ -/]*[@-~]|\x1b[@-Z\\-_]`)

// stripANSI removes escape sequences (CSI, OSC, single-char) from s.
func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// rig drives a Model deterministically: every input is fed through Update and
// every returned tea.Cmd is executed and its message fed back, so a test can
// assert on the rendered screen after a scripted interaction. It is the
// primary TUI test surface (bubbletea model tests, no terminal needed).
type rig struct {
	t      *testing.T
	m      Model
	copied []string
	quit   bool
	depth  int
}

// newRig builds a model wired to a recording copy function and the default
// theme, sized to 100x30 unless size() is called.
func newRig(t *testing.T) *rig {
	t.Helper()
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatalf("default theme: %v", err)
	}
	r := &rig{t: t}
	r.m = New(func(prompt string) (string, error) { return "", nil }, th, func(s string) tea.Cmd {
		return func() tea.Msg { r.copied = append(r.copied, s); return nil }
	})
	// The cursor blink timer otherwise waits a real 530ms per input action.
	r.m.input.Cursor.BlinkSpeed = time.Nanosecond
	r.size(100, 30)
	return r
}

// seed appends a markdown document to the chat.
func (r *rig) seed(md string) *rig {
	r.t.Helper()
	r.m = r.m.SeedMarkdown(md)
	return r
}

// withSender replaces the prompt sender (canned replies in tests).
func (r *rig) withSender(fn func(string) (string, error)) *rig {
	r.m.send = fn
	return r
}

// size delivers a WindowSizeMsg (sets width/height and chat viewport).
func (r *rig) size(w, h int) *rig {
	r.t.Helper()
	return r.do(tea.WindowSizeMsg{Width: w, Height: h})
}

// do feeds one message and runs any returned command to completion.
func (r *rig) do(msg tea.Msg) *rig {
	r.t.Helper()
	if r.depth > 32 {
		r.t.Fatal("command cascade exceeded 32 iterations")
	}
	nm, cmd := r.m.Update(msg)
	r.m = nm.(Model)
	r.exec(cmd)
	return r
}

// exec runs a command chain, feeding resulting messages back into Update.
func (r *rig) exec(cmd tea.Cmd) {
	for i := 0; i < 32 && cmd != nil; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		switch m := msg.(type) {
		case tea.QuitMsg:
			r.quit = true
			return
		case tea.BatchMsg:
			// A batch (send + tick) must not be fed to Update as one
			// message; its sub-commands are the real payloads.
			for _, sub := range m {
				if sub != nil {
					r.exec(sub)
				}
			}
			return
		default:
			// UI timers (cursor blink, turn tick) reschedule themselves.
			// Feeding them back would cascade forever, so stop before the
			// next sleep.
			if n := reflect.TypeOf(m); n != nil {
				name := strings.ToLower(n.Name())
				if strings.Contains(name, "blink") || strings.Contains(name, "tick") {
					return
				}
			}
		}
		r.depth++
		nm, c := r.m.Update(msg)
		r.depth--
		r.m = nm.(Model)
		cmd = c
	}
}

// pending runs a command once without feeding the message back — used to
// assert that effects are deferred to the runtime (Elm purity).
func (r *rig) pending(cmd tea.Cmd) tea.Msg {
	r.t.Helper()
	if cmd == nil {
		r.t.Fatal("expected a command")
	}
	return cmd()
}

// keyOf maps a key name (as reported by tea.KeyMsg.String()) to a KeyMsg.
func keyOf(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// key presses a named key ("enter", "tab", "up", "ctrl+c", "esc", "c", ...).
func (r *rig) key(s string) *rig {
	r.t.Helper()
	return r.do(keyOf(s))
}

// typeLine types runes as one KeyRunes message (realistic input path).
func (r *rig) typeLine(text string) *rig {
	r.t.Helper()
	return r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

// click presses the left mouse button at screen coordinates.
func (r *rig) click(x, y int) *rig {
	r.t.Helper()
	return r.do(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

// move reports pointer motion (hover).
func (r *rig) move(x, y int) *rig {
	r.t.Helper()
	return r.do(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
}

// view is the raw (styled) frame.
func (r *rig) view() string { return r.m.View() }

// screen is the ANSI-stripped frame — assertions run against this.
func (r *rig) screen() string { return stripANSI(r.m.View()) }

// lines returns the stripped frame split into lines.
func (r *rig) lines() []string {
	return strings.Split(r.screen(), "\n")
}

// contains asserts the screen contains sub.
func (r *rig) contains(sub string) *rig {
	r.t.Helper()
	if !strings.Contains(r.screen(), sub) {
		r.t.Fatalf("screen missing %q\n--- screen ---\n%s", sub, r.screen())
	}
	return r
}

// notContains asserts the screen does not contain sub.
func (r *rig) notContains(sub string) *rig {
	r.t.Helper()
	if strings.Contains(r.screen(), sub) {
		r.t.Fatalf("screen should not contain %q\n--- screen ---\n%s", sub, r.screen())
	}
	return r
}

// lineContains asserts line i (trimmed right) contains sub.
func (r *rig) lineContains(i int, sub string) *rig {
	r.t.Helper()
	ls := r.lines()
	if i < 0 || i >= len(ls) {
		r.t.Fatalf("line %d out of range (%d lines)", i, len(ls))
	}
	if !strings.Contains(strings.TrimRight(ls[i], " "), sub) {
		r.t.Fatalf("line %d = %q, want substring %q", i, ls[i], sub)
	}
	return r
}

// copiedAll returns everything written to the clipboard during the test.
func (r *rig) copiedAll() []string { return r.copied }
