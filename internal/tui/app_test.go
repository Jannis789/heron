package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSendRunsInCommand(t *testing.T) {
	var gotPrompt string
	m := New(func(prompt string) (string, error) {
		gotPrompt = prompt
		return "Antwort X", nil
	}, nil, nil)

	m.input.SetValue("hallo")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := nm.(Model)
	if cmd == nil {
		t.Fatal("expected a command on enter")
	}
	// Enter may return the send command alone or wrapped in a batch; walk
	// whatever comes back and pick up the reply.
	var reply replyMsg
	var have bool
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				walk(sub)
			}
			return
		}
		if rm, isReply := msg.(replyMsg); isReply {
			reply = rm
			have = true
		}
	}
	walk(cmd)
	if !have {
		t.Fatal("expected a reply from the command")
	}
	if reply.err != nil {
		t.Fatalf("reply err: %v", reply.err)
	}
	m3, _ := m2.Update(reply)
	m4 := m3.(Model)

	if gotPrompt != "hallo" {
		t.Fatalf("sender not wired: gotPrompt=%q", gotPrompt)
	}
	if !strings.Contains(m4.View(), "Antwort X") {
		t.Fatalf("reply missing in view: %q", m4.View())
	}
	if !strings.Contains(m4.View(), "hallo") {
		t.Fatalf("view missing user text: %q", m4.View())
	}
}

func TestModelQuitsOnCtrlC(t *testing.T) {
	m := New(nil, nil, nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd on ctrl+c")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("not a quit msg, got %T", cmd())
	}
}

func TestEnterCopiesWhenCodeFocused(t *testing.T) {
	var copied string
	m := New(nil, nil, nil)
	m.chat = m.chat.Append(NewCodeBlock("go", "x := 1", func(s string) tea.Cmd {
		return func() tea.Msg { copied = s; return nil }
	}))
	var handled bool
	m.chat, _, handled = m.chat.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !handled || !m.chat.Focused() {
		t.Fatal("tab should focus the code block")
	}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a deferred copy cmd")
	}
	if copied != "" {
		t.Fatalf("copy must not run at Update time, got %q", copied)
	}
	cmd()
	if copied != "x := 1" {
		t.Fatalf("copied=%q want code", copied)
	}
	if !strings.Contains(nm.(Model).View(), "COPIED") {
		t.Fatalf("view should show COPIED: %q", nm.(Model).View())
	}
}
