package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// findCode returns the code block carrying the stable id.
func findCode(t *testing.T, l BlockList, id int) CodeBlock {
	t.Helper()
	for _, it := range l.items {
		if cb, ok := it.(CodeBlock); ok && cb.id == id {
			return cb
		}
	}
	t.Fatalf("no code block with id %d", id)
	return CodeBlock{}
}

// TestPlantUMLJobsCollectedOnce (FR-6): only unconverted plantuml blocks are
// queued, and a failed conversion is never queued again.
func TestPlantUMLJobsCollectedOnce(t *testing.T) {
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	jobs := r.m.diagramJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d want 1", len(jobs))
	}
	if jobs[0].id < 1 || strings.TrimSpace(jobs[0].code) == "" {
		t.Fatalf("bad job: %+v", jobs[0])
	}

	l, n := r.m.chat.applyDiagrams([]diagramResult{{id: jobs[0].id, ok: false}}, false, TermCaps{})
	if n != 1 {
		t.Fatalf("applied=%d want 1", n)
	}
	r.m.chat = l
	if again := r.m.diagramJobs(); len(again) != 0 {
		t.Fatalf("failed block queued again: %v", again)
	}
}

// TestPlantUMLApplyReplacesSource (FR-6): a successful conversion replaces
// the raw source in place, keeping the stable id.
func TestPlantUMLApplyReplacesSource(t *testing.T) {
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	id := r.m.diagramJobs()[0].id

	ascii := "+---+     +---+\n| A | --> | B |\n+---+     +---+"
	l, n := r.m.chat.applyDiagrams([]diagramResult{{id: id, ascii: ascii, ok: true}}, false, TermCaps{})
	if n != 1 {
		t.Fatalf("applied=%d want 1", n)
	}
	cb := findCode(t, l, id)
	if !cb.converted {
		t.Fatal("block not marked converted")
	}
	if !strings.Contains(cb.code, "+---+") {
		t.Fatalf("source not replaced: %q", cb.code)
	}
	if cb.id != id {
		t.Fatalf("id changed: %d want %d", cb.id, id)
	}
}

// TestPlantUMLNoPendingReturnsNil: no work -> no goroutine.
func TestPlantUMLNoPendingReturnsNil(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	if cmd := r.m.pendingDiagrams(); cmd != nil {
		t.Fatal("expected nil cmd when nothing is pending")
	}
}

// TestPlantUMLFallbackWhenMissing (FR-6): without plantuml on PATH the
// conversion fails and the raw source stays visible — no crash, no empty box.
func TestPlantUMLFallbackWhenMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	id := r.m.diagramJobs()[0].id
	before := findCode(t, r.m.chat, id).code

	cmd := r.m.pendingDiagrams()
	if cmd == nil {
		t.Fatal("expected a conversion cmd")
	}
	done, ok := cmd().(diagramsDoneMsg)
	if !ok {
		t.Fatalf("msg type %T", cmd())
	}
	if len(done.results) != 1 || done.results[0].ok {
		t.Fatalf("expected one failed result, got %+v", done.results)
	}

	l, n := r.m.chat.applyDiagrams(done.results, false, TermCaps{})
	if n != 1 {
		t.Fatalf("applied=%d", n)
	}
	if got := findCode(t, l, id).code; got != before {
		t.Fatalf("raw source changed on failure:\nbefore %q\nafter  %q", before, got)
	}
	if jobs := (Model{chat: l}).diagramJobs(); len(jobs) != 0 {
		t.Fatalf("still pending after failure: %v", jobs)
	}
}

// TestPlantUMLRealConversion runs the actual plantuml binary when available —
// this is the FR-6 acceptance path (halfblock image alongside raw source).
func TestPlantUMLRealConversion(t *testing.T) {
	if _, err := exec.LookPath("plantuml"); err != nil {
		t.Skip("plantuml not installed")
	}
	r := newRig(t).seed("```plantuml\n@startuml\nAlice -> Bob: hi\n@enduml\n```")
	cmd := r.m.pendingDiagrams()
	if cmd == nil {
		t.Fatal("expected a conversion cmd")
	}
	done, ok := cmd().(diagramsDoneMsg)
	if !ok {
		t.Fatalf("msg type %T", cmd())
	}
	if len(done.results) != 1 {
		t.Fatalf("results=%d want 1", len(done.results))
	}
	if !done.results[0].ok {
		t.Fatalf("plantuml failed: %q", done.results[0].ascii)
	}
}

// seedDoc baut ein Dokument mit plantuml-Zaun in der Mitte und viel Text
// drumherum — die Geometrie des Sprung-Szenarios: Der Zaun ist niedrig,
// das fertige Bild (800x600) deutlich hoeher, das Dokument laenger als das
// Fenster.
func seedDoc(pre, post int) string {
	var sb strings.Builder
	for i := 0; i < pre; i++ {
		fmt.Fprintf(&sb, "Zeile vor dem Diagramm %03d\n", i)
	}
	sb.WriteString("```plantuml\n@startuml\nA -> B\nB --> A\n@enduml\n```\n")
	for i := 0; i < post; i++ {
		fmt.Fprintf(&sb, "Zeile nach dem Diagramm %03d\n", i)
	}
	return sb.String()
}

// imgResults wandelt alle offenen Jobs in (fake) PNG-Ergebnisse um — die
// Bloecke werden damit zu ImageBlocks mit anderer Hoehe.
func imgResults(m Model) []diagramResult {
	var res []diagramResult
	for _, j := range m.diagramJobs() {
		res = append(res, diagramResult{id: j.id, lang: j.lang, png: realPNG(800, 600), ascii: j.code, src: j.code, ok: true})
	}
	return res
}

// TestPlantUMLApplyKeepsBottomPinned: Startup mit Seed — der Pager klebt am
// Ende (follow). Laeuft die Konvertierung nach und ersetzt die Zaeune durch
// HOEHERE Bilder, muss der Viewport unten FESTGEHALTEN werden (wie bei
// Stream/Think), statt auf dem alten Zeilenoffset mitten im Dokument
// stehen zu bleiben: Beim Oeffnen von quirks-test.md startete die Ansicht
// sonst ohne Tastendruck bei ~der Haelfte des Dokuments.
func TestPlantUMLApplyKeepsBottomPinned(t *testing.T) {
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed(seedDoc(40, 200))
	nm, _ := r.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	r.m = nm.(Model)
	if !r.m.chat.follow {
		t.Fatal("Seed-Startup sollte follow=true haben (Pager am Ende)")
	}
	want := lastVisibleLine(r.m.chat)
	if !strings.Contains(want, "nach dem Diagramm 199") {
		t.Fatalf("Vorher am Dokumentende? Letzte Zeile: %q", want)
	}
	r.do(diagramsDoneMsg{results: imgResults(r.m)})
	l := r.m.chat
	if l.offset != l.totalLines()-l.height {
		t.Fatalf("offset=%d, want %d (unten festgehalten)", l.offset, l.totalLines()-l.height)
	}
	if got := lastVisibleLine(l); got != want {
		t.Fatalf("letzte sichtbare Zeile gewandert:\nvorher %q\nnachher %q", want, got)
	}
}

// TestPlantUMLApplyKeepsScrolledAnchor: Der Nutzer hat HOCHGESCROLLT
// (follow=false), wenn die Bilder nachladen — dann muss der Inhalt am
// oberen Viewportrand der GLEICHE bleiben (block-relativer Anker), egal
// wie die Diagramm-Hoehe waechst.
func TestPlantUMLApplyKeepsScrolledAnchor(t *testing.T) {
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed(seedDoc(40, 200))
	nm, _ := r.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	r.m = nm.(Model)
	r.do(tea.KeyMsg{Type: tea.KeyPgUp}) // Nutzer scrollt hoch: follow=false
	if r.m.chat.follow {
		t.Fatal("pgup sollte follow loeschen")
	}
	want := firstVisibleLine(r.m.chat)
	r.do(diagramsDoneMsg{results: imgResults(r.m)})
	if got := firstVisibleLine(r.m.chat); got != want {
		t.Fatalf("obere Viewportzeile gewandert:\nvorher %q\nnachher %q", want, got)
	}
}

// firstVisibleLine rendert die erste Zeile des Chat-Viewports.
func firstVisibleLine(l BlockList) string {
	return strings.Split(l.View(), "\n")[0]
}

// lastVisibleLine rendert die letzte Zeile des Chat-Viewports.
func lastVisibleLine(l BlockList) string {
	lines := strings.Split(l.View(), "\n")
	return lines[len(lines)-1]
}

// TestPlantUMLApplyKeepsMark: Ein markierter Zaun bleibt durch die
// Konvertierung zum Bild MARKIERT (Chip sichtbar, Auswahl zaehlt) — vorher
// verlor nur der Ersatz den Chip, die marks-Map behielt die Auswahl
// unsichtbar (y kopierte sie trotzdem).
func TestPlantUMLApplyKeepsMark(t *testing.T) {
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed("```plantuml\n@startuml\nA -> B\n@enduml\n```")
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	r.do(runes(" "))
	if r.m.chat.markedCount() != 1 {
		t.Fatal("Vorbedingung: genau ein markierter Block")
	}
	r.do(diagramsDoneMsg{results: imgResults(r.m)})
	if n := r.m.chat.markedCount(); n != 1 {
		t.Fatalf("markedCount=%d want 1 nach Konvertierung", n)
	}
	found := false
	for _, it := range r.m.chat.items {
		if ib, ok := it.(ImageBlock); ok {
			found = true
			if !ib.Marked() {
				t.Fatal("ImageBlock hat die Markierung des Zauns verloren")
			}
		}
	}
	if !found {
		t.Fatal("kein ImageBlock nach applyDiagrams")
	}
}
