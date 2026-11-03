package tui

import (
	"strconv"
	"strings"

	"heron/internal/render"
)

// ---- Zeichen-Cursor (FR-42): Texteditor-Caret ueber dem Chat ---------------
//
// shift+enter IST der Cursor: ein Texteditor-Caret (nur lesend), das im
// dargestellten Text des Chats frei laeuft — ueber Zeilen (up/down), Buch-
// staben (left/right) UND Sektionsgrenzen: Der Dokumenttext ist die Folge
// der Zeichen-faehigen Bloecke, verduennt um Rahmen/Kopfzeilen. Wie im
// Editor gilt:
//
//	←↑↓→          Caret bewegen (Auswahl kollabiert zum Caret)
//	shift+←↑↓→    Auswahl vom Anker her erweitern/umschlagen
//	home/end      Zeilenanfang/-ende
//	c / enter     Auswahl kopieren (leere Auswahl = ganze Sektion)
//	esc           Modus sofort verlassen
//	shift+enter   Modus verlassen (zurueck zur Block-Auswahl, falls aktiv)
//
// Gestartet wird der Cursor an der LESEPOSITION: laeuft eine Block-Auswahl
// (shift+up/down), parkt das Caret an deren Cursor-Kante; sonst am
// fokussierten Block bzw. obersten sichtbaren Segment.

// charSelWidget ist ein Block, dessen Inhalt aus simplen Textzeilen besteht:
// CharLines liefert die SICHTBAREN Inhalt-Zeilen (ohne Rahmen, Kopf-, Fuss-
// und Leerzeilen) exakt wie sie gerendert werden — der Cursor adressiert den
// daraus gefalteten Text (Zeilen mit \n verbunden). CharPad ist die Anzahl
// gerenderter Zeilen VOR der ersten Inhalt-Zeile, CharLeft die Zellen links
// vom Inhalt (Rahmen/Einzug) — beides bildet flat Offset -> Bildschirmzelle ab.
type charSelWidget interface {
	Widget
	CharLines(width int) []string
	CharPad(width int) int
	CharLeft(width int) int
}

// charActive meldet, ob der Zeichen-Cursor laeuft.
func (l BlockList) charActive() bool { return l.charSel && len(l.items) > 0 }

// charView liefert die Zeichen-Ansicht eines Blocks (leer, wenn der Block
// keine unterstuetzt — User-Schiene, Bilder, Tabellen).
func (l BlockList) charView(i, w int) (charSelWidget, []string, bool) {
	if i < 0 || i >= len(l.items) {
		return nil, nil, false
	}
	cw, ok := l.items[i].(charSelWidget)
	if !ok {
		return nil, nil, false
	}
	lines := cw.CharLines(w)
	if len(lines) == 0 {
		return nil, nil, false
	}
	return cw, lines, true
}

// charCapableAt: Kann der Cursor in Block i sitzen?
func (l BlockList) charCapableAt(i int) bool {
	_, _, ok := l.charView(i, l.Width())
	return ok
}

// nextCharBlock sucht den naechsten Zeichen-faehigen Block ab i (exklusiv)
// in Richtung dir — Rahmen-Bloecke (User-Schiene, Bild, Tabelle) ueberspringt
// der Cursor, wie ein Editor keine Leerzeile im Diagramm adressiert. Ein
// PlantUML/Mermaid-Block ist nur in der QUELLTEXT-Ansicht (t-Switch) faehig:
// Als gerendertes Bild hat er keine Zellen, als Zaun-Quelltext laeuft der
// Cursor durch (ImageBlock.CharLines).
func (l BlockList) nextCharBlock(i, dir int) int {
	for j := i + dir; j >= 0 && j < len(l.items); j += dir {
		if l.charCapableAt(j) {
			return j
		}
	}
	return -1
}

// enterCursorMode setzt das Caret an die Leseposition. Prioritaet: (1) die
// laufende Block-Auswahl (deren Cursor-Block, Kante nach Ausloeser-Richtung
// — der Unterschied sind jetzt BUCHSTABEN, nicht mehr Sektionen), (2) der
// fokussierte Block, (3) der oberste sichtbare Block (ggf. der naechste
// Zeichen-faehige darunter). Kollabierte Tool-Karten klappt der Eintritt
// auf; ein Wiedereintritt nach esc haelt die Caret-Position (FR-43-Geist).
func (l BlockList) enterCursorMode() BlockList {
	if l.charActive() || len(l.items) == 0 {
		return l
	}
	// Eine laufende MARKIERUNG weicht dem Caret — zur VERGANGENEN Auswahl
	// (FR-43), nicht ins Nichts: Ihr Rahmen bleibt in Sekundaer-Farbe
	// stehen, bis esc alles aufloest. (Eine laufende Block-Auswahl wird
	// dagegen uebernommen, s.u.)
	if l.markedCount() > 0 {
		l = l.demoteMarks()
	}
	w := l.Width()
	blk := -1
	edge := 0 // Caret-Kante: 0 = Anfang (Leserichtung), -1 = Ende
	if l.selActive() {
		blk = l.selCur
		if l.selDir < 0 {
			edge = -1
		}
	} else if l.Focused() {
		blk = l.focus
	} else if i := l.topVisibleIndex(); i >= 0 {
		blk = i
	}
	// Auto-t (Bonus-FR, User-Wunsch): Die Leseposition ist ein Diagramm-
	// BILD mit Zaun-Quelle — shift+enter schaltet SELBST auf den Quelltext
	// um und spawnt das Caret DARIN (statt den Block zu ueberspringen und
	// den Nutzer erst t druecken zu lassen). Das Bild hat keine Zellen,
	// der Zaun-Quelltext ist buchstaben-faehig (ImageBlock.CharLines).
	if blk >= 0 && blk < len(l.items) {
		if img, ok := l.items[blk].(ImageBlock); ok && !img.showSrc && img.src != "" {
			nb, _ := img.toggleSrc()
			l.items[blk] = nb
		}
	}
	if !l.charCapableAt(blk) {
		// Der Leseposition-Block ist nicht buchstaben-faehig (Tabelle,
		// User-Schiene, Bild OHNE Quelle — z. B. tab auf einem ImageBlock
		// aus HERON_IMAGES=force): Der Cursor sucht den naechsten faehigen
		// Block VORWAERTS, sonst zurueck — er spawnt IMMER, nie stumm
		// (Hauptfeature).
		nb := l.nextCharBlock(blk-1, 1)
		if nb < 0 {
			nb = l.nextCharBlock(blk+1, -1)
		}
		if nb < 0 {
			return l // der ganze Chat hat keinen Buchstaben-Text
		}
		blk = nb
	}
	// Kollabierte Tool-Karte aufklappen: Das Caret adressiert den
	// VOLLSTAENDIGen Inhalt, nicht den gefalteten Rest.
	if tb, ok := l.items[blk].(ToolBlock); ok && !tb.expanded {
		tb.expanded = true
		l.items[blk] = tb
	}
	text, _ := l.charTextAt(blk, w)
	n := len([]rune(text))
	l.charSel = true
	l.charBlinkHide = false // Caret startet SICHTBAR und blinkt ab dem ersten Takt
	// Rest-Auswahl restaurieren (VERALLGEMEINERT — User-Reports: "der
	// cursor mode verschiebt den ankerpunkt" + "die selection im cursor
	// mode soll erhalten bleiben"): Gueltige Anker/Caret-Reste sind die
	// FORTLAUFENDE Auswahl — der Eintritt nimmt sie EXAKT wieder auf, auch
	// ohne darunterliegende Block-Auswahl. Laeuft eine Block-Auswahl,
	// restaurieren die Reste nur bei DECKUNGSGLEICHEN Grenzen; veraenderte
	// sie sich unterdessen (shift+up/down), konvertiert der Eintritt wie
	// bisher von den Kanten. Nur esc raeumt die Reste (resetCharResidual).
	if l.charFromBlock >= 0 && l.charBlock >= 0 {
		match := !l.selActive()
		if !match {
			lo, hi := l.selBounds()
			rlo, rhi := l.charFromBlock, l.charBlock
			if rlo > rhi {
				rlo, rhi = rhi, rlo
			}
			match = rlo == lo && rhi == hi
		}
		if match {
			if t, ok := l.charTextAt(l.charBlock, w); ok {
				if n := len([]rune(t)); l.charTo < 0 || l.charTo > n {
					l.charTo = n // Text unterdessen gewachsen: klemmen
				}
			}
			l.follow = false
			l = l.clearFocus() // hoechstens EIN Neon-Signal (FR-39)
			return l.scrollCharIntoView()
		}
	}
	if l.selActive() {
		// Konvertierung MIT VORRANG (User-Report: shift+tab auf einer
		// Auswahl "killte" sie): Der Wiedereintritts-Merk charBlock ist
		// ein RESTZUSTAND — stand er zufaellig auf blk (Nullwert 0 oder
		// Rest einer frueheren Session), lief die "Position halten"-
		// Verzweigung statt der Konvertierung und leerte die Auswahl.
		// Die laufende Block-Auswahl wird jetzt ZUVERLAESSIG zur Zeichen-
		// Auswahl ueber demselben Bereich.
		// Der ANKER ueberlebt auch die Block-Granularitaet (klassisches
		// Select — User-Report: "man hat einen Anker, dieser bleibt
		// aktiv"): Liegt der feine Anker-Rest IM Bereich der Block-
		// Auswahl, haelt er EXAKT (nur ans Textende geklemmt); neu ge-
		// baut wird er nur OHNE solchen Rest.
		keepAnchor := l.charFromBlock >= 0
		if keepAnchor {
			lo, hi := l.selBounds()
			if l.charFromBlock < lo || l.charFromBlock > hi {
				keepAnchor = false
			} else if at, ok := l.charTextAt(l.charFromBlock, w); ok {
				if n := len([]rune(at)); l.charFrom > n {
					l.charFrom = n
				}
			}
		}
		if !keepAnchor {
			anchor := l.selFrom
			ao := 0
			if at, ok := l.charTextAt(anchor, w); ok {
				// Adressierbarer Anker: ab seiner Wachstums-Kante (unten =
				// Ende, oben = Anfang).
				if l.selDir < 0 {
					ao = len([]rune(at))
				}
			} else if na := l.nextCharBlock(anchor, l.selAnchorDir()); na >= 0 {
				// NICHT adressierbarer Anker (PlantUML-Bild, User-Schiene,
				// Tabelle): Der Anker huepft zum naechsten faehigen Block
				// AUSSERHALB der Auswahl — die unadressierbaren Bloecke bis
				// dahin bleiben GANZ Teil der Auswahl (als "Mitte"-Bloecke,
				// s. all()/charMarkdown), statt an der Kante verloren zu
				// gehen (User-Report: Auswahl "uebersprang" Elemente).
				anchor = na
				if l.selDir >= 0 {
					at, _ := l.charTextAt(anchor, w)
					ao = len([]rune(at))
				}
			}
			l.charFrom, l.charFromBlock = ao, anchor
		}
		if l.selDir < 0 {
			l.charTo, l.charBlock = 0, blk
		} else {
			l.charTo, l.charBlock = n, blk
		}
		// Einheitlicher Auswahl-Vertrag: Der Eintritt KONSUMIERT die Block-
		// Auswahl — im Modus existiert genau EINE Auswahl (die feine
		// Konvertierung). Bereich selFrom/selCur und selDir bleiben fuer
		// den esc-Rueckweg stehen (charFromSel).
		l.charFromSel = true
		l.sel = false
	} else if l.charBlock == blk { // Wiedereintritt: Position halten
		if l.charTo < 0 || l.charTo > n {
			l.charFrom, l.charTo = 0, 0
		}
		l.charFromBlock = l.charBlock
	} else if edge < 0 {
		l.charFrom, l.charTo, l.charBlock = n, n, blk
		l.charFromBlock = blk
	} else {
		l.charFrom, l.charTo, l.charBlock = 0, 0, blk
		l.charFromBlock = blk
	}
	l.follow = false
	l = l.clearFocus() // hoechstens EIN Neon-Signal (FR-39): das Caret
	return l.scrollCharIntoView()
}

// clearCharSel verlaesst den Zeichen-Cursor (shift+enter-Toggle, esc-Stufe
// 2). FR-43: Die Zeichen-Auswahl geht dabei NICHT verloren — laeuft keine
// Block-Auswahl weiter (die beim Eintritt uebernommen wurde und denselben
// Bereich zeigt), wird sie zur VERGANGENEN Auswahl delegiert: Rahmen in
// Sekundaer-Farbe, bis esc alles aufloest.
func (l BlockList) clearCharSel() BlockList {
	if l.charActive() && !l.charEmptySelection() && !l.selActive() {
		ba, _, bb, _ := l.charRange()
		l = l.setPrevSel(ba, bb)
	}
	l.charSel = false
	l.charFromSel = false
	return l
}

// clearCharSelEsc ist die esc-Variante des Verlassens: esc ist ABBRUCH,
// kein "gleich wieder rein" — anders als beim Toggle (shift+enter haelt
// die Rest-Position als Wiedereintritts-Merk) raeumt sie auch Anker und
// Caret-Rest weg. Sonst spawnt der naechste shift+enter mit einer ALTEN
// Auswahl statt einem frischen Caret (User-Flow: Auswahl → Cursor →
// esc → esc → tab → shift+enter; Umgebung des esc-Flip-Flop-Reports).
func (l BlockList) clearCharSelEsc() BlockList {
	l = l.clearCharSel()
	return l.resetCharResidual()
}

// resetCharResidual verwirft die Rest-Position des Zeichen-Cursors (Anker
// UND Caret). esc ist der EINZIGE Anker-Reset (User-Report: "der ankerpunkt
// sollte nur durch beendigung mit esc zurueckgesetzt werden") — auch im
// Block-Modus: Wurde der Cursor-Modus per Toggle verlassen, haelt die Rest-
// Auswahl als Wiedereintritts-Merk; loest esc die Block-Auswahl (oder den
// Fokus), raeumt es die Reste MIT weg, sonst restaurierte der naechste
// Eintritt die "wegge-esc-te" Auswahl.
func (l BlockList) resetCharResidual() BlockList {
	l.charFrom, l.charTo = 0, 0
	l.charFromSel = false
	l.charFromBlock, l.charBlock = -1, -1
	return l
}

// residualSelShown: Die ERHALTENE Auswahl (Toggle-Austritt ausserhalb des
// Anker-Blocks) ist sichtbar, solange der Modus geschlossen ist — bis esc
// sie loest (User-Report: "wenn ich cursor mode schliesse wird die
// selection entfernt, gehe ich wieder rein ist sie wieder drin — das soll
// so nicht sein"). Nicht sichtbar, solange der Modus laeuft (charActive
// rendert inklusive Caret/Blink) und nicht, wenn eine deckungsgleiche
// Block-Auswahl denselben Bereich bereits GANZ zeigt (Anker-Block-Austritt).
func (l BlockList) residualSelShown() bool {
	if l.charSel || l.charFromBlock < 0 || l.charBlock < 0 || l.charEmptySelection() {
		return false
	}
	if l.selActive() {
		lo, hi := l.selBounds()
		rlo, rhi := l.charFromBlock, l.charBlock
		if rlo > rhi {
			rlo, rhi = rhi, rlo
		}
		if rlo >= lo && rhi <= hi {
			// Die Block-Auswahl ENTHAELT den erhaltenen Bereich bereits
			// ganz (deckungsgleich ODER darueber hinaus gewachsen —
			// User-Report: wuchs sie ueber den Austritts-Block hinaus,
			// poppte die feine Auswahl ZURUECK ins Bild, als stuende der
			// Cursor-Modus wieder an): der Rahmen zeigt ihn schon, ein
			// zweites Neon-Signal waere Rauschen (FR-39). Sie taucht erst
			// wieder auf, wenn die Auswahl ihn nicht mehr abdeckt.
			return false
		}
	}
	return true
}

// charSelVisible meldet, ob eine Zeichen-Auswahl zu sehen ist — im aktiven
// Modus ODER als erhaltene Auswahl danach. Render-Gates (all) und Copy
// (selMarkdown) nutzen dieses eine Predicate.
func (l BlockList) charSelVisible() bool {
	return l.charActive() || l.residualSelShown()
}

// demoteResidualSel ueberfuehrt die erhaltene Auswahl in die VERGANGENE
// (Sekundaer-Rahmen), wenn eine NEUE Auswahl beginnt (FR-43-Muster: alte
// Auswahl weicht nicht ins Nichts, sondern verblasst bis esc).
func (l BlockList) demoteResidualSel() BlockList {
	if !l.residualSelShown() {
		return l
	}
	ba, _, bb, _ := l.charRange()
	l = l.setPrevSel(ba, bb)
	return l.resetCharResidual()
}

// extendResidualSel fuehrt shift+up/down auf der ERHALTENEN Auswahl
// (Modus geschlossen) mit KLASSISCHER Select-Semantik aus (User-Report:
// "ganz klassisches Select — man hat einen Anker, dieser bleibt aktiv;
// ob der Cursor-Modus verlassen wird, ist egal"): Der ANKER haelt EXAKT
// (charFrom*), der CARET wandert um GENAU EINE Sektion — auch zurueck
// zum Anker (klassisches Schrumpfen), kein Neu-Ankern am Viewport-Rand,
// kein Ersetzen der Auswahl durch eine andere irgendwo im Dokument.
// Ausserhalb des Modus gilt der GANZE Block als select (User-Report:
// "der einzige Unterschied ist, dass der Cursor-Modus verlassen werden
// kann und dann der ganze Block gilt"): Die Auswahl laeuft als Block-
// Auswahl Anker-Block .. Caret-Block (Rahmen, Kopie), die feinen Anker-
// Koordinaten ueberleben fuer den exakten Wiedereintritt.
// extendResidualSel fuehrt shift+up/down AUSSEN auf der Anker-Position
// aus (User-Report: "ich habe den Cursor gesetzt — von da aus beginnt
// mein Select, egal was ich vorher ausgewaehlt habe; der Anker ist IMMER
// der Startpunkt, das Select expandiert nur in Druckrichtung — nicht nach
// unten und nicht nach rechts, weil ich shift+up gedrueckt habe; wenn ich
// 2 mal shift+down druecke ist der Ankerpunkt immer noch der selbe, nur
// esc kann ihn auf null setzen"): KLASSISCHE Zeichenauswahl — der ANKER
// steht char-scharf fest (die gesetzte Cursor-Position, AUCH OHNE laufen-
// de Auswahl), der CARET laeuft Blockrand fuer Blockrand in Druck-
// richtung: hoch zunaechst bis zum ANFANG des Anker-Blocks (der Ausschnitt
// UEBER dem Anker — nicht der ganze Block, nichts unterhalb), dann ganze
// Bloecke darueber; runter spiegelbildlich. Ueberquerte Bloecke zaehlen
// GANZ (Mittelblock-Regel), nicht faehige werden uebersprungen (Diagramm-
// BILD mit Quelle wird per Auto-t buchstabenfaehig). KEIN Ganzblock-
// Neustart vom Viewport-Anker (das "waehlte den ganzen Schnippsel"), KEIN
// Wiedereintritt in den Modus (charSel bleibt aus).
func (l BlockList) extendResidualSel(dir int) BlockList {
	if l.charFromBlock < 0 || l.charBlock < 0 || len(l.items) == 0 {
		return l
	}
	// Ein Blockrand pro Druck. Landet der Schritt auf dem Anker-Rand
	// (leere Auswahl), folgt SOFORT der naechste — der Erstdruck darf
	// nichts "verschenken" (User-Report Runde 6: "2 statt 1 mal druecken").
	for i := 0; i <= len(l.items); i++ {
		var ok bool
		l, ok = l.stepResidualCaret(dir)
		if !ok {
			return l // Kettenende: Auswahl haelt (kein Ueberschwingen)
		}
		if !l.charEmptySelection() {
			break
		}
	}
	l.follow = false
	return l.scrollCharIntoView()
}

// stepResidualCaret bewegt den Caret-Rest um EINEN Blockrand in dir:
// innerhalb des Blocks an dessen naechsten Rand (hoch: Anfang, runter:
// Ende), am Rand in den adjazenten faehigen Block (dessen Anfang bzw.
// Ende — uebersprungene Bloecke bleiben GANZ in der Auswahl, s. char-
// Markdown/charDecorate Mittelblock-Regel).
func (l BlockList) stepResidualCaret(dir int) (BlockList, bool) {
	w := l.Width()
	if dir < 0 {
		if l.charTo > 0 {
			l.charTo = 0
			return l, true
		}
		nl := l.autoTextModeOnCross(-1)
		if nb := nl.nextCharBlock(l.charBlock, -1); nb >= 0 {
			nl.charBlock, nl.charTo = nb, 0
			return nl, true
		}
		return l, false
	}
	if t, ok := l.charTextAt(l.charBlock, w); ok {
		if end := len([]rune(t)); l.charTo < end {
			l.charTo = end
			return l, true
		}
	}
	nl := l.autoTextModeOnCross(1)
	if nb := nl.nextCharBlock(l.charBlock, 1); nb >= 0 {
		if nt, ok := nl.charTextAt(nb, w); ok {
			nl.charBlock, nl.charTo = nb, len([]rune(nt))
			return nl, true
		}
	}
	return l, false
}

// exitCursorToSel verlaesst den Zeichen-Cursor per Toggle (shift+enter)
// und KONVERTIERT eine nicht-leere Zeichen-Auswahl zur BLOCK-AUSWAHL
// zurueck — die Spiegelrichtung des Eintritts (Eintritt: Block-Auswahl
// wird Zeichen-Auswahl ueber demselben Bereich; Austritt: Zeichen-Auswahl
// wird Block-Auswahl ueber deren Bloecken). User-Report: "cursor mode zu
// beenden speichert die auswahl nicht korrekt — die auswahl sollte eher
// den ganzen block kopieren": c kopiert danach ganze Bloecke als Markdown
// (selMarkdown), statt die Auswahl als VERGANGENE nur noch verblassen zu
// lassen. Eine LEERE Auswahl (nur Caret) bleibt beim alten Toggle-Ver-
// halten: Modus raus, Rest-Position haelt (Wiedereintritt).
//
// Der ANKER ueberlebt den Kreislauf EXAKT (User-Report: "der cursor mode
// verschiebt den ankerpunkt — zuruecksetzen sollte nur esc"): Anker und
// Caret bleiben als Rest stehen; der Wiedereintritt restauriert sie
// deckungsgleich (enterCursorMode), statt sie auf Blockkanten neu
// abzuleiten. Nur esc raeumt die Reste weg (resetCharResidual).
func (l BlockList) exitCursorToSel() BlockList {
	if l.charEmptySelection() {
		return l.clearCharSel()
	}
	// KLASSISCHER Austritt (User-Report: "ich gehe aus den Cursor-Mode —
	// der Anker bleibt der selbe, von da aus beginnt unser Select; jetzt
	// waehlt er alles aus, das ist falsch — er soll vom Anker starten"):
	// Der Austritt konvertiert NICHTS — ob der Cursor-Modus offen ist, ist
	// fuer die Auswahl bedeutungslos. Sie bleibt Anker..Caret in ihrer
	// FEINEN Granularitaet erhalten (sichtbar + kopierbar bis esc, s.
	// residualSelShown); die fruehere Ganz-Block-Konversion beim Austritt
	// im Anker-Block ("eher den ganzen Block kopieren") ist damit weg —
	// sie griff VOR den Anker und waehlte damit "alles" statt ab ihm.
	// Block-Granularitaet ("der ganze Block gilt") kommt erst mit shift+
	// up/down VON AUSSEN (extendResidualSel: Anker-Block .. Caret-Block).
	// Eine darunterliegende Block-Auswahl (Modus-Eintritt UEBER ihr)
	// bleibt daneben bestehen und zeigt den Bereich wieder ganz, solange
	// sie die feine Auswahl enthaelt (residualSelShown-Deckelung).
	l.charSel = false
	// Einheitlicher Auswahl-Vertrag: Eine darunterliegende Block-Auswahl
	// (Modus-Eintritt UEBER ihr) wurde beim Eintritt bereits KONSUMIERT
	// (charFromSel, sel=false) — draussen zaehlt nur EINE Auswahl, die
	// feine Anker..Caret (anderfalls faengen ihre Alt-Grenzen den
	// naechsten shift-Druck ab und die Auswahl springt auf Blockkanten
	// statt vom Anker zu starten).
	l.sel = false
	l.charFromSel = false
	return l
}

// toggleCharBlink schaltet die Blink-Phase des Carets um (charBlinkMsg,
// app.go) — nur gerufen, solange der Modus laeuft.
func (l BlockList) toggleCharBlink() BlockList {
	l.charBlinkHide = !l.charBlinkHide
	return l
}

// charCollapse zieht die Auswahl aufs Caret zusammen (esc-Stufe 1).
func (l BlockList) charCollapse() BlockList {
	l.charFrom, l.charFromBlock = l.charTo, l.charBlock
	return l
}

// charEmptySelection: Caret steht auf dem Anker (kein Bereich markiert).
func (l BlockList) charEmptySelection() bool {
	return l.charFrom == l.charTo && l.charFromBlock == l.charBlock
}

// charTextAt ist der flat Text eines Blocks (Inhalt-Zeilen, \n-verbunden).
func (l BlockList) charTextAt(i int, w int) (string, bool) {
	_, lines, ok := l.charView(i, w)
	if !ok {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

// autoTextModeOnCross (User-Report: "im cursor mode kann ich kein mermaid
// diagramm auswählen — am besten automatisch im text mode wechseln"): Ein
// gerendertes Diagramm-BILD hat keine Zellen, der Zeichen-Cursor kann es
// nicht betreten — als RAND-Block (kein Text darunter/darueber) war es
// damit GAR nicht auswaehlbar, als Mittelblock nur "ganz". Erreicht die
// Caret-Bewegung (Buchstabe oder Zeile) einen Blockrand, schaltet heron
// die Diagramm-Bloecke MIT Quelle auf dem UEBERSPRING-PFAD automatisch auf
// den Zaun-QUELLTEXT um (dasselbe Auto-t wie beim Modus-Eintritt): Der
// naechste (jetzt) faehige Block ist die Grenze — der Sprung ENDET im
// ersten Diagramm statt DAHINTER (User-Report: "shift+up springt eine
// unbekannte Zahl an Abschnitten"). Permanent unfaehige Bloecke (User-
// Schiene, Tabelle, Bild OHNE Quelle) bleiben uebersprungen; traege pro
// Bewegungsschritt — nur was der Cursor tatsaechlich ansteuert, wechselt
// die Ansicht.
func (l BlockList) autoTextModeOnCross(dir int) BlockList {
	for j := l.charBlock + dir; j >= 0 && j < len(l.items); j += dir {
		if l.charCapableAt(j) {
			return l // faehiger Block vorweg: kein Diagramm zu schalten
		}
		if img, ok := l.items[j].(ImageBlock); ok && !img.showSrc && img.src != "" {
			nb, _ := img.toggleSrc()
			l.items[j] = nb
			return l // jetzt faehig: Der Blockrand-Sprung endet HIER
		}
	}
	return l
}

// charMove bewegt das Caret um EINEN Buchstaben (dir) — am Blockrand in den
// Nachbar-Block. extend=true haelt den Anker (shift+left/right waehlt);
// ohne shift gilt FR-43: Eine BESTEHENDE Auswahl kollabiert NICHT mehr —
// der Anker bleibt stehen, die Auswahl folgt dem Caret live (auch mit
// Richtungsumschlag). Nur eine LEERE Auswahl bleibt leer (Anker folgt), und
// esc kollabiert sie weiterhin (Stufe 1).
func (l BlockList) charMove(dir int, extend bool) BlockList {
	w := l.Width()
	text, ok := l.charTextAt(l.charBlock, w)
	if !ok {
		return l
	}
	wasEmpty := l.charEmptySelection()
	rs := []rune(text)
	next := l.charTo + dir
	switch {
	case next >= 0 && next <= len(rs):
		l.charTo = next
	case next < 0:
		l = l.autoTextModeOnCross(-1)
		if pb := l.nextCharBlock(l.charBlock, -1); pb >= 0 {
			pt, _ := l.charTextAt(pb, w)
			l.charBlock, l.charTo = pb, len([]rune(pt))
		}
	default: // hinter das Blockende
		l = l.autoTextModeOnCross(1)
		if nb := l.nextCharBlock(l.charBlock, 1); nb >= 0 {
			l.charBlock, l.charTo = nb, 0
		}
	}
	if !extend && wasEmpty {
		l = l.charCollapse()
	}
	l.charBlinkHide = false // Bewegung blendet das Caret ein (Blink startet neu)
	return l.scrollCharIntoView()
}

// charMoveLine bewegt das Caret um EINE Zeile (dir) und haelt die Spalte —
// an Blockraendern in die erste/letzte Zeile des Nachbarn (Klemme an der
// Zeilenlaenge wie im Editor). FR-43: Ohne shift kollabiert eine BESTEHENDE
// Auswahl nicht — der Anker bleibt, die Auswahl folgt dem Caret.
func (l BlockList) charMoveLine(dir int, extend bool) BlockList {
	w := l.Width()
	text, ok := l.charTextAt(l.charBlock, w)
	if !ok {
		return l
	}
	wasEmpty := l.charEmptySelection()
	line, col := charLineCol(text, l.charTo)
	nLines := strings.Count(text, "\n") + 1
	nl := line + dir
	if nl >= 0 && nl < nLines {
		l.charTo = charAtLineCol(text, nl, col)
	} else if nl < 0 {
		l = l.autoTextModeOnCross(-1)
		if pb := l.nextCharBlock(l.charBlock, -1); pb >= 0 {
			pt, _ := l.charTextAt(pb, w)
			pLines := strings.Count(pt, "\n") + 1
			l.charBlock = pb
			l.charTo = charAtLineCol(pt, pLines-1, col)
		}
	} else {
		l = l.autoTextModeOnCross(1)
		if nb := l.nextCharBlock(l.charBlock, 1); nb >= 0 {
			nt, _ := l.charTextAt(nb, w)
			l.charBlock = nb
			l.charTo = charAtLineCol(nt, 0, col)
		}
	}
	if !extend && wasEmpty {
		l = l.charCollapse()
	}
	l.charBlinkHide = false // Bewegung blendet das Caret ein (Blink startet neu)
	return l.scrollCharIntoView()
}

// charHome/charEnd springen an Anfang/Ende der Caret-Zeile. FR-43: Eine
// BESTEHENDE Auswahl ueberlebt den Sprung (Anker bleibt, nur esc loest auf);
// eine leere Auswahl bleibt leer.
func (l BlockList) charHome() BlockList {
	text, ok := l.charTextAt(l.charBlock, l.Width())
	if !ok {
		return l
	}
	wasEmpty := l.charEmptySelection()
	line, _ := charLineCol(text, l.charTo)
	l.charTo = charAtLineCol(text, line, 0)
	l.charBlinkHide = false // Bewegung blendet das Caret ein (Blink startet neu)
	if wasEmpty {
		return l.charCollapse()
	}
	return l
}

func (l BlockList) charEnd() BlockList {
	text, ok := l.charTextAt(l.charBlock, l.Width())
	if !ok {
		return l
	}
	wasEmpty := l.charEmptySelection()
	line, _ := charLineCol(text, l.charTo)
	l.charTo = charAtLineCol(text, line, charLineLen(text, line))
	l.charBlinkHide = false // Bewegung blendet das Caret ein (Blink startet neu)
	if wasEmpty {
		return l.charCollapse()
	}
	return l
}

// charRange liefert die Auswahl in DOKUMENT-Reihenfolge: (Block, Offset)
// des Ankers und des Carets — tauscht bei umgeschlagener Richtung.
func (l BlockList) charRange() (ba, oa, bb, ob int) {
	ba, oa, bb, ob = l.charFromBlock, l.charFrom, l.charBlock, l.charTo
	if ba > bb || (ba == bb && oa > ob) {
		ba, oa, bb, ob = bb, ob, ba, oa
	}
	return ba, oa, bb, ob
}

// charMarkdown ist der Copy-Payload: der markierte Bereich als dargestellter
// Text (Blockgrenzen als Leerzeile), eine LEERE Auswahl kopiert die ganze
// Caret-Sektion — konsistent mit c im Block-Modus.
func (l BlockList) charMarkdown() string {
	w := l.Width()
	ba, oa, bb, ob := l.charRange()
	if ba == bb {
		text, ok := l.charTextAt(ba, w)
		if !ok {
			return ""
		}
		if oa == ob {
			return text
		}
		rs := []rune(text)
		return string(rs[oa:min(ob, len(rs))])
	}
	var parts []string
	ta, _ := l.charTextAt(ba, w)
	parts = append(parts, string([]rune(ta)[oa:]))
	for i := ba + 1; i < bb; i++ {
		if t, ok := l.charTextAt(i, w); ok {
			parts = append(parts, t)
		} else {
			// Nicht adressierbare Bloecke im Auswahlbereich (PlantUML-BILD,
			// User-Schiene, Tabelle) werden GANZ kopiert — der Cursor kann
			// sie nicht betreten, aber die Auswahl uebernimmt sie komplett
			// (Rahmen leuchtet live, s. all()). Nur im QUELLTEXT-Modus
			// (t-Switch) waere der Block buchstabenweise adressierbar.
			parts = append(parts, l.items[i].Markdown())
		}
	}
	tb, _ := l.charTextAt(bb, w)
	parts = append(parts, string([]rune(tb)[:ob]))
	return strings.Join(parts, "\n\n")
}

// charDecorate hebt Cursor/Auswahl in den gerenderten Zeilen von Block i
// hervor (aufgerufen aus all()): Eine LEERE Auswahl invertiert genau den
// Caret-Buchstaben (das Editor-Caret, BLINKEND via charBlinkHide), sonst
// faerbt sie den Bereich als HINTERGRUND (Theme-Akzent, kein Unterstrich)
// — am Anker-Block ab Anker-Offset, im Caret-Block bis Caret-Offset, volle
// Bloecke dazwischen komplett. Der Farbstreich wird ANSI-bewusst in die
// STYLED Zeile gezeichnet (Highlighting bleibt sichtbar). Die CARET-KANTE
// traegt zusaetzlich Invertierung als Richtungs-Marker (s.u.).
func (l BlockList) charDecorate(i int, blk []string, w int) []string {
	cw, cls, ok := l.charView(i, w)
	if !ok {
		return blk
	}
	pad, left := cw.CharPad(w), cw.CharLeft(w)
	if l.charEmptySelection() {
		if i != l.charBlock || l.charBlinkHide {
			return blk // Abgeblendete Blink-Phase: kein Caret auf dem Schirm
		}
		return caretBlock(blk, cls, pad, left, l.charTo)
	}
	ba, oa, bb, ob := l.charRange()
	if i < ba || i > bb {
		return blk
	}
	lo := 0
	if i == ba {
		lo = oa
	}
	hi := 1 << 30
	if i == bb {
		hi = ob
	}
	if lo >= hi && i != l.charBlock {
		return blk // leerer Block-Schnitt und nicht das Caret: nichts zu tun
	}
	blk = l.rangeHighlight(blk, cls, pad, left, lo, hi)
	// Richtungs-Marker: Die CARET-Kante ist zusaetzlich INVERTIERT abge-
	// hoben — oben, wenn die Auswahl nach oben waechst, unten bei Aus-
	// dehnung nach unten (caretBlock klemmt an Block-/Zeilengrenzen auf
	// die Wurzelzeile). Das Auswahl-Ende, an dem der Cursor steht, ist so
	// STETS farblich vom Rest der Flaeche unterscheidbar (und blinkt
	// bewusst nicht: Der Marker darf nicht verschwinden).
	if i == l.charBlock && l.charActive() {
		// NUR im AKTIVEN Modus: Nach dem Verlassen (residualSelShown —
		// Toggle-Austritt) verschwindet der Cursor, die Auswahl-Flaeche
		// bleibt (User-Report: "wenn ich den cursor mode verlasse,
		// existiert der cursor immer noch" — der invertierte Marker IST
		// der Cursor; draussen gehoert er weg).
		blk = caretBlock(blk, cls, pad, left, l.charTo)
	}
	return blk
}

// caretBlock invertiert den Buchstaben am flat Offset at — auf der
// Zeile, die ihn enthaelt (an Zeilengrenzen zur naechsten Zeilenwurzel).
func caretBlock(blk, cls []string, pad, left, at int) []string {
	joined := strings.Join(cls, "\n")
	line, _ := charLineCol(joined, at)
	base := 0
	for j, cl := range cls {
		rs := []rune(cl)
		if j == line && len(rs) > 0 {
			col := clamp(at-base, 0, len(rs)-1)
			if row := pad + j; row < len(blk) {
				blk[row] = styleRange(blk[row], left+col, left+col+1, "\x1b[7m")
			}
			return blk
		}
		base += len(rs) + 1
	}
	return blk
}

// charSelBgSgr ist der SGR-Opener der Auswahl: der Theme-Akzent (Neon-
// Violett, FR-39 — dieselbe Farbe wie Fokus-Rahmen und Hotkeys) als
// TrueColor-HINTERGRUND PLUS Hell-Weiss als VORDERGRUND — die Auswahl
// ueberschreibt die Textfarbe bewusst: Ueberschriften (Akzent-FG) waeren
// auf Akzent-BG nahezu unsichtbar (User-Feedback: "kein Kontrast").
// Ohne Theme oder bei kaputtem Hex greift ein 256-Farb-Violett — die
// Auswahl ist dann immer noch Farbe statt Strich.
func (l BlockList) charSelBgSgr() string {
	col := render.DefaultFocusColor
	if l.th != nil {
		col = l.th.FocusColor()
	}
	if r, g, b, ok := hexToRGB(col); ok {
		return "\x1b[48;2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b) + "m\x1b[97m"
	}
	return "\x1b[48;5;92m\x1b[97m"
}

// rangeHighlight faerbt den HINTERGRUND der Runen [lo,hi) des Blocktexts
// zeilenweise (Theme-Akzent) — echte Auswahl wie im Editor, kein Unterstrich.
func (l BlockList) rangeHighlight(blk, cls []string, pad, left, lo, hi int) []string {
	sgr := l.charSelBgSgr()
	base := 0
	for j, cl := range cls {
		rs := []rune(cl)
		a := max(lo-base, 0)
		b := min(hi-base, len(rs))
		if row := pad + j; a < b && row < len(blk) {
			blk[row] = styleRange(blk[row], left+a, left+b, sgr)
		}
		base += len(rs) + 1
	}
	return blk
}

// scrollCharIntoView zieht den Viewport nach, bis die Zeile des Carets
// sichtbar ist (blockweise wie scrollSelIntoView, aber zeilenscharf).
func (l BlockList) scrollCharIntoView() BlockList {
	if l.height <= 0 || l.inline {
		return l
	}
	w := l.Width()
	cw, _, ok := l.charView(l.charBlock, w)
	if !ok {
		return l
	}
	text, _ := l.charTextAt(l.charBlock, w)
	line, _ := charLineCol(text, l.charTo)
	row := cw.CharPad(w) + line
	y := 0
	for i, it := range l.items {
		h := it.Height(w)
		if i == l.charBlock {
			abs := y + row
			if abs < l.offset {
				l.offset = abs
			}
			if abs >= l.offset+l.height {
				l.offset = abs - l.height + 1
			}
			break
		}
		y += h
	}
	l.follow = false
	return l.clamp()
}

// ---- Flat-Text-Koordinaten --------------------------------------------------

// charLineCol liefert Zeile und Spalte (in Runen) eines flat Offsets im
// \n-verbundenen Text.
func charLineCol(text string, at int) (line, col int) {
	rs := []rune(text)
	at = clamp(at, 0, len(rs))
	for i := 0; i < at; i++ {
		if rs[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return line, col
}

// charLineLen ist die Runenlaenge einer Zeile (ohne \n).
func charLineLen(text string, line int) int {
	rs := []rune(text)
	cur := 0
	for i, r := range rs {
		if cur == line {
			end := len(rs)
			for j := i; j < len(rs); j++ {
				if rs[j] == '\n' {
					end = j
					break
				}
			}
			return end - i
		}
		if r == '\n' {
			cur++
		}
	}
	if cur == line {
		return len(rs)
	}
	return 0
}

// charAtLineCol liefert den flat Offset zu Zeile/Spalte (Spalte wird an der
// Zeilenlaenge geklemmt, so landet das Caret am Zeilenende statt im \n).
func charAtLineCol(text string, line, col int) int {
	rs := []rune(text)
	start := 0
	cur := 0
	for i := 0; i <= len(rs); i++ {
		if i == len(rs) || rs[i] == '\n' {
			if cur == line {
				return start + clamp(col, 0, i-start)
			}
			cur++
			start = i + 1
		}
	}
	return len(rs)
}

// styleRange hebt die Runen [from,to) einer STYLED Zeile mit dem SGR-Opener
// sgr hervor (z.B. "\x1b[4m" Unterstrich, "\x1b[7m" Invertiert, TrueColor-
// Hintergrund "\x1b[48;2;R;G;Bm") — ANSI-bewusst: Escapes zaehlen nicht als
// Zellen, die laufende Formatierung wird nach dem Bereich wiederhergestellt
// (wie undercursor, aber als Bereich statt einzelner Zelle). from==to (oder
// to<from) hebt genau EINEN Buchstaben hervor: das Caret.
func styleRange(ln string, from, to int, sgr string) string {
	if to <= from {
		to = from + 1
	}
	rs := []rune(ln)
	var b strings.Builder
	lastSGR := ""
	cell := 0
	open := false
	rearm := false // seit dem Bereichs-Opener kam ein eingebettetes SGR
	for i := 0; i < len(rs); {
		if rs[i] == 0x1b {
			j := skipEscape(rs, i, &lastSGR)
			b.WriteString(string(rs[i:j]))
			if open {
				// Eingebettete SGRs (zeichenweise gestylte Zeilen: Ueber-
				// schriften, Syntax-Highlighting) veraendern fg/bg — der
				// Bereichs-Stil muss vor der naechsten Zelle NEU gesetzt
				// werden, sonst bricht die Auswahl nach dem ersten
				// Buchstaben ab (User-Report: "nur die Anfangsbuchstaben").
				rearm = true
			}
			i = j
			continue
		}
		if open && cell >= to {
			b.WriteString("\x1b[0m")
			if lastSGR != "" {
				b.WriteString(lastSGR)
			}
			open = false
		} else if !open && cell >= from && cell < to {
			b.WriteString(sgr)
			open = true
		} else if open && rearm {
			b.WriteString(sgr)
			rearm = false
		}
		b.WriteRune(rs[i])
		cell++
		i++
	}
	if open {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// hexToRGB parst "#RRGGBB" (auch ohne #) in die Byte-Kanaele — Theme-
// Farben sind Hex-Strings, der Auswahl-Hintergrund braucht sie numerisch.
func hexToRGB(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 { // #ABC → #AABBCC
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff), true
}
