package tui

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Byte-Literals statt Escape-Folgen (JSON-Template-Kollisionen vermeiden).
var (
	escB     = []byte{0x1b}
	escStr   = string(escB)
	dcsTerm  = string([]byte{0x1b, 0x5c}) // ESC backslash
	fgIDMark = escStr + "[38:2:"
	transmit = escStr + "_Ga=T"
)

// stripEsc entfernt ANSI-/DCS-Sequenzen, verwirft aber NIEMALS den Rest
// (ein fehlender DCS-Terminator darf die Messung nicht ungueltig machen).
func stripEsc(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '_' {
			if j := strings.Index(s[i+2:], dcsTerm); j >= 0 {
				i += 2 + j + len(dcsTerm)
				continue
			}
			i++
			continue
		}
		j := i + 1
		if j < len(s) && (s[j] == '[' || s[j] == 'O') {
			j++
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		i = j
	}
	return b.String()
}

// TestRestingFrame misst den ruhenden Schirm (follow-mode, unten, OHNE
// Scrollen) bei der Fenstergroesse des Nutzers (337x59) — die entscheidende
// Groesse fuer "text wird nicht gerendert": wenn ein Diagramm das ganze
// Fenster fuellt, ist kein Text da, obwohl korrekt gerendert wird.
func TestRestingFrame(t *testing.T) {
	if _, err := exec.LookPath("plantuml"); err != nil {
		t.Skip("plantuml nicht im PATH")
	}
	t.Setenv("HERON_IMAGES", "force")
	t.Cleanup(KittyCleanup)
	src, err := os.ReadFile("../../docs/render-test.md")
	if err != nil {
		t.Skipf("render-test.md: %v", err)
	}
	r := newRig(t).seed(string(src))
	nm, cmd := r.m.Update(tea.WindowSizeMsg{Width: 337, Height: 59})
	r.m = nm.(Model)
	if cmd == nil {
		t.Fatal("keine Diagramm-Jobs")
	}
	done := cmd().(diagramsDoneMsg)
	chat, _ := r.m.chat.applyDiagrams(done.results, true, r.m.caps)
	r.m.chat = chat.ScrollEnd()

	// Text ueber die volle Chat-Laenge: Text-Bloecke sind keine rangedWidgets,
	// all() enthaelt ihre Zeilen vollstaendig — unabhaengig vom Scrollpunkt.
	allLines, _ := r.m.chat.all()
	plain := stripEsc(strings.Join(allLines, "\n"))
	words := []string{"Absatz", "Donaudampf", "kursiv", "package main", "Code-Bl", "Inline-Markdown"}
	textHits := 0
	for _, w := range words {
		if c := strings.Count(plain, w); c > 0 {
			textHits += c
		}
	}
	// Bild DIREKT am Block pruefen (Scroll-unabhaengig): all() rendert
	// rangedWidgets nur im sichtbaren Fenster, und die Leerzeilen um Ueber-
	// schriften/Listen verschieben den Diagrammstandort am Ruhepunkt (FR:
	// "eine Zeile frei"). Ein ImageBlock muss Platzhalterzeilen (fgIDMark)
	// liefern und durch den Zeilen-Cap begrenzt sein.
	w := r.m.chat.Width()
	imgRows, imgHeight := 0, 0
	for _, it := range r.m.chat.items {
		if ib, ok := it.(ImageBlock); ok {
			if rows := strings.Count(ib.View(w), fgIDMark); rows > 0 {
				imgRows += rows
				imgHeight = ib.Height(w)
			}
		}
	}
	// Das Ruhe-Fenster selbst darf nicht leer sein (Header/Eingabe/Footer).
	if strings.TrimSpace(stripEsc(r.m.View())) == "" {
		t.Fatal("ruhe-fenster ist leer")
	}
	t.Logf("text-treffer=%d bild-platzhalterzeilen=%d (blockhoehe=%d, chat-hoehe=%d)",
		textHits, imgRows, imgHeight, r.m.chat.height)
	if textHits == 0 {
		t.Fatal("DOKUMENT OHNE TEXT")
	}
	if imgRows == 0 {
		t.Fatal("kein Bild-Platzhalter (ImageBlock rendert 0 zeilen) — cap/Render kaputt")
	}
	// Cap-Garantie: ein Bild darf das Chat-Fenster nie ganz fuellen.
	if imgHeight <= 0 || imgHeight >= r.m.chat.height {
		t.Fatalf("bild fuellt das chat-fenster: blockhoehe=%d chat-hoehe=%d", imgHeight, r.m.chat.height)
	}
}
