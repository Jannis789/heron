package tui

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

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

func TestKittyImageEscapes(t *testing.T) {
	png := fakePNG(8, 4)
	esc := kittyImageEscapes(png, 40, 10)
	// Ein kleiner Payload ist ein einzelner Chunk: m=0 ist protokollrichtig.
	if !strings.HasPrefix(esc, "\x1b_Ga=T,f=100,q=2,c=40,r=10,m=0;") {
		t.Fatalf("kitty header wrong: %q", esc[:48])
	}
	if !strings.HasSuffix(esc, base64.StdEncoding.EncodeToString(png)+"\x1b\\") {
		t.Fatal("kitty escape not terminated with the full payload")
	}
	if strings.Count(esc, "\x1b_G") != 1 {
		t.Fatal("small png must be a single chunk")
	}

	// Payloads > 4096 Base64-Zeichen werden mit m=1 gekettet.
	big := append(append([]byte{}, png...), bytes.Repeat([]byte{0xAB}, 6000)...)
	bigEsc := kittyImageEscapes(big, 40, 10)
	if n := strings.Count(bigEsc, "\x1b_G"); n < 2 {
		t.Fatalf("large payload not chunked: %d transmissions", n)
	}
	if !strings.Contains(bigEsc, "m=1;") || !strings.Contains(bigEsc, "m=0;") {
		t.Fatal("chunk continuation markers missing")
	}
	if !strings.HasSuffix(bigEsc, "\x1b\\") {
		t.Fatal("chunked escape not terminated")
	}
	if strings.Count(bigEsc, "c=40,r=10") != 1 {
		t.Fatal("geometry must only be sent in the first transmission")
	}
}



func TestImageTermDetection(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("HERON_IMAGES", "")
	if !imageProtocolTerm() {
		t.Fatal("kitty TERM not detected")
	}
	t.Setenv("HERON_IMAGES", "off")
	if imageProtocolTerm() {
		t.Fatal("HERON_IMAGES=off must disable images")
	}
	t.Setenv("HERON_IMAGES", "force")
	if !imageProtocolTerm() {
		t.Fatal("HERON_IMAGES=force must enable images")
	}
	t.Setenv("HERON_IMAGES", "")
	t.Setenv("HERON_NO_IMAGES", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "/x")
	if !imageProtocolTerm() {
		t.Fatal("ghostty env not detected")
	}
}

func TestApplyDiagramsInstallsImageBlock(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	r := newRig(t)
	r.do(tea.WindowSizeMsg{Width: 80, Height: 24})
	r.do(runes("x"))
	l := r.m.chat.Append(NewImageBlock(fakePNG(80, 40), "ASCII-Fallback", nil, r.m.theme))
	img, ok := l.items[len(l.items)-1].(ImageBlock)
	if !ok {
		t.Fatalf("expected ImageBlock, got %T", l.items[len(l.items)-1])
	}
	if img.Height(80) < 1 {
		t.Fatal("image height must reserve rows")
	}
	if !strings.Contains(img.View(80), "\x1b_G") {
		t.Fatal("kitty view missing graphics escape")
	}
	if img.Markdown() != "ASCII-Fallback" {
		t.Fatal("markdown fallback wrong")
	}
	_ = render.Render
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
	// Der Rig fuehrt den Cmd aus: mit plantuml auf dem PATH ist der Block
	// jetzt ASCII, ohne plantuml bleibt der Roh-Text — beides enthaelt
	// "Alice", und die Queue ist abgearbeitet.
	if len(r.m.diagramJobs()) != 0 {
		t.Fatal("diagram jobs survived the first frame")
	}
	r.contains("Alice")
}