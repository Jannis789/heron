package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// toolCount zaehlt die Tool-Karten im Chat.
func toolCount(r *rig) int {
	n := 0
	for _, it := range r.m.chat.items {
		if _, ok := it.(ToolBlock); ok {
			n++
		}
	}
	return n
}

// toolAt liefert die erste Tool-Karte (mit Index).
func toolAt(r *rig) (int, ToolBlock, bool) {
	for i, it := range r.m.chat.items {
		if tb, ok := it.(ToolBlock); ok {
			return i, tb, true
		}
	}
	return 0, ToolBlock{}, false
}

// TestToolCardUpsertsInPlace (FR-2/FR-31): tool_call startet die laufende
// Karte im Chat, tool_call_update aktualisiert DIESELBE Karte in Place
// (callID-Identitaet) — Zustand ✓, Inhalt aktualisiert, kein Duplikat.
func TestToolCardUpsertsInPlace(t *testing.T) {
	r := newRig(t).seed("Antwort kommt gleich.")
	r.do(toolMsg{callID: "call-1", name: "bash", text: "go build ./...", done: false})
	r.do(toolMsg{callID: "call-1", name: "bash", text: "go build ./...\nok", done: true})
	if n := toolCount(r); n != 1 {
		t.Fatalf("genau EINE Karte erwartet (In-place-Update), got %d", n)
	}
	_, tb, ok := toolAt(r)
	if !ok {
		t.Fatal("keine Tool-Karte im Chat")
	}
	if tb.running {
		t.Fatal("done-Update muss die Karte auf fertig stellen")
	}
	if !strings.Contains(r.screen(), "🔧 bash ✓") {
		t.Fatalf("Karte zeigt Titel und Zustand: %q", r.screen())
	}
	if tb.ID() < 1 {
		t.Fatalf("Karte braucht eine stabile Blocknummer, got %d", tb.ID())
	}
}

// TestToolCardCollapsesAndExpands: Zugeklappt zeigt die Karte die LETZTEN
// Zeilen (das Resultat lebt am Ende) plus [+ show more]; enter am
// fokussierten Block klappt auf, Footer verschwindet bei kurzem Inhalt.
func TestToolCardCollapsesAndExpands(t *testing.T) {
	long := "line1\nline2\nline3\nline4\nline5"
	r := newRig(t).seed("x")
	r.do(toolMsg{callID: "c", name: "bash", text: long, done: true})
	r.contains("+ show more").notContains("line1").notContains("line2")
	// Fokus auf die Karte (letztes Segment) und aufklappen.
	r.key("tab")
	r.key("enter")
	r.contains("line1").contains("- show less")
	// Copy-Payload: Titel + Inhalt.
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || !strings.Contains(got[len(got)-1], "bash") ||
		!strings.Contains(got[len(got)-1], "line3") {
		t.Fatalf("c muss Titel+Inhalt kopieren: %q", got)
	}
}

// TestToolCardInShiftSelectionChain: Die Karte ist ein volles Ketten-
// Segment — tab fokussiert sie, shift+enter startet die Auswahl AUF ihr,
// shift+up waechst darueber hinaus (Sektions-Modus), und der Zeichen-Modus
// adressiert ihren Inhalt buchstabenweise.
func TestToolCardInShiftSelectionChain(t *testing.T) {
	r := newRig(t).seed("oben\n\nunten")
	r.do(toolMsg{callID: "c0", name: "bash", text: "go test ./...", done: true})
	last := len(r.m.chat.items) - 1
	r.key("tab") // Fokus: letztes Segment = die Karte
	if r.m.chat.focus != last {
		t.Fatalf("tab muss die Tool-Karte fokussieren, got focus=%d", r.m.chat.focus)
	}
	// shift+up startet die Block-Auswahl am fokussierten Block (die Karte
	// ist der Anker) und greift SOFORT den Block darueber — der Erstdruck
	// waechst inklusive (shift+enter ist der Zeichen-Cursor, FR-42).
	r.key("shift+up")
	if !r.m.chat.selActive() || r.m.chat.selCur != last-1 {
		t.Fatalf("shift+up muss ueber die Karte wachsen: selCur=%d", r.m.chat.selCur)
	}
	lo, hi := r.m.chat.selBounds()
	if lo != last-1 || hi != last {
		t.Fatalf("shift+up muss ueber die Karte wachsen: %d..%d", lo, hi)
	}
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || !strings.Contains(got[len(got)-1], "go test") {
		t.Fatalf("Bereichskopie muss die Karte einschliessen: %q", got)
	}
	// Zeichen-Modus in der Karte: Cursor zurueck auf die Karte (shift+up
	// hatte ihn ein Block darueber geparkt), dann abtauchen — die Karte
	// klappt dabei auf und der Bereich waechst buchstabenweise.
	r.key("shift+down")
	if r.m.chat.selCur != last {
		t.Fatalf("Cursor zurueck auf die Karte: %d", r.m.chat.selCur)
	}
	r.key("shift+tab")
	if !r.m.chat.charActive() {
		t.Fatal("Zeichen-Cursor muss in der Karte aufgehen")
	}
	// esc ueber der wartenden Block-Auswahl: SOFORT zurueck in sie — die
	// konvertierte Zeichen-Auswahl kollabiert NICHT erst (User-Report:
	// Auswahl "weg" und beim naechsten esc "wieder da" wirkte wie ein Bug).
	r.key("esc")
	if r.m.chat.charActive() || !r.m.chat.selActive() {
		t.Fatal("esc verlaesst den Cursor sofort in die Block-Auswahl")
	}
	r.key("esc") // Block-Auswahl loesen — Fokus parkt auf der Leseposition
	if r.m.chat.selActive() {
		t.Fatal("zweites esc loest die Block-Auswahl")
	}
	if r.m.chat.focus != last || r.m.inputFocus {
		t.Fatalf("esc parkt den Fokus auf der Karte (Leseposition), got focus=%d input=%v",
			r.m.chat.focus, r.m.inputFocus)
	}
	// Frischer Zeichen-Cursor OHNE wartende Block-Auswahl: shift+enter
	// spawnt das Caret am Karten-Anfang: Die EIGENE buchstabenweise
	// Auswahl waechst vom Caret — und esc kollabiert hier wieder in
	// STUFEN (Stufe 1 Kollaps, Stufe 2 raus).
	r.key("shift+tab")
	if !r.m.chat.charActive() || !r.m.chat.charEmptySelection() {
		t.Fatal("frischer Zeichen-Cursor startet mit leerer Auswahl am Karten-Anfang")
	}
	r.key("shift+right").key("shift+right")
	r.key("c")
	got = r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "go" {
		t.Fatalf("Buchstaben-Kopie aus der Karte: %q", got)
	}
}

// TestToolCardStatusLineIntact: Die Statuszeile (FR-2) bleibt neben der
// Karte — laufender Call zeigt 🔧 + Label, done raeumt.
func TestToolCardStatusLineIntact(t *testing.T) {
	r := newRig(t).seed("x")
	r.do(toolMsg{callID: "c1", name: "read_file", text: "", done: false})
	if r.m.toolStatus != "read_file" {
		t.Fatalf("Statuszeile muss den Tool-Namen zeigen: %q", r.m.toolStatus)
	}
	r.do(toolMsg{callID: "c1", name: "read_file", text: "inhalt", done: true})
	if r.m.toolStatus != "" {
		t.Fatalf("done muss die Statuszeile raeumen: %q", r.m.toolStatus)
	}
}

// TestToolCardMarksSurviveUpdate: Die Shift-Auswahl-Markierung (Render-
// Highlight) und der Fokus ueberleben das In-place-Update der Karte.
func TestToolCardMarksSurviveUpdate(t *testing.T) {
	r := newRig(t).seed("x")
	r.do(toolMsg{callID: "k", name: "bash", text: "a", done: false})
	r.key("tab") // Fokus auf die Karte
	r.do(toolMsg{callID: "k", name: "bash", text: "a\nb", done: true})
	_, tb, ok := toolAt(r)
	if !ok || !tb.Focused() {
		t.Fatal("Fokus muss das Karten-Update ueberleben")
	}
}

// dummy: tea.Msg-Typ-Kompatibilitaet fuer do() (toolMsg ist unexportiert).
var _ tea.Msg = toolMsg{}

// TestToolCardKindGlyphAndErrorState (FR-2): Das Kopf-Icon folgt der
// ACP-Tool-Kategorie ($ execute, ↯ fetch …), ein gescheitertes Call zeigt
// ✗ statt ✓ — als In-place-Update auf derselben Karte (callID).
func TestToolCardKindGlyphAndErrorState(t *testing.T) {
	r := newRig(t)
	r.do(toolMsg{callID: "sh", name: "bash", kind: "execute", text: "go test ./...", done: false})
	_, tb, ok := toolAt(r)
	if !ok {
		t.Fatal("keine Tool-Karte im Chat")
	}
	if head := stripANSI(tb.View(80)); !strings.Contains(head, "$ bash …") {
		t.Fatalf("laufende Execute-Karte: Kopf %q", head)
	}
	r.do(toolMsg{callID: "sh", name: "bash", kind: "execute", text: "go test ./...\nFAIL", done: true, failed: true})
	_, tb, _ = toolAt(r)
	if head := stripANSI(tb.View(80)); !strings.Contains(head, "$ bash ✗") {
		t.Fatalf("fehlgeschlagene Karte: Kopf %q", head)
	}
	// Erfolgreiche fetch-Karte: ↯-Icon und ✓.
	r.do(toolMsg{callID: "w", name: "fetch docs", kind: "fetch", text: "curl example.com", done: true})
	if n := toolCount(r); n != 2 {
		t.Fatalf("2 Karten erwartet, got %d", n)
	}
	var wb ToolBlock
	for _, it := range r.m.chat.items {
		if tb, ok := it.(ToolBlock); ok && tb.callID == "w" {
			wb = tb
		}
	}
	if head := stripANSI(wb.View(80)); !strings.Contains(head, "↯ fetch docs ✓") {
		t.Fatalf("fetch-Karte: Kopf %q", head)
	}
}

// TestToolCallFooterCounter (FR-31): Der Footer zaehlt die Tool-Calls des
// Turns als distinct callIDs (In-place-Updates zaehlen nicht doppelt), ein
// neuer Turn setzt zurueck.
func TestToolCallFooterCounter(t *testing.T) {
	r := newRig(t)
	r.do(toolMsg{callID: "a", name: "bash", kind: "execute", text: "x", done: false})
	r.do(toolMsg{callID: "a", name: "bash", kind: "execute", text: "x", done: true})
	if foot := stripANSI(r.m.footerView()); !strings.Contains(foot, "🔧 1 tool call") {
		t.Fatalf("Footer nach 1 Call: %q", foot)
	}
	r.do(toolMsg{callID: "b", name: "fetch", kind: "fetch", text: "y", done: true})
	if foot := stripANSI(r.m.footerView()); !strings.Contains(foot, "🔧 2 tool calls") {
		t.Fatalf("Footer nach 2 Calls: %q", foot)
	}
	r.m.startTurn()
	if foot := stripANSI(r.m.footerView()); strings.Contains(foot, "tool call") {
		t.Fatalf("neuer Turn muss den Zaehler raeumen: %q", foot)
	}
}
