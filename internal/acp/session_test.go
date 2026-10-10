package acp

import (
	"context"
	"os"
	"strings"
	"sync"
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

// TestSessionStreamRealGoose proves FR-2 end to end: chunks must reach the
// callback while the prompt request is still in flight, not only at the end.
func TestSessionStreamRealGoose(t *testing.T) {
	if os.Getenv("HERON_E2E_GOOSE") != "1" {
		t.Skip("set HERON_E2E_GOOSE=1 for a direct goose test")
	}
	wd, _ := os.Getwd()
	sess, err := NewSession(context.Background(), "goose", wd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	// OnChunk runs on the reader goroutine, so the slice needs a guard.
	var mu sync.Mutex
	var chunks []string
	ev := &Events{
		OnChunk: func(s string) {
			mu.Lock()
			chunks = append(chunks, s)
			mu.Unlock()
		},
	}

	got, err := sess.PromptStream(context.Background(),
		"Answer briefly in one short sentence: say Hallo.", ev)
	if err != nil {
		t.Fatalf("PromptStream: %v", err)
	}

	mu.Lock()
	n := len(chunks)
	first := ""
	if n > 0 {
		first = chunks[0]
	}
	mu.Unlock()

	if n == 0 {
		t.Fatal("no live chunks arrived - streaming is not wired (FR-2)")
	}
	if !strings.Contains(got, "Hallo") {
		t.Fatalf("assembled answer missing: %q", got)
	}
	t.Logf("streamed %d chunks, first=%q", n, first)
}
