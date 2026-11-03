package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	content  string
	command  string
	stdout   string
	stderr   string
	exit     int
	hasOut   bool
	live     string
	running  bool
	failed   bool
	expanded bool
	focused  bool
	marked   bool
	// stale: Karte ist Teil der VERGANGENEN Auswahl (FR-43) — Rahmen in
	// der verblichenen Sekundaer-Farbe.
	stale  bool
	th     *render.Theme
	copyFn CopyFunc
	// Hover ist ZIELGENAU wie beim ThinkBlock: Kopfzeile und Footer-
	// Aktion sind getrennte Ziele, nie gleichzeitig.
	hoverHead bool
	hoverFoot bool
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
	return ToolBlock{id: -1, callID: d.CallID, title: d.Title, kind: d.Kind,
		content: d.Text, command: d.Command, stdout: d.Stdout, stderr: d.Stderr,
		exit: d.Exit, hasOut: d.HasOutput, live: d.Live,
		running: d.Running, failed: d.Failed, th: th, copyFn: copyFn}
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
	if inner <= 0 {
		return b.bodyLines()
	}
	var out []string
	for _, ln := range b.bodyLines() {
		if ln == "" {
			out = append(out, "")
			continue
		}
		out = append(out, render.WrapWords(ln, inner)...)
	}
	return out
}

// visibleRows: alle Zeilen aufgeklappt, sonst die LETZTEN toolCollapsed
// (das Resultat des Calls lebt am Ende) — die $-Kommandozeile bleibt
// gepinnt sichtbar, sie identifiziert den Call.
func (b ToolBlock) visibleRows(width int) []string {
	rows := b.wrappedBody(codeInnerWidth(width))
	if b.expanded || len(rows) <= toolCollapsed {
		return rows
	}
	tail := rows[len(rows)-toolCollapsed:]
	if b.command != "" && strings.HasPrefix(rows[0], "$ ") {
		return append([]string{rows[0]}, tail...)
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
	if len(b.wrappedBody(codeInnerWidth(width))) > toolCollapsed {
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

func (b ToolBlock) actionLabel() string {
	if b.expanded {
		return "- show less"
	}
	return "+ show more"
}

func (b ToolBlock) action() (string, string) {
	action := b.actionLabel()
	key := "+"
	if b.expanded {
		key = "-"
	}
	return action, styledAction(b.th, action, key, b.hoverFoot)
}

func (b ToolBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	inner := codeInnerWidth(width)
	// Fokus ODER Auswahl am RAHMEN (Neon-Violett), wie Think/CodeBlock.
	// Die VERGANGENE Auswahl (stale, FR-43) traegt die Sekundaer-Farbe.
	bs := borderStyleFor(b.th, b.focused || b.marked, b.stale)
	ls := b.th.LabelStyle()

	label := b.stateLabel(width)
	labelW := runewidth.StringWidth(label)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	rows := b.wrappedBody(inner)
	lines := make([]string, 0, b.Height(width))
	lines = append(lines, bs.Render("┌─ ")+actionStyle(b.th, b.hoverHead).Render(label)+
		bs.Render(" "+strings.Repeat("─", dashes)+"┐"))
	for _, ln := range b.visibleRows(width) {
		txt := ls.Render(ln)
		gap := inner - lipgloss.Width(txt)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+" "+txt+strings.Repeat(" ", gap)+" "+bs.Render("│"))
	}
	if len(rows) > toolCollapsed {
		action, styled := b.action()
		lines = append(lines, render.CenteredActionLine(width, action, styled,
			runewidth.StringWidth(action), bs))
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
	nb.hoverHead, nb.hoverFoot = false, h
	return nb
}

// SetHoverAt: Hover zielgenau auf Kopfzeile ODER Footer-Aktion.
func (b ToolBlock) SetHoverAt(width, x, y int) Widget {
	nb := b
	nb.hoverHead, nb.hoverFoot = false, false
	if !nb.HitAction(width, x, y) {
		return nb
	}
	if y == 0 {
		nb.hoverHead = true
	} else {
		nb.hoverFoot = true
	}
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
	if len(b.wrappedBody(codeInnerWidth(width))) <= toolCollapsed {
		return false // nichts versteckt — keine Klappe, auch nicht im Kopf
	}
	if y == 0 {
		return x >= 3 && x < 3+runewidth.StringWidth(b.stateLabel(width))
	}
	if y != 1+b.bodyRows(width) {
		return false
	}
	start, end := render.CenteredActionRange(width, runewidth.StringWidth(b.actionLabel()))
	return x >= start && x < end
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
		ntb.hoverHead, ntb.hoverFoot = tb.hoverHead, tb.hoverFoot
		l.items[i] = ntb.SetFocused(tb.focused).SetMarked(tb.marked)
		return l
	}
	// Neue Karte: unten anfuegen (Append vergibt die Blocknummer und
	// scrollt im Follow-Modus mit).
	return l.Append(NewToolBlock(d, th, copyFn))
}
