package tui

import (
	"heron/internal/render"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestCursorModeSpawnsInActiveThinking (User-Report: "du kannst nicht bei
// einem aktiven thinking block in den cursor mode wechseln"): Der laufende
// Streaming-Think (ThinkBlockID) ist die SPAWN-Prioritaet — shift+enter
// spawnt das Caret in ihm, auch wenn Chat-Ende/Viewport woanders stehen.
func TestCursorModeSpawnsInActiveThinking(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "denkerische Zeile"})
	r.do(chunkMsg{text: "und eine antwort mit laengerem text drin"})
	r.do(tea.KeyMsg{Type: tea.KeyShiftTab}) // shift+enter-Ersatz im Test
	if !r.m.chat.char.active {
		t.Fatal("cursor mode did not activate")
	}
	if r.m.chat.char.block != 0 {
		t.Fatalf("caret spawned in block %d, want the active think block 0", r.m.chat.char.block)
	}
	text, ok := r.m.chat.charTextAt(0, r.m.chat.Width())
	if !ok || !strings.Contains(text, "denkerische Zeile") {
		t.Fatalf("think text not addressable: %q ok=%v", text, ok)
	}
}

// TestCursorModeSpawnFallsBackToReadingPos: Ohne laufenden Think gilt die
// Leseposition (Viewport-Rand) — der Spawn darf nie stumm fehlschlagen.
func TestCursorModeSpawnFallsBackToReadingPos(t *testing.T) {
	r := newRig(t)
	r.m.chat = r.m.chat.Append(NewTextBlock("eins", ""), NewTextBlock("zwei", ""))
	r.m.chat = r.m.chat.SetSize(80, 24)
	r.m.chat = r.m.chat.ScrollEnd()
	r.do(tea.KeyMsg{Type: tea.KeyShiftTab})
	if !r.m.chat.char.active {
		t.Fatal("cursor mode did not activate without a running think")
	}
}

// TestScrollCharIntoViewWithJoinedStack (User-Report: "nach unten wandert
// der screen mit, der cursor mit nach oben allerdings nicht"): Joined
// Karten sparen eine Kopfzeile — die kumulierte y-Berechnung muss
// heightAt (mit Joining) nutzen, sonst driftet die Scroll-Zielzeile um
// die Zahl der Joins.
func TestScrollCharIntoViewWithJoinedStack(t *testing.T) {
	th := mustTheme(t)
	l := NewBlockList()
	for i := 0; i < 5; i++ {
		l = l.Append(NewThinkBlock(strings.Repeat("zeile\n", 3), th))
	}
	l = l.SetSize(80, 24)
	// 5 Karten joinen zu EINEM Kasten: 4 Kopfzeilen gespart — heightAt
	// muss das widerspiegeln (rohe Height tut es nicht).
	raw := 0
	for _, it := range l.items {
		raw += it.Height(l.Width())
	}
	joined := 0
	for i := range l.items {
		joined += l.heightAt(i, l.Width())
	}
	if raw-joined != 4 {
		t.Fatalf("stack join unexpected: raw=%d joined=%d (want diff 4)", raw, joined)
	}
	// Caret in den letzten Block parken und scrollen: Der Offset muss den
	// JOINED y-Wert treffen (joined < raw), sonst liegt das Ziel unter dem
	// Block und der Screen laeuft zu weit nach unten.
	l.char.active = true
	l.char.block = 4
	l.char.to = 0
	l = l.scrollCharIntoView()
	y := 0
	for i := range l.items {
		if i == 4 {
			break
		}
		y += l.heightAt(i, l.Width())
	}
	if l.offset > y {
		t.Fatalf("offset=%d liegt unterm JOINED Ziel y=%d (Drift)", l.offset, y)
	}
}

// TestCharCopySkipsCardHeaders (User-Report: "nicht die überschrift tool
// markieren sondern nur den text"): charMarkdown im Kartenblock liefert
// den Inhalt OHNE "thinking ▸"-/Tool-Titel; die Markierung deckt im
// gerenderten Bild nur Inhalt-Zellen (keine Rahmen/Kopfzellen).
func TestCharCopySkipsCardHeaders(t *testing.T) {
	th := mustTheme(t)
	l := NewBlockList().Append(NewToolBlock(ToolData{CallID: "c", Title: "mein tool", Kind: "execute", Text: "ausgabe eins\nausgabe zwei"}, th, nil))
	l = l.SetSize(80, 24)
	l.char.active = true
	l.char.block, l.char.fromBlock = 0, 0
	l.char.from, l.char.to = 0, 0
	md := l.charMarkdown()
	if strings.Contains(md, "mein tool") || strings.Contains(md, "thinking") {
		t.Fatalf("copy payload enthaelt Karten-Header: %q", md)
	}
	if !strings.Contains(md, "ausgabe eins") {
		t.Fatalf("copy payload fehlt der Inhalt: %q", md)
	}
}

func mustTheme(t *testing.T) *render.Theme {
	t.Helper()
	th, err := render.DefaultTheme()
	if err != nil {
		t.Fatal(err)
	}
	return th
}
