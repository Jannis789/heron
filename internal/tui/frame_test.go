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
	fgIDMark = escStr + "[38;5;"
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

	raw := r.m.View()
	plain := stripEsc(raw)
	rawLines := strings.Split(raw, "\n")
	t.Logf("raw-zeilen=%d plain-zeilen=%d chat-hoehe=%d apc=%v",
		len(rawLines), len(strings.Split(plain, "\n")), r.m.chat.height,
		strings.Contains(raw, transmit))

	words := []string{"Absatz", "Donaudampf", "kursiv", "package main", "Code-Bl", "Inline-Markdown"}
	textHits := 0
	for _, w := range words {
		if c := strings.Count(plain, w); c > 0 {
			textHits += c
			t.Logf("text %q = %d", w, c)
		}
	}
	// Bildzeilen am RAW-Frame zaehlen (FG-ID-Markierung), nicht am gestrippten.
	imgLines := 0
	for _, ln := range rawLines {
		if strings.Contains(ln, fgIDMark) {
			imgLines++
		}
	}
	t.Logf("text-treffer=%d bild-zeilen=%d von %d raw-zeilen", textHits, imgLines, len(rawLines))
	if textHits == 0 {
		t.Fatalf("RUHENDER SCHIRM OHNE TEXT: %d bild-zeilen von %d", imgLines, len(rawLines))
	}
	// Beide Richtungen sichern: ohne Bilder waere der Test ein Blindgaenger.
	if imgLines == 0 {
		t.Fatalf("ruhender schirm ohne bild-zeilen — cap/Render kaputt")
	}
	// Ein einzelnes Bild darf nie das ganze Fenster fuellen (Cap-Garantie).
	if imgLines >= len(rawLines) {
		t.Fatalf("bild fuellt weiterhin den ganzen schirm: %d/%d", imgLines, len(rawLines))
	}
}
