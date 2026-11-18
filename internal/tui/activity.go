package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// ActivityTree ist die feste Activity-Struktur des Agent-Prozesses (Activity-
// Tree-Spec): GENAU drei Hauptknoten — Thinking (N), Tool Calls (N) und
// Working on — plus eine Trennlinie und die letzten drei Agent-Textzeilen.
// Sie ersetzt die ehemaligen Think-/Tool-Karten im Chat: Der Chat bleibt
// waehrend des Turns stabil, der Prozess lebt komplett hier.
//
// Thinking und Tool Calls sind eingeklappt (▸); der Auswahlmodus (tab)
// laeuft mit ↑/↓ genau diese beiden Knoten ab, und enter klappt den Knoten
// unter dem Cursor auf UND wieder zu — auch den aktiven Denkprozess. Kein
// Modifier, keine zweite Taste; ein Mausklick auf die Kopfzeile tut
// dasselbe, das Rad scrollt die Tool-Historie. Working on ist immer
// ausgeklappt (▼) und zeigt den aktuellsten Vorgang: Thinking-Text, Tool-
// Name und Argument bzw. bash mit gekuerztem Originalbefehl. Ist der Turn
// abgeschlossen (settled), verschwinden die letzten drei Textzeilen — die
// Antwort steht dann fertig im Chat.
type ActivityTree struct {
	th *render.Theme

	// thoughts: geschlossene Denk-Sektionen (ein Tool-Call schliesst die
	// laufende Sektion; neues Thinking oeffnet eine neue — FR-30-Geist).
	thoughts []string
	// live: die aktuelle, noch offene Denk-Sektion.
	live string

	tools []activityTool

	expandThinking bool
	expandTools    bool
	// scroll: Zeilen, die im aufgeklappten Tool-Call-Bereich nach OBEN
	// ueber das Fenster hinaus sichtbar gemacht werden (0 = Tail).
	scroll int

	// sel: der Knoten des Auswahlmodus (nodeNone = Baum nicht selektiert).
	// ↑/↓ laufen Thinking ↔ Tool Calls ab, enter klappt den Knoten unter
	// dem Cursor auf und wieder zu. Der Cursor haengt an der STRUKTUR, nicht
	// an Zeilennummern — Aufklappen verschiebt die Zeilen darunter.
	sel activityNode
	// settled: der Turn ist abgeschlossen — die letzten drei Textzeilen
	// samt Trennlinie verschwinden, die Zusammenfassung bleibt.
	settled bool

	// textLines/pending: die normalen Agent-Textausgabezeilen (nur
	// agent_message_chunk — Thinking und Tool-Argumente zaehlen NICHT,
	// § 8 der Spec). pending ist die noch unvollstaendige letzte Zeile.
	textLines []string
	pending   string
}

// activityTool ist EIN Tool-Call der Historie (callID identifiziert ihn
// ueber In-place-Updates, FR-31). Ein mehrzeiliger bash-Befehl bleibt
// GENAU EIN Call — er wird nie in mehrere zerlegt.
type activityTool struct {
	callID  string
	name    string
	kind    string // ACP-Kategorie (execute/fetch/edit/create/think/other)
	text    string // generischer Inhalt (Legacy-/Demo-Pfad)
	command string // rawInput.command (Shell)
	running bool
	failed  bool
}

// NewActivityTree baut den leeren Baum; Thinking/Tool Calls starten
// eingeklappt (§ 4).
func NewActivityTree(th *render.Theme) ActivityTree {
	return ActivityTree{th: th}
}

// maxActivityTextLines begrenzt den Zeilenpuffer: gezeigt werden ohnehin
// nur die letzten drei (§ 8), mehr als 64 Zeilen braucht niemand.
const maxActivityTextLines = 64

// activityDetailCap begrenzt die Zeilen des aufgeklappten Laufend-Details:
// Befehle koennen mehrere tausend Zeichen lang sein — der Bereich scrollt,
// statt jede Zeile zu layouten.
const activityDetailCap = 32

// Thought haengt einen Thinking-Chunk an die offene Sektion.
func (a ActivityTree) Thought(text string) ActivityTree {
	a.live += text
	return a
}

// CloseThought schliesst die offene Denk-Sektion ab (leere Sektionen
// entstehen nie).
func (a ActivityTree) CloseThought() ActivityTree {
	if strings.TrimSpace(a.live) != "" {
		a.thoughts = append(a.thoughts, a.live)
	}
	a.live = ""
	return a
}

// hasTool meldet, ob die callID schon bekannt ist (In-place-Update).
func (a ActivityTree) hasTool(callID string) bool {
	for _, t := range a.tools {
		if t.callID == callID {
			return true
		}
	}
	return false
}

// Tool nimmt ein Tool-Update entgegen: ein NEUER Call schliesst zuerst die
// laufende Denk-Sektion (der Agent denkt nicht weiter, während er werkelt),
// danach wird der Eintrag angelegt oder in Place aktualisiert.
func (a ActivityTree) Tool(u ToolUpdate) ActivityTree {
	if a.live != "" && !a.hasTool(u.CallID) {
		a = a.CloseThought()
	}
	entry := activityTool{callID: u.CallID, name: u.Label, kind: u.Kind,
		text: u.Text, command: u.Command, running: !u.Done, failed: u.Failed}
	for i := range a.tools {
		if a.tools[i].callID == u.CallID {
			a.tools[i] = entry
			return a
		}
	}
	a.tools = append(a.tools, entry)
	return a
}

// Text nimmt einen Agent-Text-Chunk entgegen; vollstaendige Zeilen landen
// im Puffer (maximal maxActivityTextLines, der Tail zaehlt).
func (a ActivityTree) Text(s string) ActivityTree {
	a.pending += s
	for {
		i := strings.IndexByte(a.pending, '\n')
		if i < 0 {
			break
		}
		a.textLines = append(a.textLines, a.pending[:i])
		a.pending = a.pending[i+1:]
	}
	if len(a.textLines) > maxActivityTextLines {
		a.textLines = a.textLines[len(a.textLines)-maxActivityTextLines:]
	}
	return a
}

// FlushText uebernimmt die noch unvollstaendige letzte Zeile (Turn-Ende:
// der letzte Chunk kommt oft ohne schliessenden Zeilenumbruch).
func (a ActivityTree) FlushText() ActivityTree {
	if strings.TrimSpace(a.pending) != "" {
		a.textLines = append(a.textLines, a.pending)
	}
	a.pending = ""
	if len(a.textLines) > maxActivityTextLines {
		a.textLines = a.textLines[len(a.textLines)-maxActivityTextLines:]
	}
	return a
}

// ToggleThinking klappt den Thinking-Knoten um.
func (a ActivityTree) ToggleThinking() ActivityTree {
	a.expandThinking = !a.expandThinking
	return a
}

// ToggleTools klappt den Tool-Call-Knoten um; Zuklappen setzt den Scroll
// zurueck (aufgeklappt startet der Blick am aktuellsten Call).
func (a ActivityTree) ToggleTools() ActivityTree {
	a.expandTools = !a.expandTools
	if !a.expandTools {
		a.scroll = 0
	}
	return a
}

// Wheel scrollt den aufgeklappten Tool-Call-Bereich (dir: +1 aelterer
// Inhalt sichtbar, -1 zurueck zum Tail). Ohne aufgeklappte Tool Calls ist
// das Rad ein No-Op.
func (a ActivityTree) Wheel(dir int) ActivityTree {
	if !a.expandTools {
		return a
	}
	a.scroll += dir
	if a.scroll < 0 {
		a.scroll = 0
	}
	if rows, _ := a.toolChildren(80); a.scroll > len(rows)-1 {
		a.scroll = len(rows) - 1
	}
	if a.scroll < 0 {
		a.scroll = 0
	}
	return a
}

// ThinkingCount: Zahl der Denk-Sektionen (geschlossene + die offene).
func (a ActivityTree) ThinkingCount() int {
	n := len(a.thoughts)
	if strings.TrimSpace(a.live) != "" {
		n++
	}
	return n
}

// ToolCount: Zahl der Tool-Calls (distinct callIDs).
func (a ActivityTree) ToolCount() int { return len(a.tools) }

// Empty: kein Thinking, kein Tool-Call, keine Textzeile — nichts zu zeigen.
func (a ActivityTree) Empty() bool {
	return a.ThinkingCount() == 0 && len(a.tools) == 0 &&
		len(a.visibleTextLines()) == 0
}

// currentTool liefert den zuletzt aktualisierte­n Call (laufend oder fertig).
func (a ActivityTree) currentTool() (activityTool, bool) {
	if len(a.tools) == 0 {
		return activityTool{}, false
	}
	return a.tools[len(a.tools)-1], true
}

// visibleTextLines: die letzten (höchstens drei) Textausgabezeilen, ohne
// haengende Leerzeilen (§ 8). Die unvollstaendige letzte Zeile (pending)
// zaehlt mit — sie IST die aktuelle Ausgabe, waehrend sie streamt.
func (a ActivityTree) visibleTextLines() []string {
	lines := a.textLines
	if strings.TrimSpace(a.pending) != "" {
		lines = append(lines, a.pending)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// ---- Prädikate ---------------------------------------------------------------

// isBash: der Call zeigt einen Shell-Befehl (ACP execute-Kategorie oder
// bash/shell-Name) — sein Name leuchtet Neon-Pink und sein Befehl bekommt
// das mehrzeilige Detail-Rendering (§ 6).
func (t activityTool) isBash() bool {
	if t.kind == "execute" {
		return true
	}
	n := strings.ToLower(t.name)
	return n == "bash" || n == "shell" || strings.HasPrefix(n, "bash ")
}

// ---- Styling -----------------------------------------------------------------

// activityStyle: Vordergrundfarbe auf dem Theme-Hintergrund — keine
// flächigen Fuellungen, alle Farben kommen zentral aus dem Theme (§ 2).
func (a ActivityTree) style(color string) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	if a.th != nil && a.th.Background != "" {
		s = s.Background(lipgloss.Color(a.th.Background))
	}
	return s
}

func (a ActivityTree) valida() lipgloss.Style { return a.style(a.th.ValidaColor()) }
func (a ActivityTree) neon() lipgloss.Style   { return a.style(a.th.NeonPinkColor()) }
func (a ActivityTree) muted() lipgloss.Style  { return a.style(a.th.MutedColor()) }
func (a ActivityTree) textSt() lipgloss.Style { return a.th.TextStyle() }
func (a ActivityTree) stateSt(t activityTool) lipgloss.Style {
	switch {
	case t.failed:
		return a.style(a.th.ErrColor())
	case t.running:
		return a.valida()
	default:
		return a.style(a.th.OkColor())
	}
}

// ---- Kürzung -----------------------------------------------------------------

// cutCells schneidet plain auf w Zellen ab (ohne Markierung).
func cutCells(s string, w int) string {
	if w < 1 || runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "")
}

// cutWords kuerzt plain auf w Zellen am letzten sinnvollen Wortuebergang
// und haengt " ..." an — die Ellipse folgt einem LEERZEICHEN, nie klebt sie
// am Token (§ 6: "grep ...", nicht "grep -rn..."). Ohne Wortuebergang wird
// hart geschnitten.
func cutWords(s string, w int) string {
	if w <= 4 || runewidth.StringWidth(s) <= w {
		return s
	}
	cut := cutCells(s, w-4)
	if j := strings.LastIndexByte(cut, ' '); j > 0 {
		if t := strings.TrimRight(cut[:j], " "); t != "" {
			cut = t
		}
	}
	return cut + " ..."
}

// joinCommandLines fuegt die Zeilen eines (mehrzeiligen) Befehls zu EINER
// Vorschauzeile zusammen: Continuation-Backslashes fallen weg, Einrueckung
// wird zu einfachen Leerzeichen — der Inhalt bleibt unangetastet (§ 6:
// keine kuenstliche Zusammenfassung, nur Zusammenfuegen).
func joinCommandLines(cmd string) string {
	var parts []string
	for _, ln := range strings.Split(strings.ReplaceAll(cmd, "\r\n", "\n"), "\n") {
		ln = strings.TrimSpace(ln)
		ln = strings.TrimSuffix(ln, "\\")
		ln = strings.TrimSpace(ln)
		if ln != "" {
			parts = append(parts, ln)
		}
	}
	return strings.Join(parts, " ")
}

// ---- Zeilen-Bau --------------------------------------------------------------

// Tree-Grammatik (§ 3/§ 4): Kindpraefix `│  ├─ `/`│  └─ ` (6 Zellen),
// Fortsetzung `│     `, Laufend-Detail `│     │` mit 2 Zellen Inhalt.
const (
	actChild = "│  ├─ "
	actLast  = "│  └─ "
	actCont  = "│     "
	actRail  = "│"
)

// detailPrefix praefigiert den Laufend-Block: `│     │` + 2 Zellen Abstand;
// visuelle Umbruch-Fortsetzungen ruecken 2 Zellen tiefer als echte Zeilen
// (§ 6: Umbruch darf nicht wie ein echter Zeilenumbruch aussehen).
const (
	actDetail     = "│     │  "
	actDetailCont = "│     │    "
)

// thinkingChildren: eingeklappte Denk-Sektionen als je eine Zeile (Tail,
// wenn das Budget kneift) — Inhalt in Neon-Pink (Thinking-Inhalte, § 2).
func (a ActivityTree) thinkingChildren(width int) []string {
	inner := width - 6
	if inner < 8 {
		inner = 8
	}
	secs := a.thoughts
	if strings.TrimSpace(a.live) != "" {
		secs = append(secs, a.live)
	}
	var rows []string
	for i, sec := range secs {
		first := strings.TrimSpace(sec)
		if j := strings.IndexByte(first, '\n'); j >= 0 {
			first = first[:j]
		}
		branch := actChild
		if i == len(secs)-1 {
			branch = actLast
		}
		rows = append(rows, a.muted().Render(branch)+a.neon().Render(cutWords(first, inner)))
	}
	return rows
}

// toolRow baut EINE Historienzeile: Zustandssymbol + Call-Label.
func (a ActivityTree) toolRow(t activityTool, last bool, width int) string {
	branch := actChild
	if last {
		branch = actLast
	}
	sym := "✓"
	if t.running {
		sym = "●"
	} else if t.failed {
		sym = "✗"
	}
	var label string
	inner := width - 9 // │  ├─ + Symbol + Leerzeichen + Puffer
	if inner < 8 {
		inner = 8
	}
	if t.isBash() {
		name := t.name
		if name == "" {
			name = "bash"
		}
		if t.running {
			// Laufender bash-Call: NUR der Name — der Befehl lebt im
			// aufklappbaren Detail darunter (§ 5), nicht als Inline-Kopie.
			return a.muted().Render(branch) + a.stateSt(t).Render(sym) + " " +
				a.neon().Render(cutCells(name, inner))
		}
		cmd := t.command
		if cmd == "" {
			cmd = t.text
		}
		label = a.neon().Render(cutCells(name, inner)) +
			a.textSt().Render(": "+cutWords(joinCommandLines(cmd), inner-6))
	} else {
		label = a.textSt().Render(cutCells(t.name, inner))
		if arg := firstLine(t.text); arg != "" {
			label += a.textSt().Render("(" + cutWords(arg, inner-runewidth.StringWidth(t.name)-2) + ")")
		}
	}
	return a.muted().Render(branch) + a.stateSt(t).Render(sym) + " " + label
}

// toolDetail baut den Laufend-Block eines bash-Calls: Original-Zeilenum-
// brueche bleiben erhalten, lange Zeilen brechen VISUELL um (§ 6).
func (a ActivityTree) toolDetail(t activityTool, width int) []string {
	// Die Fortsetzungszeile traegt den BREITEREN Praefix — der Umbruch
	// rechnet mit ihr, sonst laufen Visuell-umgebrochene ueber das Terminal.
	inner := width - 11
	if inner < 8 {
		inner = 8
	}
	src := t.command
	if src == "" {
		src = t.text
	}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	if len(lines) > activityDetailCap {
		lines = append([]string{"…"}, lines[len(lines)-activityDetailCap:]...)
	}
	var rows []string
	rows = append(rows, a.muted().Render("│     │"))
	for _, ln := range lines {
		// Original-Einrueckung bleibt erhalten (§ 5: die Fortsetzungs-
		// zeilen des Befehls ruecken genau wie im Original ein) —
		// WrapWords fasst Woerter, Einrueckung praefigieren wir selbst.
		indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
		body := strings.TrimLeft(ln, " \t")
		wrapped := render.WrapWords(body, inner-runewidth.StringWidth(indent))
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		rows = append(rows, a.muted().Render(actDetail)+a.textSt().Render(indent+wrapped[0]))
		for _, w := range wrapped[1:] {
			rows = append(rows, a.muted().Render(actDetailCont)+a.textSt().Render(w))
		}
	}
	return rows
}

// toolChildren: alle Historienzeilen der Tool-Calls (nur aufgeklappt
// gerendert); Tail-Anker + Scroll regelt View. lastStart ist der Index der
// Kopfzeile des LETZTEN Calls — View ankert ihn im Fenster, damit der
// laufende Call nie mitten im Detail abgeschnitten startet.
func (a ActivityTree) toolChildren(width int) (rows []string, lastStart int) {
	for i, t := range a.tools {
		last := i == len(a.tools)-1
		lastStart = len(rows)
		rows = append(rows, a.toolRow(t, last, width))
		if t.running {
			rows = append(rows, a.toolDetail(t, width)...)
		}
	}
	return rows, lastStart
}

// workingChildren: der kompakte Blick auf den AKTUELLSTEN Vorgang (§ 7) —
// keine zweite Ausfuehrung, nur Vorschau.
func (a ActivityTree) workingChildren(width int) []string {
	inner := width - 6
	if inner < 8 {
		inner = 8
	}
	// Thinking ist aktuell, solange die offene Sektion waechst.
	if strings.TrimSpace(a.live) != "" {
		rows := []string{a.muted().Render(actChild) + a.neon().Render("Thinking")}
		lines := textWrapped(a.live, inner)
		if len(lines) > 5 {
			lines = lines[len(lines)-5:] // max. fuenf Inhaltszeilen (§ 7)
		}
		for i, ln := range lines {
			prefix := actCont
			if i == 0 {
				prefix = actLast
			}
			rows = append(rows, a.muted().Render(prefix)+a.neon().Render(ln))
		}
		return rows
	}
	if t, ok := a.currentTool(); ok {
		if t.isBash() {
			cmd := t.command
			if cmd == "" {
				cmd = t.text
			}
			return []string{
				a.muted().Render(actChild) + a.neon().Render("bash"),
				a.muted().Render(actLast) + a.textSt().Render(cutWords(joinCommandLines(cmd), inner)),
			}
		}
		first := firstLine(t.text)
		if first == "" {
			return []string{a.muted().Render(actLast) + a.textSt().Render(t.name)}
		}
		return []string{
			a.muted().Render(actChild) + a.textSt().Render(t.name),
			a.muted().Render(actLast) + a.textSt().Render(cutWords(first, inner)),
		}
	}
	return nil
}

// firstLine liefert die erste nichtleere Zeile eines Textes.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			return ln
		}
	}
	return ""
}

// headerRow baut eine Hauptknoten-Zeile: Faltmarke in Valida, Label in der
// Knotenfarbe (Thinking Neon-Pink, Tool Calls/Working on Valida). Steht der
// Auswahlmodus auf diesem Knoten, traegt seine Kopfzeile den Unterstrich —
// gesetzt auf Marke UND Label, nie ueber die fertige Zeile: deren innere
// Resets wuerden den Unterstrich nach dem ersten Segment abreissen und die
// Sequenz zeichenweise zerlegen.
func (a ActivityTree) headerRow(n activityNode, collapsed bool, label string, count int, labelSt lipgloss.Style) string {
	mark := "▼"
	if collapsed {
		mark = "▸"
	}
	txt := label
	if count >= 0 {
		txt += " (" + strconv.Itoa(count) + ")"
	}
	markSt := a.valida()
	if n != nodeNone && a.sel == n {
		markSt = markSt.Underline(true)
		labelSt = labelSt.Underline(true)
	}
	return markSt.Render(mark) + " " + labelSt.Render(txt)
}

// activityNode benennt die aufklappbaren Hauptknoten des Baums. Der
// Auswahlmodus laeuft GENAU diese beiden ab: Working on ist immer offen und
// hat nichts zu klappen — dort gibt es auch keinen Cursor.
type activityNode int

const (
	nodeNone activityNode = iota
	nodeThinking
	nodeTools
)

// activityNodes ist die Anzeige-Reihenfolge der aufklappbaren Knoten.
var activityNodes = []activityNode{nodeThinking, nodeTools}

// activityLayout ist das gerenderte Bild samt Struktur fuer den
// Auswahlmodus: rowNode ordnet jeder Zeile ihren Hauptknoten zu
// (nodeNone = Working on, Trennlinie, Text). Der Cursor wandert an
// STRUKTUR, nicht an Zeilennummern — ein aufgeklappter Knoten verschiebt
// die Zeilen darunter, ohne dass enter das Ziel verliert.
type activityLayout struct {
	rows    []string
	rowNode []activityNode
}

// headRow: Zeile der Kopfzeile des Knotens (-1 = nicht sichtbar). Der Kopf
// ist die erste Zeile der Knoten-Spanne; seine Kinder folgen darunter.
func (l activityLayout) headRow(n activityNode) int {
	if n == nodeNone {
		return -1
	}
	for i, x := range l.rowNode {
		if x == n {
			return i
		}
	}
	return -1
}

// actIndent: Jede Zeile beginnt mindestens EIN Leerzeichen spaeter als die
// Terminalkante — der Baum klebt nie am Rand (User-Wunsch).
const actIndent = " "

// layout rendert den Baum in maximal budget Zeilen (§ 9): Prioritaet —
// 1. die letzten Textzeilen, 2. Working on, 3. Thinking-/Tool-Kinder
// werden gekuerzt/gescrollt. Abgeschlossene Turns (settled) zeigen die
// Textzeilen nicht mehr. Der Auswahlmodus unterstreicht die Kopfzeile des
// gewaehlten Knotens.
func (a ActivityTree) layout(width, budget int) activityLayout {
	if budget < 1 || width < 9 {
		return activityLayout{}
	}
	aw := width - 1 // 1 Zelle linker Rand

	thinkHdr := a.headerRow(nodeThinking, !a.expandThinking, "Thinking", a.ThinkingCount(), a.neon())
	toolsHdr := a.headerRow(nodeTools, !a.expandTools, "Tool Calls", a.ToolCount(), a.valida())
	workHdr := a.headerRow(nodeNone, false, "Working on", -1, a.valida())

	work := a.workingChildren(aw)
	text := a.visibleTextLines()
	if len(text) > 3 {
		text = text[len(text)-3:]
	}
	// Settled (§ 8, User-Wunsch): ist ALLES abgeschlossen, verschwinden
	// die drei Textzeilen mitsamt der Trennlinie — die Zusammenfassung
	// des Baumes reicht.
	sep := 0
	if len(text) > 0 && !a.settled {
		sep = 1
	} else if a.settled {
		text = nil
	}

	// Prioritaet 1: Textzeilen + Trennlinie bleiben sichtbar.
	avail := budget - len(text) - sep
	if avail < 0 {
		keep := budget - sep
		if keep < 1 && budget >= 1 {
			sep = 0
			keep = budget
		}
		if keep < 1 {
			return activityLayout{}
		}
		if keep < len(text) {
			text = text[len(text)-keep:]
		}
		avail = 0
	}

	var tail []string
	if sep > 0 {
		tail = append(tail, a.muted().Render(strings.Repeat("─", aw)))
	}
	for i := range text {
		tail = append(tail, a.textSt().Render(text[i]))
	}

	if avail < 4 {
		// Kein Platz fuer alle Hauptknoten: Working on hat Vorrang vor
		// Thinking/Tool Calls (§ 9); die Reihenfolge bleibt immer
		// Thinking → Tool Calls → Working on. Thinking faellt zuerst weg —
		// sein Cursor ist dann unsichtbar und wird gesnappt.
		if avail >= 3 {
			return a.pack([]string{toolsHdr, workHdr, a.muted().Render(actRail)},
				[]activityNode{nodeTools, nodeNone, nodeNone}, tail)
		}
		if avail == 2 {
			return a.pack([]string{workHdr, a.muted().Render(actRail)},
				[]activityNode{nodeNone, nodeNone}, tail)
		}
		if avail == 1 {
			return a.pack([]string{workHdr}, []activityNode{nodeNone}, tail)
		}
		return a.pack(nil, nil, tail)
	}
	avail -= 4
	if len(work) > avail {
		work = work[:avail]
	}
	avail -= len(work)

	var thinkRows, toolRows []string
	lastStart := 0
	if a.expandThinking {
		thinkRows = a.thinkingChildren(aw)
	}
	if a.expandTools {
		toolRows, lastStart = a.toolChildren(aw)
	}
	kids := avail
	reserve := 0
	if a.expandTools && len(toolRows) > 0 {
		reserve = 1
	}
	nTool := len(toolRows)
	if nTool > kids-reserve {
		nTool = kids - reserve
	}
	if nTool < 0 {
		nTool = 0
	}
	bottom := len(toolRows) - a.scroll
	if bottom < nTool {
		bottom = nTool
	}
	if bottom > len(toolRows) {
		bottom = len(toolRows)
	}
	top := bottom - nTool
	if top < 0 {
		top = 0
	}
	if a.scroll == 0 && top > lastStart {
		top = lastStart
		bottom = top + nTool
		if bottom > len(toolRows) {
			bottom = len(toolRows)
		}
	}
	toolRows = toolRows[top:bottom]
	if nTool > 0 {
		toolRows = append(toolRows, a.muted().Render(actRail))
	}
	nThink := len(thinkRows)
	if r := kids - nTool - reserve; nThink > r {
		nThink = r
	}
	if nThink < 0 {
		nThink = 0
	}
	thinkRows = thinkRows[len(thinkRows)-nThink:]

	head := make([]string, 0, 2+len(thinkRows)+len(toolRows)+len(work))
	headNode := make([]activityNode, 0, cap(head))
	head = append(head, thinkHdr)
	headNode = append(headNode, nodeThinking)
	head = append(head, thinkRows...)
	headNode = append(headNode, repeatNode(nodeThinking, len(thinkRows))...)
	head = append(head, toolsHdr)
	headNode = append(headNode, nodeTools)
	head = append(head, toolRows...)
	headNode = append(headNode, repeatNode(nodeTools, len(toolRows))...)
	head = append(head, workHdr)
	headNode = append(headNode, nodeNone)
	head = append(head, work...)
	headNode = append(headNode, repeatNode(nodeNone, len(work))...)
	head = append(head, a.muted().Render(actRail))
	headNode = append(headNode, nodeNone)
	return a.pack(head, headNode, tail)
}

// repeatNode: k-mal derselbe Knoten (parallele Zeilen-Zuordnung).
func repeatNode(n activityNode, k int) []activityNode {
	out := make([]activityNode, k)
	for i := range out {
		out[i] = n
	}
	return out
}

// pack haengt Kopf und Tail zusammen und versetzt jede Zeile um den linken
// Rand. Die Zuordnung Zeile→Knoten wandert mit, damit die Maus denselben
// Cursor setzt wie ↑/↓. Der Auswahl-Unterstrich sitzt in headerRow (dort,
// wo die Knotenfarbe gesetzt wird) — nicht hier auf der fertigen Zeile.
func (a ActivityTree) pack(head []string, headNode []activityNode, tail []string) activityLayout {
	rows := make([]string, 0, len(head)+len(tail))
	nodes := make([]activityNode, 0, len(head)+len(tail))
	rows = append(rows, head...)
	nodes = append(nodes, headNode...)
	rows = append(rows, tail...)
	nodes = append(nodes, repeatNode(nodeNone, len(tail))...)
	for i := range rows {
		rows[i] = actIndent + rows[i]
	}
	return activityLayout{rows: rows, rowNode: nodes}
}

// View rendert den Activity-Tree in maximal budget Zeilen.
func (a ActivityTree) View(width, budget int) []string {
	return a.layout(width, budget).rows
}

// ---- Auswahlmodus: ↑/↓ laufen die Hauptknoten ab, enter klappt auf/zu ----

// Focused meldet, ob der Auswahlmodus den Baum haelt.
func (a ActivityTree) Focused() bool { return a.sel != nodeNone }

// Focus setzt den Auswahlmodus auf den Thinking-Knoten (bzw. auf den
// ersten Knoten, der im aktuellen Budget ueberhaupt sichtbar ist).
func (a ActivityTree) Focus(width, budget int) ActivityTree {
	if a.sel == nodeNone {
		a.sel = nodeThinking
	}
	return a.snap(width, budget)
}

// snap haelt den Cursor auf einem SICHTBAREN Knoten: in winzigen Terminals
// faellt der Thinking-Kopf weg — der Cursor darf dann nicht ins Leere
// zeigen (enter wuerde sonst stumm schalten).
func (a ActivityTree) snap(width, budget int) ActivityTree {
	if a.sel == nodeNone {
		return a
	}
	l := a.layout(width, budget)
	if l.headRow(a.sel) >= 0 {
		return a
	}
	for _, n := range activityNodes {
		if l.headRow(n) >= 0 {
			a.sel = n
			return a
		}
	}
	return a
}

// Blur raeumt den Cursor (zurueck in die Kette).
func (a ActivityTree) Blur() ActivityTree {
	a.sel = nodeNone
	return a
}

// Enter klappt den Knoten unter dem Cursor auf — und mit demselben Druck
// wieder zu. Die beiden Knoten sind unabhaengig; kein Modifier, keine
// zweite Taste. Working on kennt kein Zu und bleibt stumm.
func (a ActivityTree) Enter() ActivityTree {
	switch a.sel {
	case nodeThinking:
		return a.ToggleThinking()
	case nodeTools:
		return a.ToggleTools()
	}
	return a
}

// Move laeuft die Knoten mit ↑/↓ ab; an den Enden bleibt der Cursor stehen
// (der Baum ist ein kurzes Menue, kein Karussell). Knoten, die im Budget
// nicht sichtbar sind, werden uebersprungen.
func (a ActivityTree) Move(dir, width, budget int) ActivityTree {
	l := a.layout(width, budget)
	var vis []activityNode
	for _, n := range activityNodes {
		if l.headRow(n) >= 0 {
			vis = append(vis, n)
		}
	}
	if len(vis) == 0 {
		return a
	}
	cur := -1
	for i, n := range vis {
		if n == a.sel {
			cur = i
		}
	}
	if cur < 0 {
		// (Noch) kein sichtbarer Cursor: in Laufrichtung aufsetzen.
		if dir < 0 {
			a.sel = vis[len(vis)-1]
		} else {
			a.sel = vis[0]
		}
		return a
	}
	if cur += dir; cur < 0 {
		cur = 0
	} else if cur >= len(vis) {
		cur = len(vis) - 1
	}
	a.sel = vis[cur]
	return a
}

// Click setzt den Cursor auf den Knoten der Zeile; ein Klick auf dessen
// Kopfzeile klappt ihn um (Maus-Pendant zu ↑/↓ + enter). Eine Zeile ohne
// Knoten (Working on, Trennlinie, Text) selektiert den Baum trotzdem —
// der Cursor springt auf den Knoten darueber, geklappt wird nichts.
func (a ActivityTree) Click(row, width, budget int) ActivityTree {
	l := a.layout(width, budget)
	if row < 0 || row >= len(l.rowNode) {
		return a
	}
	if n := l.rowNode[row]; n != nodeNone {
		a.sel = n
		if l.headRow(n) == row {
			return a.Enter()
		}
		return a
	}
	if n := l.nodeAbove(row); n != nodeNone {
		a.sel = n
	}
	return a
}

// nodeAbove: der unterste sichtbare Knoten, dessen Kopfzeile ueber row
// liegt (der Knoten, zu dem eine Zeile ohne eigene Zuordnung gehoert).
func (l activityLayout) nodeAbove(row int) activityNode {
	found := nodeNone
	for _, n := range activityNodes {
		if h := l.headRow(n); h >= 0 && h < row {
			found = n
		}
	}
	if found == nodeNone && len(activityNodes) > 0 {
		if h := l.headRow(activityNodes[0]); h >= 0 {
			found = activityNodes[0]
		}
	}
	return found
}

// Settle markiert den Turn als abgeschlossen (keine drei Textzeilen mehr).
func (a ActivityTree) Settle() ActivityTree {
	a.settled = true
	return a
}
