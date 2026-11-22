package main

import (
	"strings"
	"testing"

	"heron/internal/acp"
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

// TestInventoryFavoritesIgnoresKeylessConfigured: die Ruhe-Liste des
// /provider-Pickers speist sich aus providers:-Block UND eigenen Custom-
// Providern — das configured-Flag des Inventars (lügt bei Schlüssellosen
// wie lmstudio/local/claude-code) darf niemanden einschmuggeln.
// Nachbau des Live-Befunds gegen goose 1.55.0 (2026-11-22): 17 von 88 als
// configured markiert, wahr sind 4 (custom_nous, custom_opencode,
// custom_zai, zai_coding_plan).
func TestInventoryFavoritesIgnoresKeylessConfigured(t *testing.T) {
	inv := []acp.InventoryEntry{
		{ProviderID: "lmstudio", ProviderType: "Declarative", Configured: true},  // lügt
		{ProviderID: "local", ProviderType: "Builtin", Configured: true},         // lügt
		{ProviderID: "claude-code", ProviderType: "Preferred", Configured: true}, // lügt
		{ProviderID: "openai", ProviderType: "Preferred", Configured: false},
		{ProviderID: "custom_zai", ProviderType: "Custom", Configured: true},
		{ProviderID: "custom_opencode", ProviderType: "Custom", Configured: true},
	}
	got := inventoryFavorites(inv, []string{"custom_nous", "zai_coding_plan"})
	want := []string{"custom_nous", "custom_opencode", "custom_zai", "zai_coding_plan"}
	if len(got) != len(want) {
		t.Fatalf("favorites = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("favorites = %v, want %v", got, want)
		}
	}
}

// TestInventoryFavoritesWithoutConfigRead: fällt config/read aus (altes
// goose, Schema-Drift), bleiben mindestens die Custom-Provider — die
// Ruhe-Liste verliert ihre Kern-Einträge nie ganz.
func TestInventoryFavoritesWithoutConfigRead(t *testing.T) {
	inv := []acp.InventoryEntry{
		{ProviderID: "lmstudio", ProviderType: "Declarative", Configured: true},
		{ProviderID: "custom_zai", ProviderType: "Custom", Configured: true},
	}
	got := inventoryFavorites(inv, nil)
	if len(got) != 1 || got[0] != "custom_zai" {
		t.Fatalf("favorites = %v, want [custom_zai]", got)
	}
}
