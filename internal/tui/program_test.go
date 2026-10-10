package tui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// syncBuffer is a goroutine-safe sink for the program's rendered output.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestProgramRunsEndToEnd starts a real bubbletea program with scripted input
// and a non-terminal output, then asserts the frame was rendered and that
// ctrl+c shuts it down. This is the "does the actual app run" smoke test —
// no pty required.
func TestProgramRunsEndToEnd(t *testing.T) {
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatalf("theme: %v", err)
	}
	inR, inW := io.Pipe()
	out := &syncBuffer{}

	m := New(func(prompt string) (string, error) {
		return "PONG:" + prompt, nil
	}, th, func(s string) tea.Cmd {
		return func() tea.Msg { return nil }
	})

	p := tea.NewProgram(m,
		tea.WithInput(inR),
		tea.WithOutput(out),
		tea.WithAltScreen(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	// Script: type a prompt, submit it, then quit.
	go func() {
		w := func(b []byte) { _, _ = inW.Write(b) }
		time.Sleep(200 * time.Millisecond)
		w([]byte("ping"))
		time.Sleep(150 * time.Millisecond)
		w([]byte("\r")) // Enter
		time.Sleep(400 * time.Millisecond)
		w([]byte{3}) // Ctrl+C
		time.Sleep(200 * time.Millisecond)
		_ = inW.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program error: %v", err)
		}
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("program did not shut down within 10s")
	}

	screen := stripANSI(out.String())
	if !strings.Contains(screen, "heron") {
		t.Fatalf("rendered frame missing header:\n%s", screen)
	}
	if !strings.Contains(screen, "PONG:ping") {
		t.Fatalf("rendered frame missing reply (input path broken?):\n%s", screen)
	}
}
