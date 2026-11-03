package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// withTrueColor steht jetzt in harness_test.go (diese Datei war unversioniert
// — der Nix-Bau sieht nur den committeten Baum und selection_test.go
// referenzierte den Helfer).

// neon ist die SGR-Sequenz von #BF00FF (Theme "focus") in TrueColor.
const neon = "38;2;191;0;255"
const neonWhite = "38;2;255;255;255"

// TestCopyHintInFooterBar: Die Copy-Aktion lebt in der zentralen Hint-
// Leiste ("c copy", Taste c) — nicht in den Block-Footern (info-only) und
// nicht in der Eingabebox. Nach dem Kopieren steht kurz "✓ kopiert" dort.
func TestCopyHintInFooterBar(t *testing.T) {
	m := New(nil, nil, nil)
	m.chat = m.chat.Append(NewCodeBlock("go", "x := 1", func(s string) tea.Cmd {
		return func() tea.Msg { return nil }
	}))
	var handled bool
	m.chat, _, handled = m.chat.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !handled || !m.chat.Focused() {
		t.Fatal("precondition: block focused")
	}
	m.width, m.height = 80, 24
	if !strings.Contains(m.footerView(), "c copy") {
		t.Fatalf("Hint-Leiste ohne c copy: %q", m.footerView())
	}
	if strings.Contains(m.inputView(), "COPY") {
		t.Fatalf("Eingabebox-Rahmen darf keinen COPY-Chip tragen: %q", m.inputView())
	}
	nm, _ := m.copySelection()
	if !strings.Contains(nm.footerView(), "✓ kopiert") {
		t.Fatalf("Flash fehlt in der Hint-Leiste: %q", nm.footerView())
	}
}

// TestFocusSignalsBorderNotLabel: Tab-Fokus faerbt den RAMEN des Blocks
// Neon-Violett — das Action-Label bleibt unveraendert (kein Reverse).
func TestFocusSignalsBorderNotLabel(t *testing.T) {
	withTrueColor(t)
	th, _ := render.DefaultTheme()
	b := NewCodeBlock("go", "x := 1", nil)
	b.th = th
	w := 40
	plain := b.View(w)
	if strings.Contains(plain, "\x1b[7;") {
		t.Fatal("resting view must not reverse")
	}
	// Ohne Fokus und ohne copy ist der Footer VERBORGEN (Progressive
	// Disclosure) — 0 Neon-Runs, kein Hotkey im sichtbaren Bereich.
	if n := strings.Count(plain, neon); n != 0 {
		t.Fatalf("unfocused code block: want 0 neon runs (footer hidden), got %d", n)
	}
	fb := b.SetFocused(true).(CodeBlock)
	focused := fb.View(w)
	// Der Rahmen (mehrere Runs: Ecken, Segmente) plus der C-Hotkey —
	// ohne Fokus war es genau EIN Neon-Run (nur der Hotkey).
	if n := strings.Count(focused, neon); n < 2 {
		t.Fatalf("focused border not neon (only %d neon runs): %q", n, focused[:40])
	}
	if strings.Contains(focused, "\x1b[7;") {
		t.Fatal("focus must not reverse anything")
	}
}

// TestThinkHoverTargetsSingleCell: Hover trifft GENAU EINE Zelle — Kopfzeile
// ODER Footer-Aktion, nie beide gleichzeitig (der alte blockweite Hover
// leuchtete zwei Dinge fuer eine Mausposition auf).
func TestThinkHoverTargetsSingleCell(t *testing.T) {
	withTrueColor(t)
	th, _ := render.DefaultTheme()
	b := NewThinkBlock("a\nb\nc\nd\ne", th) // > thinkCollapsed Zeilen → faltbar
	w := 40
	if b.Height(w) != 5 { // Kopf + 3 (gekappte) Zeilen + Footer
		t.Fatalf("unexpected height %d", b.Height(w))
	}
	foot := b.SetHoverAt(w, w/2, 4).(ThinkBlock)
	if foot.hoverHead || !foot.hoverFoot {
		t.Fatal("footer hover must hit only the footer")
	}
	head := b.SetHoverAt(w, 5, 0).(ThinkBlock)
	if !head.hoverHead || head.hoverFoot {
		t.Fatal("header hover must hit only the header")
	}
	none := b.SetHoverAt(w, 5, 1).(ThinkBlock)
	if none.hoverHead || none.hoverFoot {
		t.Fatal("body hover must hit nothing")
	}
}

// TestThinkHotkeysFold: + klappt auf, - zu — der Neon-Buchstabe im Label
// ist buchstaeblich die Taste (nur im Fokus).
func TestThinkHotkeysFold(t *testing.T) {
	th, _ := render.DefaultTheme()
	b := NewThinkBlock("a\nb\nc\nd\ne", th)
	b = b.SetFocused(true).(ThinkBlock) // Hotkeys wirken nur im Fokus
	if b.expanded {
		t.Fatal("starts collapsed")
	}
	w, _ := b.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if !w.(ThinkBlock).expanded {
		t.Fatal("+ must expand the focused think block")
	}
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	if w.(ThinkBlock).expanded {
		t.Fatal("- must collapse the focused think block")
	}
	// Ohne Fokus passiert nichts.
	plain := NewThinkBlock("a\nb\nc\nd\ne", th)
	w2, _ := plain.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if w2.(ThinkBlock).expanded {
		t.Fatal("+ without focus must not expand")
	}
}

// TestImageFooterHoverSingleBox: Im Image-Footer leuchtet beim Hover GENAU
// DER getroffene Kasten — [ TEXT ]/Switch oder [ COPY ], nie beide. Und die
// Hotkey-Buchstaben (T bei TEXT, C bei COPY) sind immer Neon.
// TestImageHotkeyTTogglesBothWays (FR-38): EINE Taste t schaltet in BEIDE
// Richtungen (Bild <-> Quelltext); die Zusatztaste i ist entfallen und
// loest nichts mehr aus.
func TestImageHotkeyTTogglesBothWays(t *testing.T) {
	th, _ := render.DefaultTheme()
	img := NewImageBlock(nil, "roh", false, TermCaps{}, nil, th)
	img = img.SetSource("plantuml", "@startuml\n@enduml").(ImageBlock)
	if img.showSrc {
		t.Fatal("precondition: image view active")
	}
	w, _ := img.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if !w.(ImageBlock).showSrc {
		t.Fatal("t must switch to the source view")
	}
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if w.(ImageBlock).showSrc {
		t.Fatal("t must switch back to the image view")
	}
	// i ist keine Aktion mehr: die Bild-Ansicht bleibt bestehen (und
	// ohne Fokus tippt i normal ins Eingabefeld).
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if w.(ImageBlock).showSrc {
		t.Fatal("i must not toggle anything (FR-38)")
	}
}
