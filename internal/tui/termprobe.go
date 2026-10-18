package tui

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// TermCaps sind die ermittelten Faehigkeiten des Terminals (FR-27):
// Kitty-Graphics inkl. Unicode-Platzhalter, Zellgroesse in Pixeln und
// ob der Hintergrund HELL ist (OSC 11) — hell bedeutet: Text der
// Dark-Palette (#D9D4EC etc.) waere hell-auf-hell unsichtbar.
type TermCaps struct {
	Graphics     bool
	Placeholders bool
	CellW        int
	CellH        int
	LightBG      bool
}

// ProbeTerminal fragt das Terminal ab: Zellpixel (CSI 16 t), Hintergrund
// (OSC 11), Kitty-Graphics (a=q) und Unicode-Platzhalter (a=q,U=1). Die
// Cursorposition (CSI 6 n) dient als garantiertes Antwortende. Ausserhalb
// eines Terminals oder ohne Antworten bleibt alles auf "nicht unterstuetzt"
// (ASCII-Fallback / Dark-Palette).
func ProbeTerminal() TermCaps {
	var caps TermCaps
	f := os.Stdin
	if !term.IsTerminal(int(f.Fd())) {
		imageLogf("probe: kein TTY -> caps leer (lightbg=false)")
		return caps
	}
	old, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return caps
	}
	defer term.Restore(int(f.Fd()), old)

	// SGR-Maus-Reste abgestuerzter Laeufe verwerfen, bevor gefragt wird —
	// sonst liest bubbletea Fragmente als Tastatur ("[<35;110;29H").
	drainInput(f, 15*time.Millisecond)

	os.Stdout.WriteString("\x1b[16t")
	// OSC 11: das Terminal meldet seine Hintergrundfarbe. Hell -> heron
	// schaltet auf die dunkle Textpalette (catppuccin-latte), sonst ist
	// der Dokument-Text hell-auf-hell unsichtbar (gemessen: Default-
	// Thema #D9D4EC auf hellem Ghostty-Hintergrund = 0 Kontrast).
	os.Stdout.WriteString("\x1b]11;?\x1b\\")
	os.Stdout.WriteString("\x1b_Ga=q,i=31,f=24,s=1,v=1;AAAA\x1b\\\x1b\\")
	os.Stdout.WriteString("\x1b_Ga=q,i=32,f=24,s=1,v=1,U=1;AAAA\x1b\\\x1b\\")
	os.Stdout.WriteString("\x1b[6n")

	// Antworten sammeln — mit select(2)-Timeouts statt SetReadDeadline:
	// Letzteres greift auf os.Stdin nicht zuverlaessig (fd 0 ist oft nicht
	// im Go-Poller registriert), der Read blockierte dann ENDLOS. Der
	// Sandbox-Strace bewies genau das als Ursache des Endlos-Ladens.
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 256)
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, _ := probeRead(f, tmp, 25*time.Millisecond)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if strings.HasSuffix(string(buf), "R") {
				break
			}
		}
	}
	// Nachgestellte Antworten/Maus-Reports schlucken: auch verspaetete
	// a=q-OKs duerfen bubbletea nie erreichen — dort wuerden sie als Runen-
	// Muell zerschnitten (s. kittyResponseFilter, PTY-Desync-Beweis).
	drainInput(f, 120*time.Millisecond)
	caps = parseTermCaps(string(buf))
	imageLogf("probe: graphics=%v placeholders=%v cell=%dx%d lightbg=%v raw=%q",
		caps.Graphics, caps.Placeholders, caps.CellW, caps.CellH, caps.LightBG, printableForLog(string(buf)))
	return caps
}

// drainInput verwirft anstehende Eingabe (z. B. SGR-Maus-Reports waehrend
// und nach der Probe). Bricht ab, wenn d Millisekunden lang nichts kam —
// blockiert dank select(2) nie.
func drainInput(f *os.File, d time.Duration) {
	var tmp [256]byte
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		n, _ := probeRead(f, tmp[:], 10*time.Millisecond)
		if n == 0 {
			break
		}
	}
}

// printableForLog macht Rohterminalbytes lesbar- und logbar.
func printableForLog(s string) string {
	r := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
	if len(r) > 160 {
		r = r[:160] + "..."
	}
	return r
}

// cellSizeRe matcht die XTWINOPS-16t-Zellpixel-Antwort ESC[6;h;wt.
var cellSizeRe = regexp.MustCompile(string([]byte{0x1b}) + `\[6;(\d+);(\d+)t`)

// osc11BgRe matcht die OSC-11-Hintergrundantwort: ESC]11;rgb:RRRR/GGGG/BBBB ESC\
// (Ghostty/kitty) oder die #RRGGBB-Form anderer Terminals.
var osc11BgRe = regexp.MustCompile(string([]byte{0x1b}) + `]11;(rgb:([0-9a-fA-F]{4})/([0-9a-fA-F]{4})/([0-9a-fA-F]{4})|#([0-9a-fA-F]{6}))` + string([]byte{0x1b}) + `\\`)

// imagesEnabled entscheidet, ob das Terminal echte Bilder zeigt (FR-27).
// HERON_IMAGES=force/off uebersteuert; sonst braucht es Placeholder- und
// Graphics-Unterstuetzung aus der Probe (ohne Platzhalter ASCII-Fallback).
func (m Model) imagesEnabled() bool {
	switch strings.ToLower(os.Getenv("HERON_IMAGES")) {
	case "force":
		return true
	case "off":
		return false
	}
	return m.caps.Placeholders && m.caps.Graphics
}

// parseTermCaps wertet die gesammelten Antworten aus (testbar ohne pty).
func parseTermCaps(s string) TermCaps {
	var caps TermCaps
	if strings.Contains(s, "i=31;OK") {
		caps.Graphics = true
	}
	if strings.Contains(s, "i=32;OK") {
		caps.Placeholders = true
	}
	// Zellpixel: die XTWINOPS-16t-Antwort ist ESC[6;HOEHE;BREITEt — sie
	// endet mit 't', NICHT mit dem 'R' der CPR-Antwort.
	if m := cellSizeRe.FindStringSubmatch(s); m != nil {
		h, e1 := strconv.Atoi(m[1])
		w, e2 := strconv.Atoi(m[2])
		if e1 == nil && e2 == nil && h > 0 && w > 0 {
			caps.CellH, caps.CellW = h, w
		}
	}
	// OSC-11-Hintergrund: Helligkeit >= 140/255 bedeutet heller Grund ->
	// dunkle Textpalette noetig (sonst hell-auf-hell unsichtbar).
	if m := osc11BgRe.FindStringSubmatch(s); m != nil {
		hex := m[2] + m[3] + m[4]
		if hex == "" {
			hex = strings.TrimPrefix(m[5], "#")
			hex += hex // #RRGGBB -> RRRRGGGGBBBB (16-bit Kanaele)
		}
		if len(hex) >= 12 {
			hi := func(o int) int { v, _ := strconv.ParseInt(hex[o:o+4], 16, 32); return int(v >> 8) }
			r, g, b := hi(0), hi(4), hi(8)
			if 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) >= 140 {
				caps.LightBG = true
			}
		}
	}
	// HERON_BG=light|dark erzwingt die Palettenwahl (Test/Override).
	if v := strings.ToLower(os.Getenv("HERON_BG")); v == "light" {
		caps.LightBG = true
	} else if v == "dark" {
		caps.LightBG = false
	}
	return caps
}
