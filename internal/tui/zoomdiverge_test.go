package tui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func plusKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}} }

func zoomIn21x(b ImageBlock) ImageBlock {
	w, _ := b.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Pan-Modus oeffnen
	w, _ = w.Update(plusKey())
	for i := 0; i < 20; i++ {
		w, _ = w.Update(plusKey())
	}
	return w.(ImageBlock)
}

// Der ⤢-Indikator muss den WIRKSAMEN Zoom zeigen. Frueher klemmte der
// Render-Pfad per diacriticSafeZoom(dims(breite)) und ein veralteter
// frameWidth (80er-Fallback) liess den Indikator bis ~390% zaehlen, obwohl
// das Raster bei ~190% stoppte. Seit dem Fenster-Crop ist zoomMax die
// einzige Grenze — Indikator und Raster wachsen stufenweise zusammen bis
// 400%, auch fuer breite Diagramme.
func TestZoomIndicatorMatchesEffectiveZoom(t *testing.T) {
	caps := TermCaps{CellW: 9, CellH: 20}
	// Breites Diagramm (1000x400 px): dims(200)=156 Spalten — frueher
	// Diakritika-Grenze bei 190%, heute voller Zoom bis zoomMax.
	img := NewImageBlock(realPNG(1000, 400), "roh", true, caps, nil, nil)
	img = img.SetID(1).(ImageBlock)
	img = img.SetKittyID(1).(ImageBlock)
	img = img.SetMaxRows(35).(ImageBlock)

	// Szenario A (frueherer Bug-Zustand): kein Resize, frameWidth=80.
	stale := zoomIn21x(img)
	if stale.zoomPct() != zoomMax {
		t.Fatalf("stored zoom=%d, will zoomMax=%d auch am 80er-Fallback", stale.zoomPct(), zoomMax)
	}
	if stale.effZoomPct(200) != zoomMax {
		t.Fatalf("effZoomPct(200)=%d, will %d", stale.effZoomPct(200), zoomMax)
	}
	label, _ := stale.footBoxes(200)
	if !strings.Contains(label, "⤢ "+strconv.Itoa(zoomMax)+"%") {
		t.Fatalf("Indikator=%q, will ⤢ %d%%", label, zoomMax)
	}

	// Szenario B: mit gesetzter Breite identisch — gespeicherter ==
	// wirksamer Zoom, keine Divergenz mehr.
	sized := zoomIn21x(img.Resize(200).(ImageBlock))
	if sized.zoomPct() != sized.effZoomPct(200) {
		t.Fatalf("stored=%d != effektiv=%d bei Breite 200",
			sized.zoomPct(), sized.effZoomPct(200))
	}
}

// Bug (Ursache des alten Indikator-Luegens): applyDiagrams baute den
// ImageBlock OHNE Resize — lastWidth blieb 0 (frameWidth-Fallback 80),
// obwohl der Render-Pfad mit der echten Chat-Breite rechnet.
func TestZoomClampUsesChatWidthAfterConversion(t *testing.T) {
	r := newRig(t).size(200, 59)
	r.seed("```plantuml\nAlice -> Bob: hi\n```")
	id := r.m.diagramJobs()[0].id

	l, n := r.m.chat.applyDiagrams([]diagramResult{{
		id:    id,
		lang:  "plantuml",
		ascii: "roh",
		src:   "Alice -> Bob: hi",
		png:   realPNG(1000, 400),
		ok:    true,
	}}, true, TermCaps{CellW: 9, CellH: 20})
	if n != 1 {
		t.Fatalf("applied=%d want 1", n)
	}
	ib, ok := findImage(l, id)
	if !ok {
		t.Fatal("kein ImageBlock nach Konvertierung")
	}
	if got := ib.(ImageBlock).frameWidth(); got != 200 {
		t.Fatalf("frameWidth=%d, want 200 (Chat-Breite)", got)
	}

	zb := zoomIn21x(ib.(ImageBlock))
	if zb.zoomPct() != zoomMax {
		t.Fatalf("stored zoom=%d, want %d — breites Diagramm zoomt seit dem Fenster-Crop durch",
			zb.zoomPct(), zoomMax)
	}
	label, _ := zb.footBoxes(200)
	if !strings.Contains(label, "⤢ "+strconv.Itoa(zoomMax)+"%") {
		t.Fatalf("Indikator=%q, will ⤢ %d%%", label, zoomMax)
	}
}

// cropRect: Das Fenster (pan, cols×rows) wird proportional ins Raster
// abgebildet — gleiche Stelle, die das Vollbild-Placement zeigen wuerde,
// immer im Raster, nie 0 breit/hoch.
func TestCropRectGeometry(t *testing.T) {
	// Vollbild: Fenster == virtuelles Raster -> ganzer Raster.
	if r := cropRect(3000, 400, 624, 36, 624, 36, 0, 0); r != image.Rect(0, 0, 3000, 400) {
		t.Fatalf("Vollbild-Crop=%v, will (0,0)-(3000,400)", r)
	}
	// Linkes oberes Viertel bei 200% (virtual 2x Fenster): exakt die Haelfte.
	if r := cropRect(3000, 400, 624, 36, 312, 18, 0, 0); r != image.Rect(0, 0, 1500, 200) {
		t.Fatalf("Halb-Crop=%v, will (0,0)-(1500,200)", r)
	}
	// Verschoben: pan (312,18) bei gleichem Fenster -> untere rechte Haelfte.
	if r := cropRect(3000, 400, 624, 36, 312, 18, 18, 312); r != image.Rect(1500, 200, 3000, 400) {
		t.Fatalf("Pan-Crop=%v, will (1500,200)-(3000,400)", r)
	}
	// Rundung + Rand: kleines Fenster im riesigen Raster bleibt >= 1px und
	// im Raster.
	r := cropRect(97, 31, 990, 310, 10, 3, 300, 900)
	if r.Dx() < 1 || r.Dy() < 1 || r.Max.X > 97 || r.Max.Y > 31 || r.Min.X < 0 || r.Min.Y < 0 {
		t.Fatalf("Rand-Crop=%v ausserhalb/leer (Raster 97x31)", r)
	}
	// Degeneriert: leeres virtuelles Raster darf nicht durch 0 teilen.
	if r := cropRect(50, 50, 0, 0, 5, 5, 0, 0); r.Dx() < 1 || r.Dy() < 1 {
		t.Fatalf("Div0-Crop=%v, will >= 1x1", r)
	}
}

// Fenster-Crop im Render-Pfad (PNG-Block, ohne resvg): Bei Zoom-Ueberschuss
// traegt der Transmit die FENSTER-Geometrie (c/r = Box, s/v = Crop-Pixel),
// die Platzhalter adressieren fensterlokal — der Pan-Schritt cropt neu,
// ohne erneut zu dekodieren (windowRaster-Reuse).
func TestRenderCacheWindowCrop(t *testing.T) {
	t.Cleanup(KittyCleanup)
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	img := NewImageBlock(realPNG(1000, 400), "roh", true, TermCaps{CellW: 9, CellH: 20}, nil, nil)
	img = img.SetID(11).(ImageBlock)
	img = img.SetKittyID(11).(ImageBlock)
	img = img.SetMaxRows(35).(ImageBlock)
	width := 120
	img = img.Resize(width).(ImageBlock)

	// Zoom 400%: virtual 4x, Box am Rahmen/Zeilen-Cap -> echter Ueberschuss.
	z := zoomIn21x(img)
	vCols, vRows, wCols, wRows, _, _ := z.zoomLayout(width)
	if vCols == wCols && vRows == wRows {
		t.Fatalf("Setup: kein Ueberschuss (v=%dx%d w=%dx%d)", vCols, vRows, wCols, wRows)
	}

	c, ok := z.renderCache(vCols, vRows, wCols, wRows, 0, 0)
	if !ok {
		t.Fatal("renderCache scheitert im Fenster-Modus")
	}
	if !strings.Contains(c.prefix, fmt.Sprintf("c=%d,r=%d,", wCols, wRows)) {
		t.Fatalf("Transmit c/r will FENSTER %dx%d (Crop-Placement): %q", wCols, wRows, c.prefix)
	}
	want := cropRect(z.pngW, z.pngH, vCols, vRows, wCols, wRows, 0, 0)
	want = aspectFillCrop(want, z.pngW, z.pngH, wCols, wRows, 9, 20)
	if c.pw != want.Dx() || c.ph != want.Dy() {
		t.Fatalf("Crop pw/ph=%dx%d, want %dx%d (10:N box-aspektgetreu)", c.pw, c.ph, want.Dx(), want.Dy())
	}
	// Platzhalter fensterlokal: Zeile 0 = Diakritikon 0 (U+0305), Zeile 1
	// = Index 1 (U+030D) — NICHT absolut um panRow versetzt.
	if c.lines[0] != placeholderRow(z.kittyID, 0, wCols) {
		t.Fatal("Zeile 0 nicht fensterlokal adressiert")
	}
	if c.lines[min(1, len(c.lines)-1)] != placeholderRow(z.kittyID, 1, wCols) {
		t.Fatal("Zeile 1 nicht fensterlokal adressiert")
	}

	// Pan-Schritt: neues Fenster im GLEICHEN virtuellen Raster -> neuer
	// Crop mit anderem Ausschnitt; das dekodierte Vollraster wird vom
	// Vorgaenger-Eintrag uebernommen (kein zweiter Decode noetig — hier
	// nur verhaltenstestbar: der neue Eintrag existiert und traegt andere
	// Pixelmasse/Pan-Adressen).
	panRow, panCol := max(vRows-wRows, 0), max(vCols-wCols, 0)
	c2, ok := z.renderCache(vCols, vRows, wCols, wRows, panRow, panCol)
	if !ok {
		t.Fatal("renderCache scheitert beim Pan-Schritt")
	}
	want2 := cropRect(z.pngW, z.pngH, vCols, vRows, wCols, wRows, panRow, panCol)
	want2 = aspectFillCrop(want2, z.pngW, z.pngH, wCols, wRows, 9, 20)
	if c2.pw != want2.Dx() || c2.ph != want2.Dy() || want2.Min.X <= want.Min.X {
		t.Fatalf("Pan-Crop pw/ph=%dx%d, want %dx%d rechts/unten von %v",
			c2.pw, c2.ph, want2.Dx(), want2.Dy(), want)
	}
	// Crop-Pixel sind wirklich der ANDERE Ausschnitt: decode + vergleichen.
	img1, err1 := png.Decode(bytes.NewReader(c.png))
	img2, err2 := png.Decode(bytes.NewReader(c2.png))
	if err1 != nil || err2 != nil {
		t.Fatalf("Crop-PNGs nicht dekodierbar: %v / %v", err1, err2)
	}
	b1, b2 := img1.Bounds(), img2.Bounds()
	if b1.Dx() != want.Dx() || b2.Dx() != want2.Dx() {
		t.Fatalf("Crop-PNG-Masse %v/%v, want %v/%v", b1, b2, want, want2)
	}
	// Der Original-Raster ist ein Regenbogen-Muster (x mod 256 im Rot-Kanal,
	// s. realPNG) — linker und rechter Ausschnitt MUSSEN sich unterscheiden.
	if b1.Dx() == b2.Dx() && b1.Dy() == b2.Dy() && imagesEqual(img1, img2) && want.Min.X != want2.Min.X {
		t.Fatal("Pan-Schritt liefert denselben Ausschnitt — Fenster bewegt sich nicht")
	}
}

func imagesEqual(a, b image.Image) bool {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return false
	}
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			ar, ag, abl, _ := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			br, bg, bbl, _ := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			if ar != br || ag != bg || abl != bbl {
				return false
			}
		}
	}
	return true
}
