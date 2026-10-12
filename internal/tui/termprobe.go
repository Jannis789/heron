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

// placeholderChar ist das Kitty-Platzhalterzeichen (U+10EEEE).
const placeholderChar = "\U00010EEEE"

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
	return parseTermCaps(string(buf))
}

// imagesEnabled: HERON_IMAGES=force|off uebersteuert, sonst das Probe-
// Ergebnis (FR-27).
func (m Model) imagesEnabled() bool {
	switch strings.ToLower(os.Getenv("HERON_IMAGES")) {
	case "force":
		return true
	case "off":
		return false
	}
	return m.caps.Placeholders
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