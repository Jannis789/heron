package render

import (
	"context"
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
	cmd := exec.CommandContext(ctx, "plantuml", "-ttxt", src)
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
