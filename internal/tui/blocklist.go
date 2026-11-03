package tui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// BlockList renders a stack of Widgets as one scrollable pager with keyboard
// focus cycling, mouse-wheel scrolling and hover/click on COPY actions. It is
// a value type in the Elm sense: Update returns the next state instead of
// mutating in place — the caller must adopt the returned BlockList.
type BlockList struct {
	// nextID is the last stable block number handed out (FR-3).
	nextID int
	// nextKittyID vergibt Terminal-Bild-IDs (1..250, FR-27): nur dieser
	// Bereich ist in \e[38;5;N m codierbar.
	nextKittyID int
	items       []Widget
	offset      int
	width       int
	height      int
	inline      bool // timg-Modell: kein Viewport-Slice, Terminal scrollt nativ
	focus       int
	hoverIdx    int
	// hoverX/hoverY merken die Zelle des letzten Hovers: Innerhalb EINES
	// Blocks kann der Hover zwischen mehreren Zielen wechseln (Kopfzeile
	// vs. Footer, [ TEXT ] vs. [ COPY ]) — der Fruehabbruch darf dann nicht
	// greifen, sonst leuchten mehrere Zellen gleichzeitig.
	hoverX, hoverY int
	follow         bool
	// marks selects blocks for the range copy (FR-29); copyFn executes it.
	marks  map[int]bool
	copyFn CopyFunc
	// Shift-Auswahl (FR-42/43) — GROBES Werkzeug: Blockbereich von Anker
	// (selFrom) bis Cursor (selCur), schliesst ALLE Bloecke dazwischen ein,
	// auch nicht fokussierbare (Codeblock gefolgt von Fliesstext). shift+
	// up/down bewegt den Underscore-Cursor blockweise; c kopiert GANZE
	// Bloecke.
	//
	// AUSWAHL-VERTRAG (Entscheidung, s. selection_contract_test.go): heron
	// hat ZWEI Werkzeuge mit getrennten Rollen, aber zu jeder Zeit GENAU
	// EIN sichtbares Auswahl-Signal.
	//   Block-Auswahl (grob):     shift+up/down im Antwort-Modus.
	//   Zeichen-Cursor (fein):    shift+enter, Caret im Text; shift+Pfeile
	//                             waehlen Buchstaben/Zeilen, c kopiert exakt.
	// EINTRITT  (shift+enter ueber einer Block-Auswahl) KONVERTIERT sie in
	//           die feine Auswahl ueber demselben Bereich (Anker bleibt)
	//           und KONSUMIERT die Block-Auswahl (charFromSel merkt die
	//           Herkunft — im Modus existiert NIE sel UND charSel zugleich).
	// AUSTEITT  (shift+enter-Toggle): die feine Auswahl bleibt als EINZIGE
	//           sichtbare + kopierbare Auswahl (residual); der CURSOR ist
	//           weg. esc ist ABBRUCH: aus der Konvertierung kehrt EIN esc
	//           zur wartenden Block-Auswahl zurueck, sonst kollabiert Stufe
	//           1 und Stufe 2 verlaesst — einziger Auswahl-Verlustpunkt.
	sel     bool
	selFrom int
	selCur  int
	// selDir merkt die Ausloeserrichtung der Shift-Auswahl: +1 (shift+down)
	// parkt den Selection-Cursor am ANFANG der Einheit, -1 (shift+up) am
	// ENDE — der Underscore-Cursor unterstreicht die jeweilige Kante.
	selDir int
	// prevSel (FR-43): die VERGANGENE aktive Auswahl. Eine Auswahl geht nur
	// per esc verloren — startet der Nutzer eine NEUE Auswahl (oder bewegt
	// der Fokus sich aus ihr heraus), wird die alte NICHT geloescht, sondern
	// hierher delegiert: Ihr Blockbereich bleibt sichtbar, die Rahmen-
	// zeichen in der verblichenen Sekundaer-Farbe — klar unterscheidbar
	// von der AKTUELLEN Auswahl (Neon). Erst esc loest auch sie auf.
	prevSel bool
	prevLo  int
	prevHi  int
	// Zeichen-Cursor (FR-42): shift+enter IST das Editor-Caret ueber dem
	// Chat-Text (nur lesend) — Anker (charFromBlock/charFrom) und Caret
	// (charBlock/charTo) sind Runen-Offsets in den dargestellten Texten
	// der Zeichen-faehigen Bloecke. Pfeile bewegen das Caret (alle Rich-
	// tungen, ueber Sektionsgrenzen hinweg), shift+Pfeile waehlen vom
	// Anker her, esc kollabiert/verlaesst (Details: charsel.go).
	charSel       bool
	charFrom      int // Anker-Offset
	charTo        int // Caret-Offset
	charBlock     int // Caret-Block (auch Wiedereinstiegs-Merkmal)
	charFromBlock int // Anker-Block
	// charFromSel merkt die Herkunft des Zeichen-Cursors: wurde er durch
	// KONVERTIERUNG ueber einer laufenden Block-Auswahl geoeffnet, hat der
	// Eintritt sie konsumiert (l.sel=false — EIN sichtbares Signal) und den
	// Bereich in selFrom/selCur fuer den Rueckweg stehen lassen. esc (Stufe
	// 1) kehrt damit in einem Schritt zur wartenden Block-Auswahl zurueck,
	// statt zu kollabieren (kein Flip-Flop, User-Report).
	charFromSel bool
	// charBlinkHide ist die Blink-PHASE des Carets: true = gerade abge-
	// blendet. Jede Bewegung blendet es ein (Editor-Gefuehl), tea.Tick
	// (app.go, charBlinkMsg) toggelt im ~530ms-Takt — nur die leere
	// Auswahl blinkt, eine MARKIERUNG steht stabil (ihr Rand ist das
	// Caret).
	charBlinkHide bool
	// th liefert die Akzent-Farbe der Zeichen-Auswahl (Hintergrund-
	// Highlight); nil-Themes laufen auf den Neon-Fallback.
	th *render.Theme
}

func NewBlockList() BlockList {
	// charBlock/charFromBlock starten bei -1: 0 waere ein gueltiger Block-
	// Index und wuerde in enterCursorMode den "Wiedereintritt"-Zweig
	// spuriously triggern (User-Report: shift+tab leerte die Auswahl, weil
	// der Nullwert zufaellig auf den Auswahl-Cursor-Block fiel).
	return BlockList{focus: -1, hoverIdx: -1, charBlock: -1, charFromBlock: -1, width: 100, follow: true, marks: map[int]bool{}}
}

// SetCopy wires the clipboard command used by the selection copy (FR-29).
func (l BlockList) SetCopy(fn CopyFunc) BlockList { l.copyFn = fn; return l }

// SetTheme legt das Theme ab, aus dem die Zeichen-Auswahl ihre Akzent-Farbe
// (Hintergrund-Highlight) bezieht — Neon-Violett-Fallback ohne Theme (Tests).
func (l BlockList) SetTheme(th *render.Theme) BlockList { l.th = th; return l }

// SetInline schaltet das timg-Modell (HERON_INLINE): alle Zeilen rendern,
// das Scrollen uebernimmt der Terminal-Scrollback. Die Fensterhoehe bleibt
// gesetzt, damit der Zeilen-Cap der Bilder weiter greift (ein Diagramm soll
// nicht eine ganze Bildschirmseite fuellen).
func (l BlockList) SetInline(on bool) BlockList { l.inline = on; return l }

func (l BlockList) Empty() bool   { return len(l.items) == 0 }
func (l BlockList) Focused() bool { return l.focus >= 0 && l.focus < len(l.items) }

// FocusedImageToggle berichtet, ob der fokussierte Block ein Diagramm-Block
// mit Quelle ist — nur dann bietet die zentrale Hint-Leiste das
// t (Bild<->Quelltext) an.
func (l BlockList) FocusedImageToggle() bool {
	if !l.Focused() {
		return false
	}
	im, ok := l.items[l.focus].(ImageBlock)
	return ok && im.src != ""
}
func (l BlockList) Width() int {
	if l.width <= 0 {
		return 100
	}
	return l.width
}

func (l BlockList) Append(items ...Widget) BlockList {
	for _, it := range items {
		// Copyable blocks receive a stable 1-based number (FR-3), assigned
		// once and never changed, so keyboard copy (alt+<n>) and in-place
		// updates (PlantUML result) can address the block.
		if it.Focusable() && it.ID() < 1 {
			l.nextID++
			it = it.SetID(l.nextID)
		}
		l.items = append(l.items, it)
	}
	if l.follow {
		l = l.ScrollEnd()
	}
	return l
}

// UpdateBlock replaces the widget with the given stable id and keeps its
// focus — the in-place update path (FR-3), e.g. a PlantUML conversion result.
func (l BlockList) UpdateBlock(id int, w Widget) (BlockList, bool) {
	if id < 1 {
		return l, false
	}
	for i, it := range l.items {
		if it.ID() != id {
			continue
		}
		w = w.SetID(id)
		if it.Focused() {
			w = w.SetFocused(true)
		}
		// Markierung mitnehmen: Der ThinkBlock wird waehrend des Streams
		// in Place aktualisiert — ohne das verschwand der MARKED-Chip, die
		// Auswahl klebte unsichtbar in der marks-Map.
		if it.Marked() {
			w = w.SetMarked(true)
		}
		l.items[i] = w
		return l, true
	}
	return l, false
}

// removeBlock drops the widget with the given id. It retires the transient
// streaming block once the final, parsed answer arrives (FR-2).
func (l BlockList) removeBlock(id int) BlockList {
	for i, it := range l.items {
		if it.ID() != id {
			continue
		}
		l.items = append(l.items[:i], l.items[i+1:]...)
		return l
	}
	return l
}

// copyByID copies the block carrying the stable number n — the keyboard
// fallback of the copy box (FR-5). Reports whether a copy was triggered.
func (l BlockList) copyByID(n int) (BlockList, tea.Cmd, bool) {
	if n < 1 {
		return l, nil, false
	}
	for i, it := range l.items {
		if it.ID() != n {
			continue
		}
		// Ueberschrift: alt+<n> kopiert den GANZEN Abschnitt bis zur
		// naechsten Ueberschrift — konsistent mit c auf dem Fokus.
		if headingAt(it) {
			if l.copyFn == nil {
				return l, nil, false
			}
			return l, l.copyFn(l.sectionMarkdown(i)), true
		}
		nw, cmd := it.Copy()
		if cmd == nil {
			return l, nil, false
		}
		l.items[i] = nw
		return l, cmd, true
	}
	return l, nil, false
}

// headingAt meldet, ob ein Block eine Ueberschrift ist — das Signal fuer
// die Abschnitt-Kopie.
func headingAt(it Widget) bool {
	hb, ok := it.(TextBlock)
	return ok && hb.Heading()
}

// sectionMarkdown sammelt den Abschnitt einer Ueberschrift: die Ueberschrift
// selbst plus alle folgenden Bloecke bis (exklusiv) zur NAECHSTEN
// Ueberschrift — "c" auf der Ueberschrift kopiert den ganzen Abschnitt.
func (l BlockList) sectionMarkdown(idx int) string {
	end := len(l.items)
	for i := idx + 1; i < len(l.items); i++ {
		if headingAt(l.items[i]) {
			end = i
			break
		}
	}
	parts := make([]string, 0, end-idx)
	for i := idx; i < end; i++ {
		parts = append(parts, l.items[i].Markdown())
	}
	return strings.Join(parts, "\n\n")
}

// heightCap ist optional fuer Bloecke, die ihre Zeilenzahl nach der
// Fensterhoehe richten (ImageBlock, FR-27): ohne Cap fuellt ein hohes
// Portrait-Diagramm den ganzen Schirm und fuer Text bleibt kein Platz.
type heightCap interface {
	SetMaxRows(int) Widget
}

// panner meldet Pan-&-Zoom-Modus: solange Panning() gilt, steuern die
// Pfeiltasten das Bild des fokussierten Blocks statt der Fokus-Kette
// (BlockList.handleKey), und esc verlaesst den Modus vor allem anderen.
type panner interface {
	Panning() bool
}

// FocusedError meldet, ob der fokussierte Block ein Fehler-Block ist —
// die Hint-Leiste zeigt dann die Retry-Aktion (FR-33).
func (l BlockList) FocusedError() bool {
	if !l.Focused() {
		return false
	}
	_, ok := l.items[l.focus].(ErrorBlock)
	return ok
}

// FocusedPanning meldet, ob der fokussierte Block im Pan-&-Zoom-Modus ist
// — die zentrale Hint-Leiste (app.go) zeigt dann Zoom/Bewegungs-Tasten.
func (l BlockList) FocusedPanning() bool {
	if !l.Focused() {
		return false
	}
	p, ok := l.items[l.focus].(panner)
	return ok && p.Panning()
}

// imgRowCap: Zeilen, die ein Bild maximal belegen darf (Anteil der
// Fensterhoehe, s. imgRowPctVal). 0 ohne Fensterhoehe.
func (l BlockList) imgRowCap() int {
	if l.height <= 0 {
		return 0
	}
	c := l.height * imgRowPctVal() / 100
	if c < 3 {
		c = 3
	}
	return c
}

// SetSize stores the viewport size and lets every widget rebuild its layout
// cache for the new width (dynamic re-flow on terminal resize).
func (l BlockList) SetSize(w, h int) BlockList {
	l.width, l.height = w, h
	cap := l.imgRowCap()
	for i, it := range l.items {
		it = it.Resize(w)
		if hc, ok := it.(heightCap); ok && cap > 0 {
			it = hc.SetMaxRows(cap)
		}
		l.items[i] = it
	}
	return l.clamp()
}

func (l BlockList) totalLines() int {
	total := 0
	for _, it := range l.items {
		total += it.Height(l.Width())
	}
	return total
}

func (l BlockList) ScrollEnd() BlockList {
	l.offset = l.totalLines()
	return l.clamp()
}

// topAnchor liefert den Block und die Zeile IN ihm, die den oberen
// Viewportrand markiert. Der Anker ist block-relativ und damit stabil
// gegen Hoehenaenderungen: applyDiagrams ersetzt Diagramm-Zaeune IN PLACE
// durch Bild/ASCII mit ANDERER Hoehe — ein absoluter Zeilenoffset wuerde
// danach auf fremden Inhalt zeigen.
func (l BlockList) topAnchor() (idx, rel int) {
	w := l.Width()
	y := l.offset
	for i, it := range l.items {
		h := it.Height(w)
		if y < h {
			return i, y
		}
		y -= h
	}
	return len(l.items), 0
}

// restoreTopAnchor setzt den Viewport auf den Anker (Block idx, Zeile rel)
// an der oberen Kante zurueck. rel klemmt an die neue Blockhoehe — ein
// geschrumpfter Block zieht den Anker an sein Ende, der Inhalt darunter
// rueckt nicht hoch.
func (l BlockList) restoreTopAnchor(idx, rel int) BlockList {
	w := l.Width()
	y := 0
	for i := 0; i < idx && i < len(l.items); i++ {
		y += l.items[i].Height(w)
	}
	if idx < len(l.items) {
		if h := l.items[idx].Height(w); rel > h-1 {
			rel = max(h-1, 0)
		}
		y += rel
	}
	l.offset = y
	return l.clamp()
}

func (l BlockList) clamp() BlockList {
	total := l.totalLines()
	if l.height > 0 && l.offset > total-l.height {
		l.offset = total - l.height
	}
	if l.offset < 0 {
		l.offset = 0
	}
	return l
}

// rangedWidget kann nur einen Teil seiner Zeilen rendern (ImageBlock:
// Kitty-Transmit auf der ersten sichtbaren Zeile, FR-27). Ohne das Interface
// gilt weiterhin die volle View.
type rangedWidget interface {
	Widget
	ViewRange(width, from, to int) string
}

// all stacks every widget into a full-height line list (with blank padding).
// Bildbloecke liefern nur ihre sichtbaren Zeilen: Der Transmit-Escape landet
// auf der ersten sichtbaren Zeile, damit hohe Bilder das Terminal auch dann
// erreichen, wenn ihr Blockanfang ausserhalb des Fensters liegt (FR-27).
func (l BlockList) all() (lines []string, total int) {
	w := l.Width()
	winL, winR := 0, 0 // sichtbares Fenster im Zeilenraum, nur wenn Hoehe bekannt
	if l.height > 0 && !l.inline {
		winL = l.offset
		if winL < 0 {
			winL = 0
		}
		winR = winL + l.height
	}
	start := 0
	selLo, selHi := -1, -1
	charBa, charBb := -1, -1 // Zeichen-Auswahl: Blockgrenzen (FR-42)
	cursorLine := -1          // absolute Zeilennummer der ersten Cursor-Zeile
	if l.selActive() {
		selLo, selHi = l.selBounds()
	}
	if l.charSelVisible() {
		charBa, _, charBb, _ = l.charRange()
	}
	for i, it := range l.items {
		h := it.Height(w)
		// Vergangene Auswahl (FR-43): Rahmen in der verblichenen Sekundaer-
		// Farbe, bis esc sie aufloest — aber nur, wo sie nicht von der
		// AKTUELLEN Auswahl uebermalt wird (Neon schlaegt Verblichen).
		if l.prevSel && i >= l.prevLo && i <= l.prevHi && !it.Marked() {
			if !(selLo >= 0 && i >= selLo && i <= selHi && !l.charSelVisible()) &&
				!(l.charSelVisible() && i >= charBa && i <= charBb) {
				it = it.SetStale(true)
			}
		}
		if selLo >= 0 && i >= selLo && i <= selHi && !it.Marked() && !l.charSelVisible() {
			// Shift-Auswahl: Der ganze Bereich leuchtet (FR-29-Markierung
			// als Render-Highlight, ohne den Markierungs-Zustand anzufassen).
			// Im Zeichen-Cursor-Modus zeigt stattdessen die UNTERSTREICHUNG
			// des markierten Bereichs (charDecorate) — ein zusaetzliches
			// Block-Highlight wuerde sie ueberdecken.
			it = it.SetMarked(true)
		}
		if l.charSelVisible() && i > charBa && i < charBb && !l.charCapableAt(i) {
			// Zeichen-Auswahl ueber NICHT adressierbare Bloecke (User-
			// Schiene, Tabelle, Bild OHNE Quelle): Der Cursor kann sie
			// nicht betreten (keine Zellen), aber die Auswahl nimmt sie
			// GANZ auf — live, waehrend shift+Pfeile sie ueberqueren
			// (User-Wunsch). Diagramm-BILDER MIT Quelle schaltet der
			// Cursor beim Ueberqueren AUTOMATISCH auf den Zaun-Quelltext
			// um (autoTextModeOnCross, User-Report "mermaid nicht
			// auswaehlbar") — sie sind dann adressierbar und laufen
			// HIER nicht mehr auf. Das Bild hat keinen Text-Hintergrund —
			// der NEON-RAHMEN IST die Auswahl-Signatur.
			it = it.SetMarked(true)
		}
		if selLo >= 0 && i == l.selCur && !l.charSel {
			// Cursor-Kante je Ausloeserrichtung: shift+down parkt den
			// Cursor am ANFANG (erste Zeile), shift+up am ENDE (letzte
			// Zeile) der Einheit.
			cursorLine = start
			if l.selDir < 0 {
				cursorLine = start + h - 1
			}
		}
		blk := make([]string, h)
		if rw, ok := it.(rangedWidget); ok && h > 0 && winR > winL {
			from := winL - start
			to := winR - start
			if from < 0 {
				from = 0
			}
			if to > h {
				to = h
			}
			if from < to {
				vs := strings.Split(rw.ViewRange(w, from, to), "\n")
				for i, s := range vs {
					if from+i < h {
						blk[from+i] = s
					}
				}
			}
		} else {
			view := strings.Split(it.View(w), "\n")
			copy(blk, view)
		}
		// Zeichen-Cursor: Caret-Buchstabe bzw. Auswahlbereich unterstreichen
		// (ANSI-bewusst in die gerenderten Zeilen gezeichnet, auch ueber
		// mehrere Bloecke hinweg).
		if l.charSelVisible() {
			blk = l.charDecorate(i, blk, w)
		}
		lines = append(lines, blk...)
		total += h
		start += h
	}
	// Underscore-Cursor: nicht-leere Zeile der Cursor-KANTE unterstreichen
	// (die Blockrandzeilen koennen Gap-Leerzeilen sein). Bei shift+down
	// (Richtung +1) die ERSTE nicht-leere Zeile des Cursor-Blocks (Anfang
	// der Einheit), bei shift+up (Richtung -1) die LETZTE (Ende der Einheit)
	// — der Selection-Cursor zeigt die Kante, von der die Auswahl waechst.
	if cursorLine >= 0 && cursorLine < len(lines) {
		blkH := max(l.items[l.selCur].Height(w), 1)
		lo := max(cursorLine-blkH+1, 0)
		hi := min(cursorLine+blkH-1, len(lines)-1)
		target := -1
		if l.selDir < 0 {
			for i := hi; i >= lo; i-- {
				if strings.TrimSpace(stripSeq.ReplaceAllString(lines[i], "")) != "" {
					target = i
					break
				}
			}
		} else {
			for i := lo; i <= hi; i++ {
				if strings.TrimSpace(stripSeq.ReplaceAllString(lines[i], "")) != "" {
					target = i
					break
				}
			}
		}
		if target >= 0 {
			lines[target] = undercursor(lines[target])
		}
	}
	return lines, total
}

func (l BlockList) View() string {
	lines, total := l.all()
	if total == 0 {
		return ""
	}
	if l.height <= 0 || l.inline {
		// timg-Modell: alles rendern, der Terminal-Scrollback scrollt.
		return strings.Join(lines, "\n")
	}
	left := l.offset
	if left < 0 {
		left = 0
	}
	right := left + l.height
	if right > total {
		right = total
	}
	if left > right {
		left = right
	}
	return strings.Join(lines[left:right], "\n")
}

// Update routes a message to the pager and returns the next state plus an
// optional command. The bool reports whether the message was consumed.
func (l BlockList) Update(msg tea.Msg) (BlockList, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		return l.handleMouse(msg)
	case tea.KeyMsg:
		return l.handleKey(msg)
	}
	return l, nil, false
}

func (l BlockList) handleKey(k tea.KeyMsg) (BlockList, tea.Cmd, bool) {
	// Zeichen-Cursor (FR-42) zuerst: Wie im TextEditor bewegen die Pfeile
	// das Caret (left/right Buchstabe, up/down Zeile — ueber Sektions-
	// grenzen hinweg), shift+Pfeile waehlen vom Anker her. esc kollabiert
	// zuerst die Auswahl, verlaesst dann den Cursor (Block-Auswahl bleibt).
	if l.charActive() {
		switch k.String() {
		case "shift+enter", "shift+tab":
			// Toggle raus: Eine nicht-leere Zeichen-Auswahl KONVERTIERT zur
			// Block-Auswahl zurueck (c kopiert ganze Bloecke, User-Report);
			// eine leere haelt die Rest-Position (Wiedereintritt).
			return l.exitCursorToSel(), nil, true
		case "t":
			// t ist ein BILD-Modus-Schalter — im Zeichen-Cursor ist er
			// STUMM (konsumiert): Ein Rueckschalten wuerde dem Caret den
			// Block unter den Fuessen wegziehen (charView schlaegt fehl,
			// Cursor UND Auswahl verschwanden — User-Report "t entfert
			// selection"). Erst per esc/shift+enter raus, dann t.
			return l, nil, true
		case "esc":
			// esc in STUFEN (FR-42: "kollabiert erst die Auswahl und
			// verlaesst dann"): Laeuft eine EIGENE Auswahl, zieht esc sie
			// aufs Caret zusammen (charCollapse, esc-Stufe 1) — der Cursor
			// BLEIBT. Erst das zweite esc ohne Auswahl verlaesst den Modus.
			// Einheitlicher Auswahl-Vertrag: Die Herkunft ist entweder ein
			// frisches Caret ODER eine per shift+enter KONVERTIERTE Block-
			// Auswahl (im Modus existiert nie sel UND charSel zugleich —
			// der Eintritt hat die Block-Auswahl konsumiert, charFromSel
			// merkt die Herkunft). Aus der Konvertierung verlaesst esc den
			// Cursor SOFORT zurueck in die wartende Block-Auswahl: Erst
			// der Kollaps-Zwischenschritt machte die Auswahl "weg" und das
			// naechste esc brachte sie zurueck — ein Flip-Flop, das wie
			// ein Bug wirkte (User-Report: "esc bricht Cursor und
			// Selection ab"). esc raeumt also in STUFEN NACH UNTEN:
			// Cursor-Modus raus, Auswahl bleibt; das weitere esc loest sie.
			if l.charFromSel {
				// Herkunft Konvertierung: EIN esc zurueck in die Block-
				// Auswahl — Bereich (selFrom/selCur) und Richtung (selDir)
				// stehen unveraendert aus dem Eintritt.
				l.charSel = false
				l.charFromSel = false
				l = l.resetCharResidual() // esc = einziger Anker-Reset
				l.sel = true
				l.follow = false
				l = l.clearFocus()
				return l.scrollSelIntoView(), nil, true
			}
			if !l.charEmptySelection() {
				return l.charCollapse(), nil, true
			}
			return l.clearCharSelEsc(), nil, true
		case "left":
			return l.charMove(-1, false), nil, true
		case "shift+left":
			return l.charMove(-1, true), nil, true
		case "right":
			return l.charMove(1, false), nil, true
		case "shift+right":
			return l.charMove(1, true), nil, true
		case "up":
			return l.charMoveLine(-1, false), nil, true
		case "shift+up":
			return l.charMoveLine(-1, true), nil, true
		case "down":
			return l.charMoveLine(1, false), nil, true
		case "shift+down":
			return l.charMoveLine(1, true), nil, true
		case "home":
			return l.charHome(), nil, true
		case "end":
			return l.charEnd(), nil, true
		}
	}
	switch k.String() {
	case "shift+enter", "shift+tab":
		// shift+enter IST der Cursor (FR-42): Editor-Caret an der Lese-
		// position — laeuft eine Block-Auswahl, parkt es an deren Cursor-
		// Block (jetzt Buchstaben statt Sektionen); sonst am fokussierten
		// bzw. obersten sichtbaren Zeichen-Block.
		return l.enterCursorMode(), nil, true
	case "shift+down":
		return l.extendSel(1), nil, true
	case "shift+up":
		return l.extendSel(-1), nil, true
	case "up", "down", "left", "right":
		// Pan & Zoom: solange der fokussierte Block im Pan-Modus ist,
		// steuern die Pfeiltasten das BILD — die Fokus-Kette pausiert.
		// Ausserhalb dessen sind left/right KEINE Block-Tasten (fallen
		// durch), up/down laufen weiter blockweise.
		if l.Focused() {
			if p, ok := l.items[l.focus].(panner); ok && p.Panning() {
				w, cmd := l.items[l.focus].Update(k)
				l.items[l.focus] = w
				return l, cmd, true
			}
		}
		if k.String() == "left" || k.String() == "right" {
			return l, nil, false
		}
		if k.String() == "up" {
			// Fokus-Kette statt Zeilen-Scroll: arrow up laeuft blockweise nach
			// oben (Bild ↑/↓ scrollt seitenweise, Mausrad zeilenweise).
			// FR-43: Normale Pfeile LOESCHEN die Auswahl nicht mehr — sie
			// ueberlebt die Cursor-Bewegung (nur esc loest auf). Verlaesst
			// der Fokus den Auswahlbereich, parkt setFocus sie als VER-
			// GANGENE Auswahl (Rahmen in Sekundaer-Farbe) statt sie zu
			// entfernen.
			nl, _ := l.focusStep(-1)
			return nl, nil, true
		}
		// arrow down: naechster Block; am letzten Block STOPPT die Kette —
		// zurueck ins Eingabefeld fuehrt ausschliesslich tab.
		nl, _ := l.focusStep(1)
		return nl, nil, true
	case "pgup":
		before := l.offset
		l.offset -= max(l.height, 1)
		l.follow = false
		l = l.clamp()
		perfScroll("key-pgup", before, l.offset)
		return l, nil, true
	case "pgdown":
		before := l.offset
		l.offset += max(l.height, 1)
		l = l.clamp()
		if l.height > 0 && l.offset >= l.totalLines()-l.height {
			l.follow = true
		}
		perfScroll("key-pgdown", before, l.offset)
		return l, nil, true
	case "enter", "c", "+", "=", "-", "_", "t", "r":
		// enter/c kopieren; +,- klappen den Think-Block auf/zu; r retryt den
		// fokussierten Fehler-Block (FR-33); t toggelt EIN-TASTIG
		// Bild/Quelltext des fokussierten Image-Blocks (FR-38,
		// i ist entfallen). Nur wenn ein Block im Fokus ist.
		if l.Focused() {
			w, cmd := l.items[l.focus].Update(k)
			l.items[l.focus] = w
			return l, cmd, true
		}
		// t AUCH waehrend der Shift-Auswahl: Der Block-Fokus weicht der
		// Auswahl (FR-39), also greift t am SELECTION-CURSOR-Block — aber
		// nur auf einem Image-Block (sonst faellt t wie bisher durch und
		// tippt ins Eingabefeld). So wird der PlantUML-Quelltext OHNE
		// Abbruch der Auswahl erreichbar: t -> Quelltext-Ansicht, dann
		// spawnt shift+enter den Zeichen-Cursor mitten im Zaun (FR-42).
		// (Im Zeichen-Cursor-Modus ist das unreachable — t ist dort No-Op,
		// s.o.)
		if k.String() == "t" && l.selActive() && !l.charActive() {
			if _, ok := l.items[l.selCur].(ImageBlock); ok {
				w, cmd := l.items[l.selCur].Update(k)
				l.items[l.selCur] = w
				return l, cmd, true
			}
		}
		return l, nil, false
	case " ":
		// Space marks the focused block for the selection copy (FR-29).
		if l.Focused() {
			return l.toggleMarkFocused(), nil, true
		}
		return l, nil, false
	case "y":
		// Yank: copy the marked selection as plain markdown (FR-29).
		if n := l.markedCount(); n > 0 && l.copyFn != nil {
			md := l.markedMarkdown()
			l = l.clearMarks()
			return l, l.copyFn(md), true
		}
		return l, nil, false
	case "esc":
		// FR-43: esc ist die EINZIGE Stelle, an der eine Auswahl verloren
		// geht — die ERSTE esc-Stufe loest daher ALLE Auswahl-Generationen
		// auf: die laufende Shift-Auswahl, die MARKierung (FR-29) und die
		// VERGANGENE Auswahl (Sekundaer-Rahmen). Danach folgen wie bisher
		// Pan-Modus und Fokus.
		if l.selActive() || l.markedCount() > 0 || l.prevSel || l.residualSelShown() {
			// Leseposition parken (User-Report: "selection esc bringt
			// mich im Eingabemodus — das ist falsch"): Der Fokus faellt
			// auf den Auswahl-Cursor-Block und die Kette BLEIBT im
			// Antwort-Modus ("danach ist die normale Navigation wieder
			// frei", FR-43). Erst das WEITERE esc raeumt den Fokus und
			// kehrt auf den Standard (Eingabefeld) zurueck. Der Fokus
			// muss NACH dem Aufloesen gesetzt werden — setFocus wuerde
			// eine noch laufende Auswahl sonst zur Vergangenen demoten.
			cur := l.selCur
			hadSel := l.selActive()
			hadRes := l.residualSelShown()
			if hadRes && !hadSel {
				// Nur die erhaltene Auswahl (kein Block-Cursor): Die
				// Leseposition ist der CARET-Rest — esc parkt den Fokus
				// dort (sonst fiel die Kette sofort ins Eingabefeld,
				// User-Flow: Anker setzen, raus, shift+up, esc).
				cur = l.charBlock
			}
			l = l.clearSel()
			l = l.clearPrevSel()
			l = l.clearMarks()
			l = l.resetCharResidual() // esc = einziger Anker-Reset (Rest-Auswahl weg)
			if (hadSel || hadRes) && cur >= 0 && cur < len(l.items) {
				l = l.setFocus(cur)
			}
			return l, nil, true
		}
		// Pan & Zoom: esc verlaesst ZUERST den Pan-Modus (der Block-Fokus
		// bleibt) — erst ein zweites esc raeumt Auswahl/Fokus weg.
		if l.Focused() {
			if p, ok := l.items[l.focus].(panner); ok && p.Panning() {
				w, cmd := l.items[l.focus].Update(k)
				l.items[l.focus] = w
				return l, cmd, true
			}
		}
		// Esc first clears the selection, then the focus.
		if l.markedCount() > 0 {
			return l.clearMarks(), nil, true
		}
		if l.Focused() {
			l = l.clearFocus()
			return l.resetCharResidual(), nil, true
		}
		return l, nil, false
	}
	return l, nil, false
}

// blockAt returns the index of the widget whose rows contain (x, y) plus the
// y coordinate relative to the block top; y is list-relative.
func (l BlockList) blockAt(x, y int) (int, int, bool) {
	w := l.Width()
	y0 := 0
	for i, it := range l.items {
		h := it.Height(w)
		if y >= y0 && y < y0+h {
			return i, y - y0, true
		}
		y0 += h
	}
	return 0, 0, false
}

// Clear drops every chat block and resets paging/marks — /new starts fresh
// without losing width/copy wiring (FR-20).
func (l BlockList) Clear() BlockList {
	l.items = nil
	l.offset = 0
	l.focus = -1
	l.hoverIdx = -1
	l.follow = true
	l.marks = map[int]bool{}
	l.sel = false
	l.charSel = false
	l.charBlock, l.charFromBlock = -1, -1
	l.charFromSel = false
	l = l.clearPrevSel()
	return l
}

// toggleMarkFocused flips the selection mark of the focused block (FR-29).
func (l BlockList) toggleMarkFocused() BlockList {
	if !l.Focused() {
		return l
	}
	return l.toggleMarkIdx(l.focus)
}

// toggleMarkAt flips the selection mark of the block under (x, y) — the
// mouse path of FR-29. The bool reports whether a mark was toggled.
func (l BlockList) toggleMarkAt(x, y int) (BlockList, bool) {
	idx, _, ok := l.blockAt(x, y)
	if !ok {
		return l, false
	}
	if !l.items[idx].Focusable() || l.items[idx].ID() < 1 {
		return l, false
	}
	return l.toggleMarkIdx(idx), true
}

func (l BlockList) toggleMarkIdx(idx int) BlockList {
	if l.marks == nil {
		l.marks = map[int]bool{}
	}
	id := l.items[idx].ID()
	if l.marks[id] {
		// Abwaehlen: Die Auswahl darf leer sein.
		delete(l.marks, id)
		l.items[idx] = l.items[idx].SetMarked(false)
		return l
	}
	// Eine NEUE Markierung ist eine NEUE AKTUELLE Auswahl: Eine laufende
	// Zeichen- oder Shift-Auswahl weicht ihr nicht ins Nichts, sondern wird
	// zur VERGANGENEN Auswahl (FR-43: geloescht wird ausschliesslich per
	// esc — der Rahmen bleibt in Sekundaer-Farbe stehen). Dasselbe gilt
	// fuer die bisherige Markierung (Radio statt Checkbox: es gibt zu
	// jedem Zeitpunkt HOECHSTENS EINEN markierten Block; additive
	// Mehrfachauswahl bleibt FR-42/43 vorbehalten).
	if l.selActive() {
		l = l.demoteSelRange()
	}
	if l.charActive() {
		l = l.demoteCharSel()
	}
	l = l.demoteMarks() // keine Marken: No-Op; Radio-Ersatz wird delegiert
	// FR-39-Invariante "hoechstens EIN Neon-Rahmen": Der Klick-Markierung
	// auf einem ANDEREN Block als dem fokussierten (Maus-Pfad) weicht der
	// Fokus-Rahmen — die Markierung IST die Auswahl. Am selben Block
	// bleiben Fokus und Markierung zusammen (space am fokussierten Block).
	if l.Focused() && l.focus != idx {
		l = l.clearFocus()
	}
	l.marks[id] = true
	l.items[idx] = l.items[idx].SetMarked(true)
	return l
}

// markedCount counts the marked blocks that are still in the list.
func (l BlockList) markedCount() int {
	n := 0
	for _, it := range l.items {
		if l.marks[it.ID()] {
			n++
		}
	}
	return n
}

// markedMarkdown joins the raw markdown of the marked blocks in chat order —
// plain markdown, never the ANSI-rendered view (FR-29). Ist die Markierung
// eine Ueberschrift, wird ihr GANZER Abschnitt bis zur naechsten
// Ueberschrift ge-yankt (konsistent mit c und alt+<n>).
func (l BlockList) markedMarkdown() string {
	parts := make([]string, 0, len(l.marks))
	for i, it := range l.items {
		if l.marks[it.ID()] {
			if headingAt(it) {
				parts = append(parts, l.sectionMarkdown(i))
			} else {
				parts = append(parts, it.Markdown())
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// clearMarks unmarks every block (FR-29).
func (l BlockList) clearMarks() BlockList {
	l.marks = map[int]bool{}
	for i, it := range l.items {
		l.items[i] = it.SetMarked(false)
	}
	return l
}

// hoverWidget ist ein Block mit MEHREREN Hover-Zielen: SetHoverAt setzt
// den Hover zielgenau auf die getroffene Zelle. Der einfache
// SetHover-Pfad (ein Ziel) bleibt fuer CodeBlock/TableBlock.
type hoverWidget interface {
	SetHoverAt(width, x, y int) Widget
}

// setHoverAt updates the hover highlight for a mouse position.
func (l BlockList) setHoverAt(x, y int) BlockList {
	// Hover = Maus UEBER einem Block (irgendeine Zeile): das schaltet die
	// Progressive-Disclosure-Footer des Blocks sichtbar (Fokus/Hover) —
	// nicht mehr nur das Treffen einer Footer-Action. actionAt-Blocks
	// (ImageBlock/ThinkBlock) setzen den zielgenauen Kasten ueber
	// SetHoverAt; alle anderen melden blockweiten Hover.
	idx := -1
	relY := 0
	if i, ry, ok := l.blockAt(x, y); ok {
		idx = i
		relY = ry
	}
	// Unveraenderte Position (kein Block ODER dieselbe Zelle): nichts tun.
	if idx == l.hoverIdx && (idx < 0 || (x == l.hoverX && y == l.hoverY)) {
		return l
	}
	if l.hoverIdx >= 0 && l.hoverIdx < len(l.items) {
		l.items[l.hoverIdx] = l.items[l.hoverIdx].SetHover(false)
	}
	if idx >= 0 {
		if hw, is := l.items[idx].(hoverWidget); is {
			l.items[idx] = hw.SetHoverAt(l.Width(), x, relY)
		} else {
			l.items[idx] = l.items[idx].SetHover(true)
		}
	}
	l.hoverIdx, l.hoverX, l.hoverY = idx, x, y
	return l
}

func (l BlockList) handleMouse(msg tea.MouseMsg) (BlockList, tea.Cmd, bool) {
	if msg.Action == tea.MouseActionMotion {
		// The visible row is offset-relative; translate to absolute lines.
		return l.setHoverAt(msg.X, l.offset+msg.Y), nil, false
	}
	if msg.Action != tea.MouseActionPress {
		return l, nil, false
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		// Click on a footer action triggers that action: COPY copies (FR-5),
		// the fold action folds the think block (FR-30), the ImageBlock
		// switches between image and source via ActionAt (mehrere Kaesten).
		if idx, relY, ok := l.blockAt(msg.X, l.offset+msg.Y); ok {
			if aw, is := l.items[idx].(actionAtWidget); is {
				w, cmd, hit := aw.ActionAt(l.Width(), msg.X, relY)
				if hit {
					l.items[idx] = w
					return l, cmd, true
				}
			} else if l.items[idx].HitAction(l.Width(), msg.X, relY) {
				switch a := l.items[idx].(type) {
				case actionWidget:
					w, cmd := a.Action()
					l.items[idx] = w
					return l, cmd, true
				default:
					w, cmd := l.items[idx].Copy()
					l.items[idx] = w
					return l, cmd, true
				}
			}
		}
		// Click on a block body toggles its selection mark (FR-29).
		if nl, toggled := l.toggleMarkAt(msg.X, l.offset+msg.Y); toggled {
			return nl, nil, true
		}
	case tea.MouseButtonWheelUp:
		before := l.offset
		l.offset--
		l.follow = false
		l = l.clamp()
		perfScroll("wheel-up", before, l.offset)
		return l, nil, true
	case tea.MouseButtonWheelDown:
		before := l.offset
		l.offset++
		l = l.clamp()
		if l.height > 0 && l.offset >= l.totalLines()-l.height {
			l.follow = true
		}
		perfScroll("wheel-down", before, l.offset)
		return l, nil, true
	}
	return l, nil, false
}

// topVisibleIndex liefert den ersten Block, der in den Viewport ragt —
// der Anker fuer frisches tab: der Fokus startet DORT, wo der Nutzer gerade
// liest, nicht am Chat-Anfang (lange Chats: tab duerfte nie nach oben
// springen).
func (l BlockList) topVisibleIndex() int {
	w := l.Width()
	y := 0
	for i, it := range l.items {
		if y+it.Height(w) > l.offset {
			return i
		}
		y += it.Height(w)
	}
	return max(len(l.items)-1, 0)
}

// focusStep bewegt den Tastatur-Fokus blockweise: arrow down runter (+1),
// arrow up hoch (-1). OHNE Wrap — in langen Chats springt der Fokus nie zum
// Chat-Anfang. Ohne bestehenden Fokus startet die Bewegung am oberen
// Viewport-Rand. exhausted meldet, dass es in Richtung dir keinen weiteren
// Block gibt: nach unten gibt die Liste den Fokus dann FREI (App zieht die
// Kette ins Eingabefeld), nach oben bleibt der Fokus einfach stehen.
func (l BlockList) focusStep(dir int) (BlockList, bool) {
	n := len(l.items)
	if n == 0 {
		return l.clearFocus(), dir > 0
	}
	start := l.focus
	if start < 0 {
		start = l.topVisibleIndex()
		if l.items[start].Focusable() {
			return l.setFocus(start), false
		}
	}
	for i := start + dir; i >= 0 && i < n; i += dir {
		if l.items[i].Focusable() {
			return l.setFocus(i), false
		}
	}
	// Kettenende: BEIDE Richtungen stoppen — ↓ am letzten Block bleibt
	// stehen (kein Wrap, keine Fokus-Abgabe). Der Rueckweg ins Eingabe-
	// feld fuehrt AUSSCHLIESSLICH tab (bzw. esc als Reset), s. app.go.
	return l, true
}

// stripSeq entfernt ANSI-Sequenzen — hier nur fuer die Renderentscheidung
// "Zeile leer?" (Underscore-Cursor), bewusst lokal statt render.StripANSI:
// Der Nix-Bau sieht nur den GETRACKTEN Baum, render/ansi.go ist noch nicht
// committet und wuerde den Build brechen.
var stripSeq = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// ---- Shift-Auswahl (FR-42/43) ---------------------------------------------

// selActive meldet, ob gerade eine Shift-Auswahl laeuft.
func (l BlockList) selActive() bool { return l.sel && len(l.items) > 0 }

// selBounds liefert den Blockbereich [lo,hi] — Anker und Cursor in
// Lese-Reihenfolge, auf die Liste geklemmt.
func (l BlockList) selBounds() (lo, hi int) {
	if n := len(l.items) - 1; n >= 0 {
		lo, hi = l.selFrom, l.selCur
		if lo > hi {
			lo, hi = hi, lo
		}
		lo = clamp(lo, 0, n)
		hi = clamp(hi, 0, n)
	}
	return lo, hi
}

// selAnchorDir liefert die HUEPF-Richtung fuer einen nicht adressierbaren
// Anker-Block der Shift-Auswahl (enterCursorMode): vom Anker WEG vom
// Auswahl-Inneren — Anker unten (Wachstum nach oben) => weiter nach unten,
// Anker oben => weiter nach oben. So bleibt der unadressierbare Block
// GANZ in der Auswahl, statt an der Kante verloren zu gehen.
func (l BlockList) selAnchorDir() int {
	if l.selDir < 0 {
		return 1
	}
	return -1
}

// ---- Vergangene Auswahl (FR-43) --------------------------------------------
//
// "Eine Auswahl bleibt bis esc": Startet der Nutzer eine NEUE Auswahl, oder
// bewegt sich der Fokus aus einer laufenden heraus, wird die alte zum
// Blockbereich prevLo..prevHi DELEGIERT — nicht geloescht. Ihr Rahmen bleibt
// sichtbar, in der verblichenen Sekundaer-Farbe (Theme.StaleColor), deutlich
// verschieden vom Neon der aktuellen Auswahl (User-Wunsch: Die Rahmenzeichen
// der vergangenen Auswahl weichen farblich von der aktuellen ab). Erst esc
// loest auch sie auf; eine neuere Demotion ueberschreibt die aeltere (nie
// mehr als eine vergangene Auswahl auf dem Schirm).

// setPrevSel uebernimmt einen Blockbereich als vergangene Auswahl.
func (l BlockList) setPrevSel(lo, hi int) BlockList {
	if lo < 0 || hi < lo || lo >= len(l.items) {
		return l
	}
	l.prevSel = true
	l.prevLo = lo
	l.prevHi = min(hi, len(l.items)-1)
	return l
}

// clearPrevSel loest die vergangene Auswahl auf (esc-Stufe).
func (l BlockList) clearPrevSel() BlockList {
	l.prevSel = false
	return l
}

// markedRange liefert den Indexbereich der markierten Bloecke (FR-29).
func (l BlockList) markedRange() (lo, hi int) {
	lo, hi = -1, -1
	for i, it := range l.items {
		if l.marks[it.ID()] {
			if lo < 0 {
				lo = i
			}
			hi = i
		}
	}
	return lo, hi
}

// demoteSelRange uebernimmt die laufende Shift-Auswahl als vergangene und
// beendet sie (Zustand sel=false — der Bereich bleibt ueber prevLo/prevHi
// sichtbar). Eine LEERE Auswahl (nur Cursor) wird nicht delegiert.
func (l BlockList) demoteSelRange() BlockList {
	if !l.selActive() {
		return l
	}
	lo, hi := l.selBounds()
	l.sel = false
	return l.setPrevSel(lo, hi)
}

// demoteMarks uebernimmt die Markierung als vergangene Auswahl und hebt sie
// auf (FR-29-Radio wird zur FR-43-Demotion statt zum stillen Verschwinden).
func (l BlockList) demoteMarks() BlockList {
	if l.markedCount() == 0 {
		return l
	}
	lo, hi := l.markedRange()
	l = l.clearMarks()
	return l.setPrevSel(lo, hi)
}

// demoteCharSel uebernimmt die laufende Zeichen-Auswahl als vergangene und
// verlaesst den Zeichen-Cursor-Modus (etwa wenn eine Block-Markierung die
// aktuelle Auswahl uebernimmt).
func (l BlockList) demoteCharSel() BlockList {
	if !l.charActive() || l.charEmptySelection() {
		return l
	}
	ba, _, bb, _ := l.charRange()
	l = l.clearCharSel()
	return l.setPrevSel(ba, bb)
}

// beginSel startet die Shift-Auswahl an der LESEPOSITION: Anker und Cursor
// parken auf dem fokussierten Block — sonst dem obersten sichtbaren — und
// NICHT mehr auf Block 0 (der Chat-Anfang hat mit der Leseposition nichts
// zu tun, der Viewport springt nicht mehr nach oben). Der Cursor steht am
// ANFANG der Einheit (Leserichtung); die erste Richtungstaste entscheidet
// ueber die Wachstumsrichtung. Ein bestehender Block-Fokus weicht (FR-39-
// Geist: hoechstens EIN Neon-Signal).
func (l BlockList) beginSel() BlockList {
	if len(l.items) == 0 {
		return l
	}
	anchor := 0
	if l.Focused() {
		anchor = l.focus
	} else if i := l.topVisibleIndex(); i > 0 {
		anchor = i
	}
	l.sel = true
	l.selDir = 1
	l.selFrom, l.selCur = anchor, anchor
	l.follow = false
	l = l.clearFocus()
	return l.scrollSelIntoView()
}

// extendSel bewegt den Cursor blockweise durch ALLE Bloecke (nicht nur
// fokussierbare — so greift die Auswahl auch Codeblock-gefolgt-von-Text)
// und haelt ihn im Viewport. Der AUSLOESER startet die Auswahl AN der
// Anker-Einheit und wächst SOFORT um einen Block in Ausloeser-Richtung:
// EIN shift+up = Anker-Einheit PLUS der Block darueber ("der nächste
// Abschnitt") — die Einheit bleibt komplett in der Auswahl, und JEDE
// weitere Taste waechst um genau einen Block.
func (l BlockList) extendSel(dir int) BlockList {
	if len(l.items) == 0 {
		return l
	}
	// Eine gesetzte Anker-POSITION (Cursor gesetzt — AUCH OHNE laufende
	// Auswahl) ist der Start JEDES shift-Drucks von aussen (User-Report:
	// "ich habe den Cursor gesetzt, von da aus beginnt mein Select, egal
	// was ich vorher ausgewaehlt habe; er selektiert NICHT den ganzen
	// Schnippsel"): Die Auswahl waechst KLASSISCH als Zeichen-Auswahl ab
	// dem Anker (Blockrand fuer Blockrand in Druckrichtung) — kein frischer
	// Block-Anker von der Leseposition/Viewport, der "den ganzen Schnippsel"
	// greift. Erst NACH esc (Anker auf null) startet shift neu als Block-
	// Auswahl.
	if !l.sel && l.charFromBlock >= 0 && l.charBlock >= 0 {
		return l.extendResidualSel(dir)
	}
	// Startet/erweitert eine Block-Auswahl, weicht eine ERHALTENE Zeichen-
	// Auswahl (neben einer laufenden Block-Auswahl sichtbar) der Neuen als
	// VERGANGENE (FR-43-Muster).
	l = l.demoteResidualSel()
	if !l.sel {
		l.sel = true
		l.selDir = dir
		anchor := l.selAnchor(dir)
		l.selFrom, l.selCur = anchor, anchor
		// Eine laufende MARKIERUNG (FR-29) weicht der frischen Shift-Auswahl
		// nicht ins Nichts, sondern wird zur VERGANGENEN Auswahl (FR-43:
		// geloescht wird ausschliesslich per esc — der Rahmen bleibt in
		// Sekundaer-Farbe stehen).
		l = l.demoteMarks()
		// Der ERSTE Schritt greift SOFORT den naechsten Abschnitt in
		// Richtung dir — Anker-Einheit inklusive (User-Report: "bei einem
		// plantuml muss ich 2 statt 1 mal shift+up druecken"): EIN shift+up
		// = der Block ueber der Leseposition ist in der Auswahl, kein
		// "verschenkter" Erstdruck auf der Anker-Einheit allein.
		if dir < 0 && anchor > 0 {
			l.selCur = anchor - 1
		} else if dir > 0 && anchor < len(l.items)-1 {
			l.selCur = anchor + 1
		}
		// Der Block-Fokus weicht der Auswahl (FR-39: hoechstens EIN
		// Neon-Signal) — der Anker hat ihn gerade ersetzt.
		l = l.clearFocus()
		return l.scrollSelIntoView()
	}
	l.selDir = dir // Caret-Kante = Anfahrtsrichtung (Wiedereintritt)
	l.selCur = clamp(l.selCur+dir, 0, len(l.items)-1)
	return l.scrollSelIntoView()
}

// selAnchor ist der Startpunkt einer frischen Shift-Auswahl: die Leseposition,
// nicht der Viewport-Rand. Rangfolge: (1) der fokussierte Block — tab/Pfeile
// haben ihn bewusst gewaehlt, genau dort sitzt der Nutzer; (2) beim
// Aufwaertsschritt der UNTERSTE sichtbare Block (shift+up waechst von der
// Leseposition nach oben, nicht vom Viewport-Dach zwei Abschnitte tiefer);
// (3) beim Abwaertsschritt der oberste sichtbare Block.
func (l BlockList) selAnchor(dir int) int {
	if l.Focused() {
		return l.focus
	}
	if dir < 0 {
		return l.bottomVisibleIndex()
	}
	return l.topVisibleIndex()
}

// bottomVisibleIndex liefert den letzten Block, der in den Viewport ragt —
// die Leseposition am unteren Rand. Inline-Modus (timg): der Scrollback
// zeigt das Ende, der Anker ist der letzte Block.
func (l BlockList) bottomVisibleIndex() int {
	n := len(l.items) - 1
	if n < 0 {
		return 0
	}
	if l.height <= 0 || l.inline {
		return n
	}
	w := l.Width()
	y := 0
	for i, it := range l.items {
		if y >= l.offset+l.height {
			return max(i-1, 0)
		}
		y += it.Height(w)
	}
	return n
}

// scrollSelIntoView zieht den Viewport nach, bis die Cursor-Blockzeile
// sichtbar ist.
func (l BlockList) scrollSelIntoView() BlockList {
	if l.height <= 0 || l.inline {
		return l
	}
	w := l.Width()
	y := 0
	for i, it := range l.items {
		h := it.Height(w)
		if i == l.selCur {
			if y < l.offset {
				l.offset = y
			}
			if y+h > l.offset+l.height {
				l.offset = y + h - l.height
			}
			break
		}
		y += h
	}
	l.follow = false
	return l.clamp()
}

// clearSel hebt die Shift-Auswahl auf (esc) — inklusive Zeichen-Modus.
func (l BlockList) clearSel() BlockList {
	l.sel = false
	l.charSel = false
	return l
}

// selMarkdown sammelt die Roh-Markdowns des Auswahlbereichs (FR-29-Payload).
// Im Zeichen-Modus (FR-42) ist der Payload der markierte BUCHSTABEN-Bereich
// der Cursor-Sektion, nicht der Blockverband.
func (l BlockList) selMarkdown() string {
	if l.charActive() {
		return l.charMarkdown()
	}
	if l.residualSelShown() {
		// Erhaltene Auswahl (User-Report 22:18): auch von AUSSEN kopierbar —
		// die sichtbare Auswahl IST der c-Payload, nicht der Fokus-Block.
		return l.charMarkdown()
	}
	lo, hi := l.selBounds()
	parts := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		parts = append(parts, l.items[i].Markdown())
	}
	return strings.Join(parts, "\n\n")
}

// undercursor unterstreicht die erste SICHTBARE Zelle einer Zeile — der
// Underscore-Cursor der Shift-Auswahl (Zellen-Cursor im Terminal-Stil).
// ANSI-Escapes und Rahmen-/Leer-Zellen werden uebersprungen; die laufende
// Formatierung wird nach dem Cursor wiederhergestellt.
func undercursor(ln string) string {
	rs := []rune(ln)
	lastSGR := ""
	for i := 0; i < len(rs); {
		if rs[i] == 0x1b {
			j := skipEscape(rs, i, &lastSGR)
			i = j
			continue
		}
		switch rs[i] {
		case ' ', '\u2502', '\u250c', '\u2510', '\u2514', '\u256f', '\u2500', '\u258c', '\u255e', '\u2570', '\u256a', '\u2524', '\u251c':
			i++ // Gutter und Rahmen: Cursor steht IM Text
			continue
		}
		return string(rs[:i]) + "\x1b[4m" + string(rs[i]) + "\x1b[0m" + lastSGR + string(rs[i+1:])
	}
	return ln
}

// skipEscape konsumiert eine ANSI-Escape-Sequenz ab Index i (der auf 0x1b
// zeigt) und gibt den Index hinter der Sequenz zurueck. Erkannt werden CSI
// (ESC [ ... final), OSC (ESC ] ... ST|BEL) und 2-Zeichen-Escapes (ESC + final).
// lastSGR nimmt eine vollstaendige SGR-Sequenz (Ende 'm') fuer die Wieder-
// herstellung nach dem Cursor auf. Im Gegensatz zum frueheren Parser wird das
// '[' (0x5b) nicht mehr als finales Byte missverstanden — Truecolor-SGR
// (38;2;R;G;B) bleibt unversehrt statt als Gibberish durchzuschlagen.
func skipEscape(rs []rune, i int, lastSGR *string) int {
	n := len(rs)
	if i >= n {
		return i
	}
	if i+1 < n && rs[i+1] == '[' {
		// CSI: Parameter (0x30-0x3f) und Intermediate (0x20-0x2f), dann Finale (0x40-0x7e).
		j := i + 2
		for j < n && ((rs[j] >= 0x30 && rs[j] <= 0x3f) || (rs[j] >= 0x20 && rs[j] <= 0x2f)) {
			j++
		}
		if j < n {
			j++
		}
		if seq := string(rs[i:j]); strings.HasSuffix(seq, "m") {
			// KETTE statt Einzel-SGR: Der naechste SGR ERGAENZT den Zustand
			// (fg + bg), \x1b[0m leert ihn. styleRange stellt nach einem
			// Bereich so ALLE aktiven Attribute wieder her (frueher ging z.B.
			// die Textfarbe verloren, wenn der Caret-Marker in eine schon
			// gefaerbte Auswahl hineinzeichnete).
			if seq == "\x1b[0m" {
				*lastSGR = ""
			} else {
				*lastSGR += seq
			}
		}
		return j
	}
	if i+1 < n && rs[i+1] == ']' {
		// OSC: ESC ] ... abgeschlossen durch BEL (0x07) oder ST (ESC \\) .
		j := i + 2
		for j < n && rs[j] != 0x07 && !(rs[j] == 0x1b && j+1 < n && rs[j+1] == 0x5c) {
			j++
		}
		if j < n {
			if rs[j] == 0x07 {
				j++
			} else {
				j += 2
			}
		}
		return j
	}
	// 2-Zeichen-Escape (ESC + finales Byte).
	if i+1 < n {
		return i + 2
	}
	return i + 1
}

// Unfocus gibt den Block-Fokus frei — tab zurueck ins Eingabefeld (App-
// Seite ruft das beim Umschalten).
func (l BlockList) Unfocus() BlockList { return l.clearFocus() }

// FocusLast fokussiert den letzten fokussierbaren Block — der Uebergang
// vom Eingabe- in den Antwort-Modus via tab.
func (l BlockList) FocusLast() BlockList {
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].Focusable() {
			return l.setFocus(i)
		}
	}
	return l
}

// FocusVisible ist das tab-Ziel: der letzte fokussierbare Block IM
// VIEWPORT — tab landet dort, wo der Nutzer gerade liest, statt ganz unten
// im Chat (von dort muesste man sich sonst mit vielen ↑ hocharbeiten).
// Inline-Modus druckt append-only, "sichtbar" ist dort das Ende -> FocusLast.
// Ohne fokussierbaren Block im Viewport: der erste DARUNTER, sonst der
// letzte ueberhaupt (alter FocusLast-Fallback).
func (l BlockList) FocusVisible() BlockList {
	if l.inline || l.height <= 0 {
		return l.FocusLast()
	}
	w := l.Width()
	bottom := l.offset + l.height
	lastVis, firstBelow := -1, -1
	y := 0
	for i, it := range l.items {
		h := it.Height(w)
		if it.Focusable() {
			switch {
			case y+h > l.offset && y < bottom: // ragt in den Viewport
				lastVis = i
			case y >= bottom && firstBelow < 0:
				firstBelow = i
			}
		}
		y += h
	}
	switch {
	case lastVis >= 0:
		return l.setFocus(lastVis)
	case firstBelow >= 0:
		return l.setFocus(firstBelow)
	default:
		return l.FocusLast()
	}
}

func (l BlockList) setFocus(idx int) BlockList {
	// FR-43: Die Auswahl ueberlebt die Cursor-Bewegung — geloescht wird sie
	// AUSSCHLIESSLICH durch esc. Verlaesst der Fokus den markierten Block
	// oder den Bereich der Shift-Auswahl, weicht sie nicht ins Nichts,
	// sondern wird zur VERGANGENEN Auswahl: Ihr Rahmen bleibt in der
	// Sekundaer-Farbe stehen (kein Neon — die FR-39-Invariante "hoechstens
	// ein Neon-Rahmen" bleibt gewahrt), bis esc alles rueckt. Innerhalb des
	// Auswahlbereichs bewegt sich der Fokus frei, ohne sie zu beruehren.
	id := l.items[idx].ID()
	if l.markedCount() > 0 && !l.marks[id] {
		l = l.demoteMarks()
	}
	if l.selActive() {
		if lo, hi := l.selBounds(); idx < lo || idx > hi {
			l = l.demoteSelRange()
		}
	}
	for i, it := range l.items {
		l.items[i] = it.SetFocused(i == idx)
	}
	l.focus = idx
	return l.reveal(idx)
}

func (l BlockList) clearFocus() BlockList {
	for i, it := range l.items {
		l.items[i] = it.SetFocused(false)
	}
	l.focus = -1
	return l
}

// reveal scrolls so the focused widget is visible.
func (l BlockList) reveal(idx int) BlockList {
	w := l.Width()
	y := 0
	for i := 0; i < idx; i++ {
		y += l.items[i].Height(w)
	}
	h := l.items[idx].Height(w)
	if y < l.offset {
		l.offset = y
		l.follow = false
	}
	if l.height > 0 && y+h > l.offset+l.height {
		l.offset = y + h - l.height
		l.follow = l.offset >= l.totalLines()-l.height
	}
	return l.clamp()
}
