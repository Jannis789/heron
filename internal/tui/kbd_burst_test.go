package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// TestKittyBurstSelects: Der komplette Tasten-Burst (shift+enter + Pfeile +
// shift+Pfeile) in EINEM Read — Terminals (Paste, tmux-Flush, Test-PTYs)
// liefern Sequenzen gebuendelt. kittyReader muss sie alle durchreichen,
// bubbletea muss sie alle parsen: am Ende steht die Auswahl [2,5) als
// HINTERGRUND-Farbe im Frame (Regression: Burst schien die Auswahl zu fressen).
func TestKittyBurstSelects(t *testing.T) {
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatalf("theme: %v", err)
	}
	inR, inW := io.Pipe()
	out := &syncBuffer{}
	m := New(func(string) (string, error) { return "", nil }, th, nil)
	m = m.SeedMarkdown("Ein Satz zum Auswaehlen im Zeichenmodus.")

	p := tea.NewProgram(m,
		tea.WithInput(wrapKittyInput(inR)),
		tea.WithOutput(out),
		tea.WithAltScreen(),
	)
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	go func() {
		time.Sleep(200 * time.Millisecond)
		// EIN Write, EIN Read: Cursor an, 2x right, 3x shift+right.
		_, _ = inW.Write([]byte("\x1b[13;2u\x1b[C\x1b[C\x1b[1;2C\x1b[1;2C\x1b[1;2C"))
		time.Sleep(600 * time.Millisecond)
		_, _ = inW.Write([]byte{3})
		time.Sleep(200 * time.Millisecond)
		_ = inW.Close()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program error: %v", err)
		}
	case <-time.After(15 * time.Second):
		p.Kill()
		t.Fatal("program did not shut down within 15s")
	}
	raw := out.String()
	if bg := m.chat.charSelBgSgr(); !strings.Contains(raw, bg+"n S\x1b[0m") {
		t.Fatalf("Burst: Auswahl [2,5) muss als Hintergrund (%sn S) erscheinen:\n%q", bg, raw)
	}
}
