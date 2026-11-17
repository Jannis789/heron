package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// toolCollapsed ist die Anzahl Zeilen, die eine zugeklappte Tool-Karte vom
// ENDE des Inhalts zeigt (das Resultat lebt unten — wie beim ThinkBlock).
const toolCollapsed = 3

// ToolBlock ist die Karte einer Agent-Aktivitaet im Chat: Tool-Call ODER
// Command (der Agent laeuft seine Tools, bash-Kommandos inklusive — heron
// zeigt nur an, FR-2/FR-31). Aufbau wie ThinkBlock: Kopfzeile mit Zustand
// (… laeuft / ✓ fertig), kollapsierbarer Inhalt (zugeklappt die letzten
// Zeilen), Footer-Klappe. Die Karte ist ein volles Ketten-Segment: tab/
// Pfeile fokussieren sie, die Shift-Auswahl (FR-42/43) schliesst sie ein,
// der Zeichen-Modus adressiert ihren Inhalt buchstabenweise.
type ToolBlock struct {
	id     int
	callID string // ACP toolCallId — Identitaet fuer In-place-Updates
	title  string
	// kind ist die ACP-Tool-Kategorie (execute/fetch/think/edit/create/
	// other) — sie waehlt das Icon der Kopfzeile.
	kind string
	// content ist der Text des Calls, soweit der Stream ihn liefert
	// (Kommando, Argumente, Ausgabe) — Legacy-/Demo-Pfad. Die echten
	// goose-Frames liefern Struktur: command (rawInput) als $-Zeile,
	// stdout/stderr/exit (rawOutput) und live als akkumulierter Live-Output
	// waehrend des Laufs. bodyLines setzt das zusammen.
	content string
	command string
	stdout  string
	stderr  string
	exit    int
	hasOut  bool
	live    string
	// body caches the assembled body lines: Height()/View()/stateLabel()
	// fragen sie mehrfach pro Frame ab — ein 20k-Zeilen-Output wurde sonst
	// pro Aufruf neu gesplittet.
	body     []string
	running  bool
	failed   bool
	expanded bool
	focused  bool
	marked   bool
	// stale: Karte ist Teil der VERGANGENEN Auswahl (FR-43) — Rahmen in
	// der verblichenen Sekundaer-Farbe.
	stale bool
	// Stack-Joining (nur auf RENDER-KOPIEN in BlockList.stack gesetzt):
	// wie beim ThinkBlock — joinTop ohne ┌─┐, joinAsDivider mit ├─┤-Boden.
	joinTop       bool
	joinAsDivider bool
	dividerLabel  string
	// dividerSel: die Folgekarte (deren Caption in der Trennlinie lebt)
	// ist ausgewaehlt — das Label leuchtet neon wie eine Kopfzeile.
	dividerSel bool
	th         *render.Theme
	copyFn     CopyFunc
	// Hover ist ZIELGENAU wie beim ThinkBlock: nur die Kopfzeile
	// highlightet — die Falt-Marke (▸/▾) lebt im Kopf, der Footer ist
	// eine reine Rahmenlinie (kein "+ show more" mehr).
	hoverHead bool
}

// ToolData bündelt die Anzeige-Daten eines Tool-Call-Stands: Identität
// (callID), Kopf (title/kind → Icon) und den strukturierten Koerper aus den
// echten goose-Frames — Command aus rawInput, Stdout/Stderr/Exit aus
// rawOutput (HasOutput markiert das finale Update), Live der akkumulierte
// Live-Output. Text ist der generische Fallback (Legacy/Demo).
type ToolData struct {
	CallID    string
	Title     string
	Kind      string
	Text      string
	Command   string
	Stdout    string
	Stderr    string
	Exit      int
	HasOutput bool
	Live      string
	Running   bool
	Failed    bool
}

// NewToolBlock baut die Karte; Append vergibt die stabile Blocknummer.
// kind ist die ACP-Tool-Kategorie (Icon), failed der Fehler-Zustand (✗).
func NewToolBlock(d ToolData, th *render.Theme, copyFn CopyFunc) ToolBlock {
	b := ToolBlock{id: -1, callID: d.CallID, title: d.Title, kind: d.Kind,
		content: d.Text, command: d.Command, stdout: d.Stdout, stderr: d.Stderr,
		exit: d.Exit, hasOut: d.HasOutput, live: d.Live,
		running: d.Running, failed: d.Failed, th: th, copyFn: copyFn}
	b.body = assembleToolBody(b)
	return b
}

// kindGlyph waehlt das Kopf-Icon nach ACP-Tool-Kategorie: $ execute (Shell),
// ↯ fetch, ✎ edit, ✚ create, ◦ think — unbekannte/leere Kategorien bleiben
// beim generischen Schraubenschluessel.
func kindGlyph(kind string) string {
	switch kind {
	case "execute":
		return "$"
	case "fetch":
		return "↯"
	case "edit":
		return "✎"
	case "create":
		return "✚"
	case "think":
		return "◦"
	}
	return "🔧"
}

// bodyLines setzt den Karten-Koerper zusammen: das Kommando als $-Zeile,
// dahinter das Resultat — strukturiert (stdout, ⚠-Zeilen fuer stderr,
// exit-N bei Fehlschlag) oder, falls goose keine Struktur lieferte, der
// generische Text (Legacy/Demo). Waehrend des Laufs zeigt live den bisher
// gestreamten Output.
func (b ToolBlock) bodyLines() []string {
	if b.body != nil {
		return b.body
	}
	return assembleToolBody(b)
}

// assembleToolBody baut die Body-Zeilen der Karte (einmal je Aufbau).
func assembleToolBody(b ToolBlock) []string {
	var out []string
	if b.command != "" {
		out = append(out, "$ "+b.command)
	}
	switch {
	case b.hasOut:
		n := len(out) // Zeilen VOR dem Resultat (nur die $-Zeile, falls da)
		out = append(out, bodyText(b.stdout)...)
		for _, ln := range bodyText(b.stderr) {
			out = append(out, "⚠ "+ln)
		}
		if b.exit != 0 {
			out = append(out, fmt.Sprintf("exit %d", b.exit))
		}
		if len(out) == n { // rawOutput komplett leer (goose: "(no output)")
			out = append(out, "(keine Ausgabe)")
		}
	case b.live != "":
		out = append(out, bodyText(b.live)...)
	case b.content != "":
		out = append(out, bodyText(b.content)...)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// bodyText splittet einen Roh-Text in getrimmte Zeilen.
func bodyText(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// wrappedBody wrappt den Inhalt an der Innenbreite (Wortgrenzen, FR-30-Geist).
func (b ToolBlock) wrappedBody(inner int) []string {
	return wrapSourceLines(b.bodyLines(), inner)
}

// visibleRows: alle Zeilen aufgeklappt, sonst die LETZTEN toolCollapsed
// (das Resultat des Calls lebt am Ende) — die $-Kommandozeile bleibt
// gepinnt sichtbar, sie identifiziert den Call.
//
// Zugeklappt wird nur der SCHLUSS gewrappt: ein langer Output (ls -R,
// Build-Log) wurde vorher in jedem Frame komplett umbrochen — der
// groesste Einzelposten im Render-Pfad (User-Report: "laggy bei grossem
// Chat-Verlauf").
func (b ToolBlock) visibleRows(width int) []string {
	inner := codeInnerWidth(width)
	src := b.bodyLines()
	if b.expanded || !sourceExceeds(src, inner, toolCollapsed) {
		return wrapSourceLines(src, inner)
	}
	body := src
	cmdPinned := false
	if b.command != "" && len(src) > 0 && strings.HasPrefix(src[0], "$ ") {
		body, cmdPinned = src[1:], true
	}
	tail := wrapSourceTail(body, inner, toolCollapsed)
	if cmdPinned {
		if head := render.WrapWords("$ "+b.command, inner); len(head) > 0 {
			return append([]string{head[0]}, tail...)
		}
	}
	return tail
}

func (b ToolBlock) bodyRows(width int) int { return len(b.visibleRows(width)) }

func (b ToolBlock) Height(width int) int { return 1 + b.bodyRows(width) + 1 }

// stateLabel: Kopf-Caption der Karte — Icon, Titel, Zustand, Falt-Marke
// (▸ zugeklappt / ▾ aufgeklappt, nur wenn es etwas zu falten gibt).
func (b ToolBlock) stateLabel(width int) string {
	state := "✓"
	if b.running {
		state = "…"
	} else if b.failed {
		// Fehlgeschlagenes Tool: ✗ statt ✓ — der Kopf trennt Scheitern
		// vom Erfolg, ohne die Karte umzufaerben.
		state = "✗"
	}
	l := kindGlyph(b.kind) + " " + b.title + " " + state
	if sourceExceeds(b.bodyLines(), codeInnerWidth(width), toolCollapsed) {
		if b.expanded {
			l += " ▾"
		} else {
			l += " ▸"
		}
	}
	if b.marked {
		l += " ✓"
	}
	return l
}

func (b ToolBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	inner := codeInnerWidth(width)
	// Die AKTUELLE Auswahl highlightet NUR die Kopfzeile in Neon — der
	// Kasten bleibt in der ruhigen Karten-Farbe (wie ThinkBlock, User-
	// Feedback: "statt alles auszuwaehlen einfach nur die Ueberschrift
	// highlighten"); die VERGANGENE Auswahl (stale, FR-43) traegt weiter
	// nur den Rahmen in der Sekundaer-Farbe. Normalzustand: Rahmen in der
	// Pink-Purple-Karten-Farbe.
	sel := b.focused || b.marked
	bs := cardBorderStyleFor(b.th, false, b.stale)
	ls := b.th.LabelStyle()

	label := b.stateLabel(width)
	labelW := runewidth.StringWidth(label)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	lines := make([]string, 0, b.Height(width))
	if !b.joinTop {
		headSt := actionStyle(b.th, b.hoverHead)
		if sel {
			// Auswahl: Neon im Kopf — hover unterstreicht weiter.
			headSt = hotkeyStyle(b.th, b.hoverHead)
		}
		lines = append(lines, bs.Render("┌─ ")+headSt.Render(label)+
			bs.Render(" "+strings.Repeat("─", dashes)+"┐"))
	}
	// Innenraum als EINE Zelle durch den Zeilenstil — die Fuellung geht
	// lueckenlos von Rahmenzeichen zu Rahmenzeichen (keine bg-Luecken an
	// den Pad-Zellen, User-Feedback "ganzen bg faerben").
	for _, ln := range b.visibleRows(width) {
		gap := inner - runewidth.StringWidth(ln)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+ls.Render(" "+ln+strings.Repeat(" ", gap+1))+bs.Render("│"))
	}
	// Der Boden ist reine Rahmenlinie: im Stack die Trennlinie zur
	// naechsten Karte, sonst └┘ — keine "+ show more"-Zeile mehr, die den
	// Kasten mitten im Stack vorzeitig geschlossen haette.
	if b.joinAsDivider {
		dls := bs
		if b.dividerSel {
			// Ausgewaehlte Folgekarte: ihr Label in der Trennlinie neon.
			dls = hotkeyStyle(b.th, false)
		}
		lines = append(lines, stackDivider(bs, dls, b.dividerLabel, width))
	} else {
		lines = append(lines, bs.Render("└"+strings.Repeat("─", width-2)+"┘"))
	}
	return strings.Join(lines, "\n")
}

func (b ToolBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok || !b.focused {
		return b, nil
	}
	nb := b
	switch km.String() {
	case "enter":
		nb.expanded = !b.expanded
	case "+", "=":
		if !b.expanded {
			nb.expanded = true
		}
	case "-", "_":
		if b.expanded {
			nb.expanded = false
		}
	default:
		return b, nil
	}
	return nb, nil
}

func (b ToolBlock) Focused() bool   { return b.focused }
func (b ToolBlock) Focusable() bool { return true }
func (b ToolBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}
func (b ToolBlock) SetHover(h bool) Widget {
	nb := b
	nb.hoverHead = h
	return nb
}

// SetHoverAt: Hover zielgenau auf die Kopfzeile — das einzige Falt-Ziel
// (▸/▾ im Kopf; die "+ show more"-Zeile ist weg).
func (b ToolBlock) SetHoverAt(width, x, y int) Widget {
	nb := b
	nb.hoverHead = y == 0 && b.HitAction(width, x, y)
	return nb
}

func (b ToolBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b ToolBlock) Marked() bool { return b.marked }

func (b ToolBlock) SetStale(s bool) Widget {
	nb := b
	nb.stale = s
	return nb
}

// Markdown: Titel + Inhalt als Copy-Payload (FR-29 — roh, ohne ANSI).
func (b ToolBlock) Markdown() string {
	return b.title + "\n\n" + strings.Join(b.bodyLines(), "\n")
}

func (b ToolBlock) Resize(width int) Widget { return b }

func (b ToolBlock) HitAction(width, x, y int) bool {
	if !sourceExceeds(b.bodyLines(), codeInnerWidth(width), toolCollapsed) {
		return false // nichts versteckt — keine Klappe, auch nicht im Kopf
	}
	// Die Kopf-Caption ("$ bash ✓ ▸") ist die Falt-Stelle — Klick klappt
	// die Karte, ohne die Markierung anzufassen. Der Footer ist reine
	// Rahmenlinie (kein "+ show more" mehr) — kein Klickziel.
	if y != 0 {
		return false
	}
	return x >= 3 && x < 3+runewidth.StringWidth(b.stateLabel(width))
}

// Action: Klick auf Kopf/Fuss klappt die Karte (wie ThinkBlock).
func (b ToolBlock) Action() (Widget, tea.Cmd) {
	nb := b
	nb.expanded = !b.expanded
	return nb, nil
}

// Copy: Titel + Inhalt (der Chip im Eingabefeld-Footer, alt+<n>).
func (b ToolBlock) Copy() (Widget, tea.Cmd) {
	if b.copyFn == nil {
		return b, nil
	}
	return b, b.copyFn(b.Markdown())
}

func (b ToolBlock) ID() int { return b.id }
func (b ToolBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- Zeichen-Modus (FR-42): Buchstaben-Cursor im Karten-Inhalt -------------

// CharLines liefert die SICHTBAREN Inhalt-Zeilen (aufgeklappt alles) — der
// beginCharSel klappt die Karte vorher auf.
func (b ToolBlock) CharLines(width int) []string {
	rows := b.wrappedBody(codeInnerWidth(width))
	if b.expanded || len(rows) <= toolCollapsed {
		return rows
	}
	return rows[len(rows)-toolCollapsed:]
}

func (b ToolBlock) CharPad(width int) int  { return 1 }
func (b ToolBlock) CharLeft(width int) int { return 2 }

// upsertToolBlock legt die Karte eines Tool-Call an oder aktualisiert sie in
// Place (stabiler Index, Fokus/Markierung/Faltzustand bleiben) — tool_call
// startet die laufende Karte, tool_call_update schreibt Zustand + Inhalt.
func (l BlockList) upsertToolBlock(d ToolData, th *render.Theme, copyFn CopyFunc) BlockList {
	for i, it := range l.items {
		tb, ok := it.(ToolBlock)
		if !ok || tb.callID != d.CallID {
			continue
		}
		ntb := NewToolBlock(d, th, copyFn)
		ntb.id = tb.id
		ntb.expanded = tb.expanded
		ntb.hoverHead = tb.hoverHead
		l.items[i] = ntb.SetFocused(tb.focused).SetMarked(tb.marked)
		return l
	}
	// Neue Karte: unten anfuegen (Append vergibt die Blocknummer und
	// scrollt im Follow-Modus mit).
	return l.Append(NewToolBlock(d, th, copyFn))
}
