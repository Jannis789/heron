package tui

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// ImageBlock zeigt ein Diagramm als PNG im Kitty-Graphics-Protokoll —
// ueber Unicode-Platzhalter (FR-27): Die Platzhalter sind echte Zellen,
// dadurch steht das Bild genau dort, wo der Code/puml stand, und es
// scrollt mit dem Inhalt. Terminals ohne Platzhalter-Unterstuetzung
// rendern den ASCII-/Roh-Text als CodeBlock.
type ImageBlock struct {
	id         int
	png        []byte
	pngW, pngH int
	useImages  bool
	caps       TermCaps
	ascii      string
	fallback   CodeBlock
	copyFn     CopyFunc
	focused    bool
	hover      bool
	marked     bool
	th         *render.Theme
}

// NewImageBlock baut den Bildblock: PNG-Dimensionen aus dem IHDR-Chunk.
func NewImageBlock(png []byte, ascii string, useImages bool, caps TermCaps, copyFn CopyFunc, th *render.Theme) ImageBlock {
	w, h, ok := pngDims(png)
	if !ok {
		w, h = 400, 300
	}
	b := ImageBlock{
		png:        png,
		pngW:       w,
		pngH:       h,
		useImages:  useImages,
		caps:       caps,
		ascii:      ascii,
		fallback:   NewCodeBlock("text", ascii, nil),
		copyFn:     copyFn,
		th:         th,
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

// colsFor begrenzt die Darstellung auf die Blockbreite.
func (b ImageBlock) colsFor(width int) int {
	c := width - 2
	if c < 10 {
		c = 10
	}
	return c
}

// rowsFor rechnet die Bildhoehe in Zellen — mit echter Zellpixeologia
// aus dem Probe, sonst via 1:2-Zellenschaetzung. Das Seitenverhaeltnis
// stimmt, weil das Terminal die Platzhalter mit der echten Zellgroesse
// schneidet und wir die Zeilen aus derselben Groesse rechnen.
func rowsFor(pngW, pngH, cols, cellW, cellH int) int {
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 9, 20
	}
	rows := pngH * cols * cellW / (pngW * cellH)
	if rows < 1 {
		rows = 1
	}
	if rows > 24 {
		rows = 24
	}
	return rows
}

func (b ImageBlock) rowsFor(width int) int {
	cols := b.colsFor(width)
	return rowsFor(b.pngW, b.pngH, cols, b.caps.CellW, b.caps.CellH)
}

func (b ImageBlock) View(width int) string {
	if !b.useImages || b.id < 1 {
		return b.fallback.View(width)
	}
	cols := b.colsFor(width)
	rows := b.rowsFor(width)
	var out strings.Builder
	out.WriteString(kittyTransmit(b.png, b.pngW, b.pngH, b.id))
	out.WriteString(fmt.Sprintf("\x1b_Ga=p,U=1,i=%d,q=2,c=%d,r=%d;\x1b\\\\", b.id, cols, rows))
	ph := placeholderChar
	for r := 0; r < rows; r++ {
		out.WriteString(strings.Repeat(ph, cols))
		if r < rows-1 {
			out.WriteString("\\n")
		}
	}
	return out.String()
}

func (b ImageBlock) Height(width int) int {
	if !b.useImages {
		return b.fallback.Height(width)
	}
	return b.rowsFor(width)
}

func (b ImageBlock) Update(msg tea.Msg) (Widget, tea.Cmd) { return b, nil }
func (b ImageBlock) Focused() bool   { return b.focused }
func (b ImageBlock) Focusable() bool { return true }
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
func (b ImageBlock) ID() int { return b.id }
func (b ImageBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// kittyTransmit uebertraegt das PNG einmalig mit stabiler Bild-ID (a=t);
// die Platzhalter im Text referenzieren diese ID.
func kittyTransmit(png []byte, pngW, pngH, id int) string {
	enc := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	for {
		chunk := enc
		if len(chunk) > 4096 {
			chunk = chunk[:4096]
		}
		enc = enc[len(chunk):]
		more := "m=0;"
		if len(enc) > 0 {
			more = "m=1;"
		}
		opts := fmt.Sprintf("a=t,f=100,i=%d,q=2,s=%d,v=%d,%s", id, pngW, pngH, more)
		b.WriteString("\x1b_G" + opts + chunk + "\x1b\\\\")
		if len(enc) == 0 {
			break
		}
	}
	return b.String()
}