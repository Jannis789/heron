package render

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Dieser Pfad macht die Bilder scharf (FR-27, Metrik 2): PlantUML rendert
// VECOR (SVG), resvg rastert auf die EXAKTE Terminal-Zielgroesse — das
// Kitty-Protokoll selbst kann nur Raster (f=24/32/100), also ist die
// Rasterung auf Ziel-pixel die einzige Art, dem Terminal jede Skalierung
// zu nehmen. Bei Fenster-Resize wird aus dem gecachten SVG neu gerastert
// (Millisekunden statt ~500 ms Java-Start).

// PlantUMLToSVG renders a @startuml source through `plantuml -tsvg` to SVG
// bytes — the vector source for resolution-adaptive rasterization (FR-27).
// PLANTUML_BIN overrides the binary.
func PlantUMLToSVG(ctx context.Context, source string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "heron-plantuml-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "diagram.puml")
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		return nil, err
	}
	bin := os.Getenv("PLANTUML_BIN")
	if bin == "" {
		bin = "plantuml"
	}
	cmd := exec.CommandContext(ctx, bin, "-tsvg", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("plantuml -tsvg: %w: %s", err, out)
	}
	svg, err := os.ReadFile(filepath.Join(dir, "diagram.svg"))
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(svg), "<svg") {
		return nil, fmt.Errorf("plantuml: keine gueltige SVG-Ausgabe")
	}
	return svg, nil
}

var (
	svgWidthRe   = regexp.MustCompile(`\bwidth="([0-9]+(?:\.[0-9]+)?)`)
	svgHeightRe  = regexp.MustCompile(`\bheight="([0-9]+(?:\.[0-9]+)?)`)
	svgViewBoxRe = regexp.MustCompile(`viewBox="[^"]*\s([0-9.]+)\s([0-9.]+)"`)
)

// SvgDims liest die intrinsische Groesse aus dem SVG-Header: zuerst width/
// height-Attribute (plantuml schreibt z.B. width="251px"), sonst viewBox.
// Nur der Header (4 KB) wird gelesen — die Root-Attribute stehen vorn, vor
// allen inneren Elementen (deren width/height waeren falsche Treffer).
func SvgDims(svg []byte) (int, int, bool) {
	head := string(svg)
	if len(head) > 4096 {
		head = head[:4096]
	}
	w, wok := svgAttrNum(head, svgWidthRe)
	h, hok := svgAttrNum(head, svgHeightRe)
	if wok && hok && w > 0 && h > 0 {
		return w, h, true
	}
	if m := svgViewBoxRe.FindStringSubmatch(head); m != nil {
		wf, e1 := strconv.ParseFloat(m[1], 64)
		hf, e2 := strconv.ParseFloat(m[2], 64)
		if e1 == nil && e2 == nil && wf > 0 && hf > 0 {
			return int(math.Round(wf)), int(math.Round(hf)), true
		}
	}
	return 0, 0, false
}

func svgAttrNum(s string, re *regexp.Regexp) (int, bool) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return int(math.Round(f)), true
}

// resvg-Verfuegbarkeit wird einmal gecacht (LookPath), damit die Conversion
// nicht fuer jeden Job Temp-Verzeichnisse anlegt, nur um dann zu scheitern.
var (
	resvgOnce sync.Once
	resvgPath string
)

// ResvgAvailable reports whether the resvg binary is on PATH (RESVG_BIN
// overrides). Without it heron falls back to the plain -tpng path.
func ResvgAvailable() bool {
	resvgOnce.Do(func() {
		bin := os.Getenv("RESVG_BIN")
		if bin == "" {
			bin = "resvg"
		}
		if p, err := exec.LookPath(bin); err == nil {
			resvgPath = p
		}
	})
	return resvgPath != ""
}

// ResvgToPNG rasterizes SVG to PNG at the EXACT pixel size (w,h) — the
// terminal then performs zero scaling and the image is pixel-sharp
// (FR-27, Metrik 2). Font warnings on stderr are tolerated (exit 0 = ok);
// the output dims are verified against the request (+/- 2 px Rundung).
func ResvgToPNG(ctx context.Context, svg []byte, w, h int) ([]byte, error) {
	if !ResvgAvailable() {
		return nil, fmt.Errorf("resvg nicht gefunden")
	}
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("resvg: ungueltige Zielgroesse %dx%d", w, h)
	}
	dir, err := os.MkdirTemp("", "heron-resvg-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	in := filepath.Join(dir, "diagram.svg")
	if err := os.WriteFile(in, svg, 0o600); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "diagram.png")
	cmd := exec.CommandContext(ctx, resvgPath, "--width", strconv.Itoa(w), "--height", strconv.Itoa(h), in, out)
	if o, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("resvg: %w: %s", err, o)
	}
	png, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	gw, gh, ok := pngDims(png)
	if !ok {
		return nil, fmt.Errorf("resvg: keine gueltige PNG-Ausgabe")
	}
	// resvg haelt das Seitenverhaeltnis und passt die Zeichnung IN die Box
	// ein — die Ausgabe kann daher schmaler/hoeher sein als angefordert
	// (gemessen: Box 918x360 -> 875x360). Das ist korrekt, kein Fehler:
	// der Caller rechnet die Zellzeilen aus den ECHTEN Pixeln, das
	// Terminal skaliert dann exakt auf die Box. Abgelehnt wird nur noch
	// grober Murks (> 40% neben der Box), der auf ein kaputtes SVG deutet.
	if gw > w+w/2 || gh > h+h/2 || gw*3 < w || gh*3 < h {
		return nil, fmt.Errorf("resvg: ausserhalb der box %dx%d (will %dx%d)", gw, gh, w, h)
	}
	return png, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
