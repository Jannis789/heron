package tui

import (
	"fmt"
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
	// expandKinds: aufgeklappte Tool-Kind-Knoten (Kind → offen). Die
	// Kategorien sind dynamisch — nur vorhandene Kinds bekommen einen Kopf.
	expandKinds map[string]bool
	// scroll: Zeilen, die im aufgeklappten Bereich eines Kinds nach OBEN
	// ueber das Fenster hinaus sichtbar gemacht werden (0 = Tail).
	scroll map[string]int

	// selCall: die ausgewaehlte callID INNERHALB des Kategorie-Knotens
	// (leer = der Kategorie-Kopf selbst ist selektiert).
	selCall string

	// selThought: die ausgewaehlte Denk-Sektion INNERHALB des Thinking-
	// Knotens (0 = der Kopf selbst, i+1 = Sektion i). Wie selCall haengt
	// die Auswahl an der STRUKTUR, nicht an der Zeilennummer — die
	// Detail-Alertbox zeigt so auch fuer Thoughts den Volltext.
	selThought int

	// sel: der Knoten des Auswahlmodus (nodeNone = Baum nicht selektiert).
	// ↑/↓ laufen Thinking und die Tool-Kind-Knoten ab, enter klappt den
	// Knoten unter dem Cursor auf und wieder zu. Der Cursor haengt an der
	// STRUKTUR, nicht an Zeilennummern — Aufklappen verschiebt die Zeilen
	// darunter.
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
	// Vollstaendige Ausfuehrungsdaten fuer die Detail-Alertbox.
	stdout, stderr string
	live           string
	exit           int
	hasOutput      bool
	running        bool
	failed         bool
	started        time.Time // Beginn des Calls (erster Kontakt)
	ended          time.Time // Ende des Calls (Update mit Done); zero = laeuft
}

// Detail beschreibt EIN selektierbares Element des Activity-Baums
// vollstaendig — Tool-Call ODER Denk-Sektion. Die Detail-Alertbox kennt
// damit nur EINE Form: Jedes auswaehlbare Element (auch eine einzelne
// Thought-Sektion) oeffnet dieselbe Box mit Titel, Kategorie, Status,
// Laufzeit, Metadaten und dem vollstaendigen Inhalt.
type Detail struct {
	Title    string
	Kind     string
	Status   string
	Duration time.Duration
	Meta     []string
	Body     string
}

// detail baut die Detailbeschreibung eines Tool-Calls: alles, was dort
// wirklich ausgefuehrt wurde (Kommando, stdout/stderr, exit, live).
func (t activityTool) detail() Detail {
	title := t.name
	if title == "" {
		title = kindLabel(t.kindOf())
	}
	status := "✓ fertig"
	switch {
	case t.running:
		status = "● läuft"
	case t.failed:
		status = "✗ Fehler"
	}
	d := Detail{Title: title, Kind: kindLabel(t.kindOf()), Status: status,
		Duration: t.duration()}
	d.Meta = append(d.Meta, "call "+t.callID)
	var b strings.Builder
	if t.command != "" {
		b.WriteString("$ " + t.command + "\n\n")
	}
	if t.stdout != "" {
		b.WriteString("── stdout ──\n" + t.stdout + "\n")
	}
	if t.stderr != "" {
		b.WriteString("── stderr ──\n" + t.stderr + "\n")
	}
	if t.hasOutput {
		fmt.Fprintf(&b, "── exit: %d ──\n", t.exit)
	}
	if t.live != "" {
		b.WriteString("── live ──\n" + t.live + "\n")
	}
	if b.Len() == 0 {
		b.WriteString(t.text)
	}
	d.Body = strings.TrimRight(b.String(), "\n")
	return d
}

// detail baut die Detailbeschreibung EINER Denk-Sektion: vollstaendiger
// Denktext plus Laufzeit und Status (offene Sektion = laeuft noch).
func (s thoughtSection) detail() Detail {
	status := "✓ fertig"
	if s.ended.IsZero() {
		status = "● läuft"
	}
	return Detail{Title: "Thinking", Kind: "Thinking", Status: status,
		Duration: s.duration(), Body: s.text}
}

// allThoughts liefert alle Denk-Sektionen in Anzeige-Reihenfolge —
// geschlossene zuerst, die noch offene (live) zuletzt.
func (a ActivityTree) allThoughts() []thoughtSection {
	secs := a.thoughts
	if strings.TrimSpace(a.live) != "" {
		secs = append(append([]thoughtSection(nil), secs...),
			thoughtSection{text: a.live, started: a.liveStart})
	}
	return secs
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

// snapSelKind haelt den Cursor auf einem existierenden Kind-Knoten: Der
// Platzhalter "Tool Calls (0)" verschwindet, sobald der erste echte Call
// eintrifft — der Cursor wandert dann auf den ersten sichtbaren Kind-Knoten.
func (a ActivityTree) snapSelKind() ActivityTree {
	if k, ok := kindOfNode(a.sel); ok && a.KindCount(k) == 0 {
		if ks := a.kinds(); len(ks) > 0 {
			a.sel = toolNode(ks[0])
		}
	}
	return a
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
		stdout: u.Stdout, stderr: u.Stderr, live: u.Live,
		exit: u.Exit, hasOutput: u.HasOutput,
		started: now, ended: now}
	if !u.Done {
		entry.ended = time.Time{} // laeuft noch — die Uhr tickt weiter
	}
	for i := range a.tools {
		if a.tools[i].callID == u.CallID {
			// In-place-Update: der Startzeitpunkt bleibt — die Laufzeit
			// misst den GESAMTEN Call, nicht das letzte Update.
			entry.started = a.tools[i].started
			// In-place-Update: fehlende Anzeige-Felder aus dem Start-Frame
			// behalten — goose schickt im finalen tool_call_update oft nur
			// status/content/rawOutput, ohne Name/Kind/Kommando.
			prev := a.tools[i]
			if entry.name == "" {
				entry.name = prev.name
			}
			if entry.kind == "" {
				entry.kind = prev.kind
			}
			if entry.command == "" {
				entry.command = prev.command
			}
			if entry.text == "" {
				entry.text = prev.text
			}
			if entry.stdout == "" {
				entry.stdout = prev.stdout
			}
			if entry.stderr == "" {
				entry.stderr = prev.stderr
			}
			if entry.live == "" {
				entry.live = prev.live
			}
			if !entry.hasOutput {
				entry.exit, entry.hasOutput = prev.exit, prev.hasOutput
			}
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
			return a.snapSelKind()
		}
	}
	a.tools = append(a.tools, entry)
	return a.snapSelKind()
}

// ToggleThinking klappt den Thinking-Knoten um.
func (a ActivityTree) ToggleThinking() ActivityTree {
	a.expandThinking = !a.expandThinking
	if !a.expandThinking && a.sel == nodeThinking {
		a.selThought = 0
	}
	return a
}

// ToggleKind klappt den Knoten EINES Tool-Kinds um; Zuklappen setzt
// dessen Scroll zurueck (aufgeklappt startet der Blick am aktuellsten Call).
func (a ActivityTree) ToggleKind(kind string) ActivityTree {
	if kind == "" {
		return a
	}
	m := make(map[string]bool, len(a.expandKinds)+1)
	for k, v := range a.expandKinds {
		m[k] = v
	}
	m[kind] = !m[kind]
	a.expandKinds = m
	if !m[kind] && a.selCall != "" {
		if k, ok := kindOfNode(a.sel); ok && k == kind {
			a.selCall = ""
		}
	}
	if !m[kind] {
		s := make(map[string]int, len(a.scroll))
		for k, v := range a.scroll {
			s[k] = v
		}
		s[kind] = 0
		a.scroll = s
	}
	return a
}

// ToggleAllTools klappt ALLE Tool-Kind-Knoten gemeinsam um — offen, falls
// noch nicht alle offen sind; sonst zu.
func (a ActivityTree) ToggleAllTools() ActivityTree {
	ks := a.kinds()
	if len(ks) == 0 {
		return a
	}
	open := false
	for _, k := range ks {
		if !a.expanded(k) {
			open = true
			break
		}
	}
	m := make(map[string]bool, len(ks))
	if open {
		for _, k := range ks {
			m[k] = true
		}
	}
	a.expandKinds = m
	if !open {
		a.scroll = nil
	}
	return a
}

// ToggleTools bleibt als Alias fuer den Gesamt-Schalter (Tests/Demo).
func (a ActivityTree) ToggleTools() ActivityTree { return a.ToggleAllTools() }

// expanded meldet, ob der Knoten eines Kinds offen ist.
func (a ActivityTree) expanded(kind string) bool { return a.expandKinds[kind] }

// Wheel scrollt den aufgeklappten Bereich des gewaehlten Kinds (dir: +1
// aelterer Inhalt sichtbar, -1 zurueck zum Tail); ohne Auswahl den zuletzt
// aufgeklappten Kind-Knoten. Ohne offenen Kind-Knoten ist das Rad ein No-Op.
func (a ActivityTree) Wheel(dir int) ActivityTree {
	kind := ""
	if k, ok := kindOfNode(a.sel); ok && a.expanded(k) {
		kind = k
	} else {
		ks := a.kinds()
		for i := len(ks) - 1; i >= 0; i-- {
			if a.expanded(ks[i]) {
				kind = ks[i]
				break
			}
		}
	}
	if kind == "" {
		return a
	}
	s := make(map[string]int, len(a.scroll)+1)
	for k, v := range a.scroll {
		s[k] = v
	}
	s[kind] += dir
	if s[kind] < 0 {
		s[kind] = 0
	}
	if rows, _ := a.kindChildren(kind, 80); s[kind] > len(rows)-1 {
		s[kind] = len(rows) - 1
	}
	if s[kind] < 0 {
		s[kind] = 0
	}
	a.scroll = s
	return a
}

// callsOf liefert die Calls eines Kinds in Reihenfolge.
func (a ActivityTree) callsOf(kind string) []activityTool {
	var out []activityTool
	for _, t := range a.tools {
		if t.kindOf() == kind {
			out = append(out, t)
		}
	}
	return out
}

// SelectedCall liefert den Call unter dem Cursor (Detail-Alertbox).
func (a ActivityTree) SelectedCall() (activityTool, bool) {
	if a.selCall == "" {
		return activityTool{}, false
	}
	for _, t := range a.tools {
		if t.callID == a.selCall {
			return t, true
		}
	}
	return activityTool{}, false
}

// SelectedDetail liefert das Detail des ausgewaehlten Elements — ein
// Tool-Call oder eine Thought-Sektion. Nur ein KOPF (Kategorie bzw.
// Thinking) hat kein Detail: enter klappt ihn dann auf/zu.
func (a ActivityTree) SelectedDetail() (Detail, bool) {
	if a.selCall != "" {
		if t, ok := a.SelectedCall(); ok {
			return t.detail(), true
		}
	}
	if a.sel == nodeThinking && a.selThought > 0 {
		secs := a.allThoughts()
		if i := a.selThought - 1; i >= 0 && i < len(secs) {
			return secs[i].detail(), true
		}
	}
	return Detail{}, false
}

// DetailSelected: der Cursor steht auf einem Call oder einer Thought-
// Sektion, nicht auf einem Kopf.
func (a ActivityTree) DetailSelected() bool {
	_, ok := a.SelectedDetail()
	return ok
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

// KindCount: Zahl der Calls eines Kinds.
func (a ActivityTree) KindCount(kind string) int {
	n := 0
	for _, t := range a.tools {
		if t.kindOf() == kind {
			n++
		}
	}
	return n
}

// kindRunning meldet, ob in diesem Kind gerade ein Call laeuft.
func (a ActivityTree) kindRunning(kind string) bool {
	for _, t := range a.tools {
		if t.running && t.kindOf() == kind {
			return true
		}
	}
	return false
}

// kinds liefert die vorhandenen Tool-Kinds in ACP-Reihenfolge (nur Kinds
// mit mindestens einem Call). Die Reihenfolge ist stabil — sie bestimmt die
// Anzeige-Reihenfolge der Knoten.
func (a ActivityTree) kinds() []string {
	counts := make(map[string]int, len(a.tools))
	for _, t := range a.tools {
		counts[t.kindOf()]++
	}
	var out []string
	for _, k := range toolKindOrder {
		if counts[k] > 0 {
			out = append(out, k)
			delete(counts, k)
		}
	}
	// Unbekannte Kinds (sollte nicht vorkommen) hinten anhaengen.
	for k := range counts {
		out = append(out, k)
	}
	return out
}

// childOrder: aufgeklappte Kinds werden in dieser Reihenfolge mit Budget
// versorgt — laufende Calls zuerst, dann die uebrigen; Thinking zuletzt.
func (a ActivityTree) kindChildOrder(kinds []string) []string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		if a.kindRunning(k) {
			out = append(out, k)
		}
	}
	for _, k := range kinds {
		if !a.kindRunning(k) {
			out = append(out, k)
		}
	}
	return out
}

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

// ---- Kategorien --------------------------------------------------------------

// toolKindOrder ist die ACP-Reihenfolge der Kategorien — sie bestimmt die
// stabile Anzeige-Reihenfolge der Kind-Knoten.
var toolKindOrder = []string{
	"read", "edit", "delete", "move", "search", "execute", "think", "fetch", "switch_mode", "other",
}

// kindOf liefert die effektive ACP-Kategorie eines Calls: das von goose
// gelieferte kind, sonst aus dem Tool-Namen abgeleitet (goose sendet kein
// kind — der Name steckt in _meta.goose.toolCall.toolName).
func (t activityTool) kindOf() string {
	if k := normalizeKind(t.kind); k != "" {
		return k
	}
	return inferKind(t.name)
}

// normalizeKind mappt beliebige Kind-Schreibweisen auf die ACP-Kategorien.
func normalizeKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "":
		return ""
	case "read", "edit", "delete", "move", "search", "execute", "think", "fetch", "switch_mode", "other":
		return k
	}
	return "other"
}

// inferKind leitet die Kategorie aus dem Tool-Namen ab (Fallback fuer
// Legacy-/Demo-Frames ohne kind und fuer goose-Tools, deren Name die
// Bedeutung traegt: read_file, edit_file, bash, …).
func inferKind(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "read_image"), strings.Contains(n, "read_file"), n == "read":
		return "read"
	case strings.Contains(n, "edit"), strings.Contains(n, "write"),
		strings.Contains(n, "apply_patch"), n == "text_editor", n == "editor":
		return "edit"
	case strings.Contains(n, "grep"), strings.Contains(n, "search"),
		strings.Contains(n, "find"), n == "tree", strings.Contains(n, "list_dir"):
		return "search"
	case strings.Contains(n, "fetch"), strings.Contains(n, "curl"),
		strings.Contains(n, "http"):
		return "fetch"
	case strings.Contains(n, "delete"), strings.Contains(n, "remove"), n == "rm":
		return "delete"
	case strings.Contains(n, "move"), strings.Contains(n, "rename"):
		return "move"
	case n == "think", strings.Contains(n, "thinking"):
		return "think"
	}
	switch {
	case n == "bash", n == "shell", strings.HasPrefix(n, "bash "),
		strings.Contains(n, "execute"), strings.Contains(n, "terminal"):
		return "execute"
	}
	return "other"
}

// kindLabel: lesbares Label eines Kinds.
func kindLabel(kind string) string {
	switch kind {
	case "switch_mode":
		return "Switch Mode"
	case "other", "":
		return "Other"
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

// kindIcon: Nerd-Font-Icon (Octicons, BMP F4xx) eines Kinds.
func kindIcon(kind string) string {
	switch kind {
	case "read":
		return iconRead
	case "edit":
		return iconEdit
	case "delete":
		return iconDelete
	case "move":
		return iconMove
	case "search":
		return iconSearch
	case "execute":
		return iconTool
	case "think":
		return iconThinking
	case "fetch":
		return iconFetch
	case "switch_mode":
		return iconSwitch
	}
	return iconOther
}

// ---- Prädikate ---------------------------------------------------------------

// isBash: der Call zeigt einen Shell-Befehl (ACP execute-Kategorie oder
// bash/shell-Name) — sein Befehl bekommt das mehrzeilige Detail-Rendering
// (§ 6).
func (t activityTool) isBash() bool {
	if t.kindOf() == "execute" {
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
//
// actGutter ist der FESTE Auswahl-Steg hinter den Rahmenzeichen: der
// Cursor-Pfeil ▶ spawnt dort (2 Zellen, immer reserviert), er ERSETZT
// niemals `├─`/`└─` — sonst verliert die Zeile ihre Baumstruktur und
// beim Selektieren springt der Inhalt (User-Wahl, 22.11.).
const (
	actChild  = "│  ├─ "
	actLast   = "│  └─ "
	actCont   = "│     "
	actGutter = "  "
	actCursor = "▶ "
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

	// Kind-Icons: durchgaengig Octicons — EINE visuelle Familie im Umriss-
	// Stil, passend zu Thinking/Tool Calls, statt eines Mix aus gefuellten
	// FontAwesome-Glyphen: Buch/Lupe/Zahnrad sind in Zellgroesse nahezu
	// gleiche Blobs (User-Report: Search und Other "sehen wie Read aus";
	// Pixel-Overlap IoU 0.55-0.57, die Octicons darunter bei 0.26-0.37 —
	// gegen die installierte FiraCode NF Regular UND Mono gemessen). Alle
	// Codepoints in der F400+-Range, die ueber alle Nerd-Font-Versionen
	// stabil ist, via cmap gegen beide Font-Varianten verifiziert.
	iconRead   = "\uF405" //  oct-book
	iconEdit   = "\uF448" //  oct-pencil
	iconDelete = "\uF48E" //  oct-trash
	iconMove   = "\uF4D5" //  oct-file_moved
	iconSearch = "\uF422" //  oct-search
	iconFetch  = "\uF409" //  oct-download
	iconSwitch = "\uF46A" //  oct-sync
	iconOther  = "\uF425" //  oct-tools
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
	inner := width - 8 // ├─ (6) + Auswahl-Steg (2)
	if inner < 8 {
		inner = 8
	}
	secs := a.allThoughts()
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
		gutter := actGutter
		if a.sel == nodeThinking && a.selThought == i+1 {
			gutter = actCursor
		}
		tag, tagW := a.durTag(sec.duration())
		rows = append(rows, a.muted().Render(branch+gutter)+
			a.soft().Render(cutWords(first, inner-tagW))+tag)
	}
	return rows
}

// toolRow baut EINE Historienzeile: Zustandssymbol + Call-Label.
func (a ActivityTree) toolRow(t activityTool, last bool, width int, selected bool) string {
	branch := actChild
	if last {
		branch = actLast
	}
	gutter := actGutter
	if selected {
		gutter = actCursor
	}
	sym := "✓"
	if t.running {
		sym = "●"
	} else if t.failed {
		sym = "✗"
	}
	var label string
	inner := width - 11 // │  ├─ + Auswahl-Steg + Symbol + Leerzeichen + Puffer
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
			return a.muted().Render(branch+gutter) + a.stateSt(t).Render(sym) + " " +
				a.textSt().Render(cutCells(name, iw)) + tag
		}
		cmd := t.command
		if cmd == "" {
			cmd = t.text
		}
		label = a.textSt().Render(cutCells(name, iw)) +
			a.textSt().Render(": "+cutWords(joinCommandLines(cmd), iw-6))
	} else {
		name := t.name
		if name == "" {
			name = kindLabel(t.kindOf())
		}
		label = a.textSt().Render(cutCells(name, iw))
		// Nur ein BENANNTES Tool bekommt die Argument-Vorschau; sonst
		// wuerde roher Output als "tool(insgesamt 14892)" erscheinen.
		if arg := firstLine(t.text); arg != "" && t.name != "" {
			label += a.textSt().Render("(" + cutWords(arg, iw-runewidth.StringWidth(name)-2) + ")")
		}
	}
	return a.muted().Render(branch+gutter) + a.stateSt(t).Render(sym) + " " + label + tag
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
func (a ActivityTree) kindChildren(kind string, width int) (rows []string, lastStart int) {
	var calls []activityTool
	for _, t := range a.tools {
		if t.kindOf() == kind {
			calls = append(calls, t)
		}
	}
	for i, t := range calls {
		last := i == len(calls)-1
		lastStart = len(rows)
		rows = append(rows, a.toolRow(t, last, width, a.selCall != "" && t.callID == a.selCall))
		if t.running {
			rows = append(rows, a.toolDetail(t, width)...)
		}
	}
	return rows, lastStart
}

// windowRows schneidet die Kind-Historie aufs Budget: Tail-Fenster,
// Scroll-Offset und Anker am Kopf des LETZTEN Calls (er startet nie
// mitten im Detail).
func windowRows(rows []string, budget, scroll, lastStart int) []string {
	if budget <= 0 || len(rows) == 0 {
		return nil
	}
	n := len(rows)
	if n > budget {
		n = budget
	}
	bottom := len(rows) - scroll
	if bottom < n {
		bottom = n
	}
	if bottom > len(rows) {
		bottom = len(rows)
	}
	top := bottom - n
	if top < 0 {
		top = 0
	}
	if scroll == 0 && top > lastStart {
		top = lastStart
		bottom = top + n
		if bottom > len(rows) {
			bottom = len(rows)
		}
	}
	return rows[top:bottom]
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
			label = kindLabel(t.kindOf())
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
		pods = append(pods, a.workPod(kindIcon(t.kindOf()), label, body, tag, tagW, width))
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

// activityNode benennt die aufklappbaren Hauptknoten des Baums: Thinking
// und je einen Knoten pro vorhandenem Tool-Kind. Working on ist kein
// Faltknoten und zeigt nur die laufenden Vorgaenge — dort gibt es auch
// keinen Cursor.
type activityNode string

const (
	nodeNone     activityNode = ""
	nodeThinking activityNode = "thinking"
)

// toolNode ist der Knoten eines Tool-Kinds ("tool:read", "tool:execute", …).
func toolNode(kind string) activityNode { return activityNode("tool:" + kind) }

// kindOfNode liest den Kind-Namen aus einem Tool-Knoten.
func kindOfNode(n activityNode) (string, bool) {
	s := string(n)
	if strings.HasPrefix(s, "tool:") {
		return strings.TrimPrefix(s, "tool:"), true
	}
	return "", false
}

// activityHeader ist eine Kopfzeile samt zugehoerigem Knoten.
type activityHeader struct {
	node activityNode
	row  string
}

// headerRank: Prioritaet beim Kuerzen der Kopfzeilen — laufende Kinds
// zuerst, dann sonstige Kinds (groessere zuerst), Thinking zuletzt.
func (a ActivityTree) headerRank(h activityHeader) (int, int) {
	if h.node == nodeThinking {
		return 2, -a.ThinkingCount()
	}
	k, _ := kindOfNode(h.node)
	r := 1
	if a.kindRunning(k) {
		r = 0
	}
	return r, -a.KindCount(k)
}

// pickHeaders waehlt n Kopfzeilen nach headerRank und gibt sie in der
// urspruenglichen Anzeige-Reihenfolge zurueck.
func (a ActivityTree) pickHeaders(hdrs []activityHeader, n int) []activityHeader {
	if n <= 0 {
		return nil
	}
	if n >= len(hdrs) {
		return hdrs
	}
	picked := make([]bool, len(hdrs))
	for c := 0; c < n; c++ {
		best := -1
		br, bc := 0, 0
		for i, h := range hdrs {
			if picked[i] {
				continue
			}
			r, cnt := a.headerRank(h)
			if best < 0 || r < br || (r == br && cnt < bc) {
				best, br, bc = i, r, cnt
			}
		}
		if best < 0 {
			break
		}
		picked[best] = true
	}
	out := make([]activityHeader, 0, n)
	for i, h := range hdrs {
		if picked[i] {
			out = append(out, h)
		}
	}
	return out
}

// activityLayout ist das gerenderte Bild samt Struktur fuer den
// Auswahlmodus: rowNode ordnet jeder Zeile ihren Hauptknoten zu
// (nodeNone = Working on, Trennlinie, Text). Der Cursor wandert an
// STRUKTUR, nicht an Zeilennummern — ein aufgeklappter Knoten verschiebt
// die Zeilen darunter, ohne dass enter das Ziel verliert.
type activityLayout struct {
	rows    []string
	rowNode []activityNode
	// nodes: die sichtbaren Hauptknoten in Anzeige-Reihenfolge (fuer ↑/↓,
	// snap und Klick-Zuordnung).
	nodes []activityNode
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

	// Working on zuerst bestimmen: Header + Pods haben Vorrang.
	workPods := a.workingChildren(aw)
	if a.settled {
		// Fertiger Turn: Working on verschwindet KOMPLETT — der Agent
		// arbeitet nicht mehr, die Zusammenfassung reicht (User-Wunsch).
		workPods = nil
	}
	hasWork := len(workPods) > 0
	workHdr := ""
	if hasWork {
		workHdr = a.workHeader()
	}

	// Kopfzeilen in Anzeige-Reihenfolge: Thinking, dann die vorhandenen
	// Tool-Kinds (ACP-Reihenfolge). Ohne Calls bleibt der vertraute
	// Platzhalter "Tool Calls (0)".
	var hdrs []activityHeader
	// Thinking nur zeigen, wenn es wirklich Denk-Sektionen gibt — kein
	// leerer "Thinking (0)"-Kopf (User-Wunsch).
	if a.ThinkingCount() > 0 {
		hdrs = append(hdrs, activityHeader{nodeThinking,
			a.headerRow(nodeThinking, !a.expandThinking, iconThinking+" Thinking", a.ThinkingCount(), a.valida())})
	}
	kinds := a.kinds()
	if len(kinds) == 0 {
		n := toolNode("other")
		hdrs = append(hdrs, activityHeader{n,
			a.headerRow(n, true, iconTool+" Tool Calls", 0, a.valida())})
	} else {
		for _, k := range kinds {
			n := toolNode(k)
			hdrs = append(hdrs, activityHeader{n,
				a.headerRow(n, !a.expanded(k), kindIcon(k)+" "+kindLabel(k), a.KindCount(k), a.valida())})
		}
	}

	// Bei knappem Budget weichen Kopfzeilen (Thinking zuerst, dann Kinds
	// ohne laufenden Call) — Working on bleibt sichtbar (§ 9).
	hdrBudget := budget
	if hasWork {
		// Working-on-Kopf + mindestens eine Pod-Zeile reservieren.
		hdrBudget -= 2
	}
	if hdrBudget < 0 {
		hdrBudget = 0
	}
	sel := hdrs
	if len(sel) > hdrBudget {
		sel = a.pickHeaders(hdrs, hdrBudget)
	}

	// Working-on-Pods fuellen den Rest; Kinder bekommen, was uebrig bleibt.
	remaining := budget - len(sel)
	var work []string
	if hasWork {
		work = fitWork(workPods, remaining-1)
		remaining -= 1 + len(work)
	}
	if remaining < 0 {
		remaining = 0
	}

	// Kinder in Prioritaets-Reihenfolge verteilen (laufende Kinds zuerst,
	// Thinking zuletzt) — gerendert wird wieder in Anzeige-Reihenfolge.
	kindRows := make(map[string][]string, len(kinds))
	for _, k := range a.kindChildOrder(kinds) {
		if !a.expanded(k) || remaining <= 0 {
			continue
		}
		rows, lastStart := a.kindChildren(k, aw)
		rows = windowRows(rows, remaining, a.scroll[k], lastStart)
		kindRows[k] = rows
		remaining -= len(rows)
	}
	var thinkRows []string
	if a.expandThinking && remaining > 0 {
		all := a.thinkingChildren(aw)
		n := len(all)
		if n > remaining {
			n = remaining
		}
		thinkRows = all[len(all)-n:]
	}

	head := make([]string, 0, len(sel)+len(thinkRows)+len(work))
	headNode := make([]activityNode, 0, cap(head))
	for _, h := range sel {
		head = append(head, h.row)
		headNode = append(headNode, h.node)
		if h.node == nodeThinking {
			head = append(head, thinkRows...)
			headNode = append(headNode, repeatNode(nodeThinking, len(thinkRows))...)
			continue
		}
		if k, ok := kindOfNode(h.node); ok {
			rows := kindRows[k]
			head = append(head, rows...)
			headNode = append(headNode, repeatNode(h.node, len(rows))...)
		}
	}
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
	seen := make(map[activityNode]bool, len(nodes))
	visible := make([]activityNode, 0, len(nodes))
	for _, n := range nodes {
		if n != nodeNone && !seen[n] {
			seen[n] = true
			visible = append(visible, n)
		}
	}
	return activityLayout{rows: rows, rowNode: nodes, nodes: visible}
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
	for _, n := range l.nodes {
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
	a.selCall = ""
	a.selThought = 0
	return a
}

// Enter klappt den Knoten unter dem Cursor auf — und mit demselben Druck
// wieder zu. Die beiden Knoten sind unabhaengig; kein Modifier, keine
// zweite Taste. Working on kennt kein Zu und bleibt stumm.
func (a ActivityTree) Enter() ActivityTree {
	switch a.sel {
	case nodeThinking:
		// Eine ausgewaehlte Thought-Sektion oeffnet die Detail-Alertbox
		// im Model; nur der Thinking-Kopf klappt auf/zu.
		if a.selThought != 0 {
			return a
		}
		return a.ToggleThinking()
	}
	if k, ok := kindOfNode(a.sel); ok {
		// Ein ausgewaehlter CALL oeffnet die Detail-Alertbox im Model;
		// nur der Kategorie-Kopf klappt auf/zu.
		if a.selCall != "" {
			return a
		}
		return a.ToggleKind(k)
	}
	return a
}

// Move laeuft die Knoten mit ↑/↓ ab; an den Enden bleibt der Cursor stehen
// (der Baum ist ein kurzes Menue, kein Karussell). Knoten, die im Budget
// nicht sichtbar sind, werden uebersprungen.
func (a ActivityTree) Move(dir, width, budget int) ActivityTree {
	l := a.layout(width, budget)
	type item struct {
		node    activityNode
		call    string
		thought int
	}
	var items []item
	for _, n := range l.nodes {
		items = append(items, item{node: n})
		if n == nodeThinking && a.expandThinking {
			for i := range a.allThoughts() {
				items = append(items, item{node: n, thought: i + 1})
			}
			continue
		}
		if k, ok := kindOfNode(n); ok && a.expanded(k) {
			for _, t := range a.callsOf(k) {
				items = append(items, item{node: n, call: t.callID})
			}
		}
	}
	if len(items) == 0 {
		return a
	}
	cur := -1
	for i, it := range items {
		if it.node == a.sel && it.call == a.selCall && it.thought == a.selThought {
			cur = i
			break
		}
	}
	if cur < 0 {
		// (Noch) kein sichtbarer Cursor: in Laufrichtung aufsetzen.
		if dir < 0 {
			cur = len(items) - 1
		} else {
			cur = 0
		}
	} else if cur += dir; cur < 0 {
		cur = 0
	} else if cur >= len(items) {
		cur = len(items) - 1
	}
	a.sel, a.selCall, a.selThought = items[cur].node, items[cur].call, items[cur].thought
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
			a.selCall, a.selThought = "", 0
			return a.Enter()
		}
		// Zeile im aufgeklappten Thinking: die einzelne Denk-Sektion
		// unter dem Klick selektieren (enter oeffnet dann ihr Detail).
		if n == nodeThinking && a.expandThinking {
			if idx := row - l.headRow(n); idx >= 1 && idx <= len(a.allThoughts()) {
				a.selCall, a.selThought = "", idx
				return a
			}
		}
		// Zeile in einem Kind-Knoten: eine Call-Auswahl einer anderen
		// Zeile faellt weg, der Klick waehlt den Knoten.
		a.selThought = 0
		a.selCall = ""
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
	for _, n := range l.nodes {
		if h := l.headRow(n); h >= 0 && h < row {
			found = n
		}
	}
	if found == nodeNone && len(l.nodes) > 0 {
		if h := l.headRow(l.nodes[0]); h >= 0 {
			found = l.nodes[0]
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
