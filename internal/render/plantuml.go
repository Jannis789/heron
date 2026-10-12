package render

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PlantUMLToASCII renders a @startuml source through `plantuml -ttxt` to ASCII.
func PlantUMLToASCII(ctx context.Context, source string) (string, error) {
	dir, err := os.MkdirTemp("", "heron-plantuml-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "diagram.puml")
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		return "", err
	}
	bin := os.Getenv("PLANTUML_BIN")
	if bin == "" {
		bin = "plantuml"
	}
	cmd := exec.CommandContext(ctx, bin, "-ttxt", src)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("plantuml: %w: %s", err, out)
	}
	// Modern plantuml (>= 1.2024) writes diagram.atxt for the -ttxt flag,
	// older releases wrote diagram.txt — accept either (FR-6).
	var b []byte
	for _, name := range []string{"diagram.atxt", "diagram.txt"} {
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
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		return nil, err
	}
	bin := os.Getenv("PLANTUML_BIN")
	if bin == "" {
		bin = "plantuml"
	}
	cmd := exec.CommandContext(ctx, bin, "-tpng", src)
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
