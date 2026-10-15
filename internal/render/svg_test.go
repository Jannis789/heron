package render

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// Echter plantuml-SVG-Header (gekuerzt): width/height-Attribute vorn, ein
// inneres rect mit width="8" darf NIET als Root-Groesse gelesen werden.
const sampleSVG = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" version="1.1" data-diagram-type="SEQUENCE" style="width:251px;height:218px;background:#FFFFFF;" width="251px" height="218px" viewBox="0 0 251 218"><g><rect x="0" y="0" width="8" height="134.531" fill="#FFFF00"/></g></svg>`

const viewBoxOnlySVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300.4 150.6"><rect/></svg>`

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
