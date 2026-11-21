package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// ActivityTree ist die feste Activity-Struktur des Agent-Prozesses (Activity-
// Tree-Spec): GENAU drei Hauptknoten — Thinking (N), Tool Calls (N) und
// Working on. Sie ersetzt die ehemaligen Think-/Tool-Karten im Chat: Der
// Prozess lebt komplett hier — Agent-Text gehoert NICHT dazu, er ist die
// Antwort und streamt als ein wachsender Block direkt in den Chat.
//
// Thinking und Tool Calls sind eingeklappt (▸); der Auswahlmodus (tab)
// laeuft mit ↑/↓ genau diese beiden Knoten ab, und enter klappt den Knoten
// unter dem Cursor auf UND wieder zu — auch den aktiven Denkprozess. Kein
// Modifier, keine zweite Taste; ein Mausklick auf die Kopfzeile tut
// dasselbe, das Rad scrollt die Tool-Historie. Working on ist KEIN
// Faltknoten: keine ▸/▼-Marke, enter/klick klappen dort nichts. Er zeigt
// NUR die GERADE LAUFENDEN Vorgaenge — offenes Thinking und/oder laufende
// Calls, jeweils mit der bisherigen Laufzeit (Zeitangabe: man sieht, wo
// der Agent am laengsten und am kuerzesten braucht). Ist nichts mehr im
// Gang oder der Turn abgeschlossen (settled), verschwindet der Knoten
// samt den letzten drei Textzeilen — die Antwort steht fertig im Chat.
type ActivityTree struct {
	th *render.Theme

	// thoughts: geschlossene Denk-Sektionen (ein Tool-Call schliesst die
	// laufende Sektion; neues Thinking oeffnet eine neue — FR-30-Geist),
	// je Sektion mit Start-/Endzeit fuer die Laufzeitangabe.
	thoughts []thoughtSection
	// live: die aktuelle, noch offene Denk-Sektion.
	live string
	// liveStart: Beginn der offenen Denk-Sektion (Laufzeitangabe).
	liveStart time.Time

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
	// settled: der Turn ist abgeschlossen — Working on verschwindet,
	// die Zusammenfassung (Thinking/Tool Calls) bleibt.
	settled bool
	// idleSince: Turn-Beginn (startTurn). Solange der laufende Turn kein
	// Kind hat (offenes Thinking, laufender Call), zeigt Working on eine
	// Spinner-Zeile mit Turn-Laufzeit — der Agent rechnet sichtbar weiter
	// (User-Report: "Working on gar nicht angezeigt, obwohl noch nicht
	// fertig"). Das erste Event ersetzt sie, Settle raeumt auf.
	idleSince time.Time
	// idleFrame: Spinner-Frame der Leerlauf-Zeile (folgt m.spinner ueber
	// IdleFrame, damit Baum und Statuszeile synchron rotieren).
	idleFrame int
}

// thoughtSection ist EINE Denk-Sektion samt Start-/Endzeit — die Laufzeit
// je Sektion zeigt, welche Denkphase wie lange brauchte.
type thoughtSection struct {
	text    string
	started time.Time
	ended   time.Time
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
	started time.Time // Beginn des Calls (erster Kontakt)
	ended   time.Time // Ende des Calls (Update mit Done); zero = laeuft
}

// NewActivityTree baut den leeren Baum; Thinking/Tool Calls starten
// eingeklappt (§ 4).
func NewActivityTree(th *render.Theme) ActivityTree {
	return ActivityTree{th: th}
}

// activityDetailCap begrenzt die Zeilen des aufgeklappten Laufend-Details:
// Befehle koennen mehrere tausend Zeichen lang sein — der Bereich scrollt,
// statt jede Zeile zu layouten.
const activityDetailCap = 32

// Thought haengt einen Thinking-Chunk an die offene Sektion; der erste
// Chunk startet die Laufzeituhr der Sektion.
func (a ActivityTree) Thought(text string) ActivityTree {
	a.live += text
	if a.liveStart.IsZero() {
		a.liveStart = time.Now()
	}
	return a
}

// CloseThought schliesst die offene Denk-Sektion ab (leere Sektionen
// entstehen nie).
func (a ActivityTree) CloseThought() ActivityTree {
	if strings.TrimSpace(a.live) != "" {
		a.thoughts = append(a.thoughts,
			thoughtSection{text: a.live, started: a.liveStart, ended: time.Now()})
	}
	a.live = ""
	a.liveStart = time.Time{}
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
	now := time.Now()
	entry := activityTool{callID: u.CallID, name: u.Label, kind: u.Kind,
		text: u.Text, command: u.Command, running: !u.Done, failed: u.Failed,
		started: now, ended: now}
	if !u.Done {
		entry.ended = time.Time{} // laeuft noch — die Uhr tickt weiter
	}
	for i := range a.tools {
		if a.tools[i].callID == u.CallID {
			// In-place-Update: der Startzeitpunkt bleibt — die Laufzeit
			// misst den GESAMTEN Call, nicht das letzte Update.
			entry.started = a.tools[i].started
			if u.Done {
				if a.tools[i].ended.IsZero() {
					entry.ended = now
				} else {
					entry.ended = a.tools[i].ended
				}
			} else {
				entry.ended = time.Time{}
			}
			a.tools[i] = entry
			return a
		}
	}
	a.tools = append(a.tools, entry)
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

// Empty: kein Thinking, kein Tool-Call — nichts zu zeigen. AUSNAHME: ein
// laufender Turn mit armierter Leerlauf-Anzeige (Idle) bleibt sichtbar —
// Working on verschwindet, wenn der Agent fertig ist (settled), nicht
// wenn er gerade zwischen zwei Events rechnet (User-Report).
func (a ActivityTree) Empty() bool {
	if !a.settled && !a.idleSince.IsZero() {
		return false
	}
	return a.ThinkingCount() == 0 && len(a.tools) == 0
}

// currentTool liefert den zuletzt aktualisierte­n Call (laufend oder fertig).
func (a ActivityTree) currentTool() (activityTool, bool) {
	if len(a.tools) == 0 {
		return activityTool{}, false
	}
	return a.tools[len(a.tools)-1], true
}

// duration: Laufzeit des Calls — bei laufenden Calls die bisherige Zeit.
func (t activityTool) duration() time.Duration {
	switch {
	case t.started.IsZero():
		return 0
	case t.ended.IsZero():
		return time.Since(t.started)
	}
	return t.ended.Sub(t.started)
}

// duration: Laufzeit der Denk-Sektion.
func (s thoughtSection) duration() time.Duration {
	if s.started.IsZero() {
		return 0
	}
	if s.ended.IsZero() {
		return time.Since(s.started)
	}
	return s.ended.Sub(s.started)
}

// durTag rendert die Zeitangabe hinter einem Vorgang (muted): "<1s",
// "12s", "1m03s". Ohne messbare Zeit (nie gestartet bzw. Start = Ende)
// faellt der Tag weg — dann ist keine Aussage drin.
func (a ActivityTree) durTag(d time.Duration) (string, int) {
	if d <= 0 {
		return "", 0
	}
	s := "<1s"
	if d >= time.Second {
		sec := int(d.Seconds())
		if sec < 60 {
			s = strconv.Itoa(sec) + "s"
		} else {
			t := strconv.Itoa(sec/60) + "m"
			if sec%60 < 10 {
				t += "0"
			}
			s = t + strconv.Itoa(sec%60) + "s"
		}
	}
	tag := " " + s
	return a.muted().Render(tag), runewidth.StringWidth(tag)
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

// soft ist die Inhaltsfarbe des Baums (Hellgrau, User-Wunsch): Gelesenes
// (Denk-Tails, Befehle, fortlaufende Working-on-Zeilen) liest sich still,
// Labels und Struktur tragen die Akzente.
func (a ActivityTree) soft() lipgloss.Style   { return a.style(a.th.SoftColor()) }
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
)

// Nerd-Font-Icons für die Konzept-Knoten (aus der installierten FiraCode
// Nerd Font via cmap verifiziert):  light_bulb (oct) für Thinking,
//
//	terminal (oct) für Tool Calls. Beide BMP (F400-F532, Octicons-Range —
//
// identisch in NF v2 UND v3), nicht die Plane-15-Material-Icons
// (U+F0000+, dort scheitert der Terminal-Fallback). Breite 1 (East-Asian-
// Ambiguous); Terminals OHNE Nerd Font zeigen Ersatzzeichen, die Labels
// tragen die Bedeutung weiter.
const (
	iconThinking = "\uF400" //  oct-light_bulb
	iconTool     = "\uF489" //  oct-terminal
)

// Pods des Working-on-Knotens (User-Prototyp, 21.11.): Jede Kapsel
// schliesst sich SELBST — kein Zweig-Zeichen, das zeitversetzt ins Leere
// zeigen kann. Kopf `╰─┬──── <icon> Label · tag`, Inhalt `   ┆ …`,
// Abschluss `   ╰────`.

// detailPrefix praefigiert den Laufend-Block: `│     │` + 2 Zellen Abstand;
// visuelle Umbruch-Fortsetzungen ruecken 2 Zellen tiefer als echte Zeilen
// (§ 6: Umbruch darf nicht wie ein echter Zeilenumbruch aussehen).
const (
	actDetail     = "│     │  "
	actDetailCont = "│     │    "
)

// thinkingChildren: eingeklappte Denk-Sektionen als je eine Zeile (Tail,
// wenn das Budget kneift) — Inhalt in Soft-Grau (User-Wunsch: Gelesenes
// kein Neon), je Zeile mit der Laufzeit der Sektion.
func (a ActivityTree) thinkingChildren(width int) []string {
	inner := width - 6
	if inner < 8 {
		inner = 8
	}
	secs := a.thoughts
	if strings.TrimSpace(a.live) != "" {
		secs = append(secs, thoughtSection{text: a.live, started: a.liveStart})
	}
	var rows []string
	for i, sec := range secs {
		first := strings.TrimSpace(sec.text)
		if j := strings.IndexByte(first, '\n'); j >= 0 {
			first = first[:j]
		}
		branch := actChild
		if i == len(secs)-1 {
			branch = actLast
		}
		tag, tagW := a.durTag(sec.duration())
		rows = append(rows, a.muted().Render(branch)+
			a.soft().Render(cutWords(first, inner-tagW))+tag)
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
	// Zeitangabe am Zeilenende: laufend die bisherige, fertig die Gesamt-
	// dauer — so sieht man, wo der Agent am laengsten und am kuerzesten
	// braucht. Der Tag kriegt seinen Platz VOR der Inhaltsteilung ab.
	tag, tagW := a.durTag(t.duration())
	iw := inner - tagW
	if iw < 8 {
		iw = 8
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
				a.textSt().Render(cutCells(name, iw)) + tag
		}
		cmd := t.command
		if cmd == "" {
			cmd = t.text
		}
		label = a.textSt().Render(cutCells(name, iw)) +
			a.textSt().Render(": "+cutWords(joinCommandLines(cmd), iw-6))
	} else {
		label = a.textSt().Render(cutCells(t.name, iw))
		if arg := firstLine(t.text); arg != "" {
			label += a.textSt().Render("(" + cutWords(arg, iw-runewidth.StringWidth(t.name)-2) + ")")
		}
	}
	return a.muted().Render(branch) + a.stateSt(t).Render(sym) + " " + label + tag
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

// workingChildren baut die Pods des Working-on-Knotens: Thinking-Pod
// (, Denk-Tail), je ein -Pod pro laufendem Call, Leerlauf als
// stammbloser Kopf mit Spinner. Jede Kapsel ist in sich geschlossen.
func (a ActivityTree) workingChildren(width int) [][]string {
	var pods [][]string
	// Offenes Thinking laeuft noch.
	if strings.TrimSpace(a.live) != "" {
		tag, tagW := a.durTag(time.Since(a.liveStart))
		pods = append(pods, a.workPod(iconThinking, "Thinking", a.live, tag, tagW, width))
	}
	// Laufende Calls — parallel mehrere moeglich.
	for _, t := range a.tools {
		if !t.running {
			continue
		}
		tag, tagW := a.durTag(t.duration())
		label := t.name
		if label == "" {
			label = "tool"
		}
		body := firstLine(t.text)
		if t.isBash() {
			cmd := t.command
			if cmd == "" {
				cmd = t.text
			}
			// bash: einzeilige Vorschau des zusammengeführten Befehls,
			// am Wortuebergang gekappt (§ 6) — der Volltext lebt im
			// Tool-Calls-Knoten.
			body = cutWords(joinCommandLines(cmd), width-6)
		}
		pods = append(pods, a.workPod(iconTool, label, body, tag, tagW, width))
	}
	// Leerlauf (User-Report: "Working on gar nicht angezeigt, obwohl noch
	// nicht fertig"): Spinner-Zeile mit Turn-Laufzeit; das erste Event
	// ersetzt sie. Stammblos — nichts folgt, nichts wird versprochen.
	if len(pods) == 0 && !a.idleSince.IsZero() {
		tag, _ := a.durTag(time.Since(a.idleSince))
		glyph := spinnerFrames[a.idleFrame%len(spinnerFrames)]
		pods = append(pods, []string{a.muted().Render("╰────── ") +
			a.valida().Render(glyph) + a.muted().Render(" …") + tag})
	}
	return pods
}

// workPod rendert EINE Kapsel: Kopf `╰─┬──── <icon> <label> · <tag>`,
// gewrappte Inhaltszeilen `   ┆ …`, Abschluss `   ╰────`. Ohne Inhalt
// entfaellt der Stamm (┬→─) und die Abschlusszeile — der Kopf ist dann
// die ganze Kapsel, nichts verspricht Folgetext (User-Report: das
// Rahmenzeichen darf nie ins Leere zeigen).
func (a ActivityTree) workPod(icon, label, body, tag string, tagW, width int) []string {
	c := width - 6 // Inhaltszeilen: 3 Einzug + ┆ + Leerzeichen (+Puffer)
	if c < 8 {
		c = 8
	}
	// Label kuerzen, damit der KOPF nie ueber die Breite laeuft:
	// 1 Einzug + 8 Kopf-Chrome + 2 (Icon+Leerzeichen) + 1 (·) + tag.
	lw := width - 12 - tagW
	if lw < 4 {
		lw = 4
	}
	label = cutCells(label, lw)
	head := a.muted().Render("╰─┬──── ") + a.textSt().Render(icon+" "+label)
	if tagW > 0 {
		head += a.muted().Render(" ·") + tag
	}
	rows := render.WrapWords(body, c)
	if len(rows) == 0 || strings.TrimSpace(body) == "" {
		return []string{strings.Replace(head, "┬", "─", 1)}
	}
	// Tail-Prinzip wie der alte Working-on-Blick (§ 7): ohne Platz zeigt
	// der Pod nur die LETZEN fuenf Inhaltszeilen (das Ende der Aktion).
	if len(rows) > 5 {
		rows = rows[len(rows)-5:]
	}
	out := make([]string, 0, len(rows)+2)
	out = append(out, head)
	for _, ln := range rows {
		out = append(out, a.muted().Render("  ┆ ")+a.soft().Render(ln))
	}
	out = append(out, a.muted().Render("  ╰────"))
	return out
}

// fitWork schneidet die Pods aufs Budget: ganze Pods zuerst; der letzte
// sichtbare wird GESCHLOSSEN gekappt (Kopf + Restinhalt + ╰────). Bleibt
// nur eine Zeile, ueberlebt der Kopf ohne Stamm — die Kapsel zeigt nie
// offen ins Leere.
func fitWork(pods [][]string, avail int) []string {
	out := make([]string, 0, avail)
	for _, pod := range pods {
		if avail <= 0 {
			break
		}
		if len(pod) <= avail {
			out = append(out, pod...)
			avail -= len(pod)
			continue
		}
		if avail == 1 {
			out = append(out, strings.Replace(pod[0], "┬", "─", 1))
			break
		}
		out = append(out, pod[0])
		out = append(out, pod[1:avail-1]...)
		out = append(out, pod[len(pod)-1])
		break
	}
	return out
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

// workHeader: Kopfzeile von Working on — bewusst OHNE Faltmarke (der
// Knoten klappt nicht; enter auf Working on tut nichts). Das Lauf-Dot in
// Valida signalisiert: hier ist der Agent gerade dabei.
func (a ActivityTree) workHeader() string {
	return a.valida().Render("●") + " " + a.valida().Render("Working on")
}

// headerRow baut eine Hauptknoten-Zeile: Faltmarke in Valida, Label in der
// Knotenfarbe (Thinking Neon-Pink, Tool Calls Valida). Steht der
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
// Auswahlmodus laeuft GENAU diese beiden ab: Working on ist kein
// Faltknoten und zeigt nur die laufenden Vorgaenge — dort gibt es auch
// keinen Cursor.
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
// 1. Working on, 2. Thinking-/Tool-Kinder werden gekuerzt/gescrollt.
// Abgeschlossene Turns (settled) zeigen Working on nicht mehr. Der
// Auswahlmodus unterstreicht die Kopfzeile des gewaehlten Knotens.
func (a ActivityTree) layout(width, budget int) activityLayout {
	if budget < 1 || width < 9 {
		return activityLayout{}
	}
	aw := width - 1 // 1 Zelle linker Rand

	thinkHdr := a.headerRow(nodeThinking, !a.expandThinking, iconThinking+" Thinking", a.ThinkingCount(), a.valida())
	toolsHdr := a.headerRow(nodeTools, !a.expandTools, iconTool+" Tool Calls", a.ToolCount(), a.valida())

	workPods := a.workingChildren(aw)
	if a.settled {
		// Fertiger Turn: Working on verschwindet KOMPLETT — der Agent
		// arbeitet nicht mehr, die Zusammenfassung (Thinking/Tool Calls)
		// reicht (User-Wunsch).
		workPods = nil
	}
	hasWork := len(workPods) > 0
	workHdr := ""
	if hasWork {
		workHdr = a.workHeader()
	}
	// Keine Textzeilen mehr (die Antwort streamt im Chat, FR-2): Das
	// ganze Budget gehoert den drei Knoten.
	avail := budget

	if avail < 4 {
		// Kein Platz fuer alle Hauptknoten: Working on hat Vorrang vor
		// Thinking/Tool Calls (§ 9); die Reihenfolge bleibt immer
		// Thinking → Tool Calls → Working on. Thinking faellt zuerst weg —
		// sein Cursor ist dann unsichtbar und wird gesnappt. Ohne aktiven
		// Vorgang rueckt Tool Calls nach (Working on ist dann ganz weg).
		var head []string
		var nodes []activityNode
		if hasWork {
			switch avail {
			case 3:
				head = []string{toolsHdr, workHdr}
				nodes = []activityNode{nodeTools, nodeNone}
			case 1, 2:
				head = []string{workHdr}
				nodes = []activityNode{nodeNone}
			}
		} else {
			switch avail {
			case 3:
				head = []string{thinkHdr, toolsHdr}
				nodes = []activityNode{nodeThinking, nodeTools}
			case 1, 2:
				head = []string{toolsHdr}
				nodes = []activityNode{nodeTools}
			}
		}
		return a.pack(head, nodes, nil)
	}
	fixed := 2 // thinkHdr + toolsHdr
	if hasWork {
		fixed = 3
	}
	avail -= fixed
	work := fitWork(workPods, avail) // Pods geschlossen kappen — nie offen
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
	nTool := len(toolRows)
	if nTool > kids {
		nTool = kids
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
	nThink := len(thinkRows)
	if r := kids - nTool; nThink > r {
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
	if hasWork {
		head = append(head, workHdr)
		headNode = append(headNode, nodeNone)
		head = append(head, work...)
		headNode = append(headNode, repeatNode(nodeNone, len(work))...)
	}
	return a.pack(head, headNode, nil)
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

// Settle markiert den Turn als abgeschlossen (Working on verschwindet,
// die Zusammenfassung bleibt).
func (a ActivityTree) Settle() ActivityTree {
	a.settled = true
	return a
}

// Idle armiert die Leerlauf-Anzeige (startTurn, User-Report): Solange der
// laufende Turn kein Kind meldet, zeigt Working on eine Spinner-Zeile mit
// Turn-Laufzeit statt zu verschwinden.
func (a ActivityTree) Idle(since time.Time) ActivityTree {
	a.idleSince = since
	return a
}

// IdleFrame folgt dem Model-Spinner (spinnerTickMsg) — Working on und die
// Statuszeile rotieren synchron.
func (a ActivityTree) IdleFrame(frame int) ActivityTree {
	a.idleFrame = frame
	return a
}
