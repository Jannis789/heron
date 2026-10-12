package tui

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func fakePNG(w, h int) []byte {
	b := []byte("\x89PNG\r\n\x1a\n")
	b = append(b, 0, 0, 0, 13)
	b = append(b, []byte("IHDR")...)
	var len4 [4]byte
	binary.BigEndian.PutUint32(len4[:], uint32(w))
	b = append(b, len4[:]...)
	var len4h [4]byte
	binary.BigEndian.PutUint32(len4h[:], uint32(h))
	b = append(b, len4h[:]...)
	return append(b, bytes.Repeat([]byte{0}, 8)...)
}

func TestPNGDims(t *testing.T) {
	w, h, ok := pngDims(fakePNG(320, 200))
	if !ok || w != 320 || h != 200 {
		t.Fatalf("pngDims = %d,%d,%v", w, h, ok)
	}
	if _, _, ok := pngDims([]byte("kein png")); ok {
		t.Fatal("garbage accepted as png")
	}
}

// TestImageBlockUsesPlaceholders (FR-27): mit Platzhalter-Faehigkeit steht
// das Bild als Zellen im Textfluss — es verschiebt sich beim Scrollen mit
// und die Groesse folgt der echten Zellpixeologia (Aspekt korrekt).
func TestImageBlockUsesPlaceholders(t *testing.T) {
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	caps := TermCaps{Placeholders: true, CellW: 10, CellH: 20}
	img := NewImageBlock(fakePNG(80, 40), "roh", true, caps, nil, r.m.theme)
	img = img.SetID(7).(ImageBlock)

	view := img.View(40)
	cols := 38
	rows := rowsFor(80, 40, cols, 10, 20) // 9
	if !strings.Contains(view, "a=p,U=1,i=7,q=2,c=38,r=9;") {
		t.Fatalf("placement control missing:\n%q", view[:80])
	}
	if n := strings.Count(view, placeholderChar); n != cols*rows {
		t.Fatalf("placeholder cells = %d, want %d", n, cols*rows)
	}
	if !strings.Contains(view, "a=t,f=100,i=7,q=2,s=80,v=40,") {
		t.Fatal("transmit escape missing")
	}
	if img.Height(40) != rows {
		t.Fatalf("Height=%d want %d", img.Height(40), rows)
	}

	// Aspekt folgt der Zellgroesse: breite Zellen -> weniger Zeilen.
	wide := TermCaps{Placeholders: true, CellW: 20, CellH: 10}
	if h := NewImageBlock(fakePNG(80, 40), "roh", true, wide, nil, r.m.theme).Height(40); h != 24 {
		t.Fatalf("wide-cell rows = %d, want 24 (capped)", h)
	}
}

// TestFallbackWithoutPlaceholders: ohne Platzhalter rendert der Block den
// ASCII-/Roh-Text und keine Graphics-Escapes.
func TestFallbackWithoutPlaceholders(t *testing.T) {
	r := newRig(t)
	img := NewImageBlock(fakePNG(80, 40), "roh text", true, TermCaps{}, nil, r.m.theme)
	view := img.View(80)
	if strings.Contains(view, "\x1b_G") {
		t.Fatal("fallback must not contain graphics escapes")
	}
	if !strings.Contains(stripANSI(view), "roh text") {
		t.Fatal("fallback lost the text")
	}
}

// TestApplyDiagramsPlaceholderCaps: mit Faehigkeit wird der CodeBlock zum
// ImageBlock, ohne bleibt der Code stehen (Roh-Fallback). Jede Phase in
// ihrem eigenen Rig — applyDiagrams mutiert das geteilte Backing-Array,
// daher darf der zweite Lauf nicht vom mutierten Zustand ausgehen.
func TestApplyDiagramsPlaceholderCaps(t *testing.T) {
	res := func(id int) []diagramResult {
		return []diagramResult{{id: id, png: fakePNG(80, 40), ascii: "Alice -> Bob: hi", ok: true}}
	}

	// Mit Platzhalter-Faehigkeit wird er zum ImageBlock.
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	id := r.m.diagramJobs()[0].id
	l, n := r.m.chat.applyDiagrams(res(id), true, TermCaps{Placeholders: true, CellW: 10, CellH: 20})
	if n != 1 {
		t.Fatalf("apply=%d", n)
	}
	img, ok := findImage(l, id)
	if !ok {
		t.Fatal("block is not an ImageBlock")
	}
	view := img.View(40)
	if !strings.Contains(view, "a=p,U=1,i=") {
		t.Fatal("placement control missing")
	}
	if !strings.Contains(view, placeholderChar) {
		t.Fatal("placeholder cells missing")
	}
	if img.Markdown() != "Alice -> Bob: hi" {
		t.Fatal("markdown fallback wrong")
	}

	// Ohne Platzhalter-Faehigkeit bleibt der Roh-Text sichtbar (FR-6).
	r2 := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	id2 := r2.m.diagramJobs()[0].id
	l2, n2 := r2.m.chat.applyDiagrams(res(id2), false, TermCaps{})
	if n2 != 1 {
		t.Fatalf("apply without caps = %d", n2)
	}
	cb := findCode(t, l2, id2)
	if !cb.converted {
		t.Fatal("block not marked converted")
	}
	if cb.code != "Alice -> Bob: hi" {
		t.Fatalf("raw source changed: %q", cb.code)
	}
}

// TestImagesEnabledOverride: force/off uebersteuert die Probe.
func TestImagesEnabledOverride(t *testing.T) {
	m := Model{}
	t.Setenv("HERON_IMAGES", "force")
	if !m.imagesEnabled() {
		t.Fatal("force must enable")
	}
	t.Setenv("HERON_IMAGES", "off")
	if m.imagesEnabled() {
		t.Fatal("off must disable despite caps")
	}
	t.Setenv("HERON_IMAGES", "")
	m.caps = TermCaps{Placeholders: true}
	if !m.imagesEnabled() {
		t.Fatal("probe result not honored")
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