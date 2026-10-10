package acp

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestSessionPromptRealGoose tests directly against real `goose acp` (no fake).
func TestSessionPromptRealGoose(t *testing.T) {
	if os.Getenv("HERON_E2E_GOOSE") != "1" {
		t.Skip("set HERON_E2E_GOOSE=1 for a direct goose test")
	}
	wd, _ := os.Getwd()
	sess, err := NewSession(context.Background(), "goose", wd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	got, err := sess.Prompt(context.Background(), "Answer briefly: just say Hallo.")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !strings.Contains(got, "Hallo") {
		t.Fatalf("answer missing: %q", got)
	}
	t.Logf("raw answer: %q", got)
}
