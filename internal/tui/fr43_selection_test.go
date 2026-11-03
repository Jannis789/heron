package tui

import (
	"strconv"
	"strings"
	"testing"
)

// ---- FR-43: Pausen-Selektion — Auswahl geht nur per esc verloren ---------

// hexSgr baut "R;G;B" aus einer #RRGGBB-Farbe (Vergleich von TrueColor-
// Rahmenfarben im gerenderten View, ohne lipgloss-Formatdetails).
func hexSgr(hex string) string {
	r, g, b, ok := hexToRGB(hex)
	if !ok {
		return ""
	}
	return strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b)
}

// frameLines liefert aus all() die Zeilen je Block (Kopfzeile je Block
// traegt dessen Rahmenfarbe — die Vergleichsbasis der Rahmenzeichen).
func frameLines(l BlockList) [][]string {
	w := l.Width()
	lines, _ := l.all()
	out := [][]string{}
	y := 0
	for _, it := range l.items {
		h := it.Height(w)
		out = append(out, lines[y:min(y+h, len(lines))])
		y += h
	}
	return out
}

// TestCharSelSurvivesPlainArrows (FR-43): Im Zeichen-Cursor kollabieren
// Pfeile eine BESTEHENDE Auswahl nicht mehr — der Anker haelt, die Auswahl
// folgt dem Caret live. Nur esc kollabiert (Stufe 1). Eine LEERE Auswahl
// bleibt leer (kein Auswahl-Drift ohne Anker).
func TestCharSelSurvivesPlainArrows(t *testing.T) {
	r := enterCursor(newRig(t).seed("abcdef"))
	r.key("shift+right").key("shift+right")
	if got := r.m.chat.charMarkdown(); got != "ab" {
		t.Fatalf("Vorbedingung Auswahl 'ab', got %q", got)
	}
	// Zwei normale Pfeile: Anker 0 bleibt, Caret laeuft auf 4 — die
	// Auswahl WAeCHST mit (ueberlebt die Cursor-Bewegung, FR-43).
	r.key("right").key("right")
	if got := r.m.chat.charMarkdown(); got != "abcd" {
		t.Fatalf("Auswahl muss den Pfeilen folgen (Anker haelt): %q", got)
	}
	if r.m.chat.charEmptySelection() {
		t.Fatal("Pfeile duerfen die Auswahl nicht kollabieren (nur esc)")
	}
	// esc Stufe 1: Kollaps — der EINZIGE Weg, die Auswahl zu verlieren.
	r.key("esc")
	if !r.m.chat.charActive() || !r.m.chat.charEmptySelection() {
		t.Fatal("esc Stufe 1 muss die Auswahl aufs Caret kollabieren")
	}
}

// TestSelectionSurvivesFocusMove (FR-43): Die Block-Auswahl ueberlebt die
// Fokus-Bewegung — inner halb des Bereichs unberuehrt; verlaesst der Fokus
// ihn, wird sie zur VERGANGENEN Auswahl (prevSel) statt geloescht. Erst esc
// loest auch sie auf.
func TestSelectionSurvivesFocusMove(t *testing.T) {
	r := newRig(t).seed("aaa\n\nbbb\n\nccc\n\nddd\n")
	// Antwort-Modus (tab — shift+down startet keine Auswahl mehr aus der
	// Eingabe) und den Fokus ans OBERSTE Segment fahren: Der Anker sitzt
	// oben, die Auswahl waechst nach unten.
	r.key("tab")        // Fokus: letztes sichtbares Segment (3)
	r.key("up").key("up").key("up") // Fokus auf Block 0
	r.key("shift+down") // Anker 0, sofort +1
	r.key("shift+down") // Bereich 0..2
	if lo, hi := r.m.chat.selBounds(); lo != 0 || hi != 2 {
		t.Fatalf("Vorbedingung Bereich 0..2, got %d..%d", lo, hi)
	}
	// Fokus-Bewegung im Antwort-Modus: Erster down landet IM Auswahl-
	// bereich (extendSel raeumte den Fokus; focusStep setzt ihn aufs
	// oberste sichtbare Segment zurueck) — die Auswahl bleibt.
	r.key("down")
	if !r.m.chat.selActive() {
		t.Fatal("Fokus im Auswahlbereich darf die Auswahl nicht loesen")
	}
	// Weiter nach unten: Der Fokus verlaesst den Bereich — die Auswahl
	// weicht zur VERGANGENEN (Sekundaer-Rahmen bis esc), nicht ins Nichts.
	r.key("down").key("down").key("down")
	if r.m.chat.selActive() {
		t.Fatal("Fokus ausserhalb: Shift-Auswahl muss delegiert werden")
	}
	if !r.m.chat.prevSel || r.m.chat.prevLo != 0 || r.m.chat.prevHi != 2 {
		t.Fatalf("vergangene Auswahl fehlt: prev=%v %d..%d",
			r.m.chat.prevSel, r.m.chat.prevLo, r.m.chat.prevHi)
	}
	// esc loest AUCH die vergangene Auswahl (FR-43: einzige Verlust-Stelle).
	r.key("esc")
	if r.m.chat.prevSel || r.m.chat.selActive() || r.m.chat.markedCount() > 0 {
		t.Fatal("esc muss alle Auswahl-Generationen aufloesen")
	}
}

// TestPrevSelFrameColorDiffersFromCurrent (User-Wunsch): Die Rahmenzeichen
// der VERGANGENEN Auswahl weichen farblich von der AKTUELLEN ab — aktuelle
// Auswahl im Neon-Akzent, vergangene in der verblichenen Sekundaer-Farbe.
func TestPrevSelFrameColorDiffersFromCurrent(t *testing.T) {
	withTrueColor(t) // sonst rendert go test farblos
	r := newRig(t).seed("```go\na\n```\n\n```go\nb\n```\n\n```go\nc\n```")
	focus := "38;2;" + hexSgr(r.m.theme.FocusColor())
	stale := "38;2;" + hexSgr(r.m.theme.StaleColor())
	if focus == stale {
		t.Fatalf("Sekundaer-Farbe darf nicht der Akzentfarbe gleichen: %s", stale)
	}
	// c markieren (tab fokussiert den LETZTEN Block), dann den Fokus nach
	// oben bewegen: Die Marke wird zur vergangenen Auswahl [2,2].
	r.key("tab")
	r.key(" ")
	r.key("up").key("up")
	if !r.m.chat.prevSel || r.m.chat.prevLo != 2 || r.m.chat.prevHi != 2 {
		t.Fatalf("Marke muss als vergangene Auswahl [2,2] ueberleben: prev=%v %d..%d",
			r.m.chat.prevSel, r.m.chat.prevLo, r.m.chat.prevHi)
	}
	if r.m.chat.markedCount() != 0 {
		t.Fatal("Marke ist keine aktuelle Auswahl mehr")
	}
	// Frische Shift-Auswahl am Fokus (Block 0) — aktuell [0,1] im Neon.
	r.key("shift+down")
	fl := frameLines(r.m.chat)
	for i := 0; i <= 1; i++ {
		if len(fl[i]) == 0 || !strings.Contains(fl[i][0], focus) {
			t.Fatalf("Block %d der AKTUELLEN Auswahl muss den Neon-Rahmen tragen: %q", i, fl[i][0])
		}
		if strings.Contains(strings.Join(fl[i], "\n"), stale) {
			t.Fatalf("Block %d: aktuelle Auswahl darf nicht die Sekundaer-Farbe tragen", i)
		}
	}
	if len(fl[2]) == 0 || !strings.Contains(strings.Join(fl[2], "\n"), stale) {
		t.Fatalf("VERGANGENE Auswahl (Block 2) muss die Sekundaer-Farbe tragen: %q", fl[2][0])
	}
	if strings.Contains(strings.Join(fl[2], "\n"), focus) {
		t.Fatal("vergangene Auswahl darf nicht den Neon-Rahmen tragen")
	}
}

// TestCharSelExitStartsAtAnchor (User-Report: "ich gehe aus den Cursor-
// Mode — der Anker bleibt der selbe, von da aus beginnt unser Select;
// jetzt waehlt er alles aus, das ist falsch — er soll vom Anker starten"):
// Der Toggle-AUSTRITT konvertiert NICHTS mehr — auch der Austritt IM
// Anker-Block bleibt die FEINE Auswahl Anker..Caret (die fruehere Ganz-
// Block-Konversion griff VOR den Anker und waehlte "alles"). Sie bleibt
// sichtbar und per c kopierbar, der Anker ueberlebt exakt; Block-
// Granularitaet kommt erst mit shift+up/down von aussen.
func TestCharSelExitStartsAtAnchor(t *testing.T) {
	r := enterCursor(newRig(t).seed("aaa\n\nabcdef"))
	r.key("shift+right").key("shift+right").key("shift+right")
	r.key("shift+tab") // Modus verlassen (Toggle)
	if r.m.chat.charActive() {
		t.Fatal("shift+enter muss den Zeichen-Cursor verlassen")
	}
	if r.m.chat.selActive() {
		t.Fatal("der Austritt darf KEINE Ganz-Block-Auswahl aufbauen")
	}
	if !r.m.chat.residualSelShown() {
		t.Fatal("die feine Auswahl bleibt ausserhalb des Modus sichtbar")
	}
	if r.m.chat.charFromBlock != 1 || r.m.chat.charFrom != 0 {
		t.Fatalf("Anker muss (b1,c0) bleiben, got (b%d,c%d)",
			r.m.chat.charFromBlock, r.m.chat.charFrom)
	}
	// c kopiert AB DEM ANKER bis zum Caret — nicht den ganzen Block.
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "abc" {
		t.Fatalf("c muss die Auswahl ab dem Anker kopieren: %q", got)
	}
	// shift+up von aussen: KLASSISCH ab dem Anker — erst der Ausschnitt
	// UEBER ihm bis zum Blockanfang (sein Block-Rand liegt auf dem Anker,
	// also sofort einen Rand weiter: der Block darueber GANZ + Rest bis
	// Anker), KEIN Ganzblock-Neustart, kein Modus-Eintritt.
	r.key("shift+up")
	if r.m.chat.charActive() || r.m.chat.selActive() {
		t.Fatal("shift+up bleibt ausserhalb des Modus als feine Auswahl")
	}
	// Anker c0: b1 traegt nichts bei, die Auswahl umfasst alles darueber
	// (Leerstellen-Join wie ueblich).
	if got := r.m.chat.charMarkdown(); got != "aaa\n\n" {
		t.Fatalf("Klassische Auswahl ab dem Anker nach oben: %q", got)
	}
	if r.m.chat.charFromBlock != 1 || r.m.chat.charFrom != 0 {
		t.Fatalf("Anker muss (b1,c0) halten, got (b%d,c%d)",
			r.m.chat.charFromBlock, r.m.chat.charFrom)
	}
	r.key("esc")
	if r.m.chat.selActive() || r.m.chat.residualSelShown() {
		t.Fatal("esc loest die Auswahl")
	}
}

// ---- PlantUML in der Zeichen-Auswahl: GANZ statt uebersprungen ----------

// TestCharSelCrossesDiagramAsSource (User-Report: "im cursor mode kann ich
// kein mermaid diagramm auswählen — am besten automatisch im text mode
// wechseln"): Erreicht die Caret-Bewegung ein gerendertes Diagramm-BILD
// mit Quelle, schaltet heron es AUTOMATISCH auf den Zaun-QUELLTEXT um
// (dasselbe Auto-t wie beim Modus-Eintritt): Der Block wird buchstaben-
// faehig, das Caret laeuft HINEIN — kein Sprung ueber das Bild mehr, die
// Auswahl ist buchstabenweise (und deckt den ganzen Quelltext, wenn der
// Anker im Folge-Block sitzt).
func TestCharSelCrossesDiagramAsSource(t *testing.T) {
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed("davor text\n\n```plantuml\n@startuml\nA -> B\n@enduml\n```\n\ndanach text\n")
	r.do(diagramsDoneMsg{results: imgResults(r.m)})
	// Bloecke: 0=Text, 1=ImageBlock (Bild-Modus), 2=Text.
	if img, ok := r.m.chat.items[1].(ImageBlock); !ok || img.showSrc {
		t.Fatalf("Block 1 muss der ImageBlock im BILD-Modus sein: %T", r.m.chat.items[1])
	}
	r.key("tab") // Fokus: letztes Segment = "danach text"
	if r.m.chat.focus != 2 {
		t.Fatalf("tab muss Block 2 fokussieren, got %d", r.m.chat.focus)
	}
	r.key("shift+tab")
	if !r.m.chat.charActive() || r.m.chat.charBlock != 2 {
		t.Fatal("Zeichen-Cursor muss in Block 2 starten")
	}
	// EIN shift+left am Blockrand: Das Bild darueber schaltet automatisch
	// auf Quelltext, das Caret laeuft ans ENDE der Quelle HINEIN.
	r.key("shift+left")
	if r.m.chat.charBlock != 1 {
		t.Fatalf("Caret muss in die Diagramm-Quelle (Block 1) laufen, got %d",
			r.m.chat.charBlock)
	}
	img, ok := r.m.chat.items[1].(ImageBlock)
	if !ok || !img.showSrc {
		t.Fatal("Diagramm muss automatisch im Quelltext-Modus stehen")
	}
	// Die Quelle ist buchstaben-/zeilenweise waehlbar: home zieht das Caret
	// an den Zeilenanfang, zwei WEITERE Pfeile (FR-43: Anker haelt, die
	// Auswahl folgt dem Caret) decken die QUELLE KOMPLETT — der Anker
	// sitzt im Folge-Block.
	r.key("home")
	r.key("up").key("up")
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 {
		t.Fatal("c hat nicht kopiert")
	}
	payload := got[len(got)-1]
	for _, want := range []string{"@startuml", "A -> B", "@enduml"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("Quelltext-Kopie fehlt %q: %q", want, payload)
		}
	}
}

// TestCharSelMermaidEdgeBlockSelectable (User-Report, der eigentliche
// Fehler): Ein Diagramm als LETZTER Block (kein Text darunter) war im
// Zeichen-Cursor GAR nicht erreichbar — das Caret konnte nicht hinein und
// der Ganz-Block-Pfad greift nur fuer Mittelbloecke. Der Auto-Text-Modus
// beim Ueberqueren macht den Rand-Block buchstabenfaehig: EIN shift+down
// waechst IN die Mermaid-Quelle.
func TestCharSelMermaidEdgeBlockSelectable(t *testing.T) {
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed("text davor\n\n```mermaid\nflowchart LR\n  A --> B\n```\n")
	r.do(diagramsDoneMsg{results: imgResults(r.m)})
	if img, ok := r.m.chat.items[1].(ImageBlock); !ok || img.showSrc {
		t.Fatalf("Block 1 muss der Mermaid-ImageBlock im BILD-Modus sein: %T",
			r.m.chat.items[1])
	}
	r.key("tab") // Fokus: das Bild (letztes Segment)
	r.key("up")  // Fokus auf den Text darueber (Block 0)
	if r.m.chat.focus != 0 {
		t.Fatalf("Fokus muss auf Block 0 liegen, got %d", r.m.chat.focus)
	}
	r.key("shift+tab") // Zeichen-Cursor im Text (kein Auto-t noetig)
	r.key("shift+down")
	if r.m.chat.charBlock != 1 {
		t.Fatalf("Caret muss in die Mermaid-Quelle wachsen, got Block %d",
			r.m.chat.charBlock)
	}
	img, ok := r.m.chat.items[1].(ImageBlock)
	if !ok || !img.showSrc {
		t.Fatal("Mermaid muss automatisch im Quelltext-Modus stehen")
	}
	// FR-43 am Werk: end zieht das Caret an den Zeilenende, der ANKER
	// haelt — die Auswahl waechst live in die Mermaid-Quelle hinein.
	r.key("end")
	r.key("down").key("end")
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 {
		t.Fatal("c hat nicht kopiert")
	}
	payload := got[len(got)-1]
	for _, want := range []string{"text davor", "flowchart LR", "A --> B"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("Kopie fehlt %q: %q", want, payload)
		}
	}
}

// TestCharSelSrclessImageTakenWhole: Ein Bild OHNE Quelle (kein Auto-t
// moeglich) bleibt unadressierbar — die Auswahl nimmt es ueber den Ganz-
// Block-Pfad auf: Der Cursor springt darueber hinweg, der Block-Rahmen
// leuchtet als Mittelstueck der Auswahl.
func TestCharSelSrclessImageTakenWhole(t *testing.T) {
	withTrueColor(t)
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed("vorher\n\n```plantuml\n@startuml\nA -> B\n@enduml\n```\n\nnachher\n")
	jobs := r.m.diagramJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d want 1", len(jobs))
	}
	// Ergebnis OHNE src: SetSource entfaellt, kein t-Switch verfuegbar.
	r.do(diagramsDoneMsg{results: []diagramResult{{
		id: jobs[0].id, lang: "plantuml", ascii: jobs[0].code,
		png: realPNG(200, 100), ok: true,
	}}})
	if img, ok := r.m.chat.items[1].(ImageBlock); !ok || img.src != "" {
		t.Fatalf("Block 1 muss der QUELLENLOSE ImageBlock sein: %T src=%q",
			r.m.chat.items[1], img.src)
	}
	r.key("tab")
	r.key("up").key("up") // Fokus Block 0 ("vorher") — ueber das Bild
	r.key("shift+tab")
	if r.m.chat.charBlock != 0 {
		t.Fatal("Zeichen-Cursor muss in Block 0 starten")
	}
	// shift+right bis ans Textende, der ERSTE Rand-Druck ueberquert das
	// Bild: kein Auto-t (keine Quelle) — der Cursor springt nach Block 2,
	// das Bild bleibt als Ganz-Block-Mittelstueck markiert.
	for i := 0; i < 8; i++ {
		r.key("shift+right")
	}
	if r.m.chat.charBlock != 2 {
		t.Fatalf("Cursor muss das quellenlose Bild ueberspringen (Block 2), got %d",
			r.m.chat.charBlock)
	}
	focus := "38;2;" + hexSgr(r.m.theme.FocusColor())
	fl := frameLines(r.m.chat)
	if !strings.Contains(strings.Join(fl[1], "\n"), focus) {
		t.Fatalf("Quellenloses Bild muss als Ganz-Block-Auswahl leuchten: %q", fl[1][0])
	}
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 {
		t.Fatal("c hat nicht kopiert")
	}
	payload := got[len(got)-1]
	if !strings.Contains(payload, "vorher") || !strings.Contains(payload, "A -> B") {
		t.Fatalf("Ganz-Block-Kopie fehlt Inhalte: %q", payload)
	}
}

// TestCharSelPlantUMLSourceIsCharwise: Im QUELLTEXT-Modus (Auto-t beim
// Modus-Eintritt) bleibt der PlantUML-Block buchstaben-faehig — die
// Auswahl ist dort buchstabenweise, NICHT ganz-block-weise.
func TestCharSelPlantUMLSourceIsCharwise(t *testing.T) {
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed("```plantuml\n@startuml\nA -> B\n@enduml\n```\n")
	r.do(diagramsDoneMsg{results: imgResults(r.m)})
	r.key("tab")      // Fokus: der (einzige) Bildblock
	r.key("shift+tab") // Auto-t: Quelltext-Ansicht, Caret DARIN
	ib, ok := r.m.chat.items[0].(ImageBlock)
	if !ok || !ib.showSrc {
		t.Fatalf("Auto-t muss den Zaun-Quelltext zeigen: %T showSrc=%v", r.m.chat.items[0], ib.showSrc)
	}
	r.key("shift+right").key("shift+right")
	if got := r.m.chat.charMarkdown(); got != "@s" {
		t.Fatalf("Quelltext muss BUCHSTABENWEISE waehlen, got %q", got)
	}
}

// TestPrevSelDoesNotOutliveItems: Die vergangene Auswahl haelt am Block-
// VERBAND — Clear (/new) raeumt sie mit.
func TestPrevSelDoesNotOutliveItems(t *testing.T) {
	l := NewBlockList()
	l = l.Append(NewCodeBlock("go", "a", nil))
	l = l.setPrevSel(0, 0)
	l = l.Clear()
	if l.prevSel {
		t.Fatal("Clear muss die vergangene Auswahl mitraeumen")
	}
}

// TestCharSelUpStopsAtFirstDiagramOnPath (User-Report: "shift+up springt
// eine unbekannte Zahl an Abschnitten nach oben, also mehr als 1"): Liegt
// zwischen dem Caret und dem Ziel ein permanent unfaehiger Block (hier:
// Bild OHNE Quelle), endet der Sprung trotzdem im ERSTEN Diagramm dahinter
// (Auto-t auf dem GESAMTEN Skip-Pfad) — statt es mit zu ueberspringen und
// mehrere Abschnitte weit zu springen. Permanent unfaehige Bloecke ohne
// Diagramm dahinter bleiben ein Sprung (keine Zellen, kein Auto-t).
func TestCharSelUpStopsAtFirstDiagramOnPath(t *testing.T) {
	t.Setenv("HERON_IMAGES", "force")
	r := newRig(t).seed("a\n\n```plantuml\n@startuml\nA\n@enduml\n```\n\n```plantuml\n@startuml\nB\n@enduml\n```\n\ntext unten\n")
	jobs := r.m.diagramJobs()
	if len(jobs) != 2 {
		t.Fatalf("jobs=%d want 2", len(jobs))
	}
	// Job 0 (Block 1) OHNE Quelle, Job 1 (Block 2) MIT Quelle.
	r.do(diagramsDoneMsg{results: []diagramResult{
		{id: jobs[0].id, lang: "plantuml", ascii: jobs[0].code, png: realPNG(200, 100), ok: true},
		{id: jobs[1].id, lang: "plantuml", ascii: jobs[1].code, src: jobs[1].code, png: realPNG(200, 100), ok: true},
	}})
	if img, ok := r.m.chat.items[1].(ImageBlock); !ok || img.src != "" {
		t.Fatalf("Block 1 muss das QUELLENLOSE Bild sein: %T", r.m.chat.items[1])
	}
	if img, ok := r.m.chat.items[2].(ImageBlock); !ok || img.src == "" {
		t.Fatalf("Block 2 muss das Bild MIT Quelle sein: %T", r.m.chat.items[2])
	}
	// Bloecke: 0=Text, 1=Bild ohne Quelle, 2=Bild mit Quelle, 3=Text.
	r.key("tab") // Fokus: letztes Segment ("text unten")
	r.key("shift+tab")
	if r.m.chat.charBlock != 3 {
		t.Fatal("Zeichen-Cursor muss in Block 3 starten")
	}
	// EIN up: Der Sprung endet IM Diagramm (Block 2, automatisch Quell-
	// text) — nicht dahinter, trotz Bild-ohne-Quelle dazwischen... hier
	// ist Block 2 der direkte Nachbar; das quellenlose Bild folgt beim
	// WEITEREN Weg nach oben.
	r.key("up")
	if r.m.chat.charBlock != 2 {
		t.Fatalf("up muss im ERSTEN Diagramm auf dem Pfad (Block 2) enden, got %d",
			r.m.chat.charBlock)
	}
	if img, ok := r.m.chat.items[2].(ImageBlock); !ok || !img.showSrc {
		t.Fatal("Block 2 muss automatisch im Quelltext-Modus stehen")
	}
	// Quelle zeilenweise hoch (3 Zeilen), der Rand-Sprung danach ueber-
	// springt NUR das quellenlose Bild (Block 1) — ein Sprung, ein Ziel.
	r.key("up").key("up").key("up")
	if r.m.chat.charBlock != 0 {
		t.Fatalf("Rand-Sprung muss im Text (Block 0) enden — quellenloses Bild uebersprungen, got %d",
			r.m.chat.charBlock)
	}
}

// TestCharSelAnchorSurvivesToggleRoundTrip (User-Report: "der cursor mode
// verschiebt den ankerpunkt — zuruecksetzen sollte nur esc"): Der Toggle-
// Kreislauf Zeichen-Auswahl → Block-Auswahl → Zeichen-Auswahl haelt Anker
// und Caret EXAKT (auch mitten im Block), statt sie auf Blockkanten neu
// abzuleiten. esc ist der EINZIGE Reset — erst danach startet der Wieder-
// eintritt frisch.
func TestCharSelAnchorSurvivesToggleRoundTrip(t *testing.T) {
	r := enterCursor(newRig(t).seed("abcdef"))
	r.key("shift+right").key("shift+right").key("shift+right") // Anker 0, Caret 3
	if got := r.m.chat.charMarkdown(); got != "abc" {
		t.Fatalf("Vorbedingung Auswahl 'abc', got %q", got)
	}
	// Toggle raus (feine Auswahl bleibt als Rest erhalten — KEINE Kon-
	// version mehr) und wieder rein: Die EXAKTE Auswahl kommt zurueck —
	// der Anker steht NICHT auf der Blockkante.
	r.key("shift+tab")
	if r.m.chat.charActive() || r.m.chat.selActive() || !r.m.chat.residualSelShown() {
		t.Fatal("Vorbedingung: Toggle raus haelt die feine Auswahl als Rest")
	}
	r.key("shift+tab")
	if !r.m.chat.charActive() {
		t.Fatal("Toggle rein muss den Zeichen-Cursor oeffnen")
	}
	if got := r.m.chat.charMarkdown(); got != "abc" {
		t.Fatalf("Anker muss den Kreislauf exakt ueberleben ('abc'), got %q", got)
	}
	if r.m.chat.charFrom != 0 || r.m.chat.charTo != 3 {
		t.Fatalf("Anker 0 / Caret 3 erwartet, got %d..%d",
			r.m.chat.charFrom, r.m.chat.charTo)
	}
	// ESC ist der einzige Reset: Stufe 1 kollabiert die eigene Auswahl
	// aufs Caret, Stufe 2 verlaesst den Cursor UND raeumt die Reste —
	// danach startet der Eintritt frisch an der Leseposition.
	r.key("esc")
	if !r.m.chat.charActive() || !r.m.chat.charEmptySelection() {
		t.Fatal("esc Stufe 1 kollabiert die eigene Auswahl aufs Caret")
	}
	r.key("esc")
	if r.m.chat.charActive() || r.m.chat.charFromBlock >= 0 {
		t.Fatal("esc Stufe 2 verlaesst den Cursor und raeumt die Anker-Reste")
	}
	r.key("shift+tab") // frischer Eintritt (Fokus auf der Leseposition)
	if !r.m.chat.charActive() || !r.m.chat.charEmptySelection() {
		t.Fatal("nach esc startet der Zeichen-Cursor frisch ohne Auswahl")
	}
}

// TestCharSelExitOutsideAnchorKeepsFineSelection (User-Report: "die haelfte
// eines codeblocks markiert, an einer anderen position per shift+tab raus —
// er waehlt den ganzen block aus, das sollte nicht sein"): Der Austritt
// AUSSERHALB des Anker-Blocks konvertiert NICHT zur Ganz-Block-Auswahl —
// die feine Zeichen-Auswahl bleibt als Auswahl erhalten (Rest), der
// Wiedereintritt restauriert sie EXAKT. (Die fruehere Sonderregel —
// Austritt IM Anker-Block konvertierte zum Ganzblock — ist zugunsten des
// klassischen Modells entfallen: Der Austritt startet die Auswahl stets
// am ANKER, s. TestCharSelExitStartsAtAnchor.)
func TestCharSelExitOutsideAnchorKeepsFineSelection(t *testing.T) {
	r := newRig(t).seed("aaa\n\nbbb")
	r.key("tab")       // Fokus: letztes Segment (Block 1)
	r.key("up")        // Fokus auf Block 0
	r.key("shift+tab") // Zeichen-Cursor in Block 0
	if r.m.chat.charBlock != 0 {
		t.Fatal("Caret muss in Block 0 starten")
	}
	// Haelfte von Block 0 markiert, dann Caret in Block 1 bewegt
	// (FR-43: Anker haelt, die Auswahl folgt dem Caret).
	r.key("shift+right").key("shift+right")
	r.key("down")
	if r.m.chat.charBlock != 1 {
		t.Fatalf("Caret muss in Block 1 stehen, got %d", r.m.chat.charBlock)
	}
	// Austritt AUSSERHALB des Anker-Blocks: KEINE Ganz-Block-Auswahl —
	// die Auswahl bleibt die feine Zeichen-Auswahl (erhalten bis esc).
	r.key("shift+tab")
	if r.m.chat.charActive() {
		t.Fatal("shift+tab muss den Cursor-Modus verlassen")
	}
	if r.m.chat.selActive() {
		t.Fatal("ausserhalb des Anker-Blocks darf kein Ganz-Block gewaehlt werden")
	}
	// Wiedereintritt (Runde 7): Die feine Auswahl kommt EXAKT zurueck.
	r.key("shift+tab")
	if !r.m.chat.charActive() {
		t.Fatal("Wiedereintritt muss den Cursor-Modus oeffnen")
	}
	if got := r.m.chat.charMarkdown(); got != "aaa\n\nbb" {
		t.Fatalf("feine Auswahl muss exakt restauriert werden, got %q", got)
	}
	r.key("shift+tab") // wieder raus: Die Auswahl bleibt ERHALTEN

	// SICHTBAR auch ausserhalb des Modus (User-Report 22:18: "wenn ich
	// cursor mode schliesse wird die selection entfernt, gehe ich wieder
	// rein ist sie wieder drin — das soll so nicht sein") — und von
	// AUSSEN per c kopierbar.
	if !r.m.chat.residualSelShown() {
		t.Fatal("die erhaltene Auswahl muss ausserhalb des Modus sichtbar sein")
	}
	if !strings.Contains(r.view(), r.m.chat.charSelBgSgr()) {
		t.Fatal("Auswahl-Highlight fehlt im View ausserhalb des Modus")
	}
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "aaa\n\nbb" {
		t.Fatalf("c muss die erhaltene Auswahl von aussen kopieren: %q", got)
	}

	// esc loest sie AUCH von aussen (einziger Verlust-Punkt, FR-43),
	// danach startet der Eintritt frisch ohne Auswahl.
	r.key("esc")
	if r.m.chat.residualSelShown() {
		t.Fatal("esc loest die erhaltene Auswahl von aussen")
	}
	r.key("tab").key("shift+tab")
	if !r.m.chat.charActive() || !r.m.chat.charEmptySelection() {
		t.Fatal("nach esc startet der Eintritt frisch ohne Auswahl")
	}
	r.key("esc")
	// Leere Auswahl: KEIN Kollaps-Schritt — esc verlaesst den Cursor direkt.
	if r.m.chat.charActive() {
		t.Fatal("esc verlaesst den frischen Cursor direkt (nichts zu kollabieren)")
	}
}

// TestCharSelShiftOutsideRunsClassic (User-Report: "ich setze meinen
// Cursor bei zeichen 50 — egal ob ich was selektiert habe oder nicht, das
// ist mein Anker, von da aus beginnt mein Select; er selektiert NICHT den
// ganzen Schnippsel; das Select expandiert nicht nach unten und nicht
// nach rechts, weil ich shift+up gedrueckt habe; 2 mal shift+down — der
// Ankerpunkt ist immer noch der selbe, nur esc kann ihn auf null setzen"):
// Die gesetzte Cursor-Position ist der Anker JEDES shift-Drucks von
// aussen — auch OHNE laufende Auswahl. Die Auswahl waechst KLASSISCH als
// Zeichen-Auswahl ab ihm: shift+up erfasst zunaechst den Ausschnitt UEBER
// dem Anker bis zum Blockanfang (nicht den ganzen Block, nichts unter-
// halb), weitere Druecke ganze Bloecke darueber; shift+down spiegel-
// bildlich (Caret darf den Anker passieren — die Auswahl kippt nach
// unten, der Anker haelt). Kein Viewport-Anker, kein Ganzblock-Neustart,
// kein Wiedereintritt in den Modus.
func TestCharSelShiftOutsideRunsClassic(t *testing.T) {
	r := newRig(t).seed("aaa\n\nabcdef\n\nccc\n\nddd\n\neee")
	r.key("tab").key("up").key("up").key("up") // Fokus Block 1 ("abcdef")
	r.key("shift+enter")                        // Zeichen-Cursor in Block 1
	r.key("right").key("right").key("right")
	if !r.m.chat.charEmptySelection() {
		t.Fatal("Vorbedingung: Caret auf c3, OHNE Auswahl")
	}
	r.key("shift+tab") // Austritt — die Position IST der Anker
	if r.m.chat.charFromBlock != 1 || r.m.chat.charFrom != 3 {
		t.Fatalf("Position (b1,c3) muss als Anker halten, got (b%d,c%d)",
			r.m.chat.charFromBlock, r.m.chat.charFrom)
	}
	// shift+up: Auswahl = der Ausschnitt UEBER dem Anker (b1, c0..c3) —
	// NICHT der ganze Block, nichts UNTERHALB des Ankers.
	r.key("shift+up")
	if r.m.chat.charActive() || r.m.chat.selActive() {
		t.Fatal("shift+up bleibt ausserhalb des Modus als feine Auswahl")
	}
	if !r.m.chat.residualSelShown() {
		t.Fatal("die Auswahl ab dem Anker muss sichtbar sein")
	}
	if got := r.m.chat.charMarkdown(); got != "abc" {
		t.Fatalf("1. Druck: Ausschnitt ueber dem Anker 'abc', got %q", got)
	}
	// 2. Druck: der naechste Block darueber GANZ + der Ausschnitt.
	r.key("shift+up")
	if got := r.m.chat.charMarkdown(); got != "aaa\n\nabc" {
		t.Fatalf("2. Druck: 'aaa\n\nabc', got %q", got)
	}
	// 2x shift+down: Der Caret laeuft klassisch ZURUECK und UEBER den
	// Anker hinweg — die Auswahl kippt nach unten, der ANKER haelt c3.
	r.key("shift+down").key("shift+down")
	if r.m.chat.charFromBlock != 1 || r.m.chat.charFrom != 3 {
		t.Fatalf("Anker muss (b1,c3) bleiben, got (b%d,c%d)",
			r.m.chat.charFromBlock, r.m.chat.charFrom)
	}
	if got := r.m.chat.charMarkdown(); got != "def" {
		t.Fatalf("nach 2x shift+down Auswahl UNTER dem Anker 'def', got %q", got)
	}
	r.key("c")
	if got := r.copiedAll(); got[len(got)-1] != "def" {
		t.Fatalf("c kopiert die Auswahl ab dem Anker: %q", got[len(got)-1])
	}
	// esc ist der EINZIGE Anker-Reset — danach startet shift neu als
	// frische Block-Auswahl (Leseposition, sofortiger Griff).
	r.key("esc")
	if r.m.chat.residualSelShown() || r.m.chat.charFromBlock >= 0 {
		t.Fatal("esc setzt den Anker auf null")
	}
	r.key("shift+up")
	if !r.m.chat.selActive() {
		t.Fatal("nach esc startet shift+up frisch als Block-Auswahl")
	}
}

// TestCharSelAnchorSurvivesBlockWalkReentry (User-Report: "man hat einen
// Anker, dieser bleibt aktiv"): Waechst die Auswahl ausserhalb des Modus
// UEBER den Anker-Block hinaus (Leiter: Blockrand fuer Blockrand), haelt
// der feine Anker beim Wiedereintritt exakt — nur der Caret steht an der
// Lauf-Kante.
func TestCharSelAnchorSurvivesBlockWalkReentry(t *testing.T) {
	r := newRig(t).seed("aaa\n\nbbb\n\nccc\n\nddd\n\neee")
	r.key("tab").key("up").key("up")
	r.key("shift+enter")
	r.key("shift+right").key("shift+right") // Anker (b2,c0)
	r.key("down")                           // Caret b3
	r.key("shift+tab")                      // erhaltene Auswahl
	r.key("shift+up")                       // Ausschnitt ueber dem Caret
	r.key("shift+up")                       // Anker-Rand (leer) -> Block darueber
	r.key("shift+up")                       // noch ein Block darueber: (b0,c0)
	if r.m.chat.charBlock != 0 || r.m.chat.charTo != 0 {
		t.Fatalf("Vorbedingung Caret (b0,c0), got (b%d,c%d)",
			r.m.chat.charBlock, r.m.chat.charTo)
	}
	// Wiedereintritt: Anker bleibt der FEINE Punkt im Anker-Block (kein
	// Neubau von der Blockkante), Caret an der Lauf-Kante.
	r.key("shift+enter")
	if !r.m.chat.charActive() {
		t.Fatal("Wiedereintritt muss den Cursor-Modus oeffnen")
	}
	if r.m.chat.charFromBlock != 2 || r.m.chat.charFrom != 0 {
		t.Fatalf("Anker muss (b2,c0) exakt halten, got (b%d,c%d)",
			r.m.chat.charFromBlock, r.m.chat.charFrom)
	}
	if got := r.m.chat.charMarkdown(); got != "aaa\n\nbbb\n\n" {
		t.Fatalf("Zeichen-Auswahl Anker..Caret: %q", got)
	}
}

