package tui

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// TestTranslateKittyShiftEnter: Die CSI-u-Sequenzen des Kitty-Protokolls
// fallen auf bubbletea-verstaendliche Bytes zurueck — shift+enter (13;2u)
// wird zum shift+tab-Trigger (\x1b[Z), esc (27u) zu \x1b, unbekannte
// CSI-u-Tasten verschwinden, ALLE Legacy-Sequenzen bleiben unangetastet.
func TestTranslateKittyShiftEnter(t *testing.T) {
	for _, tc := range []struct {
		in, want, held string
	}{
		{"abc", "abc", ""},
		{"\x1b[13;2u", "\x1b[Z", ""},                             // shift+enter
		{"A\x1b[13;2uB", "A\x1b[ZB", ""},                         // eingebettet
		{"\x1b[27u", "\x1b", ""},                                 // esc
		{"\x1b[13;5u", "", ""},                                   // ctrl+enter: ungebunden, weg
		{"\x1b[13;5;13u", "", ""},                                // WezTerm ctrl+enter MIT Text
		{"\x1b[27;1u", "\x1b", ""},                               // WezTerm esc: Modifier-Feld steht IMMER da
		{"\x1b[27;1:1u", "\x1b", ""},                             // WezTerm esc, Event-Typ press
		{"\x1b[27;1:3u", "", ""},                                 // WezTerm esc RELEASE: kein zweiter Druck
		{"\x1b[27;129:3u", "", ""},                               // WezTerm esc, NumLock+shift, release
		{"\x1b[27;3u", "", ""},                                   // alt+esc: ungebunden, weg
		{"\x1b[13;2;13u", "\x1b[Z", ""},                          // WezTerm shift+enter MIT Text
		{"\x1b[13;2:1u", "\x1b[Z", ""},                           // WezTerm shift+enter, event press
		{"\x1b[13;2:2u", "\x1b[Z", ""},                           // shift+enter repeat
		{"\x1b[13;2:3u", "", ""},                                 // shift+enter RELEASE: weg
		{"\x1b[97;5u", "", ""},                                   // ctrl+a: ungebunden, weg
		{"\x1b[A", "\x1b[A", ""},                                 // Pfeil hoch
		{"\x1b[1;2C", "\x1b[1;2C", ""},                           // shift+right
		{"\x1b[<0;1;1M", "\x1b[<0;1;1M", ""},                     // SGR-Maus
		// SGR-Maus vollstaendig (Regression 2026-11-01): Der Scan kannte '<'
		// nicht, zerlegte \x1b[<0;1;1M in "\x1b[<" + Runen-Rest — bei
		// Split-Reads an der ESC-Grenze lief der Rest OHNE ESC als Getippe
		// ins Eingabefeld (User-Report Mausrad: "[<64;97;43M").
		{"\x1b[<64;97;43M", "\x1b[<64;97;43M", ""},               // Mausrad hoch
		{"\x1b[<65;105;48M", "\x1b[<65;105;48M", ""},             // Mausrad runter
		{"\x1b[<34;56;73M", "\x1b[<34;56;73M", ""},               // Drag (Motion + Button 2)
		{"\x1b[<0;1;1m", "\x1b[<0;1;1m", ""},                     // Release (Finalbyte m)
		{"\x1b[<u", "\x1b[<u", ""},                               // Kitty-POP: kein CSI-u!
		{"\x1b[<64;", "", "\x1b[<64;"},                           // Split MITTE in der Maus-Sequenz
		{"\x1b[<", "", "\x1b[<"},                                 // Split direkt nach dem Introducer
		{"\x1b[Z", "\x1b[Z", ""},                                 // echtes shift+tab
		{"\x1bx", "\x1bx", ""},                                   // alt+x (ESC + Zeichen)
		{"\x1b[200~text\x1b[201~", "\x1b[200~text\x1b[201~", ""}, // Bracketed Paste
		{"\x1b[13;", "", "\x1b[13;"},                             // split: unvollstaendig
		{"\x1b[", "", "\x1b["},                                   // split: CSI-Anfang
		// Nacktes ESC am Pufferende: SOFORT durchreichen (nicht halten!) —
		// ohne aktives Protokoll ist es DIE Esc-Taste; halten wuerde sie
		// schlucken (Modus-Falle). Nur unvollstaendige CSI bleiben held.
		{"x\x1b", "x\x1b", ""},
		{"\x1b", "\x1b", ""},
	} {
		out, held := translateKitty([]byte(tc.in))
		if string(out) != tc.want || string(held) != tc.held {
			t.Fatalf("translateKitty(%q) = (%q, %q), want (%q, %q)",
				tc.in, out, held, tc.want, tc.held)
		}
	}
}

// TestKittyReaderJoinsSplitSequences: Terminal-Sequenzen koennen ueber
// zwei Reads zerrissen ankommen (\x1b[13; dann 2u) — der Reader haelt den
// Anfang zurueck und setzt ihn mit dem naechsten Chunk zusammen.
func TestKittyReaderJoinsSplitSequences(t *testing.T) {
	pr, pw := io.Pipe()
	r := wrapKittyInput(pr)
	go func() {
		_, _ = pw.Write([]byte("vor\x1b[13;"))
		time.Sleep(10 * time.Millisecond)
		_, _ = pw.Write([]byte("2unach"))
		_ = pw.Close()
	}()
	var got []byte
	buf := make([]byte, 64)
	for {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	if string(got) != "vor\x1b[Znach" {
		t.Fatalf("split reads: got %q, want %q", got, "vor\x1b[Znach")
	}
}

// TestKittyReaderJoinsSplitMouse (Regression 2026-11-01): Mausrad-Bursts
// treffen den Reader in beliebigen Stuecken — die Sequenz muss als EINE
// Einheit durchgehen und am Stueck an bubbletea (nie als Runen-Muell im
// Eingabefeld) weitergegeben werden.
func TestKittyReaderJoinsSplitMouse(t *testing.T) {
	pr, pw := io.Pipe()
	r := wrapKittyInput(pr)
	go func() {
		_, _ = pw.Write([]byte("\x1b[<64;"))
		time.Sleep(10 * time.Millisecond)
		_, _ = pw.Write([]byte("97;43M\x1b[<65;97;"))
		time.Sleep(10 * time.Millisecond)
		_, _ = pw.Write([]byte("44M"))
		_ = pw.Close()
	}()
	var got []byte
	buf := make([]byte, 64)
	for {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	want := "\x1b[<64;97;43M\x1b[<65;97;44M"
	if string(got) != want {
		t.Fatalf("split mouse: got %q, want %q", got, want)
	}
}

// TestKittyReaderTinyBuffer: Der Reader muss auch mit kleinen Aufrufer-
// Puffern liefern (pending-Rest wartet auf den naechsten Read).
func TestKittyReaderTinyBuffer(t *testing.T) {
	pr, pw := io.Pipe()
	r := wrapKittyInput(pr)
	go func() {
		_, _ = pw.Write([]byte("\x1b[13;2uXY"))
		_ = pw.Close()
	}()
	var got []byte
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		got = append(got, one[:n]...)
		if err != nil {
			break
		}
	}
	if string(got) != "\x1b[ZXY" {
		t.Fatalf("tiny buffer: got %q", got)
	}
}

// TestKittyKbdEnabled: Nur ghostty/kitty (per TERM/TERM_PROGRAM) mit TTY-
// stdin aktivieren das Protokoll; HERON_KBD=off/force uebersteuert.
func TestKittyKbdEnabled(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Skipf("devnull: %v", err)
	}
	defer devNull.Close()

	cases := []struct {
		term, prog, kbd string
		want            bool
	}{
		{"xterm-ghostty", "", "", true},
		{"xterm-kitty", "", "", true},
		{"xterm-256color", "", "", true}, // SSH: TERM luegt, Terminal kann es
		{"xterm-256color", "ghostty", "", true},
		{"tmux-256color", "", "", false}, // Multiplexer zerschneiden CSI-u
		{"screen-256color", "tmux", "", false},
		{"xterm-ghostty", "", "off", false},
		{"xterm-256color", "", "force", true},
	}
	for _, tc := range cases {
		t.Setenv("TERM", tc.term)
		t.Setenv("TERM_PROGRAM", tc.prog)
		if tc.kbd == "" {
			t.Setenv("HERON_KBD", "")
		} else {
			t.Setenv("HERON_KBD", tc.kbd)
		}
		if got := kittyKbdEnabled(devNull); got != tc.want {
			t.Fatalf("kittyKbdEnabled(TERM=%q TERM_PROGRAM=%q HERON_KBD=%q) = %v, want %v",
				tc.term, tc.prog, tc.kbd, got, tc.want)
		}
	}
}

// TestKittyShiftEnterDrivesSelectionEndToEnd: Der ECHTE Programm-Lauf mit
// Kitty-Sequenz im Input: \x1b[13;2u startet die Shift-Auswahl (Hint
// "shift+enter buchstaben" erscheint), das ZWEITE \x1b[13;2u taucht in den
// Zeichen-Modus ab — und KEIN Runen-Muell ("13;2u") landet auf dem
// Bildschirm, denn bubbletea wuerde die Sequenz ohne Uebersetzung in
// alt+"[" + Getippe zerlegen.
func TestKittyShiftEnterDrivesSelectionEndToEnd(t *testing.T) {
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatalf("theme: %v", err)
	}
	inR, inW := io.Pipe()
	out := &syncBuffer{}

	m := New(func(string) (string, error) { return "", nil }, th,
		func(string) tea.Cmd { return nil })
	m = m.SeedMarkdown("## Kopf\n\nEin Satz zum Auswaehlen im Zeichenmodus.")

	p := tea.NewProgram(m,
		tea.WithInput(wrapKittyInput(inR)),
		tea.WithOutput(out),
		tea.WithAltScreen(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	go func() {
		w := func(b []byte) { _, _ = inW.Write(b) }
		time.Sleep(200 * time.Millisecond)
		w([]byte("\x1b[13;2u")) // shift+enter: Zeichen-Cursor
		time.Sleep(400 * time.Millisecond)
		w([]byte("\x1b[1;2C")) // shift+right x3: Auswahl waechst
		w([]byte("\x1b[1;2C"))
		w([]byte("\x1b[1;2C"))
		time.Sleep(400 * time.Millisecond)
		w([]byte{3}) // ctrl+c
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
	screen := stripANSI(raw)
	if !strings.Contains(screen, "cursor · shift+pfeile auswählen") {
		t.Fatalf("\\x1b[13;2u muss den Zeichen-Cursor setzen (Hint fehlt):\n%s", screen)
	}
	if strings.Contains(screen, "13;2u") || strings.Contains(screen, "13;2") {
		t.Fatalf("Runen-Muell der Kitty-Sequenz auf dem Bildschirm (Parser-Leak):\n%s", screen)
	}
	// Die Auswahl (3x shift+right) faerbt den HINTERGRUND der ersten 3
	// Buchstaben des Caret-Blocks ("Ein Satz ...") mit dem Theme-Akzent.
	if bg := m.chat.charSelBgSgr(); !strings.Contains(raw, bg+"Ein\x1b[0m") {
		t.Fatalf("Auswahl [0,3) muss hinterlegt sein (%s):\n%q", bg, raw)
	}
}
