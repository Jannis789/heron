package tui

import (
	"strconv"
	"strings"
	"testing"
)

// ---- Auswahl-Vertrag (Entscheidung): ZWEI Werkzeuge, EINE Auswahl --------
//
// heron hat zwei Auswahl-Werkzeuge mit klar getrennten Rollen:
//
//	Block-Auswahl  (grob):  shift+up/down im Antwort-Modus. Anker = Block.
//	                          c kopiert GANZE Bloecke. Cursor = Underscore.
//	Zeichen-Cursor (fein):  shift+enter. Caret im Text, shift+Pfeile waehlen
//	                          Buchstaben/Zeilen. c kopiert EXAKT.
//
// Uebergangsgesetz (nur EIN sichtbares Auswahl-Signal zu jeder Zeit):
//
//	EINTRITT   shift+enter auf laufender Block-Auswahl KONVERTIERT sie in
//	           die feine Auswahl ueber demselben Bereich (Anker bleibt).
//	TOGGLE-EXIT shift+enter: die feine Auswahl bleibt als EINZIGE sichtbare
//	           und kopierbare Auswahl (residual); der CURSOR ist weg.
//	ESC        Abbruch: Stufe 1 kollabiert (bzw. kehrt zur wartenden Block-
//	           Auswahl zurueck), weitere esc loesen alles — einziger Verlust.
//	c / enter  kopieren GENAU die aktive Auswahl.
//
// Der Vertrag wird hier fuer ALLE Kombinationen von Herkunft (frisches Caret
// vs. konvertierte Block-Auswahl) und Austritt (Toggle vs. esc) abgeprueft.

// selStates bündelt die sichtbaren Auswahl-Zustands-Signale.
type selStates struct {
	block    bool // Block-Auswahl aktiv (selActive)
	caret    bool // Zeichen-Cursor aktiv (charActive)
	residual bool // erhaltene feine Auswahl (residualSelShown)
	invert   int  // Anzahl invertierter Caret-Zellen (\x1b[7m) im View
	marks    int  // markierte Bloecke (FR-29)
}

// states liest die aktuellen Signale aus Model+View.
func states(r *rig) selStates {
	s := selStates{
		block:    r.m.chat.selActive(),
		caret:    r.m.chat.charActive(),
		residual: r.m.chat.residualSelShown(),
		marks:    r.m.chat.markedCount(),
	}
	s.invert = strings.Count(r.view(), "\x1b[7m")
	return s
}

// visibleSum verlangt: hoechstens EIN sichtbares Auswahl-Signal.
// block+residual zusammen ist nie erlaubt (Doppel-Signal, FR-39-Geist).
func (s selStates) visibleSum() int {
	n := 0
	if s.block {
		n++
	}
	if s.caret {
		n++
	}
	if s.residual {
		n++
	}
	return n
}

func (s selStates) String() string {
	return "block=" + b(s.block) + " caret=" + b(s.caret) + " residual=" + b(s.residual) +
		" invert=" + strconv.Itoa(s.invert) + " marks=" + strconv.Itoa(s.marks)
}

func b(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// TestSelectionContractToggleExit: Der TOGGLE-Austritt ist der ERHALT-Pfad —
// unabhaengig von der Herkunft (frisches Caret ODER konvertierte Block-
// Auswahl) bleibt GENAU die feine Auswahl zurueck: sichtbar, kopierbar,
// ohne Caret, ohne Block-Auswahl daneben (ein Signal).
func TestSelectionContractToggleExit(t *testing.T) {
	cases := []struct {
		name string
		seed string
		pre  func(r *rig) // Auswahl vorbereiten, Cursor-Modus betreten
		md   string      // erwarteter c-Payload der erhaltenen Auswahl
	}{
		{
			name: "frisches-caret-nicht-leer",
			seed: "abcdef",
			pre: func(r *rig) {
				enterCursor(r)
				r.key("shift+right").key("shift+right").key("shift+right")
			},
			md: "abc",
		},
		{
			name: "konvertierte-block-auswahl",
			seed: "aaa\n\nbbb\n\nccc",
			pre: func(r *rig) {
				r.key("tab")                          // Antwort-Modus, Fokus letzter Block (2)
				r.key("up").key("up")                 // Fokus Block 0
				r.key("shift+down")                   // Block-Auswahl 0..1 (grob)
				r.key("shift+enter")                  // Konvertierung: fein ueber 0..1
			},
			md: "aaa\n\nbbb",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t).seed(tc.seed)
			tc.pre(r)
			if !r.m.chat.charActive() {
				t.Fatal("Vorbedingung: Cursor-Modus aktiv")
			}
			if st := states(r); st.visibleSum() != 1 || st.invert == 0 {
				t.Fatalf("Vorbedingung: genau EIN Signal mit Caret, got %s", st)
			}
			r.key("shift+tab") // Toggle-Austritt
			st := states(r)
			if st.caret {
				t.Fatalf("Austritt: Cursor-Modus weg, got %s", st)
			}
			if st.invert != 0 {
				t.Fatalf("Austritt: der CURSOR (invertierte Zelle) muss verschwinden, got %s", st)
			}
			if !st.residual || st.block {
				t.Fatalf("Austritt: GENAU die feine Auswahl bleibt, got %s", st)
			}
			if st.visibleSum() != 1 {
				t.Fatalf("Austritt: genau EIN sichtbares Signal, got %s", st)
			}
			if !strings.Contains(r.view(), r.m.chat.charSelBgSgr()) {
				t.Fatal("die erhaltene Auswahl muss im View sichtbar sein")
			}
			r.key("c")
			got := r.copiedAll()
			if len(got) == 0 || got[len(got)-1] != tc.md {
				t.Fatalf("c muss die erhaltene feine Auswahl kopieren %q, got %q", tc.md, got)
			}
		})
	}
}

// TestSelectionContractEscAborts: esc ist der ABBRUCH-Pfad — der einzige
// Auswahl-Verlust. Vom frischen Caret kollabiert die Stufe 1 (Modus bleibt,
// Caret-Phase unveraendert), die Stufe 2 verlaesst den Modus. Aus der
// KONVERTIERTEN Block-Auswahl kehrt esc mit einem Schritt zur wartenden
// Block-Auswahl zurueck (kein Flip-Flop), der naechste esc loest sie.
func TestSelectionContractEscAborts(t *testing.T) {
	t.Run("frisches-caret", func(t *testing.T) {
		r := enterCursor(newRig(t).seed("abcdef"))
		r.key("shift+right").key("shift+right") // Auswahl "ab"
		r.key("esc")                            // Stufe 1: kollabiert, Modus bleibt
		if !r.m.chat.charActive() || !r.m.chat.charEmptySelection() {
			t.Fatal("esc Stufe 1 kollabiert die Auswahl aufs Caret (Modus bleibt)")
		}
		if st := states(r); st.visibleSum() != 1 || st.caret == false {
			t.Fatalf("Stufe 1: nur der Cursor, got %s", st)
		}
		r.key("esc") // Stufe 2: Modus verlassen
		if st := states(r); st.caret || st.visibleSum() != 0 {
			t.Fatalf("Stufe 2: alles weg (oder nur Verblassen), got %s", st)
		}
	})
	t.Run("konvertierte-block-auswahl", func(t *testing.T) {
		r := newRig(t).seed("aaa\n\nbbb\n\nccc")
		r.key("tab").key("up").key("up") // Fokus Block 0
		r.key("shift+down")              // Block-Auswahl 0..1
		r.key("shift+enter")             // Konvertierung
		if !r.m.chat.charActive() || r.m.chat.selActive() {
			t.Fatal("Vorbedingung: Cursor-Modus, Block-Auswahl konsumiert (EIN Signal)")
		}
		r.key("esc") // SOFORT zurueck in die Block-Auswahl (kein Kollaps)
		st := states(r)
		if st.caret || !st.block || st.residual {
			t.Fatalf("esc kehrt zur wartenden Block-Auswahl zurueck, got %s", st)
		}
		r.key("esc")
		if st := states(r); st.block || st.residual || st.caret {
			t.Fatalf("das naechste esc loest die Block-Auswahl, got %s", st)
		}
	})
}

// TestSelectionContractOneSignalEver: Der Kern-Invariant — waehrend des
// gesamten Lebenszyklus (Block -> Konvertierung -> Cursor -> Toggle-Austritt
// -> Aussen-Erweiterung -> esc) nie mehr als EIN sichtbares Auswahl-Signal
// und nie ein Caret ausserhalb des Modus.
func TestSelectionContractOneSignalEver(t *testing.T) {
	r := newRig(t).seed("aaa\n\nbbb\n\nccc")
	r.key("tab").key("up").key("up")
	r.key("shift+down") // Block-Auswahl 0..1
	if st := states(r); !st.block || st.visibleSum() != 1 {
		t.Fatalf("Block-Auswahl: ein Signal, got %s", st)
	}
	r.key("shift+enter") // Konvertierung
	if st := states(r); !st.caret || st.visibleSum() != 1 {
		t.Fatalf("Konvertierung: nur der Cursor zaehlt, got %s", st)
	}
	r.key("shift+tab") // Toggle-Austritt
	if st := states(r); !st.residual || st.visibleSum() != 1 || st.invert != 0 {
		t.Fatalf("Austritt: nur die feine Auswahl, got %s", st)
	}
	r.key("shift+down") // von aussen klassisch erweitern (residual)
	if st := states(r); !st.residual || st.visibleSum() != 1 {
		t.Fatalf("Aussen-Erweiterung: residual bleibt einzige Auswahl, got %s", st)
	}
	r.key("esc")
	if st := states(r); st.residual {
		t.Fatalf("esc loest die erhaltene Auswahl, got %s", st)
	}
}
