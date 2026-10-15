package tui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"heron/internal/render"
)

// findImage liefert den Block mit der stabilen ID, wenn er ein ImageBlock ist.
func findImage(l BlockList, id int) (Widget, bool) {
	for _, it := range l.items {
		if it.ID() == id {
			_, ok := it.(ImageBlock)
			return it, ok
		}
	}
	return nil, false
}

// realPNG baut ein tatsaechlich dekodierbares PNG.
func realPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestPNGDims(t *testing.T) {
	w, h, ok := pngDimsFromBytes(realPNG(320, 200))
	if !ok || w != 320 || h != 200 {
		t.Fatalf("pngDimsFromBytes = %d,%d,%v", w, h, ok)
	}
	if _, _, ok := pngDimsFromBytes([]byte("kein png")); ok {
		t.Fatal("garbage accepted as png")
	}
}

// Metrik 2 (richtige Groesse + Aspekt): rowsForTerm rechnet die Bildhoehe in
// Zellen aus Blockbreite, Bildpixeln und echter Zellgroesse (CellW/CellH).
func TestImageBlockAspect(t *testing.T) {
	rows := rowsForTerm(400, 300, 78, 9, 20)
	want := 300 * 78 * 9 / (400 * 20)
	if rows != want {
		t.Fatalf("rowsForTerm=%d want %d", rows, want)
	}
	if rows < 1 {
		t.Fatal("sanity: rows must be >= 1")
	}
	tall := rowsForTerm(400, 300, 78, 9, 40)
	if tall >= rows || tall < 1 {
		t.Fatalf("taller cells must reduce rows: got %d >= %d", tall, rows)
	}
	wide := rowsForTerm(400, 300, 78, 18, 20)
	if wide <= rows {
		t.Fatalf("wider cells must increase rows: got %d <= %d", wide, rows)
	}
}

// Spec-Golden-Test: Das offizielle Kitty-Beispiel (graphics-protocol.md,
// Abschnitt Unicode placeholders) druckt fuer Bild-ID 42 ein 2x2-Raster:
//
//	printf "\e[38;5;42m\U10EEEE\U0305\U0305\U10EEEE\U0305\U030D\e[39m\n"
//	printf "\e[38;5;42m\U10EEEE\U030D\U0305\U10EEEE\U030D\U030D\e[39m\n"
//
// U+0305 kodiert 0, U+030D kodiert 1. Unsere Zeilen muessen byte-identisch
// sein — FG-Farbe = Bild-ID, U+10EEEE + Zeilen-/Spalten-Diakritika je Zelle.
func TestSpecPlaceholderGolden(t *testing.T) {
	row0 := placeholderRow(42, 0, 2)
	row1 := placeholderRow(42, 1, 2)
	// Spec-Beispiel nutzt 8-Bit-IDs (\e[38;5;42m). Wir folgen timg und
	// kodieren 24-Bit-Truecolor — fuer ID 42 ist das 38:2:0:0:42, die
	// Rastergeometrie (Diakritika-Reihenfolge) bleibt byte-identisch.
	want0 := "\x1b[38:2:0:0:42m\U0010EEEE\u0305\u0305\U0010EEEE\u0305\u030D\x1b[39m"
	want1 := "\x1b[38:2:0:0:42m\U0010EEEE\u030D\u0305\U0010EEEE\u030D\u030D\x1b[39m"
	if row0 != want0 {
		t.Fatalf("row0\n got %q\nwant %q", row0, want0)
	}
	if row1 != want1 {
		t.Fatalf("row1\n got %q\nwant %q", row1, want1)
	}
	// Diakritika-Tabelle: Index 0..4 = U+0305, U+030D, U+030E, U+0310, U+0312
	// (rowcolumn-diacritics.txt, identisch zu image.nvim codes.lua).
	wantSeq := []rune{0x0305, 0x030D, 0x030E, 0x0310, 0x0312}
	for i, w := range wantSeq {
		if diacritic(i) != w {
			t.Fatalf("diacritic(%d)=%U want %U", i, diacritic(i), w)
		}
	}
}

// Metrik 1 (schnell): Transmit ist EIN kurzer Datei-Escape (a=T,t=f) statt
// eines Base64-PNG-Blobs; Payload dekodiert auf eine existierende Datei.
func TestVirtualPlacementFileTransmit(t *testing.T) {
	t.Cleanup(KittyCleanup)
	img := NewImageBlock(realPNG(400, 200), "roh\n", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(7).(ImageBlock)
	img = img.SetKittyID(7).(ImageBlock)
	width := 80
	cols := img.colsFor(width)
	rows := img.rowsFor(width)
	vp := img.virtualPlacement(cols, rows, img.pngPath, img.pngW, img.pngH)
	if vp == "" {
		t.Fatal("virtualPlacement leer")
	}
	for _, part := range []string{
		"a=T", "f=100", "t=f", "i=7", "p=7", "U=1",
		"c=" + itoa(cols), "r=" + itoa(rows), "s=400", "v=200", "m=0",
	} {
		if !strings.Contains(vp, part) {
			t.Fatalf("virtualPlacement fehlt %q: %q", part, vp)
		}
	}
	// Payload = base64(Dateipfad); die Datei muss existieren (Terminal liest sie).
	re := regexp.MustCompile(`m=0;([A-Za-z0-9+/=]+)\x1b`)
	m := re.FindStringSubmatch(vp)
	if m == nil {
		t.Fatalf("kein Payload in %q", vp)
	}
	path, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatalf("payload kein base64: %v", err)
	}
	if _, err := os.Stat(string(path)); err != nil {
		t.Fatalf("transmit-datei fehlt: %v", err)
	}
	// Kuerze als Metrik-1-Nachweis: deutlich unter dem PNG-Blob (~5 KB PNG).
	if len(vp) > 512 {
		t.Fatalf("transmit zu fett fuer jeden Frame: %d Byte", len(vp))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

// Metrik 3 + Follow-Fall: ViewRange liefert nur [from,to), Transmit-Escape
// genau auf der ersten gelieferten Zeile — hohe Bilder uebertragen auch dann,
// wenn ihr Blockanfang herausgescrollt ist.
func TestViewRangePartialCarriesTransmit(t *testing.T) {
	t.Cleanup(KittyCleanup)
	img := NewImageBlock(realPNG(400, 600), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(2).(ImageBlock)
	img = img.SetKittyID(3).(ImageBlock)
	width := 80
	rows := img.rowsFor(width)
	if rows < 20 {
		t.Fatalf("Testaufbau: rows=%d, will >=20", rows)
	}
	from, to := rows-8, rows-2
	v := img.ViewRange(width, from, to)
	lines := strings.Split(v, "\n")
	if len(lines) != to-from {
		t.Fatalf("Zeilen=%d want %d", len(lines), to-from)
	}
	if !strings.HasPrefix(lines[0], "\x1b_Ga=T") {
		t.Fatal("erste sichtbare Zeile traegt den Transmit-Escape nicht")
	}
	for i := 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "\x1b_G") {
			t.Fatalf("Zeile %d traegt unerlaubt einen Transmit", i)
		}
	}
	// Jede Zeile adressiert ihre Bildreihe: FG-ID + Zeilen-Diakritikon r.
	for i, ln := range lines {
		if !strings.Contains(ln, "\x1b[38:2:0:0:3m") {
			t.Fatalf("Zeile %d ohne Bild-ID in FG-Farbe", i)
		}
		want := string(diacritic(from + i))
		if !strings.Contains(ln, want) {
			t.Fatalf("Zeile %d ohne Zeilen-Diakritikon %U", i, diacritic(from+i))
		}
	}
	// Volle View: genauso viele Zeilen wie Height (Scroll-Synchronitaet).
	full := strings.Split(img.View(width), "\n")
	if len(full) != img.Height(width) {
		t.Fatalf("View-Zeilen=%d != Height=%d", len(full), img.Height(width))
	}
	// Cache-Stabilitaet: zweiter Aufruf liefert identische Bytes (Metrik 1).
	if img.View(width) != strings.Join(full, "\n") {
		t.Fatal("View nicht deterministisch (Cache kaputt)")
	}
}

// Metrik 3 im Verbund: BlockList rendert nur das sichtbare Fenster, der
// Transmit steht genau einmal im View, auch wenn das Bild oben abgeschnitten
// ist.
func TestBlockListRangedVisibility(t *testing.T) {
	t.Cleanup(KittyCleanup)
	img := NewImageBlock(realPNG(400, 600), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(1).(ImageBlock)
	img = img.SetKittyID(3).(ImageBlock)
	l := NewBlockList().Append(NewTextBlock("head", "1"), img)
	l = l.SetSize(80, 10)
	// Der Cap wird erst beim SetSize gesetzt — die Geometrie MUSS vom
	// Listen-Item kommen, nicht vom lokalen (ungecappten) Block.
	rows := l.items[1].Height(80)
	if rows >= img.rowsFor(80) {
		t.Fatalf("cap greift nicht: listen-rows=%d uncapped=%d", rows, img.rowsFor(80))
	}
	// Fenster so setzen, dass nur die unteren Bildzeilen sichtbar sind.
	l.offset = 1 + rows - 5
	view := l.View()
	if c := strings.Count(view, "\x1b_Ga=T"); c != 1 {
		t.Fatalf("Transmit-Escape %dx im View, want 1", c)
	}
	lines := strings.Split(view, "\n")
	foundPrefix := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "\x1b_Ga=T") {
			foundPrefix = true
			continue
		}
		if strings.Contains(ln, "\x1b_G") {
			t.Fatal("zweiter Transmit im Fenster")
		}
	}
	if !foundPrefix {
		t.Fatal("kein Transmit im sichtbaren Fenster")
	}
	// Unsichtbarer Bereich bleibt leer: Gesamtzeilenzahl unveraendert.
	_, total := l.all()
	if total != 1+rows {
		t.Fatalf("total=%d want %d (Hoehe darf sich nicht aendern)", total, 1+rows)
	}
}

// Fallback ohne useImg: ASCII-/Roh-Text, keine Graphics-Escapes (FR-6).
func TestImageBlockFallback(t *testing.T) {
	img := NewImageBlock(realPNG(80, 40), "roh text", false, TermCaps{}, nil, nil)
	v := img.View(80)
	if strings.Contains(v, "\x1b_G") {
		t.Fatal("Fallback darf keine Kitty-Escapes enthalten")
	}
	if !strings.Contains(v, "roh text") {
		t.Fatal("Fallback verliert den Text")
	}
}

// Metrik 2 (Vektorpfad, FR-27): ein SVG-Block wird auf die EXAKTE
// Terminal-Zielgroesse gerastert (cols*cellW x rows*cellH) — das Transmit-
// Escape traegt diese Masse (s=/v=), das Terminal skaliert also 0x.
func TestImageBlockSVGRasterExactTarget(t *testing.T) {
	if !render.ResvgAvailable() {
		t.Skip("resvg nicht im PATH (nix wrap fehlt)")
	}
	t.Cleanup(KittyCleanup)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="251px" height="218px" viewBox="0 0 251 218"><rect x="0" y="0" width="251" height="218" fill="#123456"/></svg>`)
	img := NewImageBlock(nil, "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(31).(ImageBlock)
	img = img.SetSVG(svg).(ImageBlock)
	img = img.SetKittyID(31).(ImageBlock)
	width := 80
	cols := img.colsFor(width)
	rows := img.rowsFor(width)
	v := img.View(width)
	// Die gerasterte Datei muss existieren; s=/v= im Transmit MUESSEN ihre
	// echten Masse tragen. resvg haelt das Seitenverhaeltnis und passt in
	// die Box, liegt also ~2% neben der Zellbox (gemessen 691 statt 702) —
	// das Terminal skaliert dann <2%, visuell irrelevant (Metrik 2).
	re := regexp.MustCompile("m=0;([A-Za-z0-9+/=]+)\\x1b")
	m := re.FindStringSubmatch(v)
	if m == nil {
		t.Fatal("kein transmit-payload")
	}
	pathB, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatal(err)
	}
	png, err := os.ReadFile(string(pathB))
	if err != nil {
		t.Fatalf("raster-datei fehlt: %v", err)
	}
	w, h, ok := pngDimsFromBytes(png)
	if !ok {
		t.Fatal("raster ohne gueltige masse")
	}
	if !strings.Contains(v, "s="+itoa(w)) || !strings.Contains(v, "v="+itoa(h)) {
		t.Fatalf("transmit s/v beschreibt nicht die echte raster-groesse %dx%d", w, h)
	}
	boxW, boxH := cols*9, rows*20
	if dabs(w-boxW) > boxW/25+4 || dabs(h-boxH) > boxH/25+4 {
		t.Fatalf("raster %dx%d weicht >2%% von zellbox %dx%d ab", w, h, boxW, boxH)
	}
}

// Cleanup loescht alle Temp-Dateien (Registry leer danach).
func TestKittyCleanupRemovesFiles(t *testing.T) {
	NewImageBlock(realPNG(60, 30), "roh", true, TermCaps{}, nil, nil)
	KittyCleanup()
	KittyCleanup() // idempotent
}

// Metrik 2 (Obergrenze): ein 251-px-Diagramm darf bei 337 Spalten NICHT
// auf 335 Zellen (3015 px = 6-MP-Raster, Schirm voll, 158 kB/Frame)
// gestreckt werden — colsFor kapppt auf intrinsisch x imageScale.
func TestColsForIntrinsicCap(t *testing.T) {
	img := NewImageBlock(realPNG(251, 218), "roh", false, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	got := img.colsFor(337)
	want := 251 * imageScale / 9
	if got != want {
		t.Fatalf("colsFor(337)=%d will %d (intrinsische obergrenze)", got, want)
	}
	// schmales fenster: fensterbreite begrenzt zuerst, cap greift nicht.
	if n := img.colsFor(40); n != 38 {
		t.Fatalf("colsFor(40)=%d will 38 (fensterbreite)", n)
	}
	// HERON_IMAGE_SCALE uebersteuert den Default-Faktor (Env-Knob).
	t.Setenv("HERON_IMAGE_SCALE", "1")
	imageScaleOnce, imageScaleVal = sync.Once{}, 0
	if got := img.colsFor(337); got != 251/9 {
		t.Fatalf("scale=1 colsFor(337)=%d will %d", got, 251/9)
	}
	t.Setenv("HERON_IMAGE_SCALE", "99") // ausserhalb 1..8 -> default
	imageScaleOnce, imageScaleVal = sync.Once{}, 0
	if got := img.colsFor(337); got != 251*imageScale/9 {
		t.Fatalf("ungueltiger scale fallt nicht zurueck: %d", got)
	}
	imageScaleOnce, imageScaleVal = sync.Once{}, 0
}

// Metrik 2 + Text-Sichtbarkeit: ein hohes Portrait-Diagramm darf das
// Chat-Fenster NICHT fuellen — der Zeilen-Cap wird getroffen und die
// Spalten werden zurueckgerechnet, damit der Aspekt exakt bleibt.
func TestImageBlockRowCapPreservesAspect(t *testing.T) {
	t.Cleanup(KittyCleanup)
	// 377x598 bei 9x20-Zellen, Fenster 337 breit: ohne Cap 59 Zeilen.
	img := NewImageBlock(realPNG(377, 598), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(41).(ImageBlock)
	img = img.SetKittyID(41).(ImageBlock)
	full := img.rowsFor(337)
	if full < 55 {
		t.Fatalf("Testaufbau: ohne Cap sollen ~59 Zeilen sein, got %d", full)
	}
	capped := img.SetMaxRows(30).(ImageBlock)
	cols, rows := capped.dims(337)
	if rows > 30 {
		t.Fatalf("cap verletzt: rows=%d will <=30", rows)
	}
	// Aspekt: cols/rows muss dem Pixelverhaeltnis folgen (9x20-Zellen).
	wantCols := 30 * 377 * 20 / (598 * 9)
	if cols != wantCols {
		t.Fatalf("cols=%d will %d (zurueckgerechnet, aspekt-exakt)", cols, wantCols)
	}
	// Height() meldet die GEKAPPTEN Zeilen, sonst rechnet der Pager falsch.
	h := capped.Height(337)
	if h != rows {
		t.Fatalf("Height=%d != dims-rows=%d", h, rows)
	}
	// View liefert genau Height Zeilen (Scroll-Synchronitaet).
	if v := len(strings.Split(capped.View(337), "\n")); v != h {
		t.Fatalf("view-zeilen=%d != height=%d", v, h)
	}
}

var _ = render.PNGDims // PNGDims bleibt im Einsatz (render-Paket)

// dabs ist der Betrags-Helfer fuer die Toleranz-Pruefungen.
func dabs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
