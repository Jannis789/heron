package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// TestEscExitsCursorModeEndToEnd (Regression "Modus-Falle"): Beide ESC-
// Zustellwege muessen den Zeichen-Cursor SOFORT verlassen — ueber den
// ECHTEN Programm-Lauf, denn der Fehlschlag lag im Reader vor bubbletea:
//
// (a) Protokoll aktiv: esc kommt als \x1b[27u (Kitty disambiguate) und
//
//	wird zu \x1b uebersetzt.
//
// (b) Protokoll IGNORIERT (Terminal nimmt den Push nicht an — aeltere
//
//	ghostty, shift+enter kommt dann als \r): esc ist ein NACKTES \x1b.
//	Der Reader hielt es als moeglichen Sequenz-Anfang zurueck — die
//	Taste versackte im Haltepuffer und verschmolz mit der NAECHSTEN
//	Taste zu alt+X. Der Modus war ohne Ausgang.
//
// (c) WezTerm: Protokoll AKTIV, aber esc kommt MIT Modifier-Feld
//
//	(\x1b[27;1u — WezTerm meldet Modifier IMMER, dazu gern Event-Typ
//	und Text). Der fruehere exakte String-Match ("27") warf die Taste
//	stumm WEG — esc tat nichts, der Modus hatte KEINEN Ausgang
//	(User-Report: "ich kann nicht aus dem Cursor-Mode gehen").
//
// Beweis: Der Cursor-Footer erscheint, danach der Default-Footer — und
// nach dessen letztem Auftauchen nie wieder der Cursor-Footer.
func TestEscExitsCursorModeEndToEnd(t *testing.T) {
	for _, tc := range []struct {
		name string
		esc  []byte
	}{
		{"protokoll esc (CSI 27u)", []byte("\x1b[27u")},
		{"legacy nacktes esc", []byte("\x1b")},
		{"wezterm esc (CSI 27;1u)", []byte("\x1b[27;1u")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th, err := render.DefaultTheme()
			if err != nil {
				t.Fatalf("theme: %v", err)
			}
			inR, inW := io.Pipe()
			out := &syncBuffer{}
			m := New(func(string) (string, error) { return "", nil }, th, nil)
			m = m.SeedMarkdown("Ein Satz zum Festhalten des Modus.")

			p := tea.NewProgram(m,
				tea.WithInput(wrapKittyInput(inR)),
				tea.WithOutput(out),
				tea.WithAltScreen(),
			)
			done := make(chan error, 1)
			go func() { _, err := p.Run(); done <- err }()
			go func() {
				time.Sleep(200 * time.Millisecond)
				_, _ = inW.Write([]byte("\x1b[Z")) // shift+tab: Cursor an
				time.Sleep(400 * time.Millisecond)
				_, _ = inW.Write(tc.esc) // esc: Modus SOFORT raus
				time.Sleep(400 * time.Millisecond)
				_, _ = inW.Write([]byte("\x03")) // ctrl+c: Ende
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
			cursorAt := strings.LastIndex(raw, "←↑↓→ cursor")
			if cursorAt < 0 {
				t.Fatalf("Cursor-Modus muss aktiv geworden sein (Footer fehlt):\n%s", stripANSI(raw))
			}
			defAt := strings.LastIndex(raw, "shift+enter cursor · tab chat")
			if defAt < cursorAt {
				t.Fatalf("nach esc muss der Default-Footer zurueck sein (Footer-Ende def@%d < cursor@%d):\n%s",
					defAt, cursorAt, stripANSI(raw))
			}
			if tail := raw[defAt:]; strings.Contains(tail, "←↑↓→ cursor") {
				t.Fatalf("Cursor-Footer darf nach dem esc nie wieder erscheinen:\n%s", stripANSI(raw))
			}
		})
	}
}
