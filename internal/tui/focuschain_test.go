package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func chainModel(t *testing.T) Model {
	t.Helper()
	m := New(nil, nil, nil)
	m.chat = m.chat.Append(
		NewTextBlock("text", ""),
		NewCodeBlock("go", "a", nil),
		NewCodeBlock("go", "b", nil),
	)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return nm.(Model)
}

func step(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

// TestInputFocusIsDefault: Die Kette startet auf dem Eingabefeld — der
// Rahmen leuchtet, getippt wird ohnehin dort.
func TestInputFocusIsDefault(t *testing.T) {
	m := chainModel(t)
	if !m.inputFocus {
		t.Fatal("input focus must be the default state")
	}
}

// TestTabTogglesInputAndSegments: tab wechselt Eingabefeld <-> Chat — nach
// einem Spaziergang nach oben reicht EIN tab zurueck zum Tippen.
func TestTabTogglesInputAndSegments(t *testing.T) {
	m := chainModel(t)
	// tab: rein in den Chat (letzter Block)
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.inputFocus || !m.chat.Focused() || m.chat.focus != 2 {
		t.Fatalf("tab: input=%v focus=%d want last block", m.inputFocus, m.chat.focus)
	}
	// ein paar Schritte nach oben
	m = step(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.chat.focus != 1 {
		t.Fatalf("up focus=%d want 1", m.chat.focus)
	}
	// EIN tab reicht zurueck ins Eingabefeld (nicht 2x down)
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.inputFocus || m.chat.Focused() {
		t.Fatalf("tab back: input=%v chatFocused=%v", m.inputFocus, m.chat.Focused())
	}
	// tab von dort: wieder letzter Block
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.inputFocus || m.chat.focus != 2 {
		t.Fatalf("tab again: input=%v focus=%d", m.inputFocus, m.chat.focus)
	}
}

// TestDownStopsAtLastBlock: ↓ am letzten Block STOPPT — die Kette leakt
// nicht mehr ins Eingabefeld. Der Rueckweg fuehrt AUSSCHLIESSLICH tab
// (esc bleibt der Reset auf den Standard-Zustand).
func TestDownStopsAtLastBlock(t *testing.T) {
	m := chainModel(t)
	m = step(m, tea.KeyMsg{Type: tea.KeyTab}) // Antwort-Modus: letzter Block
	m = step(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.inputFocus || !m.chat.Focused() || m.chat.focus != 2 {
		t.Fatalf("down at the last block must stay: input=%v focus=%d",
			m.inputFocus, m.chat.focus)
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyDown}) // wiederholt: weiter stehen
	if m.inputFocus || !m.chat.Focused() || m.chat.focus != 2 {
		t.Fatalf("repeated down must keep staying: input=%v focus=%d",
			m.inputFocus, m.chat.focus)
	}
	// tab ist der einzige navigationale Rueckweg ins Eingabefeld.
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.inputFocus || m.chat.Focused() {
		t.Fatalf("tab back: input=%v chatFocused=%v", m.inputFocus, m.chat.Focused())
	}
}

// TestUpDownReservedForHistory: ↑/↓ im Eingabe-Modus sind tot — sie sind
// fuer die spaetere Eingabe-History reserviert und duerfen KEINEN Fokus
// springen (der alte Sprung ↑->letzter Block ist weg).
func TestUpDownReservedForHistory(t *testing.T) {
	m := chainModel(t)
	m = step(m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.inputFocus || m.chat.Focused() {
		t.Fatalf("up in input mode must be a no-op: input=%v chat=%v",
			m.inputFocus, m.chat.Focused())
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyDown})
	if !m.inputFocus || m.chat.Focused() {
		t.Fatalf("down in input mode must be a no-op: input=%v chat=%v",
			m.inputFocus, m.chat.Focused())
	}
}

// TestTypingStaysOutOfInputInAnswerMode: Im Antwort-Modus (Chat-Segment
// selektiert) erreicht Getippe das Eingabefeld NICHT — das nicht selektierte
// Eingabefeld ist nicht interaktionsfaehig (Textarea geblurrt).
func TestTypingStaysOutOfInputInAnswerMode(t *testing.T) {
	m := chainModel(t)
	m = step(m, tea.KeyMsg{Type: tea.KeyTab}) // Antwort-Modus: letzter Block
	if m.inputFocus {
		t.Fatal("precondition: answer mode")
	}
	if m.input.Focused() {
		t.Fatal("textarea must be blurred in answer mode")
	}
	for _, r := range "xyz" {
		m = step(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("typing leaked into the input in answer mode: %q", got)
	}
	// zurueck im Eingabe-Modus schreibt dieselben Tasten normal hinein
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	m = step(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ok")})
	if got := m.input.Value(); got != "ok" {
		t.Fatalf("input mode must type: got %q", got)
	}
	if !m.input.Focused() {
		t.Fatal("textarea must be focused in input mode")
	}
}

// TestTabOnEmptyChatStaysInInput: Ohne fokussierbares Segment bleibt tab
// im Eingabe-Modus — die Kette darf nie ohne Ziel haengen bleiben.
func TestTabOnEmptyChatStaysInInput(t *testing.T) {
	m := New(nil, nil, nil)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = nm.(Model)
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.inputFocus || m.chat.Focused() {
		t.Fatalf("tab on empty chat must stay input: input=%v chat=%v",
			m.inputFocus, m.chat.Focused())
	}
	// FR-40: Ein NUR-Text-Chat ist nicht mehr "nicht fokussierbar" — reine
	// Textabsaetze sind eigene Ketten-Segmente, tab hebt den ersten an.
	m.chat = m.chat.Append(NewTextBlock("nur text", ""))
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.inputFocus || !m.chat.Focused() || m.chat.focus != 0 {
		t.Fatalf("tab muss den Textabsatz fokussieren (FR-40): input=%v focus=%d",
			m.inputFocus, m.chat.focus)
	}
}

// TestEscReturnsToInputHome: Esc raeumt Block-Fokus weg und kehrt auf den
// Standard zurueck — Eingabefeld markiert.
func TestEscReturnsToInputHome(t *testing.T) {
	m := chainModel(t)
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.chat.Focused() {
		t.Fatal("precondition: block focused")
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.chat.Focused() || !m.inputFocus {
		t.Fatalf("esc: chatFocused=%v input=%v want input home", m.chat.Focused(), m.inputFocus)
	}
}

// TestPgKeysScrollFromInput: Bild ↓/↑ scrollen den Chat auch VOM Eingabe-
// feld aus — Lesen ohne Fokus-Wechsel.
func TestPgKeysScrollFromInput(t *testing.T) {
	m := New(nil, nil, nil)
	for i := 0; i < 30; i++ {
		m.chat = m.chat.Append(NewCodeBlock("go", "zeile", nil))
	}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m = nm.(Model)
	before := m.chat.offset
	m = step(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.chat.offset >= before {
		t.Fatalf("pgup must scroll the chat from the input: offset %d -> %d", before, m.chat.offset)
	}
	if !m.inputFocus {
		t.Fatal("pgup must not move the focus chain")
	}
}

// TestHeadingJoinFocusChain: Ueberschriften sind fokussierbar — up/down
// bleibt auf ihnen STEHEN (statt sie zu ueberspringen), der Fokus zeigt
// sich als Unterstrich.
func TestHeadingJoinFocusChain(t *testing.T) {
	withTrueColor(t)
	l := NewBlockList().
		Append(NewHeadingBlock("Titel"), NewTextBlock("prosa", ""), NewCodeBlock("go", "x", nil)).
		SetSize(60, 10)
	l = l.FocusVisible() // alles sichtbar -> letzter fokussierbarer Block
	if !l.Focused() || l.focus != 2 {
		t.Fatalf("FocusVisible sollte den Code-Block nehmen: focus=%d", l.focus)
	}
	nl, _, _ := l.Update(tea.KeyMsg{Type: tea.KeyUp})
	l = nl
	if !l.Focused() || l.focus != 1 {
		t.Fatalf("up sollte die Prosa als EIGENES Segment fokussieren (FR-40): focus=%d", l.focus)
	}
	nl, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyUp})
	l = nl
	if !l.Focused() || l.focus != 0 {
		t.Fatalf("up sollte die Ueberschrift fokussieren: focus=%d", l.focus)
	}
	if !strings.Contains(l.items[0].View(60), ";4m") {
		t.Fatalf("fokussierte Ueberschrift muss unterstrichen sein: %q", l.items[0].View(60))
	}
	// Ohne Fokus kein Unterstrich.
	idle := NewHeadingBlock("Titel").View(60)
	if strings.Contains(idle, ";4m") {
		t.Fatalf("Ueberschrift ohne Fokus darf keinen Unterstrich tragen: %q", idle)
	}
}

// TestTabFocusesViewport: tab landet IM VIEWPORT — nach dem Hochscrollen
// fokussiert es den letzten SICHTBAREN Block, nicht den letzten im Chat.
func TestTabFocusesViewport(t *testing.T) {
	// 10 Code-Bloecke a 3 Zeilen = 30 Zeilen, Viewport 6 Zeilen.
	l := NewBlockList().SetSize(60, 6)
	for i := 0; i < 10; i++ {
		l = l.Append(NewCodeBlock("go", fmt.Sprintf("x%d", i), nil))
	}
	// Append haelt follow (unten), zwei pgup: offset 24 -> 12.
	var ok bool
	l, _, ok = l.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if !ok {
		t.Fatal("pgup nicht konsumiert")
	}
	l, _, _ = l.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if l.offset != 12 {
		t.Fatalf("offset=%d, want 12", l.offset)
	}
	// Sichtbar: Zeilen 12..17 = Bloecke 4 und 5 — FocusVisible nimmt Block 5.
	l = l.FocusVisible()
	if !l.Focused() || l.focus != 5 {
		t.Fatalf("FocusVisible sollte Block 5 (im Viewport) fokussieren: focus=%d", l.focus)
	}
	// Der alte Weg (FocusLast) waere Block 9 — Regressionssicherung.
	if l2 := l.FocusLast(); l2.focus != 9 {
		t.Fatalf("FocusLast-Fallback erwartet Block 9: %d", l2.focus)
	}
}
