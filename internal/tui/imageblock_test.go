package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

// realPNG baut ein tatsaechlich dekodierbares PNG (Halbblock-Renderer
// dekodiert es — ein reiner Header reicht nicht).
func realPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestPNGDims(t *testing.T) {
	w, h, ok := render.PNGDims(realPNG(320, 200))
	if !ok || w != 320 || h != 200 {
		t.Fatalf("PNGDims = %d,%d,%v", w, h, ok)
	}
	if _, _, ok := render.PNGDims([]byte("kein png")); ok {
		t.Fatal("garbage accepted as png")
	}
}

// TestImageBlockResolution (FR-27): die Halbblock-Grafik hat die Breite der
// inneren Block-Spalten und eine zur Pixelproportion passende Zeilenzahl —
// das Bild ist lagerichtig und bleibt reiner Text (scrollbar).
func TestImageBlockResolution(t *testing.T) {
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	img := NewImageBlock(realPNG(200, 100), "roh", nil, r.m.theme)
	img = img.SetID(7).(ImageBlock)

	width := 44
	inner := width - 4 // 40
	view := img.View(width)
	if !strings.Contains(view, "\u2580") {
		t.Fatal("halfblock art missing")
	}
	if strings.Contains(view, "\x1b_G") {
		t.Fatal("must not emit graphics escapes (pure text)")
	}
	wantRows := (100*inner + 200/2) / 200 / 2
	if got := len(strings.Split(view, "\n")) - 2; got != wantRows {
		t.Fatalf("art rows = %d, want %d", got, wantRows)
	}
	if img.Height(width) != wantRows+2 {
		t.Fatalf("Height=%d want %d", img.Height(width), wantRows+2)
	}
	if img.Markdown() != "roh" {
		t.Fatalf("Markdown raw = %q", img.Markdown())
	}
}

// TestImageBlockFitRows (FR-Zoom): sehr grosse Diagramme werden auf die
// Fit-Hoehe herunterskaliert. Klein genug Bilder werden nicht vergroessert.
func TestImageBlockFitRows(t *testing.T) {
	r := newRig(t)
	img := NewImageBlock(realPNG(200, 100), "roh", nil, r.m.theme).WithFitRows(4)
	width := 44
	// cols = min(inner 40, 2*200*4/100=16) = 16; rows=(100*16+99)/200/2 = 4
	if img.Height(width) != 6 {
		t.Fatalf("fitted Height=%d want 6", img.Height(width))
	}
	// fitRows die groesser sind als die naturale Hoehe vergroessern NIE:
	// kleine Bilder bleiben bei naturaller Aufloesung (nicht unscharf).
	small := NewImageBlock(realPNG(20, 10), "roh", nil, r.m.theme).WithFitRows(100)
	// natural cols=20 (<=inner 40), rows=(10*20+9)/20/2=5, Height=7
	if small.Height(44) != 7 {
		t.Fatalf("small Height=%d want 7 (natural, not enlarged)", small.Height(44))
	}
}

// TestImageBlockZoom (Bonus-FR): - verkleinert zu grosse Diagramme zu einem
// Ueberblick, + zoemt zur vollen Aufloesung zurueck. Ueber die naturale
// Breite hinaus gewinnt bei Halbblock nichts (Terminal-Breite ist die
// Obergrenze), also wird nie unscharf aufgeblast.
func TestImageBlockZoom(t *testing.T) {
	r := newRig(t)
	img := NewImageBlock(realPNG(200, 100), "roh", nil, r.m.theme).SetID(3).SetFocused(true).(ImageBlock)
	width := 44
	inner := width - 4
	natural := (100*inner + 100) / 200 / 2
	if img.Height(width) != natural+2 {
		t.Fatalf("natural Height=%d want %d", img.Height(width), natural+2)
	}

	// - zoomt hinaus -> Ueberblick (weniger Zeilen).
	var w Widget = img
	w, _ = img.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	z := w.(ImageBlock)
	if z.zoomPct != 75 {
		t.Fatalf("zoom after - = %d, want 75", z.zoomPct)
	}
	if z.Height(width) >= img.Height(width) {
		t.Fatal("- must reduce height (Ueberblick)")
	}
	// + zoemt zurueck zur vollen Aufloesung (nie kleiner als natural).
	w, _ = z.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	z2 := w.(ImageBlock)
	if z2.zoomPct != 100 {
		t.Fatalf("zoom after + = %d, want 100", z2.zoomPct)
	}
	if z2.Height(width) != natural+2 {
		t.Fatalf("+ did not restore full res: %d want %d", z2.Height(width), natural+2)
	}
	// Bei voller Aufloesung hat + keine Wirkung (kein Aufblasen).
	w, _ = z2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if w.(ImageBlock).zoomPct != 100 {
		t.Fatal("+ must be bounded at natural")
	}
}

// TestImageBlockToggleRaw (Bonus-FR): 't' blendet zwischen Grafik und
// Roh-Quelltext um; der Rohblick enthaelt den Quelltext und keine Halbblocks.
func TestImageBlockToggleRaw(t *testing.T) {
	r := newRig(t)
	img := NewImageBlock(realPNG(200, 100), "Alice -> Bob: hi", nil, r.m.theme).
		SetID(9).SetFocused(true).(ImageBlock)
	if img.showRaw {
		t.Fatal("image mode by default")
	}
	var w Widget = img
	w, _ = img.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	raw := w.(ImageBlock)
	if !raw.showRaw {
		t.Fatal("t did not switch to raw")
	}
	if !strings.Contains(raw.View(40), "Alice -> Bob: hi") {
		t.Fatal("raw view lost the source")
	}
	if strings.Contains(raw.View(40), "\u2580") {
		t.Fatal("raw view must not render halfblocks")
	}
}

// TestImageBlockKeysThroughBlockList: Zoom/Roh-Tasten werden einem
// fokussierten ImageBlock ueber BlockList.handleKey zugestellt (FR-Zoom +
// Bonus-FR togglen), ohne den Nicht-Fokus-Tippfluss zu stoeren.
func TestImageBlockKeysThroughBlockList(t *testing.T) {
	r := newRig(t)
	img := NewImageBlock(realPNG(200, 100), "Alice -> Bob: hi", nil, r.m.theme).SetID(5)
	l := BlockList{items: []Widget{img.SetFocused(true)}}

	// '-' zoomt hinaus.
	l2, _, _ := l.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	ib, ok := l2.items[0].(ImageBlock)
	if !ok || ib.zoomPct != 75 {
		t.Fatalf("zoom after - = %d/%v, want 75", ib.zoomPct, ok)
	}
	// 't' toggelt auf Roh-Text.
	l3, _, ok := l2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if !ok || !l3.items[0].(ImageBlock).showRaw {
		t.Fatalf("t did not toggle raw (handled=%v)", ok)
	}
	// Ohne Fokus bleibt 't' unberuehrt (Tippen im Eingabefeld).
	u := BlockList{items: []Widget{img}, focus: -1}
	if _, _, handled := u.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}); handled {
		t.Fatal("unfocused blocklist must not swallow 't'")
	}
}

// TestApplyDiagrams (FR-27): png-Ergebnis wird zum ImageBlock (Halbblock,
// immer darstellbar), nur-ASCII wird im CodeBlock ersetzt.
func TestApplyDiagrams(t *testing.T) {
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	id := r.m.diagramJobs()[0].id
	l, n := r.m.chat.applyDiagrams([]diagramResult{{id: id, png: realPNG(200, 100), ascii: "Alice -> Bob: hi", ok: true}})
	if n != 1 {
		t.Fatalf("apply=%d", n)
	}
	img, ok := findImage(l, id)
	if !ok {
		t.Fatal("block is not an ImageBlock")
	}
	if img.Markdown() != "Alice -> Bob: hi" {
		t.Fatalf("markdown raw wrong: %q", img.Markdown())
	}
}

// TestImagesEnabledOverride: force/off und Default (Halbblock => immer).
func TestImagesEnabledOverride(t *testing.T) {
	m := Model{}
	t.Setenv("HERON_IMAGES", "force")
	if !m.imagesEnabled() {
		t.Fatal("force must enable")
	}
	t.Setenv("HERON_IMAGES", "off")
	if m.imagesEnabled() {
		t.Fatal("off must disable")
	}
	t.Setenv("HERON_IMAGES", "")
	if !m.imagesEnabled() {
		t.Fatal("default: halfblock is pure text, images on")
	}
}

// TestSeedQueuesDiagrams (FR-6/27/28): --seed haengt Diagramm-Fences an;
// der erste Frame (WindowSizeMsg) loest die Konvertierung aus.
func TestSeedQueuesDiagrams(t *testing.T) {
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```\n\nText.\n\n```mermaid\nflowchart LR\n```")
	if !r.m.seedDiagrams {
		t.Fatal("seed did not queue diagram conversion")
	}
	if n := len(r.m.diagramJobs()); n != 2 {
		t.Fatalf("want 2 diagram jobs (plantuml+mermaid), got %d", n)
	}
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	if r.m.seedDiagrams {
		t.Fatal("flag not consumed by the first frame")
	}
	if len(r.m.diagramJobs()) != 0 {
		t.Fatal("diagram jobs survived the first frame")
	}
	r.contains("Alice")
}
