package tui

import (
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// TermCaps sind die ermittelten Faehigkeiten des Terminals (FR-27):
// Kitty-Graphics inkl. Unicode-Platzhalter und Zellgroesse in Pixeln.
type TermCaps struct {
	Graphics     bool
	Placeholders bool
	CellW        int
	CellH        int
}

// ProbeTerminal fragt das Terminal ab: Zellpixel (CSI 16 t), Kitty-Graphics
// (a=q) und Unicode-Platzhalter (a=q,U=1). Die Cursorposition (CSI 6 n)
// dient als garantiertes Antwortende. Ausserhalb eines Terminals oder ohne
// Antworten bleibt alles auf "nicht unterstuetzt" (ASCII-Fallback).
func ProbeTerminal() TermCaps {
	var caps TermCaps
	f := os.Stdin
	if !term.IsTerminal(int(f.Fd())) {
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
	os.Stdout.WriteString("\x1b_Ga=q,i=31,f=24,s=1,v=1;AAAA\x1b\\\x1b\\")
	os.Stdout.WriteString("\x1b_Ga=q,i=32,f=24,s=1,v=1,U=1;AAAA\x1b\\\x1b\\")
	os.Stdout.WriteString("\x1b[6n")

	_ = f.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 64)
	for {
		n, err := f.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil || strings.HasSuffix(string(buf), "R") {
			break
		}
	}
	_ = f.SetReadDeadline(time.Time{})
	// Nachgestellte Maus-Reports schlucken, die nach der "R"-Antwort kommen.
	drainInput(f, 30*time.Millisecond)
	caps = parseTermCaps(string(buf))
	imageLogf("probe: graphics=%v placeholders=%v cell=%dx%d raw=%q",
		caps.Graphics, caps.Placeholders, caps.CellW, caps.CellH, printableForLog(string(buf)))
	return caps
}

// drainInput verwirft anstehende Eingabe, bis d Millisekunden lang keine
// mehr kommt (z. B. SGR-Maus-Reports waehrend und nach der Probe).
func drainInput(f *os.File, d time.Duration) {
	buf := make([]byte, 512)
	for i := 0; i < 64; i++ {
		_ = f.SetReadDeadline(time.Now().Add(d))
		n, err := f.Read(buf)
		if err != nil || n == 0 {
			break
		}
	}
	_ = f.SetReadDeadline(time.Time{})
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
	if i := strings.Index(s, "\x1b[6;"); i >= 0 {
		rest := strings.TrimPrefix(s[i+4:], "6;")
		if end := strings.IndexAny(rest, "R"); end >= 0 {
			parts := strings.Split(rest[:end], ";")
			if len(parts) == 2 {
				h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
				w, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err1 == nil && err2 == nil && h > 0 && w > 0 {
					caps.CellH, caps.CellW = h, w
				}
			}
		}
	}
	return caps
}
