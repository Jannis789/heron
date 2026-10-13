package render

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"strconv"
)

// PNGDims liefert die Pixelmasse eines PNG (fuer das Fit-auf-Hoehe).
// Liest nur den IHDR-Chunk (Header) statt das ganze Bild zu dekodieren —
// PNGDims wird pro Frame von jedem ImageBlock.mehrfach ueber colsFor
// aufgerufen; eine volle Dekodierung war die Hauptlag-Quelle (NFR-2).
func PNGDims(pngBytes []byte) (w, h int, ok bool) {
	if len(pngBytes) < 24 || string(pngBytes[:8]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0, false
	}
	w = int(binary.BigEndian.Uint32(pngBytes[16:20]))
	h = int(binary.BigEndian.Uint32(pngBytes[20:24]))
	return w, h, w > 0 && h > 0
}

// PNGToANSI skaliert ein PNG auf maxCols Spalten und rendert es als
// Truecolor-Halbblock-Zeilen (▀ = obere Pixel im Vordergrund, untere im
// Hintergrund). Reiner Text — das Bild scrollt damit wie jeder andere
// Block und braucht keinen Terminal-Image-Layer (FR-27).
//
// Performance (NFR-2/FR-27): das Ergebnis ist pro (id, cols) im ImageBlock
// gecacht; hier ist nur der _erste_ Aufruf pro Groesse. Trotzdem wird der
// SGR-Aufbau append-basiert statt fmt.Fprintf pro Zelle gemacht — das war
// ~17ms + 8MB allocs fuer ein 2000x1000-Bild, jetzt ein Bruchteil davon.
func PNGToANSI(pngBytes []byte, maxCols int) []string {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	cols := maxCols
	if cols > w {
		cols = w // kleine Bilder nicht unscharf hochskalieren
	}
	if cols < 1 {
		cols = 1
	}
	rows := (h*cols + w/2) / w / 2
	if rows < 1 {
		rows = 1
	}
	px := func(x, y int) (uint8, uint8, uint8) {
		if x >= w {
			x = w - 1
		}
		if y >= h {
			y = h - 1
		}
		r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
		af := float64(a) / 65535.0
		mix := func(v uint32) uint8 {
			return uint8(float64(v>>8)*af + 255*(1-af))
		}
		return mix(r), mix(g), mix(bl)
	}
	out := make([]string, 0, rows)
	// Puffergroesse: pro Zelle ~40 Bytes SGR + 1 Zeichen.
	cell := 48
	for r := 0; r < rows; r++ {
		buf := make([]byte, 0, cols*cell)
		for c := 0; c < cols; c++ {
			x := c
			y1 := (2 * r * h) / (rows * 2)
			y2 := ((2*r + 1) * h) / (rows * 2)
			if y1 >= h {
				y1 = h - 1
			}
			if y2 >= h {
				y2 = h - 1
			}
			tr, tg, tb := px(x, y1)
			br, bg, bb := px(x, y2)
			buf = append(buf, "[38;2;"...)
			buf = strconv.AppendInt(buf, int64(tr), 10)
			buf = append(buf, ';')
			buf = strconv.AppendInt(buf, int64(tg), 10)
			buf = append(buf, ';')
			buf = strconv.AppendInt(buf, int64(tb), 10)
			buf = append(buf, ";48;2;"...)
			buf = strconv.AppendInt(buf, int64(br), 10)
			buf = append(buf, ';')
			buf = strconv.AppendInt(buf, int64(bg), 10)
			buf = append(buf, ';')
			buf = strconv.AppendInt(buf, int64(bb), 10)
			buf = append(buf, 'm')
			buf = append(buf, "\u2580"...)
		}
		out = append(out, string(buf))
	}
	return out
}
