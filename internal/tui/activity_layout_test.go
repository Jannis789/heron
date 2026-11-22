package tui

import (
	"strings"
	"testing"
)

// TestActivityLayoutTracksTreeRows (User-Report 19.11.: "bei der Auswahl mit
// up und down verschiebt sich das ganze TUI nach oben"): Der Activity-Baum
// aendert seine Zeilenzahl, waehrend ein Turn laeuft (Thinking-Sektionen,
// Tool Calls, Settle) und per enter/Klick auf- und zuklappt. Jede dieser
// Mutationen muss den Chat-Viewport VERKLEINERN/VERGROESSERN (relayout) —
// sonst komponiert View() mehr Zeilen als das Terminal hat und der
// Zuschnitt schnitt frueher den HEADER samt erster Chatzeilen oben ab:
// Das ganze TUI schien sich nach oben zu verschieben, sobald man im
// Auswahlmodus (up/down/enter) unterwegs war.
func TestActivityLayoutTracksTreeRows(t *testing.T) {
	withTrueColor(t)
	r := newRig(t)
	r.size(80, 24)
	r.m.chat = r.m.chat.Append(NewTextBlock("Antwort", "1"))
	h0 := r.m.chat.height

	header := func() string {
		lines := strings.Split(r.m.View(), "\n")
		return strings.TrimSpace(stripANSI(lines[0]))
	}

	// Turn-Start + erster Baum-Inhalt: Der Viewport gibt Zeilen ab, der
	// Header bleibt Zeile 1.
	r.m.startTurn()
	r.do(thoughtMsg{text: "denk"})
	if h1 := r.m.chat.height; h1 >= h0 {
		t.Fatalf("chat height %d muss nach Turn-Start < %d sein (Baum belegt Zeilen)", h1, h0)
	}
	if got := header(); got != "heron · goose acp" {
		t.Fatalf("Header weg nach Turn-Start: erste Zeile = %q", got)
	}

	// Auswahlmodus: tab in den Baum, up/down bewegt den Cursor, enter
	// klappt auf — der Header bleibt STETS oben, die Hoehe folgt dem Baum.
	r.key("tab")
	r.key("down") // Cursor auf Tool Calls
	r.do(toolMsg{callID: "c1", name: "bash", kind: "execute", cmd: "make"})
	hAfterTool := r.m.chat.height
	r.key("enter") // Tool Calls aufklappen
	if h2 := r.m.chat.height; h2 >= hAfterTool {
		t.Fatalf("chat height %d muss nach Aufklappen < %d sein", h2, hAfterTool)
	}
	if got := header(); got != "heron · goose acp" {
		t.Fatalf("Header weg nach Aufklappen: erste Zeile = %q", got)
	}
	r.key("enter") // wieder zuklappen
	if h3 := r.m.chat.height; h3 != hAfterTool {
		t.Fatalf("chat height %d muss nach Zuklappen wieder %d sein", h3, hAfterTool)
	}
	for i := 0; i < 3; i++ {
		r.key("up")
		if got := header(); got != "heron · goose acp" {
			t.Fatalf("Header weg nach up: erste Zeile = %q", got)
		}
		r.key("down")
		if got := header(); got != "heron · goose acp" {
			t.Fatalf("Header weg nach down: erste Zeile = %q", got)
		}
	}

	// Turn-Ende: Settle schrumpft den Baum, der Chat kriegt Zeilen zurueck.
	r.m.endTurn()
	if h4 := r.m.chat.height; h4 <= hAfterTool {
		t.Fatalf("chat height %d muss nach Settle > %d sein", h4, hAfterTool)
	}
	if got := header(); got != "heron · goose acp" {
		t.Fatalf("Header weg nach Settle: erste Zeile = %q", got)
	}
}

// TestViewKeepsHeaderOnLayoutDrift: Selbst wenn Chat-Viewport und Bottom
// (Baum + Eingabe) ueber die Terminalhoehe driften, frisst der Zuschnitt in
// View() NUR Chatzeilen — die Titelzeile bleibt immer Zeile 1.
func TestViewKeepsHeaderOnLayoutDrift(t *testing.T) {
	withTrueColor(t)
	r := newRig(t)
	r.size(80, 10)
	r.m.chat = r.m.chat.Append(NewTextBlock("Erste Zeile des Chats", "1"))
	lines := strings.Split(r.m.View(), "\n")
	if got := strings.TrimSpace(stripANSI(lines[0])); got != "heron · goose acp" {
		t.Fatalf("erste Zeile = %q, will Header", got)
	}
	// Drift simulieren: Chat-Viewport groesser ziehen als der Screen Platz
	// hat (Baum unten + Footer) — der Header muss trotzdem bleiben.
	r.m.chat = r.m.chat.SetSize(r.m.width, r.m.chat.height+4)
	lines = strings.Split(r.m.View(), "\n")
	if got := strings.TrimSpace(stripANSI(lines[0])); got != "heron · goose acp" {
		t.Fatalf("erste Zeile bei Drift = %q, Header darf nie weggeschnitten werden", got)
	}
}
