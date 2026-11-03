package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestCopyableBlocksGetStableNumbers (FR-3+FR-40): JEDER Ketten-Block ist
// adressierbar — auch reine Textabsaetze — und bekommt fortlaufende 1-based
// IDs, die bei weiterem Append nicht wandern.
func TestCopyableBlocksGetStableNumbers(t *testing.T) {
	l := NewBlockList()
	l = l.Append(
		NewTextBlock("fließtext", ""),
		NewCodeBlock("go", "x := 1", nil),
	)
	if got := l.items[0].ID(); got != 1 {
		t.Fatalf("text block id=%d want 1 (FR-40: Text ist adressierbar)", got)
	}
	if got := l.items[1].ID(); got != 2 {
		t.Fatalf("first code block id=%d want 2", got)
	}

	l = l.Append(NewTableBlock([][]string{{"a"}, {"1"}}, nil, nil))
	if got := l.items[2].ID(); got != 3 {
		t.Fatalf("second copyable block id=%d want 3", got)
	}

	// Appending more must not renumber what already exists.
	l = l.Append(NewCodeBlock("sh", "echo hi", nil))
	if got := l.items[1].ID(); got != 2 || l.items[3].ID() != 4 {
		t.Fatalf("ids shifted: want 2 and 4, got %d and %d", l.items[1].ID(), l.items[3].ID())
	}
}

// TestCodeBlockHeaderShowsStableNumber (FR-3): the block number is visible,
// which is what the keyboard copy fallback addresses.
func TestCodeBlockHeaderShowsStableNumber(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	r.contains("#1")
}

// TestAltDigitCopiesNumberedBlock (FR-5): the keyboard fallback copies the
// block with that number, without focusing it first.
func TestAltDigitCopiesNumberedBlock(t *testing.T) {
	r := newRig(t).seed("```go\nalpha := 1\n```")
	// ESC+'1' arrives from the terminal as an alt-modified rune.
	r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1"), Alt: true})
	got := r.copiedAll()
	if len(got) != 1 || !strings.Contains(got[0], "alpha") {
		t.Fatalf("copied=%v want the code body", got)
	}
	// Der COPIED-Flash lebt im Eingabefeld-Chip — der Block-Footer zeigt
	// keine Kopier-UI mehr.
	r.notContains("COPIED")
	r.notContains("[ COPY ]")
}

// TestAltDigitUnknownBlockDoesNothing: a number with no block is a no-op, it
// must not eat normal typing.
func TestAltDigitUnknownBlockDoesNothing(t *testing.T) {
	r := newRig(t).seed("```go\nalpha := 1\n```")
	r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9"), Alt: true})
	if len(r.copiedAll()) != 0 {
		t.Fatalf("nothing should be copied, got %v", r.copiedAll())
	}
	r.notContains("COPIED")
}

// TestBlockNumFromKey pins the shortcut parsing independently of how a
// particular terminal encodes the key.
func TestBlockNumFromKey(t *testing.T) {
	cases := map[string]int{
		"alt+1": 1, "alt+9": 9, "ctrl+3": 3, "ctrl+7": 7,
		"enter": 0, "alt+": 0, "alt+0": 0, "alt+12": 0,
		"c": 0, "ctrl+c": 0, "up": 0,
	}
	for in, want := range cases {
		if got := blockNumFromKey(in); got != want {
			t.Errorf("blockNumFromKey(%q)=%d want %d", in, got, want)
		}
	}
}
