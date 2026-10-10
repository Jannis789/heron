package main

import (
	"strings"
	"testing"
)

// TestSplitChunksKeepsContent: splitting for the demo stream must neither
// lose nor reorder runes, and no piece may exceed the limit.
func TestSplitChunksKeepsContent(t *testing.T) {
	const src = "Das ist ein relativ langer Demotext für das Streaming der Antwort"
	pieces := splitChunks(src, 10)

	var joined strings.Builder
	for _, p := range pieces {
		if n := len([]rune(p)); n > 10 {
			t.Fatalf("piece too long (%d runes): %q", n, p)
		}
		joined.WriteString(p)
	}
	if joined.String() != src {
		t.Fatalf("content changed:\ngot  %q\nwant %q", joined.String(), src)
	}
	if len(pieces) < 3 {
		t.Fatalf("expected several pieces, got %d", len(pieces))
	}
}

// TestSplitChunksSplitsAtSpaces: pieces should prefer to break after a word.
func TestSplitChunksSplitsAtSpaces(t *testing.T) {
	pieces := splitChunks("alpha beta gamma delta", 11)
	for _, p := range pieces {
		if strings.HasSuffix(strings.TrimSpace(p), " ") {
			t.Fatalf("piece should not end on a space: %q", p)
		}
	}
}

// TestSplitChunksHandlesMultiByte: umlauts and emoji must survive splitting.
func TestSplitChunksHandlesMultiByte(t *testing.T) {
	const src = "nötig äöü 🎉 fertig 😀 x"
	pieces := splitChunks(src, 4)
	if strings.Join(pieces, "") != src {
		t.Fatalf("multi-byte content damaged: %q", pieces)
	}
}
