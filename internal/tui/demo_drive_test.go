package tui

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func lookPath(name string) (string, error) { return exec.LookPath(name) }

// TestDriveRenderTestEndToEnd seeds docs/render-test.md, runs the diagram
// conversion (with plantuml wrapped into PATH by the nix build) and asserts
// ImageBlocks appear. Dies ist die Akzeptanz fuer FR-27/FR-31 im echten
// Pipeline: Roh-Text -> Conversion -> Halbblock-ImageBlock.
func TestDriveRenderTestEndToEnd(t *testing.T) {
	b, err := os.ReadFile("../../docs/render-test.md")
	if err != nil {
		t.Skipf("render-test.md nicht im Testpfad: %v", err)
	}
	// plantuml muss im PATH sein (nix wrap). Bei CI ohne pfad skip.
	if _, err := lookPath("plantuml"); err != nil {
		t.Skip("plantuml nicht im PATH (nix wrap fehlt)")
	}
	t.Setenv("HERON_IMAGES", "force") // rig ohne Probe-Caps -> force
	r := newRig(t).seed(string(b))

	// Der erste WindowSizeMsg loest die Konvertierung aus (FR-6/27). Wir
	// holen den cmd direkt aus Update, statt via do() (das ihn zur Gänze
	// abarbeiten wuerde).
	nm, cmd := r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	r.m = nm.(Model)
	if cmd == nil {
		t.Fatal("kein Diagramm-Pending obwohl render-test.md plantuml enthaelt")
	}
	done := cmd().(diagramsDoneMsg)
	imgs := 0
	ascii := 0
	failed := 0
	for _, res := range done.results {
		switch {
		case res.png != nil:
			imgs++
		case res.ok && strings.TrimSpace(res.ascii) != "":
			ascii++
		default:
			failed++
		}
	}
	t.Logf("render-test: %d png, %d ascii, %d failed", imgs, ascii, failed)
	if imgs == 0 {
		t.Fatalf("kein PNG gerendert: %d ascii, %d failed", ascii, failed)
	}

	_, n := r.m.chat.applyDiagrams(done.results, true, TermCaps{CellW: 9, CellH: 20})
	if n == 0 {
		t.Fatal("applyDiagrams hat nichts angewendet")
	}
	t.Cleanup(KittyCleanup)
	found := 0
	for _, it := range r.m.chat.items {
		if ib, ok := it.(ImageBlock); ok {
			found++
			v := ib.View(118)
			// FR-27: kombinierte Uebertragung (a=T,t=f) + Platzhalter-Zellen
			// mit Bild-ID in der FG-Farbe (Spec: Unicode placeholders).
			if !strings.Contains(v, "\x1b_Ga=T,f=100,t=f,i=") {
				t.Errorf("ImageBlock id=%d fehlt Datei-Transmit (a=T,t=f)", ib.ID())
			}
			if ib.kittyID < 1 || ib.kittyID > 250 {
				t.Errorf("ImageBlock id=%d kittyID=%d ausserhalb 1..250", ib.ID(), ib.kittyID)
			}
			idFG := "38;5;" + strconv.Itoa(ib.kittyID) + "m"
			if !strings.Contains(v, idFG) {
				t.Errorf("ImageBlock id=%d: FG-Farbe %q (Bild-ID) fehlt", ib.ID(), idFG)
			}
			if !strings.Contains(v, placeholderChar) {
				t.Errorf("ImageBlock id=%d fehlt Platzhalterzeichen", ib.ID())
			}
			if !strings.Contains(v, string(diacritic(0))) {
				t.Errorf("ImageBlock id=%d fehlt Zeilen-Diakritikon", ib.ID())
			}
		}
	}
	if found == 0 {
		t.Fatal("kein ImageBlock nach applyDiagrams")
	}
}
