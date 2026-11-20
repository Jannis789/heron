package tui

import (
	"strings"
	"testing"

	"heron/internal/render"
)

// User-Report: "wenn ich im auswahl modus fetten text auswähle passiert das
// anstelle Sonstiges: fett anzuzeigen" — auf dem Schirm bzw. im Copy-Payload
// erscheint "[1msonstiges:[22m" (SGR ohne ESC-Byte = sichtbarer Muell).
//
// Detektor: StripANSI entfernt \x1b[...m komplett. Ueberlebt "[1m"/"[22m"
// das Strippen, war das ESC-Byte unterwegs verloren gegangen (zerschnittene
// Sequenz) — exakt der Fall aus dem ansiSeqLen-Kommentar.
func sgrGarbage(s string) []string {
	plain := render.StripANSI(s)
	var out []string
	for _, kw := range []string{"[1m", "[22m", "[3m", "[23m", "[4m", "[24m"} {
		if strings.Contains(plain, kw) {
			out = append(out, kw)
		}
	}
	return out
}

const reproMD = `Hier ist die Antwort.

**Nix-Setup:**
- ` + "`flake.nix`" + ` + ` + "`flake.lock`" + ` + ` + "`nix/`" + `-Verzeichnis

**Sonstiges:**
- ` + "`diagram.puml`" + ` / ` + "`diagram-seq.puml`" + ` – PlantUML-Diagramme (Architektur + Sequenz)
- Ein ` + "`.ruff_cache`" + ` – deutet auf Python-Code hin

Und **fett im Text** am Ende als weiterer Fall.`

func TestReproSelectBoldSonstigesCursorMode(t *testing.T) {
	withTrueColor(t)
	for _, w := range []int{60, 80, 100} {
		r := newRig(t).seed(reproMD)
		r.size(w, 30)
		r.key("shift+tab") // Cursor-Mode (Auswahlmodus)
		if !r.m.chat.charActive() {
			t.Fatalf("w=%d: cursor mode did not activate", w)
		}
		// Auswahl schrittweise ueber den ganzen Text ziehen, jeden Zwischen-
		// stand pruefen (der Muell kann erst ab einer bestimmten Kante).
		for step := 0; step < 12; step++ {
			r.key("shift+down")
			v := r.m.chat.View()
			if g := sgrGarbage(v); len(g) > 0 {
				for _, ln := range strings.Split(v, "\n") {
					if pg := sgrGarbage(ln); len(pg) > 0 {
						t.Fatalf("w=%d step=%d: Sichtbarer SGR-Muell %v in Zeile: %q",
							w, step, pg, render.StripANSI(ln))
					}
				}
			}
		}
		// Copy-Payload (c im Cursor-Mode): IMMER plain.
		r.key("c")
		if len(r.copied) == 0 {
			t.Fatalf("w=%d: c hat nicht kopiert", w)
		}
		got := r.copied[len(r.copied)-1]
		if strings.Contains(got, "\x1b") {
			t.Fatalf("w=%d: Copy traegt ESC: %q", w, got)
		}
		if g := sgrGarbage(got); len(g) > 0 {
			t.Fatalf("w=%d: Copy traegt SGR-Reste %v: %q", w, g, got)
		}
		if !strings.Contains(got, "Sonstiges:") {
			t.Fatalf("w=%d: Copy verliert 'Sonstiges:': %q", w, got)
		}
	}
}

func TestReproSelectBoldSonstigesShiftMode(t *testing.T) {
	withTrueColor(t)
	r := newRig(t).seed(reproMD)
	r.size(80, 30)
	r.key("tab") // in den Chat
	for step := 0; step < 8; step++ {
		r.key("shift+down")
		v := r.m.chat.View()
		if g := sgrGarbage(v); len(g) > 0 {
			for _, ln := range strings.Split(v, "\n") {
				if pg := sgrGarbage(ln); len(pg) > 0 {
					t.Fatalf("step=%d: Sichtbarer SGR-Muell %v in Zeile: %q",
						step, pg, render.StripANSI(ln))
				}
			}
		}
	}
	r.key("c")
	if len(r.copied) == 0 {
		t.Fatal("c hat nicht kopiert")
	}
	got := r.copied[len(r.copied)-1]
	if strings.Contains(got, "\x1b") {
		t.Fatalf("Copy traegt ESC: %q", got)
	}
	if g := sgrGarbage(got); len(g) > 0 {
		t.Fatalf("Copy traegt SGR-Reste %v: %q", g, got)
	}
}
