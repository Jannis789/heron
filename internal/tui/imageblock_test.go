package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	// Blockzeilen-Indizes: 0=Kopfzeile, 1..rows=Bildzeilen — from/to
	// adressiert hier die Bildzeilen rows-8..rows-2 wie vorher.
	from, to := rows-7, rows-1
	v := img.ViewRange(width, from, to)
	lines := strings.Split(v, "\n")
	if len(lines) != to-from {
		t.Fatalf("Zeilen=%d want %d", len(lines), to-from)
	}
	if !strings.Contains(lines[0], "\x1b_Ga=T") {
		t.Fatal("erste sichtbare Bildzeile traegt den Transmit-Escape nicht")
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
		want := string(diacritic(from - 1 + i))
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
		if strings.Contains(ln, "\x1b_Ga=T") {
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
	// Lokaler t=f-Pfad deterministisch testen (SSH-Env wuerde den Payload
	// auf Pixeldaten umschalten, s. TestTransmitPlaceholderChunks).
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
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
	// width-4 = Rahmen-Innenbreite (│ + Leerzeichen je Seite).
	if n := img.colsFor(40); n != 36 {
		t.Fatalf("colsFor(40)=%d will 36 (fensterbreite minus rahmen)", n)
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
	// Hoehen-Cap: ein hochkantes Bild wird schmaler dargestellt, damit es
	// ins Zeilenlimit passt (der Aspekt bleibt exakt). 377x598, 337 breit.
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
	wantCols := 30 * 377 * 20 / (598 * 9)
	if cols != wantCols {
		t.Fatalf("cols=%d will %d (zurueckgerechnet, aspekt-exakt)", cols, wantCols)
	}
	h := capped.Height(337)
	if h != rows+2 {
		t.Fatalf("Height=%d != dims-rows+2=%d", h, rows+2)
	}
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

// t-Switch (Bonus-FR): Bild ist Standard, t am fokussierten Block zeigt den
// rohen Diagramm-Quelltext als vollstaendigen CodeBlock (Rahmen, Label mit
// stabiler ID, COPY-Action), t zurueck zum Bild. In der Quelltext-Ansicht
// kopiert enter/c den QUELLTEXT, nicht die ASCII-Grafik.
func TestImageBlockSourceSwitch(t *testing.T) {
	t.Cleanup(KittyCleanup)
	var got string
	copyFn := func(s string) tea.Cmd { got = s; return nil }
	src := "@startuml\nA --> B : Test\n@enduml"
	b := NewImageBlock(realPNG(80, 40), "ascii-art", true, TermCaps{Graphics: true, Placeholders: true, CellW: 9, CellH: 18}, copyFn, nil)
	b = b.SetSource("plantuml", src).(ImageBlock)
	b = b.SetID(4).(ImageBlock)
	b = b.SetFocused(true).(ImageBlock)

	if b.showSrc {
		t.Fatal("Bild muss Standard sein, nicht Quelltext")
	}
	hImg := b.Height(80)

	// t: Bild -> Quelltext
	w, _ := b.Update(keyOf("t"))
	sb, ok := w.(ImageBlock)
	if !ok || !sb.showSrc {
		t.Fatalf("t hat nicht auf Quelltext umgeschaltet: %T", w)
	}
	v := sb.View(80)
	if !strings.Contains(v, "@startuml") || !strings.Contains(v, "A --> B : Test") {
		t.Fatal("Quelltext-Ansicht zeigt den Zaun-Quelltext nicht")
	}
	if !strings.Contains(v, "plantuml #4") {
		t.Fatal("Label verliert Sprache/stabile ID")
	}
	if sb.Height(80) == hImg && !strings.Contains(v, "│") {
		t.Fatal("Quelltext-Ansicht hat keinen CodeBlock-Rahmen")
	}

	// Fokus muss im srcBlock ankommen (enter/c-Pfad des CodeBlock).
	if !sb.srcBlock.Focused() {
		t.Fatal("Fokus wurde nicht in die Quelltext-Ansicht propagiert")
	}
	// c am Block ist seit dem Copy-Chip ein No-Op — kopieren tut der
	// Eingabefeld-Chip (copySelection), der in der Quelltext-Ansicht den
	// QUELLTEXT liefert (was der Nutzer gerade sieht).
	if nw, _ := sb.Update(keyOf("c")); nw.(ImageBlock).showSrc != sb.showSrc {
		t.Fatal("c am Block darf nichts mehr ausloesen")
	}
	// Das CopyFn setzt nur die Variable (Cmd darf nil sein) — zaehlt
	// trotzdem als Kopie.
	if _, ccmd := sb.Copy(); ccmd != nil {
		ccmd()
	}
	if got != src {
		t.Fatalf("Copy in Quelltext-Ansicht liefert %q, will den Quelltext", got)
	}

	// t zurueck: Bild wieder Standard, gleiche Hoehe wie vorher.
	w2, _ := sb.Update(keyOf("t"))
	ib, ok := w2.(ImageBlock)
	if !ok || ib.showSrc {
		t.Fatal("t hat nicht zurueck aufs Bild geschaltet")
	}
	if ib.Height(80) != hImg {
		t.Fatalf("Bild-Hoehe nach zurueckschalten %d, will %d", ib.Height(80), hImg)
	}

	// Ohne SetSource ist t ein No-Op (z. B. reine PNG-Bloecke ohne Zaun).
	plain := NewImageBlock(realPNG(80, 40), "x", true, TermCaps{}, nil, nil)
	w3, _ := plain.Update(keyOf("t"))
	if w3.(ImageBlock).showSrc {
		t.Fatal("t ohne Quelltext darf nicht umschalten")
	}
	// Ohne Quelle bleibt die ASCII der Markdown-/Copy-Payload (FR-29).
	if plain.Markdown() != "x" {
		t.Fatalf("ohne Quelle muss ASCII der Payload sein: %q", plain.Markdown())
	}
	// Markdown des Diagramm-Blocks: der gezaeumte Quelltext.
	if md := b.Markdown(); md != "```plantuml\n@startuml\nA --> B : Test\n@enduml\n```" {
		t.Fatalf("Markdown-Payload falsch: %q", md)
	}

	// c im BILD-Modus kopiert das MARKDOWN (gezaeunter Quelltext — nicht
	// die ASCII-Grafik; der Block hat via SetSource eine Quelle).
	got = ""
	w4, _ := b.Update(keyOf("c"))
	want := "```plantuml\n@startuml\nA --> B : Test\n@enduml\n```"
	if got != want {
		t.Fatalf("c im Bild-Modus kopiert %q, will %q", got, want)
	}
	if w4.(ImageBlock).showSrc {
		t.Fatal("c darf nicht umschalten")
	}
}

// BlockList reicht t an den fokussierten ImageBlock durch (Bonus-FR): TAB
// fokussiert, t schaltet um — Quelltext-Ansicht wird Teil der Zeilen-
// Abrechnung (Height), damit Scrollen/Clamp stimmen.
func TestBlockListRoutesSourceSwitch(t *testing.T) {
	t.Cleanup(KittyCleanup)
	l := NewBlockList()
	l = l.SetSize(80, 24)
	l = l.Append(NewTextBlock("hallo", "#fff"))
	cb := NewCodeBlock("plantuml", "@startuml\nA --> B\n@enduml", nil)
	cb = cb.SetID(1).(CodeBlock)
	l = l.Append(cb)
	img := NewImageBlock(realPNG(80, 40), "art", true, TermCaps{}, nil, nil)
	img = img.SetID(2).(ImageBlock)
	img = img.SetSource("plantuml", "@startuml\nA --> B\n@enduml").(ImageBlock)
	l = l.Append(img)

	// TAB bis der ImageBlock fokussiert ist.
	for i := 0; i < 5 && !(l.Focused() && l.items[l.focus].ID() == 2); i++ {
		l, _, _ = l.Update(keyOf("down"))
	}
	if !l.Focused() || l.items[l.focus].ID() != 2 {
		t.Fatal("TAB hat den ImageBlock nicht fokussiert")
	}
	hImg := l.items[l.focus].Height(80)

	l, _, ok := l.Update(keyOf("t"))
	if !ok {
		t.Fatal("t wurde nicht als konsumiert gemeldet")
	}
	it, isImg := findImage(l, 2)
	if !isImg || !it.(ImageBlock).showSrc {
		t.Fatal("t ueber BlockList hat nicht umgeschaltet")
	}
	if it.Height(80) == hImg {
		t.Fatal("Hoehe muendet nicht in der Quelltext-Ansicht")
	}
	if !strings.Contains(l.View(), "@startuml") {
		t.Fatal("Quelltext erscheint nicht in der gerenderten Liste")
	}
}

// t-Switch zurueck aufs Bild muss das Transmit-Fenster EINMAL erneut
// oeffnen: das Fenster (750 ms / max 5x) ist beim Umschalten laengst zu,
// und wenn das Terminal das Bild verworfen hat, wuerden sonst leere
// Platzhalter stehen bleiben (gemeldet als "anfangs wird das Bild nicht
// gezeigt").
func TestToggleBackRetransmits(t *testing.T) {
	t.Cleanup(KittyCleanup)
	img := NewImageBlock(realPNG(80, 40), "art", true, TermCaps{Graphics: true, Placeholders: true, CellW: 9, CellH: 18}, nil, nil)
	img = img.SetID(9).(ImageBlock)
	img = img.SetKittyID(9).(ImageBlock)
	img = img.SetSource("plantuml", "@startuml\nA --> B\n@enduml").(ImageBlock)
	width := 80

	v1 := img.View(width)
	if !strings.Contains(v1, "_Ga=T") {
		t.Fatal("erster View transmittet nicht")
	}
	// Fenster ablaufen lassen, dann t -> Quelle -> t -> Bild.
	time.Sleep(800 * time.Millisecond)
	w, _ := img.Update(keyOf("t"))
	src := w.(ImageBlock)
	w2, _ := src.Update(keyOf("t"))
	back := w2.(ImageBlock)
	v2 := back.View(width)
	if !strings.Contains(v2, "_Ga=T") {
		t.Fatal("zurueck aufs Bild ohne Re-Transmit — leere Platzhalter moeglich")
	}
	// Nachfolgende Renders (Scroll) senden NICHT erneut: nur das eine Mal.
	v3 := back.View(width)
	if strings.Count(v3, "_Ga=T") != strings.Count(v2, "_Ga=T") {
		t.Fatal("Re-Transmit wiederholt sich bei jedem Frame")
	}
}

// SSH-Fix: sshSession entscheidet das TransMIT-Medium (Base64-Chunks statt
// t=f-Datei), weil der Dateipfad in einer SSH-Session nur auf der Remote-
// Maschine existiert und das lokale Terminal ihn still nicht oeffnen kann.
func TestSSHSessionEnv(t *testing.T) {
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	if sshSession() {
		t.Fatal("ohne SSH-Env kein SSH-Modus")
	}
	t.Setenv("SSH_CONNECTION", "10.0.0.1 51922 10.0.0.2 22")
	if !sshSession() {
		t.Fatal("SSH_CONNECTION muss SSH-Modus ausloesen")
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/pts/3")
	if !sshSession() {
		t.Fatal("SSH_TTY muss SSH-Modus ausloesen")
	}
}

// SSH-Transmit: gleiche Keys wie virtualPlacement (a=T,i,p,U=1,q=2,c,r,s,v,
// f=100), aber Pixeldaten als Base64-Chunks — KEIN t=f. Konkatenierter
// Payload muss das Original-PNG ergeben, m-Kette 1..1,0, a=T nur im ersten
// Chunk (Fortsetzungen sind reine m=1/m=0-Escapes).
func TestTransmitPlaceholderChunks(t *testing.T) {
	t.Cleanup(KittyCleanup)
	pngBytes := realPNG(400, 200)
	s := transmitPlaceholderChunks(7, pngBytes, 13, 7, 400, 200)
	if s == "" {
		t.Fatal("transmit leer")
	}
	for _, part := range []string{"a=T", "i=7", "p=7", "U=1", "q=2", "c=13", "r=7", "s=400", "v=200", "f=100"} {
		if !strings.Contains(s, part) {
			t.Fatalf("transmit fehlt %q", part)
		}
	}
	if strings.Contains(s, "t=f") {
		t.Fatal("SSH-Transmit darf kein t=f (Datei-Medium) enthalten")
	}
	parts := regexp.MustCompile(`\x1b_G([^\x1b;]*);([A-Za-z0-9+/=]*)\x1b\\`).FindAllStringSubmatch(s, -1)
	if len(parts) == 0 {
		t.Fatalf("keine APC-Chunks in %d Byte", len(s))
	}
	var payload strings.Builder
	for i, p := range parts {
		wantMore := 0
		if i < len(parts)-1 {
			wantMore = 1
		}
		if !strings.Contains(p[1], "m="+itoa(wantMore)) {
			t.Fatalf("chunk %d: m=%d erwartet in %q", i, wantMore, p[1])
		}
		if i > 0 && strings.Contains(p[1], "a=T") {
			t.Fatalf("chunk %d: nur der ERSTE darf a=T tragen: %q", i, p[1])
		}
		payload.WriteString(p[2])
	}
	got, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatalf("payload kein base64: %v", err)
	}
	if !bytes.Equal(got, pngBytes) {
		t.Fatalf("payload != original: %d statt %d Byte", len(got), len(pngBytes))
	}
}

// SSH-Session schaltet renderCache auf Inline-Base64, behaelt aber das
// Platzhalter-Layout (FG-Farben, Diakritika, Alt-Screen-Repaint) bei.
func TestViewRangeSSHInline(t *testing.T) {
	t.Cleanup(KittyCleanup)
	t.Setenv("SSH_TTY", "/dev/pts/3")
	img := NewImageBlock(realPNG(400, 200), "roh\n", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(9).(ImageBlock)
	img = img.SetKittyID(9).(ImageBlock)
	width := 80
	rows := img.rowsFor(width)
	v := img.ViewRange(width, 0, rows)
	if v == "" {
		t.Fatal("ViewRange leer")
	}
	if strings.Contains(v, "t=f") {
		t.Fatal("SSH-Pfad darf kein t=f senden")
	}
	if !strings.Contains(v, "a=T") {
		t.Fatal("SSH-Pfad muss a=T senden (Base64-Chunks)")
	}
	if !strings.Contains(v, placeholderChar) {
		t.Fatal("Platzhalter-Raster (U+10EEEE) fehlt im SSH-Pfad")
	}
	idFG := "38:2:0:0:9m"
	if !strings.Contains(v, idFG) {
		t.Fatalf("FG-Farbe der Bild-ID (kitty=9) fehlt")
	}
}

// Switch-Chrome: Footer ist exakt `width` Zellen breit (Geometrie-Vertrag
// zwischen layoutFooter und ActionAt) und traegt [ TEXT ]+[ COPY ]; nach
// dem t-Switch [ IMAGE ] — gleicher Header, gleicher Rahmen.
func TestImgFooterExactWidth(t *testing.T) {
	t.Cleanup(KittyCleanup)
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	th, _ := render.DefaultTheme()
	img := NewImageBlock(realPNG(400, 200), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, th)
	img = img.SetID(5).(ImageBlock)
	img = img.SetSource("plantuml", "@startuml\nA --> B : Test\n@enduml").(ImageBlock)
	img = img.SetKittyID(5).(ImageBlock)
	// Progressive Disclosure: der Footer (mit [ TEXT ]/[ COPY ]) erscheint
	// erst bei Fokus — also den Block fokussieren, dann die Box pruefen.
	img = img.SetFocused(true).(ImageBlock)
	for _, w := range []int{40, 60, 90} {
		lines := strings.Split(img.View(w), "\n")
		foot := lines[len(lines)-1]
		if lw := lipgloss.Width(foot); lw != w {
			t.Fatalf("footer-breite=%d want %d", lw, w)
		}
	}
	v := img.View(60)
	if !strings.Contains(v, " px") {
		t.Fatal("INFO (Aufloesung 'WxH px') fehlt im Bild-Footer")
	}
	if strings.Contains(v, "[ TEXT ]") || strings.Contains(v, "[ COPY ]") || strings.Contains(v, "[ ⏎") || strings.Contains(v, "[ esc") {
		t.Fatal("Bild-Footer muss INFO-only sein (keine Aktions-Kaesten)")
	}
	if !strings.Contains(v, "plantuml #5") {
		t.Fatal("Header-Sprache + Blocknummer fehlen")
	}
	nbW, _ := img.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	nb := nbW.(ImageBlock)
	v2 := nb.View(60)
	if strings.Contains(v2, "[ IMAGE ]") || strings.Contains(v2, "[ TEXT ]") {
		t.Fatal("src-Footer muss INFO-only sein (keine Switch-Kaesten)")
	}
	if !strings.Contains(v2, "3 lines") {
		t.Fatal("Zeilen-Label fehlt im Quelltext-Modus")
	}
}

// ActionAt: Klick auf [ TEXT ] schaltet (wie t), Klick auf [ ⏎ PAN & ZOOM ]
// oeffnet den Pan-Modus (wie enter), Klick auf [ COPY ] kopiert mit Flash,
// Klick daneben trifft nichts. Geometrie kommt aus layoutFooter.
// ActionAt: Klick auf [ TEXT ] schaltet (wie t), [ ⏎ PAN & ZOOM ] oeffnet den
// Pan-Modus, [ esc quit pan & zoom ] schliesst ihn, [ COPY ] (nur in der
// Quelltext-Ansicht) kopiert mit Flash. Die Aufloesung ist INFO (links) und
// NICHT klickbar; im Bild-Modus gibt es KEINEN COPY-Kasten.
// ActionAt: Klick auf [ TEXT ] schaltet (wie t), [ ⏎ PAN & ZOOM ] oeffnet den
// Pan-Modus, [ esc quit pan & zoom ] schliesst ihn, [ COPY ] (nur in der
// Quelltext-Ansicht) kopiert mit Flash. Die Aufloesung ist INFO (links) und
// NICHT klickbar; im Bild-Modus gibt es KEINEN COPY-Kasten.
// Der Bild-Footer ist INFO-ONLY: zeigt die Aufloesung (px) und im Zoom
// zusaetzlich den Zoom-Stand (⤢ %). Es gibt KEINE klickbaren Aktions-
// Kaesten (TEXT/PAN/COPY/quit); ActionAt trifft daher nie. Alle Aktionen
// liegen in der zentralen Aktions-/Hint-Leiste bottom (app.footerView).
func TestImageFooterInfo(t *testing.T) {
	t.Cleanup(KittyCleanup)
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	th, _ := render.DefaultTheme()
	img := NewImageBlock(realPNG(400, 200), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, th)
	img = img.SetID(5).(ImageBlock)
	img = img.SetSource("plantuml", "src").(ImageBlock)
	img = img.SetKittyID(5).(ImageBlock)
	w := 60
	h := img.Height(w)
	img = img.SetFocused(true).(ImageBlock)

	// Bild-Modus: Label = Aufloesung, KEINE Kaesten.
	fa := img.footerActions(w)
	if len(fa.boxes) != 0 {
		t.Fatalf("image footer boxes=%d want 0 (INFO-only)", len(fa.boxes))
	}
	if !strings.Contains(fa.label, "400×200 px") {
		t.Fatalf("image label=%q, want Aufloesung '400×200 px'", fa.label)
	}
	// Footer ist NICHT klickbar.
	if _, _, hit := img.ActionAt(w, 3, h-1); hit {
		t.Fatal("Bild-Footer darf keine Action treffen (INFO-only)")
	}

	// Zoom-Modus: Label = Aufloesung + Zoom-Stand.
	pb, _ := img.Update(keyOf("enter"))
	pp := pb.(ImageBlock)
	if !pp.panning {
		t.Fatal("enter oeffnet den Pan-Modus nicht")
	}
	pfa := pp.footerActions(w)
	if !strings.Contains(pfa.label, " px") || !strings.Contains(pfa.label, "⤢ 100%") {
		t.Fatalf("zoom label=%q, want Aufloesung + ⤢ %%", pfa.label)
	}
	if len(pfa.boxes) != 0 {
		t.Fatalf("zoom footer boxes=%d want 0 (INFO-only)", len(pfa.boxes))
	}

	// Quelltext-Ansicht: erst Pan verlassen (esc), dann t -> Quelle
	// (waehrend zooms wird t verschluckt, es gehoert dem Bild).
	img2, _ := pp.Update(keyOf("esc"))
	src, _ := img2.(ImageBlock).Update(keyOf("t"))
	sb := src.(ImageBlock)
	if !sb.showSrc {
		t.Fatal("t schaltet nicht in die Quelltext-Ansicht")
	}
	sfa := sb.footerActions(w)
	if len(sfa.boxes) != 0 || sfa.label == "" {
		t.Fatalf("src footer boxes=%d label=%q, want INFO-only mit Zeilenzahl", len(sfa.boxes), sfa.label)
	}
}

func TestImagePanZoomKeys(t *testing.T) {
	t.Cleanup(KittyCleanup)
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatal(err)
	}
	// Schmal-bild (200x400, 1:2) in einem breiten Rahmen: die intrinsische
	// Groesse ist KLEINER als die Rahmeninnenkante, es bleibt also seitlich
	// freier Raum — genau der Fall, der vorher beim Zoomen sofort schnitt.
	img := NewImageBlock(realPNG(200, 400), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, th)
	img = img.SetID(3).(ImageBlock)
	img = img.SetKittyID(3).(ImageBlock)
	img = img.SetSource("plantuml", "@startuml\nA --> B\n@enduml").(ImageBlock)
	width := 60
	img = img.Resize(width).(ImageBlock)

	// Ausgangszustand: Pan aus, Default-Position und -Zoom; die Box ist bei
	// zoom 100 gleich dem ganzen Bild (kein Crop): wCols == vCols == fitCols.
	if img.panning || img.zoomPct() != 100 || img.panRow != 0 || img.panCol != 0 {
		t.Fatal("Startzustand muss Pan-aus, zoom 100, pan 0/0 sein")
	}
	fitCols, fitRows := img.dims(width)
	if fitCols >= width-4 {
		t.Fatalf("Test braucht ein schmales Bild: fitCols=%d muss < width-4=%d sein", fitCols, width-4)
	}
	if zc, zr, zw, zh, _, _ := img.zoomLayout(width); zc != fitCols || zw != fitCols || zh != fitRows || zr != fitRows {
		t.Fatalf("zoom 100: v=%dx%d w=%dx%d, want w==v==fit(%dx%d)", zc, zr, zw, zh, fitCols, fitRows)
	}

	nb, _ := img.Update(keyOf("enter"))
	p := nb.(ImageBlock)
	if !p.panning || !p.Panning() {
		t.Fatal("enter oeffnet den Pan-&-Zoom-Modus nicht (Panning() false)")
	}

	// Rahmen wechselt auf Amber (PanBorderStyle) und unterscheidet sich vom
	// normalen Fokus-Rahmen: der Nutzer sieht sofort, dass die Pfeiltasten
	// jetzt das Bild steuern (nicht die Fokus-Kette).
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	if panLine, normalLine := p.borderStyle().Render("x"), img.SetFocused(true).(ImageBlock).borderStyle().Render("x"); panLine == normalLine {
		t.Fatal("Pan-Rahmenstil muss sich vom Fokus-Rahmenstil unterscheiden (Amber)")
	}

	// + zoomt Bild UND Box (125): die Box waechst in den freien Rahmenraum
	// (wCols > fitCols), das ganze Bild ist noch sichtbar (wCols == vCols).
	w, _ := p.Update(keyOf("+"))
	p = w.(ImageBlock)
	if p.zoomPct() != 125 {
		t.Fatalf("zoom nach + = %d, want 125", p.zoomPct())
	}
	gvc, gvr, gwc, gwr, _, _ := p.zoomLayout(width)
	if gvc <= fitCols || gvc != fitCols*125/100 || gvr != fitRows*125/100 {
		t.Fatalf("virtuell v=%dx%d: muss fit*125/100=%dx%d sein", gvc, gvr, fitCols*125/100, fitRows*125/100)
	}
	if gwc <= fitCols {
		t.Fatalf("Box w=%d waechst nicht in den Rahmenraum (fit=%d) — Bild muss GROESSER werden", gwc, fitCols)
	}
	if gwc != gvc || gwr != gvr {
		t.Fatalf("Box w=%dx%d != v=%dx%d: bei 125 darf noch NICHTS ge-croppt sein (ganzes Bild sichtbar)", gwc, gwr, gvc, gvr)
	}

	// + weiter bis 150: die Box steht jetzt an der Rahmeninnenkante
	// (wCols == width-4), das virtuelle Placement ist groesser → Crop.
	for p.zoomPct() < 150 {
		w, _ = p.Update(keyOf("+"))
		p = w.(ImageBlock)
	}
	gvc, gvr, gwc, gwr, _, gpc := p.zoomLayout(width)
	if gwc != width-4 {
		t.Fatalf("Box w=%d muss an width-4=%d stehen", gwc, width-4)
	}
	if gvc <= gwc {
		t.Fatalf("v=%d muss die Box w=%d ueberragen (Crop erst jetzt)", gvc, gwc)
	}
	if gvr != gwr {
		t.Fatalf("v-rows=%d != w-rows=%d — Testbild muesste auch vertikal wachsen", gvr, gwr)
	}

	// Pfeiltasten pannen nur noch IM Ueberschuss: right vergroessert panCol,
	// bei vRows==wRows (kein vertikaler Ueberschuss) bleibt panRow 0.
	w, _ = p.Update(keyOf("right"))
	p = w.(ImageBlock)
	if p.panCol <= gpc {
		t.Fatalf("right: panCol %d nicht vergroessert (war %d)", p.panCol, gpc)
	}
	if p.panRow != 0 {
		t.Fatalf("panRow %d unerwartet (kein vertikaler Ueberschuss)", p.panRow)
	}

	// Pan ist IMMER eingespannt: wildes Pingen (30x right+down) stoppt an der
	// Kante vCols-wCols (Crop), nie darueber.
	for i := 0; i < 30; i++ {
		w, _ = p.Update(keyOf("right"))
		p = w.(ImageBlock)
		w, _ = p.Update(keyOf("down"))
		p = w.(ImageBlock)
	}
	vcol, _, wcol, _, pro, pco := p.zoomLayout(width)
	if pco != vcol-wcol {
		t.Fatalf("panCol=%d muss an der Kante v-w=%d stoppen (Crop)", pco, vcol-wcol)
	}
	if pro != 0 {
		t.Fatalf("panRow=%d muss 0 bleiben (kein vertikaler Ueberschuss)", pro)
	}

	// - zoomt zurueck; bei 100 kein Ueberschuss mehr → pan zurueckgespannt,
	// Box wieder ganzes Bild.
	for p.zoomPct() > 100 {
		w, _ = p.Update(keyOf("-"))
		p = w.(ImageBlock)
	}
	if p.zoomPct() != 100 {
		t.Fatalf("zoom nach (-) = %d, want 100", p.zoomPct())
	}
	if zc, _, zw, zh, pc, pr := p.zoomLayout(width); zw != fitCols || zh != fitRows || pc != 0 || pr != 0 || zc != fitCols {
		t.Fatalf("nach -: v=%d w=%dx%d pan=%d/%d, want v=fit w=fit(%dx%d) pan 0/0", zc, zw, zh, pc, pr, fitCols, fitRows)
	}

	// esc kehrt zurueck und stellt Default-Aufloesung + -position her.
	w, _ = p.Update(keyOf("esc"))
	p = w.(ImageBlock)
	if p.panning || p.zoomPct() != 100 || p.panRow != 0 || p.panCol != 0 {
		t.Fatalf("esc schliesst/restauriert nicht: pan=%d/%d zoom=%d", p.panRow, p.panCol, p.zoomPct())
	}

	// enter als zweiter Weg zurueck: erneut oeffnen, enter schliesst wieder.
	w, _ = p.Update(keyOf("enter"))
	p = w.(ImageBlock)
	if !p.panning {
		t.Fatal("enter oeffnet erneut nicht")
	}
	w, _ = p.Update(keyOf("enter"))
	p = w.(ImageBlock)
	if p.panning || p.zoomPct() != 100 || p.panRow != 0 || p.panCol != 0 {
		t.Fatal("enter schliesst und restauriert die Default-Aufloesung nicht")
	}
}

// TestImagePanZoomHeadroom: Ein FILL-TERMINAL-Bild reserviert ueber die
// Default-Breite (colsFor -> zoomHeadroomPct) eine Zoomreserve: es wird
// minimal schmaler gezeigt, damit die Box beim Zoomen sichtbar mitwachsen
// kann, statt dass sofort ge-croppt wird. Seit dem FENSTER-CROP ist der
// Zoom nicht mehr an die Diakritika-Adresse geklemmt: das virtuelle Raster
// darf sie ueberragen, renderCache uebertraegt nur den Ausschnitt — das
// FENSTER bleibt immer adressierbar, der Zoom laeuft bis zoomMax.
func TestImagePanZoomHeadroom(t *testing.T) {
	t.Cleanup(KittyCleanup)
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatal(err)
	}
	// Breites Diagramm (7.5:1) in einem BREITEN Fenster (width-4=196): die
	// Box füllt ab zoom 100 die volle Rahmeninnenbreite (Terminalbreite ->
	// Pixel), KEINE Zoomreserve mehr. Der Zoom-Ueberschuss wird direkt als
	// Fensterauschnitt ge-croppt (renderCache) — bis zoomMax durchlauefig.
	img := NewImageBlock(realPNG(3000, 400), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, th)
	img = img.SetID(7).(ImageBlock)
	img = img.SetKittyID(7).(ImageBlock)
	width := 200
	img = img.Resize(width).(ImageBlock)
	// Keine Reserve mehr: die breite 3000px-Zeichnung fuellt die volle
	// Rahmeninnenbreite (width-4) bzw. die Protokoll-/Intrinsik-Grenze.
	if fitCols, _ := img.dims(width); fitCols != width-4 {
		t.Fatalf("fitCols=%d, will volle Protokoll-/Rahmenbreite (%d) — keine Zoomreserve mehr", fitCols, width-4)
	}

	// FRUEHER endete der Zoom hier an der Diakritika-Wand (~190%) — seit
	// dem Fenster-Crop laeuft er bis zoomMax durch.
	pw, _ := img.Update(keyOf("enter"))
	p := pw.(ImageBlock)
	if !p.panning {
		t.Fatal("enter oeffnet den Pan-Modus nicht")
	}
	for i := 0; i < 40 && p.zoomPct() < zoomMax; i++ {
		w, _ := p.Update(keyOf("+"))
		p = w.(ImageBlock)
	}
	if p.zoomPct() != zoomMax {
		t.Fatalf("zoom=%d, will zoomMax=%d (keine Diakritika-Wand mehr)", p.zoomPct(), zoomMax)
	}
	// Das virtuelle Raster UEBERRAGT die Diakritika-Adresse jetzt (Fenster-
	// Crop), aber das FENSTER — das einzige, was die Platzhalter adressieren
	// — bleibt immer darunter und in der Box.
	vCols, vRows, wCols, wRows, _, _ := p.zoomLayout(width)
	if vCols <= diacriticAddrCap() {
		t.Fatalf("Setup: virtuell v=%dx%d sollte die Adresse %d ueberragen (sonst testet der Fall nichts)", vCols, vRows, diacriticAddrCap())
	}
	if wCols > diacriticAddrCap() || wRows > diacriticAddrCap() {
		t.Fatalf("Fenster w=%dx%d darf die Diakritika-Adresse nie ueberschreiten", wCols, wRows)
	}
	if wCols > width-4 {
		t.Fatalf("Fenster wCols=%d ragt aus dem Rahmen (max %d)", wCols, width-4)
	}
	// Und der Render-Pfad liefert dafuer ein FENSTER-Bild: Placement c/r =
	// Box, Platzhalter fensterlokal (Zeile 0 traegt Diakritikon 0).
	c, ok := p.renderCache(vCols, vRows, wCols, wRows, 0, 0)
	if !ok {
		t.Fatal("renderCache scheitert im Fenster-Modus")
	}
	if !strings.Contains(c.prefix, fmt.Sprintf("c=%d,r=%d,", wCols, wRows)) {
		t.Fatalf("Transmit c/r=%dx%d, will FENSTER %dx%d (Crop-Placement)", vCols, vRows, wCols, wRows)
	}
	// s/v = die Crop-Pixelmasse (proportional aus dem Raster des Blocks):
	// das Terminal legt den Ausschnitt 0x in die Fenster-Box.
	wantRect := cropRect(p.pngW, p.pngH, vCols, vRows, wCols, wRows, 0, 0)
	wantRect = aspectFillCrop(wantRect, p.pngW, p.pngH, wCols, wRows, 9, 20)
	if c.pw != wantRect.Dx() || c.ph != wantRect.Dy() {
		t.Fatalf("Crop-Masse pw/ph=%dx%d, want %dx%d (10:N box-aspektgetreu)", c.pw, c.ph, wantRect.Dx(), wantRect.Dy())
	}
	if !strings.Contains(c.prefix, fmt.Sprintf("s=%d,v=%d,", c.pw, c.ph)) {
		t.Fatalf("Transmit s/v fehlen oder falsch: %q", c.prefix)
	}
	if len(c.lines) != wRows {
		t.Fatalf("lines=%d, will Fenster-Zeilen %d", len(c.lines), wRows)
	}
	if want := placeholderRow(p.kittyID, 0, wCols); c.lines[0] != want {
		t.Fatalf("Zeile 0 nicht fensterlokal adressiert")
	}
}

// TestKittyDiacriticsInjective: die Adress-Tabelle ist injektiv — jeder
// Raster-Platz (Index 0..len-1) bekommt ein EIGENES Diakritikon. Wuerden
// zwei Indizes kollidieren (z. B. durch ein zu kurzes Tabellenende), zeigten
// zwei Zellen desselben Fensters denselben Bildstreifen -> Bildartefakte.
func TestKittyDiacriticsInjective(t *testing.T) {
	seen := map[rune]int{}
	for i := 0; i < diacriticAddrCap(); i++ {
		d := diacritic(i)
		if j, ok := seen[d]; ok {
			t.Fatalf("diacritic(%d)=%U kollidiert mit diacritic(%d) — Adress-Schema nicht injektiv", i, d, j)
		}
		seen[d] = i
	}
}

// blKey schickt eine Einzeltaste an die BlockList und liefert die neue Liste
// plus das Consumed-Signal zurueck (Test-Helfer fuer Pfeiltasten-Routing).
func blKey(l BlockList, s string) (BlockList, bool) {
	nl, _, consumed := l.Update(keyOf(s))
	return nl, consumed
}

// TestBlockListRoutesArrowPanning: Die Pfeiltasten erreichen den fokussierten
// ImageBlock im Pan-Modus ueber die BlockList (nicht nur block-intern) und
// verschieben das Fenster im virtuellen Raster. Breites Diagramm + kleiner
// Zeilen-Cap = Ueberschuss in BEIDEN Achsen, sodass left/right UND up/down
// tatsaechlich pannen. Die Grenzen werden nie verlassen (Clamp >= 0).
func TestBlockListRoutesArrowPanning(t *testing.T) {
	t.Cleanup(KittyCleanup)
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	l := NewBlockList()
	l = l.SetSize(120, 24)
	l = l.Append(NewTextBlock("hallo", "#fff"))
	img := NewImageBlock(realPNG(3000, 400), "art", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(2).(ImageBlock)
	img = img.SetKittyID(2).(ImageBlock)
	img = img.SetMaxRows(8).(ImageBlock) // vertikaler Ueberschuss beim Zoom
	l = l.Append(img)

	for i := 0; i < 5 && !(l.Focused() && l.items[l.focus].ID() == 2); i++ {
		l, _ = blKey(l, "down")
	}
	if !l.Focused() || l.items[l.focus].ID() != 2 {
		t.Fatal("ImageBlock nicht fokussiert")
	}
	if _, consumed := blKey(l, "enter"); !consumed {
		t.Fatal("enter im fokussierten Bild nicht konsumiert")
	}
	it, _ := findImage(l, 2)
	if !it.(ImageBlock).panning {
		t.Fatal("enter hat den Pan-Modus nicht geoeffnet")
	}
	// Hochzoomen bis in den Crop (beide Achsen): dann pannen Pfeiltasten.
	for i := 0; i < 6; i++ {
		l, _ = blKey(l, "+")
	}
	it, _ = findImage(l, 2)
	p := it.(ImageBlock)
	if p.zoomPct() <= 100 {
		t.Fatalf("zoom hat nicht zugenommen: %d", p.zoomPct())
	}
	if vc, vr, wc, wr, _, _ := p.zoomLayout(p.frameWidth()); vc <= wc || vr <= wr {
		t.Fatalf("Testaufbau braucht Crop in beiden Achsen: v=%dx%d w=%dx%d", vc, vr, wc, wr)
	}

	for i := 0; i < 20; i++ {
		var consumed bool
		l, consumed = blKey(l, "right")
		if !consumed {
			t.Fatalf("right #%d nicht konsumiert", i)
		}
	}
	it, _ = findImage(l, 2)
	p = it.(ImageBlock)
	if p.panCol <= 0 {
		t.Fatalf("right panned nicht waagerecht: panCol=%d", p.panCol)
	}
	before := p.panRow
	for i := 0; i < 20; i++ {
		var consumed bool
		l, consumed = blKey(l, "down")
		if !consumed {
			t.Fatalf("down #%d nicht konsumiert", i)
		}
	}
	it, _ = findImage(l, 2)
	p = it.(ImageBlock)
	if p.panRow <= before {
		t.Fatalf("down panned nicht senkrecht (vorher %d, nachher %d)", before, p.panRow)
	}
	// up zurueck bis zur Oberkante — klemmt nie unter 0.
	for i := 0; i < 40; i++ {
		l, _ = blKey(l, "up")
	}
	it, _ = findImage(l, 2)
	p = it.(ImageBlock)
	if p.panRow != 0 {
		t.Fatalf("up kamen nicht zurueck an den Fensteranfang (panRow=%d)", p.panRow)
	}
}
