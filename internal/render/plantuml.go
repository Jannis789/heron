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
	b, err := os.ReadFile(filepath.Join(dir, "diagram.txt"))
	if err != nil {
		return "", fmt.Errorf("plantuml output missing: %w", err)
	}
	return string(b), nil
}
