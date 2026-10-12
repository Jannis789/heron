package tui

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// ImageBlock zeigt ein PNG im Kitty-Graphics-Protokoll (FR-27). Terminals
// ohne Unterstuetzung (erkennbar an TERM/KITTY/GHOSTTY, abschaltbar mit
// HERON_NO_IMAGES=1) rendern stattdessen den ASCII-/Roh-Text als CodeBlock.
type ImageBlock struct {
	id       int
	png      []byte
	pngW     int
	pngH     int
	rows     int // reservierte Bildschirmzeilen
	ascii    string
	fallback CodeBlock // Nicht-Kitty-Darstellung
	copyFn   CopyFunc
	focused  bool
	hover    bool
	marked   bool
	th       *render.Theme
}

// NewImageBlock baut den Bildblock: PNG-Dimensionen aus dem IHDR, die
// reservierte Zeilenzahl aus dem Seitenverhaeltnis (Zeile ~ 1:2 Zelle).
func NewImageBlock(png []byte, ascii string, copyFn CopyFunc, th *render.Theme) ImageBlock {
	w, h, ok := pngDims(png)
	if !ok {
		w, h = 400, 300
	}
	b := ImageBlock{
		png:      png,
		pngW:     w,
		pngH:     h,
		ascii:    ascii,
		fallback: NewCodeBlock("text", ascii, nil),
		copyFn:   copyFn,
		th:       th,
	}
	b.fallback.th = th
	return b
}

// pngDims liest Breite und Hoehe aus dem IHDR-Chunk eines PNG.
func pngDims(b []byte) (int, int, bool) {
	if len(b) < 24 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0, false
	}
	w := int(binary.BigEndian.Uint32(b[16:20]))
	h := int(binary.BigEndian.Uint32(b[20:24]))
	return w, h, w > 0 && h > 0
}

// imageDebug schreibt Diagnose nach /tmp/heron-images.log — nur mit
// HERON_IMAGE_DEBUG=1 (FR-27 Sichtprüfung ohne Terminal-Geräusch).
func imageDebug(msg string) {
	if os.Getenv("HERON_IMAGE_DEBUG") != "1" {
		return
	}
	f, err := os.OpenFile("/tmp/heron-images.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
}

// imageMode liest den Override: force = Escapes immer, off = nie, auto
// (Default) = Terminalerkennung unten.
func imageMode() string {
	switch m := strings.ToLower(os.Getenv("HERON_IMAGES")); m {
	case "force", "off":
		return m
	}
	return "auto"
}

// imageProtocolTerm erkennt Terminals mit Kitty-Graphics-Protokoll.
func imageProtocolTerm() bool {
	switch imageMode() {
	case "force":
		imageDebug("images: force")
		return true
	case "off":
		imageDebug("images: off")
		return false
	}
	t := strings.ToLower(os.Getenv("TERM"))
	if strings.Contains(t, "kitty") || strings.Contains(t, "ghostty") {
		imageDebug("images: on (TERM=" + t + ")")
		return true
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		imageDebug("images: on (KITTY/GHOSTTY env)")
		return true
	}
	imageDebug("images: off (TERM=" + t + ", kein kitty/ghostty-Merkmal)")
	return false
}

// kittyImageEscapes baut die Graphic-Protocol-Escapes: Transmit + Display
// (a=T) in c Spalten x r Zeilen, Base64 in 4096er-Chunks, q=2 ohne ACK.
func kittyImageEscapes(png []byte, cols, rows int) string {
	enc := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	first := true
	for {
		chunk := enc
		if len(chunk) > 4096 {
			chunk = chunk[:4096]
		}
		enc = enc[len(chunk):]
		more := "0"
		if len(enc) > 0 {
			more = "1"
		}
		opts := "m=" + more
		if first {
			opts = "a=T,f=100,q=2,c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows) + "," + opts
			first = false
		}
		b.WriteString("\x1b_G" + opts + ";" + chunk + "\x1b\\")
		if len(enc) == 0 {
			break
		}
	}
	return b.String()
}

// imageGeometry begrenzt die Darstellung auf die Blockbreite und ~20 Zeilen.
func imageGeometry(pngW, pngH, width int) (int, int) {
	cols := width - 2
	if cols < 10 {
		cols = 10
	}
	rows := pngH * cols / pngW / 2
	if rows < 1 {
		rows = 1
	}
	if rows > 24 {
		rows = 24
	}
	return cols, rows
}

func (b ImageBlock) View(width int) string {
	if !imageProtocolTerm() {
		return b.fallback.View(width)
	}
	cols, rows := imageGeometry(b.pngW, b.pngH, width)
	esc := kittyImageEscapes(b.png, cols, rows)
	imageDebug(fmt.Sprintf("Bild %dx%d px als %dx%d Zellen, %d Bytes Escape",
		b.pngW, b.pngH, cols, rows, len(esc)))
	return esc + strings.Repeat("\n", rows-1)
}

func (b ImageBlock) Height(width int) int {
	if !imageProtocolTerm() {
		return b.fallback.Height(width)
	}
	_, rows := imageGeometry(b.pngW, b.pngH, width)
	return rows
}

func (b ImageBlock) Update(msg tea.Msg) (Widget, tea.Cmd) { return b, nil }
func (b ImageBlock) Focused() bool                        { return b.focused }
func (b ImageBlock) Focusable() bool                      { return true }
func (b ImageBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}
func (b ImageBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	return nb
}
func (b ImageBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b ImageBlock) Marked() bool { return b.marked }

// Markdown liefert den ASCII-/Roh-Text (FR-29 yank bleibt nutzbar).
func (b ImageBlock) Markdown() string { return b.ascii }
func (b ImageBlock) Resize(int) Widget { return b }
func (b ImageBlock) HitAction(int, int, int) bool { return false }

// Copy kopiert den ASCII-/Roh-Text in die Zwischenablage (FR-5-Pfad).
func (b ImageBlock) Copy() (Widget, tea.Cmd) {
	if b.copyFn == nil {
		return b, nil
	}
	return b, b.copyFn(b.ascii)
}
func (b ImageBlock) ID() int     { return b.id }
func (b ImageBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// Debug-Helfer fuer Tests: maschinenlesbare Beschreibung.
func (b ImageBlock) String() string {
	return fmt.Sprintf("ImageBlock(%dx%d, %d bytes png)", b.pngW, b.pngH, len(b.png))
}