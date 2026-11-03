package render

import (
	"bytes"
	"context"
	"image/png"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// Echter plantuml-SVG-Header (gekuerzt): width/height-Attribute vorn, ein
// inneres rect mit width="8" darf NIET als Root-Groesse gelesen werden.
const sampleSVG = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" version="1.1" data-diagram-type="SEQUENCE" style="width:251px;height:218px;background:#FFFFFF;" width="251px" height="218px" viewBox="0 0 251 218"><g><rect x="0" y="0" width="8" height="134.531" fill="#FFFF00"/></g></svg>`

const viewBoxOnlySVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300.4 150.6"><rect/></svg>`

func TestSVGBackgroundTransparent(t *testing.T) {
	out := makeSVGBackgroundTransparent([]byte(sampleSVG))
	if strings.Contains(string(out), "background:#FFFFFF") {
		t.Fatalf("weisser Root-Hintergrund blieb: %q", out)
	}
	if !strings.Contains(string(out), "background:transparent") {
		t.Fatalf("Root-Hintergrund nicht transparent gesetzt: %q", out)
	}
	// Inhaltselemente (fill=) duerfen nicht angefasst werden.
	if !strings.Contains(string(out), `fill="#FFFF00"`) {
		t.Fatal("Content-fill wurde veraendert")
	}
	// Bereits transparentes SVG bleibt unveraendert.
	s := `<svg style="a;background:transparent;b"></svg>`
	if got := string(makeSVGBackgroundTransparent([]byte(s))); got != s {
		t.Fatalf("bereits transparent wurde veraendert: %q", got)
	}
}

func TestSvgDimsAttrs(t *testing.T) {
	w, h, ok := SvgDims([]byte(sampleSVG))
	if !ok || w != 251 || h != 218 {
		t.Fatalf("SvgDims = %d,%d,%v — will 251,218,true (root attrs, nicht rect 8)", w, h, ok)
	}
}

func TestSvgDimsViewBox(t *testing.T) {
	w, h, ok := SvgDims([]byte(viewBoxOnlySVG))
	if !ok || w != 300 || h != 151 {
		t.Fatalf("SvgDims(viewBox) = %d,%d,%v — will 300,151,true", w, h, ok)
	}
}

func TestSvgDimsGarbage(t *testing.T) {
	if _, _, ok := SvgDims([]byte("kein svg")); ok {
		t.Fatal("Muell als SVG-Dims akzeptiert")
	}
}

func TestResvgFontArgs(t *testing.T) {
	t.Setenv("HERON_RESVG_FONTS", "")
	t.Setenv("HERON_RESVG_FONT", "")
	if args := resvgFontArgs(); len(args) != 0 {
		t.Fatalf("ohne env duerfen keine Font-Flags entstehen: %v", args)
	}

	t.Setenv("HERON_RESVG_FONTS", "/a:/b:")
	got := resvgFontArgs()
	want := []string{"--use-fonts-dir", "/a", "--use-fonts-dir", "/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fonts-dirs = %v, will %v (leere Eintraege ignoriert)", got, want)
	}

	t.Setenv("HERON_RESVG_FONTS", "")
	t.Setenv("HERON_RESVG_FONT", "DejaVu Sans")
	got = resvgFontArgs()
	want = []string{
		"--font-family", "DejaVu Sans",
		"--serif-family", "DejaVu Sans",
		"--sans-serif-family", "DejaVu Sans",
		"--monospace-family", "DejaVu Sans",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("font-familie = %v, will %v", got, want)
	}
}

func TestResvgToPNGExactSize(t *testing.T) {
	if !ResvgAvailable() {
		t.Skip("resvg nicht im PATH (nix wrap fehlt)")
	}
	png, err := ResvgToPNG(context.Background(), []byte(sampleSVG), 1004, 872)
	if err != nil {
		t.Fatalf("ResvgToPNG: %v", err)
	}
	w, h, ok := pngDims(png)
	if !ok || w != 1004 || h != 872 {
		t.Fatalf("Raster-Masse %dx%d will 1004x872", w, h)
	}
}

// TestPlantUMLSVGTransparentBackground: der Diagramm-Hintergrund ist nach
// dem Skinparam-Override transparent — echtes plantuml -> resvg -> PNG,
// Eckenpixel alpha 0 (per Pixel geprueft, nicht nur das CSS-Attribut).
func TestPlantUMLSVGTransparentBackground(t *testing.T) {
	if _, err := exec.LookPath("plantuml"); err != nil {
		t.Skip("plantuml nicht im PATH")
	}
	if !ResvgAvailable() {
		t.Skip("resvg nicht im PATH")
	}
	svg, err := PlantUMLToSVG(context.Background(), "@startuml\nA -> B\n@enduml\n")
	if err != nil {
		t.Fatalf("PlantUMLToSVG: %v", err)
	}
	w, h, ok := SvgDims(svg)
	if !ok {
		t.Fatal("SvgDims fehlgeschlagen")
	}
	pngBytes, err := ResvgToPNG(context.Background(), svg, w, h)
	if err != nil {
		t.Fatalf("ResvgToPNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("PNG decode: %v", err)
	}
	for _, c := range [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}} {
		_, _, _, a := img.At(c[0], c[1]).RGBA()
		if a>>8 != 0 {
			t.Fatalf("Ecke %v nicht transparent (alpha=%d)", c, a>>8)
		}
	}
}

func TestPlantUMLToSVG(t *testing.T) {
	if _, err := exec.LookPath("plantuml"); err != nil {
		t.Skip("plantuml nicht im PATH")
	}
	src := "@startuml\nAlice -> Bob: hi\n@enduml\n"
	svg, err := PlantUMLToSVG(context.Background(), src)
	if err != nil {
		t.Fatalf("PlantUMLToSVG: %v", err)
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Fatal("kein svg-Element")
	}
	w, h, ok := SvgDims(svg)
	if !ok || w < 10 || h < 10 {
		t.Fatalf("SVG-Dims unplausibel: %d,%d,%v", w, h, ok)
	}
}
