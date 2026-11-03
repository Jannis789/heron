package render

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// plantumlDefaultTheme ist das Standard-Theme fuer jede PlantUML-Darstellung:
// mimeograph — Tinten-Violett (#9275B6) auf Papiergrau (#D9D3D0). Hoher
// Pfeil-/Linienkontrast, der auch auf dunklem Terminal tragbar bleibt (die
// nackten PlantUML-Defaults/Pfeile gehen im Darkmode unter).
// HERON_PUML_THEME uebersteuert ohne Rebuild (z.B. ein Dark-Theme).
const plantumlDefaultTheme = "mimeograph"

// plantumlTheme liest das Theme aus der Umgebung, sonst das Standard-Theme.
func plantumlTheme() string {
	if t := os.Getenv("HERON_PUML_THEME"); t != "" {
		return t
	}
	return plantumlDefaultTheme
}

// withPlantUMLTheme injiziert die `!theme`-Direktive in den Quelltext, damit
// JEDE PlantUML-Darstellung das Standard-Theme bekommt. Ein explizites
// `!theme` im Quelltext des Nutzers gewinnt (wird nicht ueberschrieben);
// sonst wird direkt nach `@startuml` eingefuegt (ohne `@startuml` voran-
// gestellt). Eigene skinparams im Quelltext bleiben hoeherwertig.
func withPlantUMLTheme(source string) string {
	if strings.Contains(source, "!theme") {
		return source
	}
	theme := plantumlTheme()
	// Hintergrund transparent: PlantUML malt den Seiten-Hintergrund als
	// Full-Canvas-era <rect> (das CSS background: des SVG-Root ist ohnehin
	// transparent) — nur skinparam BackgroundColor transparent beseitigt
	// das opake Theme-Papier, damit die Grafik im Dark-TUI auf dem
	// Terminal-Hintergrund liegt statt als weisse Box (per Pixel geprueft).
	directive := "!theme " + theme + "\nskinparam BackgroundColor transparent"
	if i := strings.Index(source, "@startuml"); i >= 0 {
		nl := strings.IndexByte(source[i:], '\n')
		if nl < 0 {
			return source + "\n" + directive
		}
		at := i + nl + 1
		return source[:at] + directive + "\n" + source[at:]
	}
	return directive + "\n" + source
}

// PlantUMLToASCII renders a @startuml source through `plantuml -ttxt` to ASCII.
func PlantUMLToASCII(ctx context.Context, source string) (string, error) {
	dir, err := os.MkdirTemp("", "heron-plantuml-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "diagram.puml")
	// Theme (#mimeograph-Standard) in die Quelle injizieren, damit Pfeile/
	// Linien auch im Darkmode Kontrast haben (mitPlantUMLTheme).
	if err := os.WriteFile(src, []byte(withPlantUMLTheme(source)), 0o600); err != nil {
		return "", err
	}
	bin := os.Getenv("PLANTUML_BIN")
	if bin == "" {
		bin = "plantuml"
	}
	// -tutxt: Unicode-Kastenzeichnung (┌─┐ statt +-+) — deutlich lesbarer.
	cmd := exec.CommandContext(ctx, bin, "-tutxt", src)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("plantuml: %w: %s", err, out)
	}
	// Modern plantuml (>= 1.2024) writes diagram.atxt for the -ttxt flag,
	// older releases wrote diagram.txt — accept either (FR-6).
	var b []byte
	for _, name := range []string{"diagram.utxt", "diagram.atxt", "diagram.txt"} {
		if b, err = os.ReadFile(filepath.Join(dir, name)); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("plantuml output missing: %w", err)
}

// PlantUMLToPNG renders a @startuml source through plantuml -tpng to PNG
// bytes — the input for the Kitty image protocol (FR-27). PLANTUML_BIN
// overrides the binary.
func PlantUMLToPNG(ctx context.Context, source string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "heron-plantuml-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "diagram.puml")
	// Theme (#mimeograph-Standard) in die Quelle injizieren, damit Pfeile/
	// Linien auch im Darkmode Kontrast haben (mitPlantUMLTheme).
	if err := os.WriteFile(src, []byte(withPlantUMLTheme(source)), 0o600); err != nil {
		return nil, err
	}
	bin := os.Getenv("PLANTUML_BIN")
	if bin == "" {
		bin = "plantuml"
	}
	// -transparent: keine weisse/helle Box im Dark-TUI — das Diagramm
	// blendet mit dem Terminal-Hintergrund (herons Theme-Farbe) und die
	// mimeograph-Pfeile bleiben kontrastreich.
	cmd := exec.CommandContext(ctx, bin, "-tpng", "-transparent", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("plantuml: %w: %s", err, out)
	}
	png, err := os.ReadFile(filepath.Join(dir, "diagram.png"))
	if err != nil {
		return nil, err
	}
	if _, _, ok := pngDims(png); !ok {
		return nil, fmt.Errorf("plantuml: keine gueltige PNG-Ausgabe")
	}
	return png, nil
}

// MermaidToPNG renders a mermaid fence through mermaid-cli (mmdc) to PNG
// bytes (FR-28). MERMAID_BIN overrides the binary; without mmdc the caller
// keeps the raw source as fallback.
func MermaidToPNG(ctx context.Context, source string) ([]byte, error) {
	bin := os.Getenv("MERMAID_BIN")
	if bin == "" {
		bin = "mmdc"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("mermaid-cli (mmdc) nicht gefunden: %w", err)
	}
	dir, err := os.MkdirTemp("", "heron-mermaid-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	in := filepath.Join(dir, "diagram.mmd")
	if err := os.WriteFile(in, []byte(source), 0o600); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "diagram.png")
	cmd := exec.CommandContext(ctx, bin, "-i", in, "-o", out, "-b", "transparent")
	if o, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mmdc: %w: %s", err, o)
	}
	png, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	if _, _, ok := pngDims(png); !ok {
		return nil, fmt.Errorf("mmdc: keine gueltige PNG-Ausgabe")
	}
	return png, nil
}

// pngDims liest Breite und Hoehe aus dem IHDR-Chunk eines PNG.
func pngDims(b []byte) (int, int, bool) {
	if len(b) < 24 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0, false
	}
	w := int(binary.BigEndian.Uint32(b[16:20]))
	h := int(binary.BigEndian.Uint32(b[20:24]))
	return w, h, w > 0 && h > 0
}
