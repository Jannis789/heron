package tui

import (
	"strings"
	"testing"
)

// User-Report 23.11., zwei Baustellen am Activity-Baum:
//  1. Der Auswahl-Unterstrich der Kopfzeilen (Thinking etc.) darf die
//     Faltmarke LINKS nicht mitmarkieren — der Pfeil zeigt den Aufklapp-
//     zustand, unterstrichen wird nur alles DARAUF Folgende.
//  2. esc räumt in STUFEN ab (wie im Chat): erst offene Knoten zuklappen,
//     der Baum bleibt selektiert — erst wenn er ganz geschlossen ist,
//     verlässt der nächste Druck ihn und wechselt den Tab-Modus.

// TestHeaderUnderlineSkipsFoldMark: die Unterstreichung beginnt erst NACH
// der Faltmarke (▸/▼) — Marke trägt keinen SGR 4, Label schon.
func TestHeaderUnderlineSkipsFoldMark(t *testing.T) {
	withTrueColor(t)
	a := NewActivityTree(mustTheme(t)).
		Thought("denken").
		Tool(ToolUpdate{CallID: "c1", Label: "bash", Kind: "execute", Command: "make", Done: true}).
		Focus(60, 20)
	rows := a.View(60, 20)
	checkMarkRow(t, rows[0], "Thinking-Kopf")
	a = a.Move(1, 60, 20)
	rows = a.View(60, 20)
	checkMarkRow(t, rows[1], "Tool-Kopf")
}

// checkMarkRow prüft EINE Kopfzeile: Faltmarke vorhanden, Unterstrich
// vorhanden, und die erste Underline-Sequenz beginnt hinter der Marke.
func checkMarkRow(t *testing.T, row, what string) {
	t.Helper()
	mark := strings.IndexAny(row, "▸▼")
	if mark < 0 {
		t.Fatalf("%s ohne Faltmarke: %q", what, row)
	}
	u := underlineIndex(row)
	if u < 0 {
		t.Fatalf("%s: Label nicht unterstrichen: %q", what, row)
	}
	if u < mark {
		t.Fatalf("%s: Unterstrich erfasst auch die Faltmarke (Underline bei %d, Marke bei %d): %q",
			what, u, mark, row)
	}
}

// underlineIndex liefert den Byte-Index der ersten CSI-Sequenz, die den
// SGR-Parameter 4 (Underline) trägt — Position statt Bool, weil die Marke
// links NICHT unterstrichen sein darf, das Label rechts schon (underlined()
// in activity_test.go beantwortet nur "irgendwo").
func underlineIndex(s string) int {
	off := 0
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return -1
		}
		rest := s[i+2:]
		j := strings.IndexByte(rest, 'm')
		if j < 0 {
			return -1
		}
		for _, p := range strings.Split(rest[:j], ";") {
			if p == "4" {
				return off + i
			}
		}
		off += i + 2 + j + 1
		s = rest[j+1:]
	}
}

// TestEscClosesTreeBeforeLeaving: esc-Stufen im Baum-Fokus — Stufe 1
// klappt Thinking UND Tool-Kinds gemeinsam zu (Baum bleibt selektiert,
// Chat bekommt den Platz sofort zurück), Stufe 2 verlässt den Baum in
// die Eingabe. Ein bereits geschlossener Baum verlässt ihn mit dem
// ERSTEN Druck (kein toter Zwischenschritt).
func TestEscClosesTreeBeforeLeaving(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "denk"})
	r.do(toolMsg{callID: "c1", name: "bash", kind: "execute", cmd: "make", done: true})
	r.key("tab")
	if !r.m.activity.Focused() {
		t.Fatal("tab muss den Baum selektieren")
	}
	// Reihenfolge: Execute ZUERST aufklappen (Thinking noch zu — ↓ läuft
	// sonst in die Thought-Sektion und enter öffnet die Detailbox), dann
	// zurück und Thinking auf. Am Ende sind beide Knoten offen.
	r.key("down")
	r.key("enter") // Execute auf
	r.key("up")
	r.key("enter") // Thinking auf
	if !r.m.activity.expandThinking || !r.m.activity.expanded("execute") {
		t.Fatal("Ausgang: beide Knoten müssen offen sein")
	}
	rowsOpen := r.m.activityRows()

	r.key("esc") // Stufe 1: zuklappen, Baum bleibt
	if !r.m.activity.Focused() {
		t.Fatal("esc (Stufe 1) darf den Baum noch nicht verlassen")
	}
	if r.m.inputFocus {
		t.Fatal("esc (Stufe 1) darf den Tab-Modus noch nicht wechseln")
	}
	if r.m.activity.expandThinking || r.m.activity.expanded("execute") {
		t.Fatal("esc (Stufe 1) muss alle offenen Knoten zuklappen")
	}
	if rows := r.m.activityRows(); rows >= rowsOpen {
		t.Fatalf("Zuklappen muss den Baum schrumpfen: %d → %d", rowsOpen, rows)
	}

	r.key("esc") // Stufe 2: Baum verlassen
	if r.m.activity.Focused() || !r.m.inputFocus {
		t.Fatal("esc (Stufe 2) muss den Baum freigeben und ins Eingabefeld kehren")
	}

	// Geschlossener Baum: EIN esc genügt (kein leerer Zwischenschritt).
	r.key("tab")
	if !r.m.activity.Focused() {
		t.Fatal("tab muss zurück in den Baum führen")
	}
	r.key("esc")
	if r.m.activity.Focused() || !r.m.inputFocus {
		t.Fatal("am geschlossenen Baum muss ein einzelnes esc genügen")
	}
}
