package tui

import (
	"strings"
	"testing"
)

// enterCursor: shift+tab (== shift+enter) auf dem fokussierten Segment —
// DETERMINISTISCHE Leseposition fuer die Tests (ohne Fokus ware es der
// oberste sichtbare Block).
func enterCursor(r *rig) *rig {
	r.key("tab")
	r.key("shift+tab")
	return r
}

// TestShiftEnterIsTheCursor: EIN shift+enter setzt das Editor-Caret — kein
// Sektions-Durchhang mehr. Das Caret unterstreicht genau SEINEN Buchstaben
// (kein Bereich), die Leseposition ist der fokussierte Block.
func TestShiftEnterIsTheCursor(t *testing.T) {
	r := newRig(t).seed("Hallo Welt")
	r.key("tab")
	r.key("shift+tab")
	if !r.m.chat.charActive() {
		t.Fatal("shift+enter muss DIREKT den Zeichen-Cursor setzen")
	}
	if r.m.chat.charBlock != 0 || r.m.chat.charTo != 0 {
		t.Fatalf("Caret an der Leseposition (Block 0, Offset 0): block=%d to=%d",
			r.m.chat.charBlock, r.m.chat.charTo)
	}
	if !r.m.chat.charEmptySelection() {
		t.Fatal("Frisches Caret hat keine Auswahl")
	}
	if !strings.Contains(r.view(), "\x1b[7mH") {
		t.Fatalf("Caret-Buchstabe H muss invertiert (blinkend sichtbar) sein: %q", r.view())
	}
}

// TestCaretGoneWhenModeClosed (User-Report: "wenn ich den cursor mode
// verlasse, existiert der cursor immer noch"): Nach dem Toggle-Austritt
// (residualSelShown) bleiben Auswahl-Flaeche UND Kopie erhalten, aber der
// CURSOR — der invertierte Caret-Kanten-Marker — verschwindet. Der Cursor
// gehoert dem Modus; draussen ist er weg.
func TestCaretGoneWhenModeClosed(t *testing.T) {
	r := enterCursor(newRig(t).seed("abcdef"))
	r.key("shift+right").key("shift+right").key("shift+right") // "abc"
	sgr := r.m.chat.charSelBgSgr()
	if !r.m.chat.charActive() || !strings.Contains(r.view(), "\x1b[7md") {
		t.Fatal("Vorbedingung: aktiver Cursor, invertierte Caret-Kante ('d') sichtbar")
	}
	r.key("shift+tab") // Modus verlassen (Toggle)
	if r.m.chat.charActive() {
		t.Fatal("shift+enter muss den Zeichen-Cursor verlassen")
	}
	if !r.m.chat.residualSelShown() {
		t.Fatal("die Auswahl bleibt ausserhalb des Modus sichtbar")
	}
	v := r.view()
	if !strings.Contains(v, sgr+"a") {
		t.Fatalf("Auswahl-Flaeche muss erhalten bleiben: %q", v)
	}
	if strings.Contains(v, "\x1b[7m") {
		t.Fatalf("der Cursor (invertierte Kante) darf nach dem Austritt NICHT mehr existieren: %q", v)
	}
	// c kopiert weiterhin die feine Auswahl.
	r.key("c")
	if got := r.copiedAll(); len(got) == 0 || got[len(got)-1] != "abc" {
		t.Fatalf("c muss die erhaltene Auswahl kopieren: %q", got)
	}
}

// TestCaretBlinks: Das Caret BLINKT wie ein Terminal-Cursor — charBlinkMsg
// (tea.Tick ~530 ms) blendet es ab und wieder ein; jede BEWEGUNG blendet es
// sofort ein (Editor-Gefuehl). Ohne laufenden Modus endet die Tick-Kette.
func TestCaretBlinks(t *testing.T) {
	r := enterCursor(newRig(t).seed("Hallo Welt"))
	if !strings.Contains(r.view(), "\x1b[7mH") {
		t.Fatal("Caret startet SICHTBAR (erste Blink-Phase an)")
	}
	r.do(charBlinkMsg{}) // ein Takt: Phase auf abgeblendet
	if !r.m.chat.charBlinkHide {
		t.Fatal("charBlinkMsg muss die Blink-Phase umschalten")
	}
	if strings.Contains(r.view(), "\x1b[7mH") {
		t.Fatal("abgeblendete Phase darf das Caret NICHT zeichnen")
	}
	r.do(charBlinkMsg{}) // zweiter Takt: wieder da
	if r.m.chat.charBlinkHide || !strings.Contains(r.view(), "\x1b[7mH") {
		t.Fatal("zweiter Takt blendet das Caret wieder ein")
	}
	r.do(charBlinkMsg{}) // abgeblendet …
	r.key("right")       // … Bewegung blendet sofort ein (Blink startet neu)
	if r.m.chat.charBlinkHide {
		t.Fatal("Bewegung muss das Caret einblenden")
	}
	if !strings.Contains(r.view(), "\x1b[7ma") {
		t.Fatalf("Caret nach right auf Buchstabe 1 (a) sichtbar: %q", r.view())
	}
	// Die Tick-Kette haengt am laufenden Modus: esc stoppt sie.
	if nm, cmd := r.m.Update(charBlinkMsg{}); cmd == nil {
		t.Fatal("aktiver Cursor muss den Blink nachtakten")
	} else {
		r.m = nm.(Model)
	}
	r.key("esc")
	if r.m.chat.charActive() {
		t.Fatal("esc verlaesst den Zeichen-Cursor sofort")
	}
	if _, cmd := r.m.Update(charBlinkMsg{}); cmd != nil {
		t.Fatal("ohne laufenden Cursor darf der Blink NICHT nachtakten")
	}
}

// TestPlainArrowsMoveCaretWithoutSelecting: Wie im Texteditor bewegen die
// PFEILE das Caret (Buchstabe fuer Buchstaben), ohne zu selektieren — die
// Auswahl kollabiert auf die Caret-Position; c kopiert dann die ganze
// Sektion (leere Auswahl).
func TestPlainArrowsMoveCaretWithoutSelecting(t *testing.T) {
	r := enterCursor(newRig(t).seed("Hallo Welt"))
	r.key("right").key("right").key("right")
	if r.m.chat.charTo != 3 {
		t.Fatalf("right x3: Caret bei 3, got %d", r.m.chat.charTo)
	}
	if !r.m.chat.charEmptySelection() {
		t.Fatal("Pfeile duerfen NICHT auswaehlen (Editor-Semantik)")
	}
	// Genau EIN unterstrichener Buchstabe (Position 3 = zweites l).
	view := r.view()
	if !strings.Contains(view, "\x1b[7ml") {
		t.Fatalf("Caret auf Buchstabe 3 (l) unterstrichen: %q", view)
	}
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "Hallo Welt" {
		t.Fatalf("leere Auswahl kopiert die ganze Sektion: %q", got)
	}
}

// TestShiftArrowsSelectFromAnchor: shift+Pfeile waehlen vom Anker (Eintritts-
// position) bis Caret — links zurueck schlaegt die Richtung um, ohne den
// Anker zu verschieben.
func TestShiftArrowsSelectFromAnchor(t *testing.T) {
	r := enterCursor(newRig(t).seed("abcdef"))
	r.key("shift+right").key("shift+right").key("shift+right")
	if r.m.chat.charFrom != 0 || r.m.chat.charTo != 3 {
		t.Fatalf("shift+right x3: Anker 0, Caret 3 — got from=%d to=%d",
			r.m.chat.charFrom, r.m.chat.charTo)
	}
	bg := r.m.chat.charSelBgSgr()
	if !strings.Contains(r.view(), bg+"abc\x1b[0m") {
		t.Fatalf("Bereich [0,3) muss als HINTERGRUND-Farbe geschlossen markiert sein (%s): %q", bg, r.view())
	}
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "abc" {
		t.Fatalf("c muss [0,3) kopieren: %q", got)
	}
	// Umschlagen: shift+left zieht das Caret UEBER den Anker zurueck.
	r.key("shift+left")
	ba, oa, bb, ob := r.m.chat.charRange()
	if ba != 0 || oa != 0 || bb != 0 || ob != 2 {
		t.Fatalf("Richtungsumschlag in Leserichtung 0..2: %d,%d..%d,%d", ba, oa, bb, ob)
	}
}

// TestCaretWalksAcrossSections: Der Cursor kennt KEINE Sektionsgrenze —
// right laeuft vom Ende eines Blocks in den Anfang des naechsten, up/down
// uebernehmen die Spalte zeilenweise (Klemme an der Zeilenlaenge).
func TestCaretWalksAcrossSections(t *testing.T) {
	r := newRig(t).seed("AAAA\n\nBBBB")
	// Leseposition: letztes Segment = Block 1 — caret dort, dann zurueck.
	r.key("tab")
	r.key("shift+tab")
	if r.m.chat.charBlock != 1 {
		t.Fatalf("Caret am fokussierten Block 1, got %d", r.m.chat.charBlock)
	}
	r.key("left") // vom Blockanfang 1: ein Sprung ans ENDE von Block 0
	if r.m.chat.charBlock != 0 || r.m.chat.charTo != 4 {
		t.Fatalf("left vom Blockanfang 1 muss in Block 0 ans ENDE springen: block=%d to=%d",
			r.m.chat.charBlock, r.m.chat.charTo)
	}
}

// TestCaretUpDownAcrossSections: up/down laufen ZEILENweise und wechseln an
// Blockgrenzen in die letzte/erste Zeile des Nachbarn.
func TestCaretUpDownAcrossSections(t *testing.T) {
	r := enterCursor(newRig(t).seed("one\ntwo\n\nthree"))
	// Block 0 ist fokussiert (einziges... nein: 2 Textbloecke; tab nimmt das
	// LETZTE — also gezielt Block 0 ansteuern: up vom Block 1 aus.
	r2 := newRig(t).seed("one\ntwo\n\nthree")
	r2.key("tab").key("shift+tab") // Caret auf Block 1 ("three")
	r2.key("up")
	if r2.m.chat.charBlock != 0 || r2.m.chat.charTo != 4 {
		t.Fatalf("up von Block 1 muss in die LETZTE Zeile von Block 0 (Offset 4): block=%d to=%d",
			r2.m.chat.charBlock, r2.m.chat.charTo)
	}
	r2.key("down")
	if r2.m.chat.charBlock != 1 || r2.m.chat.charTo != 0 {
		t.Fatalf("down von der letzten Zeile muss in Block 1 Zeile 0: block=%d to=%d",
			r2.m.chat.charBlock, r2.m.chat.charTo)
	}
	_ = r
}

// TestSelectionSpansSections: Eine Auswahl mit shift+right UEBER die Sektions-
// grenze kopiert Blocktexte mit Leerzeile dazwischen — Teilbereiche an beiden
// Raendern, volle Bloecke dazwischen.
func TestSelectionSpansSections(t *testing.T) {
	r := newRig(t).seed("AAAA\n\nBBBB")
	r.key("tab").key("shift+tab") // Fokus/Caret auf Block 1? nein — letzter
	if r.m.chat.charBlock != 1 {
		t.Fatalf("Setup: Caret auf Block 1, got %d", r.m.chat.charBlock)
	}
	// Caret auf Block 0 Offset 0 bringen: 5x left (Blockanfang -> Ende 0).
	for i := 0; i < 5; i++ {
		r.key("left")
	}
	for i := 0; i < 6; i++ { // 4x bis Blockende, 1x Sprung, 1x in B
		r.key("shift+right")
	}
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "AAAA\n\nB" {
		t.Fatalf("Blockuebergreifende Auswahl: %q", got)
	}
}

// TestEscCollapsesThenLeaves: esc loest ZUERST die EIGENE Auswahl (Caret
// bleibt), erst das zweite esc verlaesst den Cursor; von einer wartenden
// BLOCK-Auswahl aus verlaesst esc den Cursor SOFORT in sie zurueck (kein
// Kollaps-Zwischenschritt) — jede esc-Stufe haelt die Kette im Chat (das
// Eingabefeld uebernimmt erst, wenn nichts mehr abzuraeumen ist).
func TestEscCollapsesThenLeaves(t *testing.T) {
	r := enterCursor(newRig(t).seed("abcdef"))
	r.key("shift+right").key("shift+right")
	r.key("esc")
	if !r.m.chat.charActive() {
		t.Fatal("erstes esc kollabiert NUR die Auswahl — der Cursor bleibt")
	}
	if !r.m.chat.charEmptySelection() {
		t.Fatal("erstes esc muss die Auswahl aufs Caret ziehen")
	}
	if r.m.inputFocus {
		t.Fatal("esc darf die Kette nicht ins Eingabefeld werfen, solange der Cursor laeuft")
	}
	// Das Caret bleibt nach dem Kollaps adressierbar: c kopiert die leere
	// Auswahl als ganze Sektion.
	r.key("c")
	if got := r.copiedAll(); len(got) == 0 || got[len(got)-1] != "abcdef" {
		t.Fatalf("nach dem Kollaps kopiert c die ganze Sektion: %q", got)
	}
	r.key("esc")
	if r.m.chat.charActive() {
		t.Fatal("erst das zweite esc verlaesst den Cursor")
	}
	if !r.m.inputFocus {
		t.Fatal("ohne Block-Auswahl kehrt das zweite esc ins Eingabefeld zurueck")
	}

	// Aus einer BLOCK-AUSWAHL heraus: esc verlaesst den Zeichen-Cursor
	// SOFORT zurueck in die wartende Block-Auswahl (User-Report: Der
	// Kollaps-Zwischenschritt machte die Auswahl erst "weg" und das
	// naechste esc brachte sie zurueck — Flip-Flop wie ein Bug). Die
	// Block-Auswahl bleibt dabei sichtbar, das weitere esc loest sie.
	r2 := newRig(t).seed("aaa\n\nbbb")
	r2.key("tab").key("shift+up") // Block-Auswahl wachsend
	if !r2.m.chat.selActive() {
		t.Fatal("Setup: Block-Auswahl muss laufen")
	}
	r2.key("shift+tab") // Cursor an der Block-Auswahl
	if !r2.m.chat.charActive() {
		t.Fatal("Aus der Auswahl heraus in den Cursor")
	}
	r2.key("esc") // SOFORT raus: Cursor weg, Block-Auswahl bleibt
	if r2.m.chat.charActive() || !r2.m.chat.selActive() {
		t.Fatal("esc kehrt sofort zur Block-Auswahl zurueck (kein Kollaps-Zwischenschritt)")
	}
	if r2.m.inputFocus {
		t.Fatal("die Block-Auswahl haelt die Kette — Eingabefeld erst nach dem naechsten esc")
	}
	r2.key("esc")
	if r2.m.chat.selActive() {
		t.Fatal("das zweite esc loest die Block-Auswahl")
	}
	if !r2.m.chat.Focused() || r2.m.inputFocus {
		t.Fatal("esc parkt den Fokus auf der Leseposition — Antwort-Modus bleibt (kein Eingabefeld)")
	}
	r2.key("esc")
	if !r2.m.inputFocus {
		t.Fatal("erst das dritte esc kehrt auf den Standard zurueck: Eingabefeld")
	}
}

// TestCaretShiftEnterTogglesOut: shift+enter verlaesst den Cursor (Toggle)
// — mit laufender Block-Auswahl zurueck in sie.
func TestCaretShiftEnterTogglesOut(t *testing.T) {
	r := enterCursor(newRig(t).seed("Hallo Welt"))
	r.key("shift+tab")
	if r.m.chat.charActive() {
		t.Fatal("shift+enter muss den Cursor verlassen (Toggle)")
	}
	r.key("shift+tab")
	if !r.m.chat.charActive() {
		t.Fatal("und wieder hinein")
	}
}

// TestCaretReentryHoldsPosition: Der TOGGLE (shift+enter raus und wieder
// rein) haelt die Caret-Position — ein kurzer Blick raus soll die Lese-
// stelle nicht kosten. esc dagegen ist ABBRUCH: Es raeumt die Rest-
// Position mit weg, der Wiedereintritt startet frisch an der Lese-
// position (User-Report-Umfeld: esc-Flip-Flop; ein nach esc "wieder-
// auferstandener" Anker waere eine ueberraschende Alt-Auswahl).
func TestCaretReentryHoldsPosition(t *testing.T) {
	r := enterCursor(newRig(t).seed("Hallo Welt"))
	r.key("right").key("right").key("right")
	r.key("shift+tab") // Toggle raus: Position bleibt Wiedereintritts-Merk
	r.key("shift+tab") // Toggle rein
	if r.m.chat.charTo != 3 {
		t.Fatalf("Toggle-Wiedereintritt haelt Position 3, got %d", r.m.chat.charTo)
	}
	// esc ist Abbruch: Rest-Position weg, der naechste Einstieg startet
	// frisch an der Leseposition (Offset 0), nicht am alten Caret.
	r.key("esc")
	r.key("tab").key("shift+tab")
	if r.m.chat.charTo != 0 {
		t.Fatalf("nach esc startet der Wiedereintritt frisch (0), got %d", r.m.chat.charTo)
	}
}

// TestCaretHomeEnd: home/end springen an den Zeilenrand (Caret-Semantik,
// Auswahl kollabiert).
func TestCaretHomeEnd(t *testing.T) {
	r := enterCursor(newRig(t).seed("erste Zeile\nzweite Zeile"))
	r.key("end")
	if r.m.chat.charTo != 11 {
		t.Fatalf("end: Offset 11 (Zeilenende), got %d", r.m.chat.charTo)
	}
	r.key("home")
	if r.m.chat.charTo != 0 {
		t.Fatalf("home: Offset 0, got %d", r.m.chat.charTo)
	}
}

// TestCaretCodeBlockUnderlinesIntoHighlighting: Das Caret arbeitet auch im
// CodeBlock — Unterstreichung ins Syntax-Highlighting, Auswahl kopiert die
// plainen Buchstaben.
func TestCaretCodeBlockUnderlinesIntoHighlighting(t *testing.T) {
	withTrueColor(t)
	r := enterCursor(newRig(t).seed("```go\na := 1\n```"))
	if !r.m.chat.charActive() {
		t.Fatal("CodeBlock muss den Zeichen-Cursor unterstuetzen")
	}
	r.key("shift+right").key("shift+right")
	if !strings.Contains(r.view(), r.m.chat.charSelBgSgr()) {
		t.Fatal("Auswahl-Hintergrund fehlt im Codeblock-View")
	}
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "a " {
		t.Fatalf("c muss die plainen Code-Buchstaben [0,2) kopieren: %q", got)
	}
}

// TestCaretSkipsUnsupportedBlocks: Ohne einen einzigen Zeichen-faehigen
// Block (nur die User-Schiene) oeffnet shift+enter KEINEN Cursor.
func TestCaretSkipsUnsupportedBlocks(t *testing.T) {
	r := newRig(t).withSender(func(string) (string, error) { return "", nil })
	r.typeLine("meine frage an den agenten")
	r.key("enter")
	if len(r.m.chat.items) == 0 {
		t.Fatal("keine Segmente nach dem Senden")
	}
	r.key("shift+tab")
	if r.m.chat.charActive() {
		t.Fatal("ohne Zeichen-Block darf kein Cursor aufgehen")
	}
}

// TestCaretSpawnsFromNonCharFocus (Regression): tab fokussiert einen nicht
// buchstaben-faehigen Block am Chat-ENDE (Tabelle — wie ein ImageBlock aus
// HERON_IMAGES=force bei quirks-test.md) — shift+enter muss den Cursor
// TROTZDEM spawnen: im naechsten faehigen Block davor, nie stumm.
func TestCaretSpawnsFromNonCharFocus(t *testing.T) {
	r := newRig(t).seed("ein absatz text\n\n| a | b |\n|---|---|\n| 1 | 2 |")
	r.key("tab")
	if !r.m.chat.Focused() {
		t.Fatal("Setup: tab muss ein Segment fokussieren")
	}
	if r.m.chat.charCapableAt(r.m.chat.focus) {
		t.Fatal("Setup: fokussierter Block muss nicht-faehig sein (Tabelle)")
	}
	r.key("shift+tab")
	if !r.m.chat.charActive() {
		t.Fatal("shift+enter muss den Cursor spawnen, auch wenn der Fokus-Block kein Text ist")
	}
	if r.m.chat.charBlock != 0 {
		t.Fatalf("Cursor im naechsten faehigen Block davor (0), got %d", r.m.chat.charBlock)
	}
	if !strings.Contains(r.view(), "\x1b[7me") {
		t.Fatalf("Caret-Buchstabe e muss unterstrichen sein: %q", r.view())
	}
}

// TestPlantumlShiftEnterAutoSource (FR-42 + Bonus-FR): shift+enter auf der
// Block-Auswahl ueber einem PlantUML-Diagramm schaltet SELBST auf den Quell-
// text um (Auto-t — kein manueller t-Schritt mehr) und spawnt das Caret
// DARIN — MIT der Block-Auswahl als Zeichen-Auswahl ueber demselben Bereich
// (User-Wunsch: "sofort im text mode, cursor mit beibehaltener selection").
// t ist im Zeichen-Cursor-Modus STUMM: kein Rueckschalten, das dem Caret den
// Block unter den Fuessen wegziehen wuerde ("t entfert selection").
func TestPlantumlShiftEnterAutoSource(t *testing.T) {
	withTrueColor(t)
	r := newRig(t).seed("text davor\n\n```plantuml\nQBC -> X\n```\n\ntext danach")
	jobs := r.m.diagramJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d want 1", len(jobs))
	}
	l, n := r.m.chat.applyDiagrams([]diagramResult{{
		id: jobs[0].id, lang: "plantuml", src: "QBC -> X", ascii: "[ ascii ]",
		png: []byte("fake"), ok: true,
	}}, true, TermCaps{})
	if n != 1 {
		t.Fatalf("applied=%d want 1", n)
	}
	r.m.chat = l
	if _, ok := r.m.chat.items[1].(ImageBlock); !ok {
		t.Fatal("Setup: Block 1 muss nach der Konvertierung ein ImageBlock sein")
	}
	if r.m.chat.charCapableAt(1) {
		t.Fatal("Bild-Modus darf keinen Zeichen-Cursor zulassen (keine Zellen)")
	}

	// Antwort-Modus betreten (tab — shift+up startet KEINE Auswahl mehr
	// aus der Eingabe), Fokus = Nachsatz unten. EIN shift+up greift den
	// naechsten Abschnitt SOFORT: Anker-Einheit (Nachsatz, Leseposition
	// unten) PLUS PlantUML-Block.
	r.key("tab")
	r.key("shift+up")
	if lo, hi := r.m.chat.selBounds(); lo != 1 || hi != 2 {
		t.Fatalf("shift+up muss sofort PlantUML+Nachsatz greifen (1..2), got %d..%d", lo, hi)
	}
	// Die Auswahl ist am BILD sichtbar: Neon-Rahmen (marked) am Diagramm.
	hdr := ""
	for _, ln := range strings.Split(r.view(), "\n") {
		if strings.Contains(ln, "plantuml") {
			hdr = ln
			break
		}
	}
	if !strings.Contains(hdr, "191;0;255") {
		t.Fatalf("Auswahl muss den Diagramm-Rahmen neon faerben: %q", hdr)
	}

	// shift+enter: Auto-t (Quelltext-Ansicht) + Cursor IN der Quelle, MIT
	// der Auswahl (Anker unten am Nachsatz-Ende, Caret oben am Quell-Anfang).
	r.key("shift+enter")
	c := r.m.chat
	if !c.charActive() {
		t.Fatal("shift+enter muss den Zeichen-Cursor spawnen")
	}
	img, ok := c.items[1].(ImageBlock)
	if !ok || !img.showSrc {
		t.Fatal("shift+enter muss das Diagramm automatisch auf Quelltext schalten")
	}
	if c.charBlock != 1 {
		t.Fatalf("Caret muss im Quelltext-Block (1) spawnen, got %d", c.charBlock)
	}
	if c.charEmptySelection() {
		t.Fatal("die Block-Auswahl muss als Zeichen-Auswahl mitkommen")
	}
	if c.charFromBlock != 2 || c.charTo != 0 {
		t.Fatalf("Anker unten (Block 2), Caret oben (Offset 0): fromBlock=%d to=%d",
			c.charFromBlock, c.charTo)
	}
	if !strings.Contains(r.view(), "\x1b[7mQ") {
		t.Fatalf("Caret-Buchstabe Q (Quell-Anfang) muss invertiert sein: %q", r.view())
	}

	// t im Zeichen-Cursor-Modus: STUMM — kein Moduswechsel, kein Verlust.
	r.key("t")
	c = r.m.chat
	if !c.charActive() || !c.items[1].(ImageBlock).showSrc {
		t.Fatal("t darf im Zeichen-Cursor nichts umschalten (Auswahlverlust)")
	}
	if c.charEmptySelection() {
		t.Fatal("t darf die Auswahl nicht entfernen")
	}

	// c kopiert die konvertierte Auswahl: Quelle + Nachsatz KOMPLETT.
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 || got[len(got)-1] != "QBC -> X\n\ntext danach" {
		t.Fatalf("c muss Quelle + Nachsatz komplett kopieren, got %q", got)
	}
}

// TestCaretEdgeMarksDirection: Die CARET-Kante der Auswahl ist INVERTIERT
// abgehoben (Richtungs-Marker) — expandiert die Auswahl nach UNTEN, sitzt der
// Marker am unteren Ende (Caret-Block), expandiert sie nach OBEN, am oberen.
// Die Anker-Seite traegt nur den flaechigen Akzent-Hintergrund.
func TestCaretEdgeMarksDirection(t *testing.T) {
	// Expansion nach unten: Anker AAAA (Flaeche), Marker auf B (BBBB).
	// (tab fokussiert den LETZTEN Block — up zieht das Caret erst nach
	// oben in den Anker-Block, dann waechst die Auswahl nach unten.)
	r := enterCursor(newRig(t).seed("AAAA\n\nBBBB"))
	r.key("up")
	r.key("shift+down")
	sgr := r.m.chat.charSelBgSgr()
	v := r.view()
	if !strings.Contains(v, sgr+"A") {
		t.Fatalf("Anker-Block muss Akzent-Hintergrund tragen: %q", v)
	}
	if !strings.Contains(v, "\x1b[7mB") {
		t.Fatalf("Caret-Kante unten muss invertiert sein (B): %q", v)
	}
	if strings.Contains(v, sgr+"B") {
		t.Fatalf("Caret-Block (leerer Block-Schnitt) darf keine Flaeche tragen: %q", v)
	}

	// Expansion nach oben: Anker BBBB (Flaeche), Marker auf A (AAAA) —
	// das Caret spawnt (tab) auf dem LETZTEN Block, end setzt den Anker
	// ans Zeilenende, shift+up waechst nach oben.
	r2 := enterCursor(newRig(t).seed("AAAA\n\nBBBB"))
	r2.key("end")      // Zeilenende (Offset 4) — Anker
	r2.key("shift+up") // Caret zurueck nach OBEN in Block 0
	v2 := r2.view()
	sgr2 := r2.m.chat.charSelBgSgr()
	if !strings.Contains(v2, sgr2+"B") {
		t.Fatalf("Anker-Block unten muss Akzent-Hintergrund tragen: %q", v2)
	}
	if !strings.Contains(v2, "\x1b[7mA") {
		t.Fatalf("Caret-Kante oben muss invertiert sein (A): %q", v2)
	}
	if strings.Contains(v2, sgr2+"A") {
		t.Fatalf("Caret-Block oben (leerer Block-Schnitt) darf keine Flaeche tragen: %q", v2)
	}
}

// TestHeadingSelectionContrast: Ueberschriften tragen den Akzent als FG —
// auf Akzent-BG waeren sie unsichtbar (User-Report: "kein Kontrast"). Die
// Auswahl setzt deshalb bg AKZENT + fg HELL-WEISS: der Opener enthaelt
// beides, der markierte Text bleibt lesbar.
func TestHeadingSelectionContrast(t *testing.T) {
	r := enterCursor(newRig(t).seed("# Ueberschrift\n\ntext"))
	// Caret spawnt auf dem LETZTEN Block (tab) — hoch in die Ueberschrift.
	r.key("up")
	r.key("shift+right")
	r.key("shift+right")
	sgr := r.m.chat.charSelBgSgr()
	if !strings.Contains(sgr, "\x1b[97m") {
		t.Fatalf("Auswahl-Opener muss fg Hell-Weiss enthalten (Kontrast): %q", sgr)
	}
	if !strings.Contains(r.view(), sgr+"Ue") {
		t.Fatalf("Ueberschrift-Buchstaben muessen bg+fg aus dem Auswahl-Opener tragen: %q", r.view())
	}
}

// TestSkipEscapeChainsActive: Der Restaurierungs-Stil nach einem markierten
// Bereich ist die KETTE aller aktiven SGRs (fg UND bg) — nicht nur des
// letzten. Sonst verlor z.B. der Caret-Marker in einer gefaerbten Auswahl
// der Restzeile die Textfarbe.
func TestSkipEscapeChainsActive(t *testing.T) {
	last := ""
	rs := []rune("\x1b[38;2;217;211;236maa\x1b[48;2;191;0;255mbb")
	i := 0
	for i < len(rs) {
		if rs[i] == 0x1b {
			i = skipEscape(rs, i, &last)
		} else {
			i++
		}
	}
	want := "\x1b[38;2;217;211;236m\x1b[48;2;191;0;255m"
	if last != want {
		t.Fatalf("Kette aller aktiven SGRs erwartet, got %q want %q", last, want)
	}
	i = skipEscape([]rune("\x1b[0mx"), 0, &last)
	if last != "" {
		t.Fatalf("Reset muss die Kette leeren, got %q", last)
	}
}
