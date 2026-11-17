package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// CopyFunc builds a tea.Cmd that copies text when the runtime executes it.
// The write must not happen at call time — effects live in Cmds (Elm).
type CopyFunc func(text string) tea.Cmd

// Widget is a single chat block as a nested Elm component: state, an Update
// returning the next state, and a pure View. SetFocused/SetHover return
// copies instead of mutating. HitAction reports whether a cell lies on the
// COPY action; Copy triggers it (mouse path).
type Widget interface {
	Height(width int) int
	View(width int) string
	Update(msg tea.Msg) (Widget, tea.Cmd)
	Focused() bool
	Focusable() bool
	SetFocused(bool) Widget
	SetHover(bool) Widget
	SetMarked(bool) Widget
	Marked() bool
	// SetStale markiert den Block als Teil der VERGANGENEN Auswahl (FR-43):
	// Render-Pass wie SetMarked — der Rahmen erscheint in der verblichenen
	// Sekundaer-Farbe, bis esc die Auswahl endgueltig aufloest. Die aktuelle
	// Auswahl (Fokus/Markierung) uebermalt ihn.
	SetStale(bool) Widget
	// Markdown returns the raw markdown source of the block — the copy
	// payload for selections (FR-29), never the ANSI-rendered view.
	Markdown() string
	Resize(width int) Widget
	HitAction(width, x, y int) bool
	Copy() (Widget, tea.Cmd)
	// ID is the stable block number (1-based, assigned to copyable blocks) or
	// -1 when the block is not addressable (FR-3).
	ID() int
	SetID(int) Widget
}

// actionWidget is a block whose footer action does something of its own:
// COPY copies (CodeBlock/TableBlock), the fold action folds the think
// block (FR-30). Mouse clicks dispatch through Action — never through Copy,
// which is the copy-only keyboard path (alt+<i>, y).
type actionWidget interface {
	Action() (Widget, tea.Cmd)
}

// actionAtWidget ist ein Block mit MEHREREN klickbaren Footer-Aktionen:
// ActionAt liefert fuer eine Zelle die passende Aktion zurueck — der
// ImageBlock traegt [ TEXT/IMAGE ] (Moduswechsel) neben [ COPY ].
type actionAtWidget interface {
	ActionAt(width, x, y int) (Widget, tea.Cmd, bool)
}

func textColorFor(th *render.Theme) string {
	if th == nil {
		return ""
	}
	return th.Text.Color
}

// wrapCache haelt den Umbruch EINES Prosa-Blocks fuer seine Breite fest:
// Height()/View()/CharLines() fragen denselben Umbruch mehrfach pro Frame
// ab (Scroll-Clamp, View, Fokus-Reveal). Ohne Cache kostet jeder Frame
// O(Textlaenge) pro Block — bei langen Chats der Grund, warum Scrollen und
// tab traege wurden. Bloecke sind Werte (Fokus/Markierung werden kopiert);
// der Zeiger teilt den Cache zwischen den Kopien.
//
// Kein Mutex: Widgets werden ausschliesslich auf der UI-Goroutine benutzt
// (Update/View des Elm-Modells); Zulieferer aus anderen Goroutinen gehen
// ueber tea.Program.Send. Ein Lock pro Height() waere bei jeder
// Layout-Wanderung durch die ganze Historie spuerbar (gemessen).
type wrapCache struct {
	width int
	lines []string
}

func (c *wrapCache) get(text string, w int) []string {
	if w <= 0 {
		w = 80
	}
	if c.lines != nil && c.width == w {
		return c.lines
	}
	c.lines = textWrapped(text, w)
	c.width = w
	return c.lines
}

// textWrapped wraps prose to w display cells at word boundaries, preserving
// line structure. Hard-wrapping would split words mid-syllable.
func textWrapped(s string, w int) []string {
	var lines []string
	if w <= 0 {
		w = 80
	}
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, render.WrapWords(l, w)...)
	}
	return lines
}

// NewHeadingBlock renders a markdown section title on its own row, bold so it
// separates from the body text around it.
func NewHeadingBlock(text string) TextBlock {
	b := NewTextBlock(text, "")
	b.style = lipgloss.NewStyle().Bold(true)
	// Ueberschriften sind fokussierbar: up/down/select erreicht sie, der
	// Fokus unterstreicht den Titel (klassisches Anker-Signal, passend zum
	// h1-Underline der HeadingStyle-Hierarchie).
	b.focusable = true
	b.heading = true
	// "Bei Ueberschriften eine Zeile frei": Leerzeile davor und danach,
	// als Teil der Block-Hoehe (Koordinaten/Scroll bleiben konsistent).
	b.beforeGap, b.afterGap = 1, 1
	return b
}

// actionStyle ist der REST eines Action-Labels: Theme-Action-Farbe in
// Ruhe, weiß + unterstrichen beim Hover — und NUR beim Hover der
// getroffenen Zelle (nie blockweit). Der Tastatur-Fokus zeigt sich am
// Rahmen (borderStyleFor), nicht mehr am Label.
const hoverWhite = "#FFFFFF"

// borderStyleFor liefert den Rahmenstil eines Blocks: Ruhefarbe aus dem
// Theme, Neon-Violett wenn der Block die AKTUELLE Auswahl ist (Fokus oder
// Markierung) — die Border ist das Auswahl-Signal. stale (vergangene
// Auswahl, FR-43) faerbt den Rahmen in die verblichene Sekundaer-Farbe —
// nur wenn keine aktuelle Auswahl denselben Block uebermalt.
func borderStyleFor(th *render.Theme, sel, stale bool) lipgloss.Style {
	if sel {
		return th.FocusBorderStyle()
	}
	if stale {
		return th.StaleBorderStyle()
	}
	return th.BorderStyle()
}

// cardBorderStyleFor ist der Rahmen der Think-/Tool-Karten: im Normal-
// Zustand in der TEXT-Farbe der Karte (faint Frame-Farbe) statt weiß —
// der Rahmen gehört zur Karte, nicht zur Konversation (User-Feedback:
// "grenzt sich nicht genug an"). Fokus-/Markierungs- und Stale-Rahmen
// bleiben unangetastet, damit die Auswahl weiterhin herausliest.
func cardBorderStyleFor(th *render.Theme, sel, stale bool) lipgloss.Style {
	if sel {
		return th.FocusBorderStyle()
	}
	if stale {
		return th.StaleBorderStyle()
	}
	return th.LabelStyle()
}

// stackDivider rendert die Trennlinie zwischen zwei gestapelten Karten:
// ├─ <Label der nächsten Karte> ──┤ (User-Skizze "thinking | tool"). Das
// Label traegt denselben Stil wie die Rahmenzeichen — die Zeile ist EINE
// durchgehende Flaeche (bg-Fuellung auch unter dem Label, sonst laeuft
// die Horizontale auf Nicht-Default-Terminals unschoen ueber).
func stackDivider(bs lipgloss.Style, label string, width int) string {
	lw := runewidth.StringWidth(label)
	dashes := width - 5 - lw
	if dashes < 0 {
		dashes = 0
	}
	return bs.Render("├─ ") + bs.Render(label) + bs.Render(" "+strings.Repeat("─", dashes)+"┤")
}

func actionStyle(th *render.Theme, hover bool) lipgloss.Style {
	st := lipgloss.NewStyle()
	if th != nil {
		st = th.ActionStyle()
	}
	if hover {
		st = st.Foreground(lipgloss.Color(hoverWhite)).Underline(true)
	}
	return st
}

// hotkeyStyle faerbt den aktivierenden Buchstaben: IMMER Neon-Violett
// fett, auch im Hover und ohne Fokus — der Buchstabe ist die Taste, die
// die Aktion ausloest (C bei COPY, + beim Aufklappen). Beim Hover zieht er
// DASSELBE Underline wie der Label-Rest, nur die Farbe bleibt Neon.
func hotkeyStyle(th *render.Theme, hover bool) lipgloss.Style {
	st := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(th.FocusColor()))
	if hover {
		st = st.Underline(true)
	}
	return st
}

// styledAction rendert ein Action-Label mit Neon-Hotkey: das erste
// Vorkommen von key (case-insensitiv) bekommt den Hotkey-Stil, der Rest
// den Action-Stil (Hover: Rest weiß + unterstrichen, der Buchstabe bleibt
// Neon — "immer neon purple"). Ohne Treffer faerbt sich das ganze Label.
func styledAction(th *render.Theme, label, key string, hover bool) string {
	var sb strings.Builder
	rest := actionStyle(th, hover)
	i := -1
	if key != "" {
		i = strings.Index(strings.ToLower(label), strings.ToLower(key))
	}
	if i < 0 {
		return rest.Render(label)
	}
	if pre := label[:i]; pre != "" {
		sb.WriteString(rest.Render(pre))
	}
	sb.WriteString(hotkeyStyle(th, hover).Render(label[i : i+len(key)]))
	if post := label[i+len(key):]; post != "" {
		sb.WriteString(rest.Render(post))
	}
	return sb.String()
}

// ---- TextBlock -------------------------------------------------------------

// TextBlock is a plain, non-interactive chat message.
type TextBlock struct {
	id   int
	text string
	// cache memoizes the wrap for this text/width (shared between copies).
	cache  *wrapCache
	style  lipgloss.Style
	marked bool
	// stale markiert den Block als Teil der VERGANGENEN Auswahl (FR-43):
	// Prosa traegt keinen Rahmen — die vergangene Auswahl unterstreicht
	// den Text (ohne Reverse, das ist der AKTUELLEN Markierung vorbehalten).
	stale bool
	// focusable markiert Ueberschriften: Sie sind Teil der Fokus-Kette
	// (up/down), der Fokus zeigt sich als Unterstrich (s. View).
	focusable bool
	focused   bool
	// heading markiert den Block ALS Ueberschrift: c/alt+<n>/y kopieren dann
	// den GESAMTEN Abschnitt bis zur naechsten Ueberschrift (BlockList.
	// sectionMarkdown), nicht nur die Titelzeile.
	heading bool
	// level ist die Ueberschriften-Stufe (1..6) — Markdown() baut daraus
	// die #-Marker zurueck, damit der Abschnitt als echtes Markdown kopiert.
	level int
	// user markiert die eigene Frage: View setzt einen fetten Pfeil in
	// Akzentfarbe davor (s. NewUserBlock) statt Fliesstext.
	user bool
	// frame ist der Pfeil-Stil — Akzentfarbe, fett nur auf dem "→".
	frame lipgloss.Style
	// beforeGap/afterGap: Leerzeilen, die den Block umschliessen (bei
	// Ueberschriften und Nummerierung "eine Zeile frei"). Sie gehoeren zur
	// Hoehe (Height) mit rein, damit Scroll/blockAt/Hit-Testing konsistent
	// bleiben; Markdown()/Copy() lassen sie weg (sauberes Yank).
	beforeGap int
	afterGap  int
}

func NewTextBlock(text, color string) TextBlock {
	s := lipgloss.NewStyle()
	if color != "" {
		s = s.Foreground(lipgloss.Color(color))
	}
	// FR-40: Reine Textabsaetze sind EIGENE Ketten-Segmente — der Block-
	// Cursor erreicht damit auch "Codeblock gefolgt von Text ohne
	// Ueberschrift" als eigenen Anlaufpunkt (Neonrahmen, y, alt+<n>),
	// statt ihn zu ueberspringen.
	return TextBlock{id: -1, text: text, style: s, focusable: true, cache: &wrapCache{}}
}

// NewUserBlock rendert die eigene Eingabe mit DICKER Links-Schiene plus
// Pfeil-Praefix (Crush-Fokus-Glyphe "▌", davor der "→"):
//
//	▌ → Wie läuft die Render-Pipeline? Das Wrapping in
//	▌   le.go ist mir unklar …
//
// Schiene traegt JEDE Zeile (Akzentfarbe), der Pfeil nur die erste; Folge-
// zeilen ruecken um die Pfeilbreite ein, der Umbruch steht bündig unter dem
// Textanfang.
func NewUserBlock(text string) TextBlock {
	ec := "12"
	if render.LightBG() {
		ec = "4" // Hellgrund -> dunkles Blau (FR-27)
	}
	return TextBlock{
		id:    -1,
		user:  true,
		text:  text,
		cache: &wrapCache{},
		frame: lipgloss.NewStyle().Foreground(lipgloss.Color(ec)),
		// Eine Leerzeile davor und danach: Die Frage soll von Seed oben und
		// Antwort unten atmen — Gaps gehoeren zur Height (Scroll/Hit-Test
		// bleiben konsistent), Markdown() laesst sie weg (FR-29).
		beforeGap: 1,
		afterGap:  1,
	}
}

// userWrapWidth ist das Wrap-Budget der eigenen Frage: Schiene (2 Zellen)
// plus Pfeil-Praefix (2 Zellen) gehen vom Terminal ab. Unter 16 Zellen
// faellt der Schmuck komplett weg — lieber schlicht als ueberlaufend.
func userWrapWidth(width int) int {
	if w := width - 4; w >= 12 {
		return w
	}
	return width
}

// textGutter indents prose blocks so answers breathe next to the framed
// code/table blocks — the text starts flush with the code content inside
// the frames (border plus one padding cell), like an editor's margin.
const textGutter = 2

// wrapWidth is the prose wrap width for a terminal width: the gutter comes
// out of the wrap budget, except on very narrow terminals where it would
// squeeze the text below usability.
func (b TextBlock) wrapWidth(width int) int {
	if w := width - textGutter; w >= 8 {
		return w
	}
	return width
}

// wrapped liefert den Prosa-Umbruch (gecacht je Breite).
func (b TextBlock) wrapped(w int) []string {
	if b.cache == nil {
		return textWrapped(b.text, w)
	}
	return b.cache.get(b.text, w)
}

func (b TextBlock) Height(width int) int {
	w := b.wrapWidth(width)
	if b.user {
		w = userWrapWidth(width) // Schiene + Pfeil kosten 4 Zellen extra
	}
	return b.beforeGap + len(b.wrapped(w)) + b.afterGap
}
func (b TextBlock) View(width int) string {
	if b.user {
		return b.userView(width)
	}
	wrapW := b.wrapWidth(width)
	pad := strings.Repeat(" ", textGutter)
	if wrapW == width {
		pad = "" // too narrow for a gutter
	}
	lines := b.wrapped(wrapW)
	st := b.style
	if b.focused {
		// Fokussierte Ueberschrift: Unterstrich — das Anker-Signal der
		// Fokus-Kette, ohne Rahmen (Prosa waechst keinen Kasten, FR-29).
		st = st.Underline(true)
	}
	if b.marked {
		// Marked prose inverts instead of growing a frame, so the wrap
		// width stays untouched (FR-29).
		st = st.Reverse(true)
	}
	if b.stale && !b.marked {
		// Vergangene Auswahl (FR-43): unterstrichen statt invertiert —
		// deutlich schwächer als die aktuelle Markierung.
		st = st.Underline(true)
	}
	all := make([]string, 0, b.beforeGap+len(lines)+b.afterGap)
	for i := 0; i < b.beforeGap; i++ {
		all = append(all, "")
	}
	for _, l := range lines {
		all = append(all, pad+st.Render(l))
	}
	for i := 0; i < b.afterGap; i++ {
		all = append(all, "")
	}
	return strings.Join(all, "\n")
}

// userView setzt die dicke Schiene vor JEDE Zeile ("▌ ", Akzentfarbe) und
// den Pfeil nur vor die erste ("→ ", fett): Gewrappte Zeilen ruecken um
// zwei Zellen ein, der Umbruch beginnt exakt unter dem Textanfang — nicht
// unter dem Pfeil. Auf zu schmalen Terminals entfaellt der Schmuck.
func (b TextBlock) userView(width int) string {
	wrapW := userWrapWidth(width)
	rail := b.frame.Render("▌") + " "
	first := b.frame.Bold(true).Render("→") + " "
	cont := "  " // Pfeil + Leerzelle: Textstart bleibt in derselben Spalte
	if wrapW == width {
		rail, first, cont = "", "", "" // zu schmal: Schmuck entfaellt
	}
	lines := b.wrapped(wrapW)
	st := b.style
	if b.marked {
		// Auswahl invertiert den Text; Schiene und Pfeil bleiben Akzent-
		// farbe und markieren weiter die eigene Frage (FR-29).
		st = st.Reverse(true)
	}
	out := make([]string, 0, b.beforeGap+len(lines)+b.afterGap)
	for i := 0; i < b.beforeGap; i++ {
		out = append(out, "")
	}
	for i, l := range lines {
		if i == 0 {
			out = append(out, rail+first+st.Render(l))
		} else {
			out = append(out, rail+cont+st.Render(l))
		}
	}
	for i := 0; i < b.afterGap; i++ {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

func (b TextBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}

func (b TextBlock) Marked() bool { return b.marked }

func (b TextBlock) SetStale(s bool) Widget {
	nb := b
	nb.stale = s
	return nb
}

// Markdown returns the stored text — plain, without ANSI (FR-29). Eine
// Ueberschrift traegt ihren #-Marker (Level-treu), damit Abschnitt-Kopien
// als echtes Markdown weiterverwendbar sind.
// ---- Zeichen-Modus (FR-42): Buchstaben-Cursor in der Sektion ---------------

// CharLines liefert die dargestellten Inhalt-Zeilen (gewrappt, exakt wie
// View sie rendert) — die Basis des Zeichen-Cursors. Die User-Schiene hat
// keinen Zeichen-Modus: Rail und Pfeil sind kein Text.
func (b TextBlock) CharLines(width int) []string {
	if b.user {
		return nil
	}
	return b.wrapped(b.wrapWidth(width))
}

// CharPad: gerenderte Zeilen vor der ersten Inhalt-Zeile (Leerzeilen der
// Ueberschriften-Rahmung gehoeren zur Blockhoehe).
func (b TextBlock) CharPad(width int) int { return b.beforeGap }

// CharLeft: Zellen-Einzug vor dem Text (Prosa-Gutter, s. View).
func (b TextBlock) CharLeft(width int) int {
	if b.wrapWidth(width) == width {
		return 0 // zu schmal fuer einen Gutter
	}
	return textGutter
}

func (b TextBlock) Markdown() string {
	if b.heading {
		lvl := b.level
		if lvl < 1 {
			lvl = 1
		}
		return strings.Repeat("#", lvl) + " " + b.text
	}
	return b.text
}
func (b TextBlock) Update(msg tea.Msg) (Widget, tea.Cmd) { return b, nil }
func (b TextBlock) Focused() bool                        { return b.focused }
func (b TextBlock) Focusable() bool                      { return b.focusable }
func (b TextBlock) SetFocused(f bool) Widget             { nb := b; nb.focused = f; return nb }
func (b TextBlock) SetHover(bool) Widget                 { return b }
func (b TextBlock) Resize(width int) Widget              { return b }
func (b TextBlock) HitAction(width, x, y int) bool       { return false }
func (b TextBlock) Copy() (Widget, tea.Cmd)              { return b, nil }
func (b TextBlock) ID() int                              { return b.id }
func (b TextBlock) SetID(i int) Widget                   { nb := b; nb.id = i; return nb }

// Heading meldet, ob der Block eine Ueberschrift ist — das Signal fuer die
// Abschnitt-Kopie (c kopiert bis zur naechsten Ueberschrift).
func (b TextBlock) Heading() bool { return b.heading }

// ---- CodeBlock -------------------------------------------------------------

type CodeBlock struct {
	id         int
	lang, code string
	th         *render.Theme
	copyFn     CopyFunc
	focused    bool
	marked     bool
	hover      bool
	// stale: Block ist Teil der VERGANGENEN Auswahl (FR-43) — Rahmen in
	// der verblichenen Sekundaer-Farbe (nur wenn nicht fokussiert/markiert).
	stale bool
	// converted marks a plantuml block whose ASCII conversion was attempted:
	// the ASCII replaced the source on success, the raw source stays on error
	// (FR-6 fallback). It prevents the block from being queued again.
	converted bool
}

func NewCodeBlock(lang, code string, copyFn CopyFunc) CodeBlock {
	// Tabs measure as width 0, which would break the box padding — expand them.
	return CodeBlock{id: -1, lang: lang, code: strings.ReplaceAll(strings.TrimSuffix(code, "\n"), "\t", "    "), copyFn: copyFn}
}

// codeInnerWidth is the room for code inside the frame: two border cells plus
// one padding cell on each side.
func codeInnerWidth(width int) int {
	w := width - 4
	if w < 8 {
		w = 8
	}
	return w
}

// bodyLines renders the code for a width: syntax highlighted, wrapped to the
// inner width, one entry per row. Height and View both use it, so the footer
// row and the mouse hit area can never disagree — they used to wrap at
// different widths, which drew the frame too short for long code.
func (b CodeBlock) bodyLines(width int) []string {
	return render.HighlightLines(b.lang, b.code, codeInnerWidth(width))
}

// linesAt returns the wrapped body line count for width.
func (b CodeBlock) linesAt(width int) int {
	return len(b.bodyLines(width))
}

// labelAt builds the footer label for a body line count.
func labelAt(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

func (b CodeBlock) Height(width int) int {
	return 1 + b.linesAt(width) + 1
}

// headerLine baut die Kopfzeile ┌─ lang #id ────┐: Rahmen in der Theme-
// Border-Farbe (beim fokussierten Block Neon-Violett — bs kommt vom
// Aufrufer), Label in der Action-Farbe, von den Ecken abgesetzt. Der
// CodeBlock benutzt sie direkt; der ImageBlock rahmt Bild- und Quelltext-
// Ansicht mit derselben Zeile — der t-Switch liest sich dadurch als
// Moduswechsel IM SELBEN Block statt als Block-Tausch.
func headerLine(th *render.Theme, lang string, id, width int, bs lipgloss.Style) string {
	if lang == "" {
		lang = "code"
	}
	// Stable block number in the header: the keyboard copy fallback (alt+<n>)
	// and the mouse both address this id (FR-3/FR-5).
	if id > 0 {
		lang = fmt.Sprintf("%s #%d", lang, id)
	}
	labelW := runewidth.StringWidth(lang)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	return bs.Render("┌─ ") + th.ActionStyle().Render(lang) +
		bs.Render(" "+strings.Repeat("─", dashes)+"┐")
}

// codeBodyLine rahmt eine Zeile links/rechts ein und fuellt den Rest mit
// Fill-Stil auf — gemeinsamer Baustein von CodeBlock.View und den
// gerahmten ImageBlock-Ansichten.
func codeBodyLine(ln string, inner int, bs, fill lipgloss.Style) string {
	gap := inner - lipgloss.Width(ln)
	if gap < 0 {
		gap = 0
	}
	return bs.Render("│") + " " + fill.Render(ln) + fill.Render(strings.Repeat(" ", gap)) + " " + bs.Render("│")
}

func (b CodeBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	body := b.bodyLines(width)
	inner := codeInnerWidth(width)
	// Auswahl faerbt den GESAMTEN Rahmen (wie Fokus, wie ImageBlock) —
	// frueher leuchtete nur die Aktionszeile: Der Block wirkte "nur in der
	// letzten Zeile markiert". Die VERGANGENE Auswahl (stale) faerbt den
	// Rahmen in der Sekundaer-Farbe (FR-43), sofern keine aktuelle Auswahl
	// uebermalt.
	bs := borderStyleFor(b.th, b.focused || b.marked, b.stale)
	fill := b.th.TableFill()

	lines := make([]string, 0, len(body)+2)
	lines = append(lines, headerLine(b.th, b.lang, b.id, width, bs))

	for _, ln := range body {
		lines = append(lines, codeBodyLine(ln, inner, bs, fill))
	}

	// Abschlusszeile ohne Aktion (Copy lebt im Eingabefeld-Footer, s.
	// Model.inputView): die leere Bahn bleibt IMMER — Hoehe konstant, kein
	// Scroll-Drift, progressive Disclosure entfaellt.
	lines = append(lines, render.FooterRail(width, bs, nil))
	return strings.Join(lines, "\n")
}
func (b CodeBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	// Copy laeuft nicht mehr ueber den Block (kein enter/c, kein Klick) —
	// die Aktion lebt im Eingabefeld-Footer (Model.inputView/Copy-Chip).
	return b, nil
}
func (b CodeBlock) Focused() bool   { return b.focused }
func (b CodeBlock) Focusable() bool { return true }
func (b CodeBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}
func (b CodeBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	return nb
}
func (b CodeBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b CodeBlock) Marked() bool { return b.marked }
func (b CodeBlock) SetStale(s bool) Widget {
	nb := b
	nb.stale = s
	return nb
}

// Markdown returns the block as a fenced markdown snippet (FR-29).
func (b CodeBlock) Markdown() string {
	return "```" + b.lang + "\n" + b.code + "\n```"
}

// ---- Zeichen-Modus (FR-42): Buchstaben-Cursor auch im Code -----------------

// CharLines liefert die dargestellten Code-Zeilen OHNE ANSI: HighlightLines
// wrappt display-scharf (WrapANSI behaelt jeden Runen), das Stripping macht
// daraus die plain Buchstaben — Offsets bleiben zellengleich mit der
// farbigen Ansicht, der Zeichen-Cursor unterstreicht in das HIGHLIGHTING
// hinein statt es zu ersetzen.
func (b CodeBlock) CharLines(width int) []string {
	hl := b.bodyLines(width)
	out := make([]string, len(hl))
	for i, ln := range hl {
		out[i] = stripSeq.ReplaceAllString(ln, "")
	}
	return out
}

// CharPad: Kopfzeile vor der ersten Code-Zeile.
func (b CodeBlock) CharPad(width int) int { return 1 }

// CharLeft: Rahmen plus Padding-Zelle vor dem Code (s. codeBodyLine).
func (b CodeBlock) CharLeft(width int) int  { return 2 }
func (b CodeBlock) Resize(width int) Widget { return b } // lays out per View anyway
func (b CodeBlock) HitAction(width, x, y int) bool {
	// Kein klickbarer COPY-Kasten mehr im Block (Copy = Eingabefeld-Footer).
	return false
}

// Copy liefert den Copy-Payload als Cmd — aufgerufen vom Eingabefeld-Chip
// und alt+<n> (copyByID), ohne eigenen Block-Zustand.
func (b CodeBlock) Copy() (Widget, tea.Cmd) {
	if b.copyFn == nil {
		return b, nil
	}
	return b, b.copyFn(b.code)
}

func (b CodeBlock) ID() int { return b.id }

func (b CodeBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- TableBlock ------------------------------------------------------------

// TableBlock renders a markdown table, laid out dynamically against the width
// it is given (re-flows on terminal resize). Chrome (borders, row separators)
// renders via the theme; cells stay plain.
type TableBlock struct {
	id      int
	cells   [][]string
	aligns  []render.Align
	th      *render.Theme
	copyFn  CopyFunc
	focused bool
	marked  bool
	hover   bool
	// stale: Block ist Teil der VERGANGENEN Auswahl (FR-43) — Tabellen-
	// Chrome in der verblichenen Sekundaer-Farbe.
	stale bool

	cacheW int      // width the cache was built for (0 = none)
	body   []string // cached body lines (no footer)
	widths []int    // cached per-column widths
}

func NewTableBlock(cells [][]string, aligns []render.Align, copyFn CopyFunc) TableBlock {
	return TableBlock{id: -1, cells: cells, aligns: aligns, copyFn: copyFn}
}

// layout returns the body lines and column widths for width.
func (b TableBlock) layout(width int) ([]string, []int) {
	if b.body != nil && b.cacheW == width {
		return b.body, b.widths
	}
	return render.TableBodyLines(b.cells, b.aligns, b.th, width)
}

func (b TableBlock) Height(width int) int {
	body, _ := b.layout(width)
	if len(body) == 0 {
		return 0
	}
	return len(body) + 1
}

// themeFor liefert das Theme fuer den Tabellen-Chrome: bei der AKTUELLEN
// Auswahl (Fokus/Markierung) eine Kopie mit Neon-Violett als Border-Farbe
// — das Auswahl-Signal. Bei der VERGANGENEN Auswahl (FR-43) faerbt die
// Kopie den Chrome in die verblichene Sekundaer-Farbe. Die Kopie aendert
// nur Farben, nie Geometrie (Breiten/Hitboxen bleiben).
func (b TableBlock) themeFor(focused, stale bool) *render.Theme {
	if b.th == nil {
		return b.th
	}
	if !focused && !stale {
		return b.th
	}
	f := *b.th
	if focused {
		f.Border = render.Style{Color: b.th.FocusColor(), Bold: true}
	} else {
		f.Border = render.Style{Color: b.th.StaleColor()}
	}
	return &f
}

func (b TableBlock) View(width int) string {
	body, widths := b.layout(width)
	if len(body) == 0 {
		return ""
	}
	rows := len(b.cells) - 1
	if rows < 0 {
		rows = 0
	}
	th := b.themeFor(b.focused || b.marked, b.stale)
	if b.focused || b.marked || b.stale {
		// Body im Neon-Theme neu bauen (der Cache haelt die Ruhefarben):
		// bei Fokus ODER Auswahl — die Markierung soll den GESAMTEN Block
		// faerben, nicht nur die Fusszeile (Konsistenz mit ImageBlock). Die
		// vergangene Auswahl (stale) baut in der Sekundaer-Farbe.
		body, widths = render.TableBodyLines(b.cells, b.aligns, th, width)
	}
	// Abschlusszeile ohne Aktion: die leere Bahn bleibt IMMER — Hoehe
	// konstant, kein Scroll-Drift. Dieselbe Thema-Instanz wie der Body:
	// bei Fokus/Markierung faerbte die fest verdrahtete Ruhefarbe die
	// LETZTE REIHE falsch (nicht die Auswahl-Farbe).
	return strings.Join(append(append([]string{}, body...),
		render.TableFooterRail(widths, th)), "\n")
}
func (b TableBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	// Copy laeuft nicht mehr ueber den Block (kein enter/c, kein Klick) —
	// die Aktion lebt im Eingabefeld-Footer.
	return b, nil
}
func (b TableBlock) Focused() bool   { return b.focused }
func (b TableBlock) Focusable() bool { return true }
func (b TableBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}
func (b TableBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	return nb
}

// Resize rebuilds the layout cache for width (called on WindowSizeMsg).
func (b TableBlock) Resize(width int) Widget {
	if b.body != nil && b.cacheW == width {
		return b
	}
	nb := b
	nb.body, nb.widths = render.TableBodyLines(b.cells, b.aligns, b.th, width)
	nb.cacheW = width
	return nb
}
func (b TableBlock) HitAction(width, x, y int) bool {
	// Kein klickbarer COPY-Kasten mehr im Block (Copy = Eingabefeld-Footer).
	return false
}

// Copy liefert den Copy-Payload als Cmd — aufgerufen vom Eingabefeld-Chip
// und alt+<n>, ohne eigenen Block-Zustand.
func (b TableBlock) Copy() (Widget, tea.Cmd) {
	if b.copyFn == nil {
		return b, nil
	}
	return b, b.copyFn(render.TableMarkdown(b.cells))
}
func (b TableBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b TableBlock) SetStale(s bool) Widget {
	nb := b
	nb.stale = s
	return nb
}
func (b TableBlock) Marked() bool { return b.marked }

// Markdown returns the table as markdown source (FR-29).
func (b TableBlock) Markdown() string { return render.TableMarkdown(b.cells) }

func (b TableBlock) ID() int { return b.id }

func (b TableBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- Body-Wrapping (gemeinsam fuer die Rahmen-Karten) ----------------------

// wrapSourceLines bricht rohe Quellzeilen an Wortgrenzen um (leere Zeilen
// bleiben leer) — der gemeinsame Body-Formatierer von Think-/Tool-Karten.
func wrapSourceLines(src []string, inner int) []string {
	if inner <= 0 {
		return src
	}
	var out []string
	for _, ln := range src {
		if ln == "" {
			out = append(out, "")
			continue
		}
		out = append(out, render.WrapWords(ln, inner)...)
	}
	return out
}

// wrapSourceTail bricht nur so viele Quellzeilen vom ENDE um, wie fuer n
// Zeilen gebraucht werden. Zugeklappte Karten zeigen ohnehin nur den
// Schluss (FR-30) — ein 20k-Zeilen-Tool-Output darf dafuer nicht in jedem
// Frame komplett neu gewrappt werden.
func wrapSourceTail(src []string, inner, n int) []string {
	if n <= 0 || len(src) == 0 {
		return nil
	}
	if inner <= 0 {
		if len(src) <= n {
			return src
		}
		return src[len(src)-n:]
	}
	var rows []string
	for i := len(src) - 1; i >= 0 && len(rows) < n; i-- {
		ln := src[i]
		var w []string
		if ln == "" {
			w = []string{""}
		} else {
			w = render.WrapWords(ln, inner)
		}
		rows = append(w, rows...)
	}
	if len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	return rows
}

// sourceExceeds meldet, ob src mehr als n Zeilen belegt — OHNE den ganzen
// Text zu wrappen: jede Quellzeile ist mindestens eine Zeile, also beweist
// schon die Zeilenzahl alles ueber n. Damit bleibt Height() einer grossen,
// zugeklappten Karte O(1) statt O(Text).
func sourceExceeds(src []string, inner, n int) bool {
	if len(src) > n {
		return true
	}
	return len(wrapSourceLines(src, inner)) > n
}

// ---- ThinkBlock ------------------------------------------------------------

// thinkCollapsed caps the visible body lines of a collapsed ThinkBlock (FR-30).
const thinkCollapsed = 3

// ThinkBlock shows the model's thinking as a collapsible chat block: long
// thinking is truncated to a few lines until unfolded (FR-30).
type ThinkBlock struct {
	id   int
	text string
	// body caches the split source lines: Height()/View() fragen die Zeilen
	// mehrfach pro Frame ab — ein 2000-Zeilen-Denktext wurde sonst pro
	// Aufruf neu gesplittet.
	body     []string
	expanded bool
	focused  bool
	marked   bool
	// stale: Block ist Teil der VERGANGENEN Auswahl (FR-43) — Rahmen in
	// der verblichenen Sekundaer-Farbe, ohne das "✓" der Markierung.
	stale bool
	// Stack-Joining (nur auf RENDER-KOPIEN in BlockList.stack gesetzt):
	// aufeinanderfolgende Think-/Tool-Karten teilen EINEN Rahmen —
	// joinTop laesst die eigene ┌─┐-Zeile weg, joinAsDivider ersetzt die
	// eigene └─┘-Zeile durch ├─ <dividerLabel> ─┤.
	joinTop       bool
	joinAsDivider bool
	dividerLabel  string
	th            *render.Theme
	// Hover ist ZIELGENAU, nicht blockweit: nur die Kopfzeile highlightet
	// — die Falt-Marke (▸/▾) lebt im Kopf, der Footer ist eine reine
	// Rahmenlinie (kein "+ show more" mehr).
	hoverHead bool
}

func NewThinkBlock(text string, th *render.Theme) ThinkBlock {
	return ThinkBlock{id: -1, text: text, th: th, body: splitBody(text)}
}

// splitBody teilt einen Roh-Text in Zeilen (einmal je Widget-Aufbau).
func splitBody(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func (b ThinkBlock) bodyLines() []string {
	if b.body != nil {
		return b.body
	}
	return splitBody(b.text)
}

// wrappedBody wraps the thinking to the inner width. The model streams
// thoughts as one long line — truncating per row would hide everything but
// the start, so lines break at word boundaries instead (FR-30).
func (b ThinkBlock) wrappedBody(inner int) []string {
	return wrapSourceLines(b.bodyLines(), inner)
}

// hidden reports whether the fold hides anything — the same predicate the
// rendered rows use, so the "+ show more" action never promises content
// that is already visible.
func (b ThinkBlock) hidden(inner int) bool {
	return sourceExceeds(b.bodyLines(), inner, thinkCollapsed)
}

// visibleRows are the rows this fold state actually shows: everything when
// expanded, otherwise the LAST thinkCollapsed rows. While collapsed only the
// tail is wrapped — a long thinking stream stays cheap to lay out.
func (b ThinkBlock) visibleRows(inner int) []string {
	if b.expanded || !b.hidden(inner) {
		return b.wrappedBody(inner)
	}
	return wrapSourceTail(b.bodyLines(), inner, thinkCollapsed)
}

// bodyRows is the rendered body row count for the current fold state: all
// wrapped rows when expanded, otherwise thinkCollapsed.
func (b ThinkBlock) bodyRows(width int) int {
	inner := codeInnerWidth(width)
	if b.expanded || !b.hidden(inner) {
		return len(b.wrappedBody(inner))
	}
	return thinkCollapsed
}

func (b ThinkBlock) Height(width int) int { return 1 + b.bodyRows(width) + 1 }

// label is the header caption: fold state, line count, selection mark.
func (b ThinkBlock) label() string {
	state := "▸"
	if b.expanded {
		state = "▾"
	}
	l := fmt.Sprintf("thinking %s %d lines", state, len(b.bodyLines()))
	if b.marked {
		l += " ✓"
	}
	return l
}

// truncateCells cuts a line to w display cells, marking the cut with an
// ellipsis.
func truncateCells(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

func (b ThinkBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	inner := codeInnerWidth(width)
	// Die AKTUELLE Auswahl (Fokus ODER Markierung) faerbt die GESAMTE
	// Kastenflaeche — bg + Rahmen in einer Farbe, symmetrisch von Rand zu
	// Rand (User-Feedback: die nur umgefaerbten Rahmenzeichen liessen die
	// Horizontal-Linien an den ungestylten Mittelsegmenten ueberlaufen).
	// Die VERGANGENE Auswahl (stale, FR-43) traegt weiter nur den Rahmen
	// in der Sekundaer-Farbe. Normalzustand: Rahmen in der TEXT-Farbe der
	// Karte (cardBorderStyle).
	sel := b.focused || b.marked
	bs := cardBorderStyleFor(b.th, false, b.stale)
	ls := b.th.LabelStyle()
	if sel {
		bs = b.th.FocusFillStyle()
		ls = bs
	}

	label := b.label()
	labelW := runewidth.StringWidth(label)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	lines := make([]string, 0, b.Height(width))
	// Stack-Joining: nicht-erste Karte einer Stacks hat keine ┌─┐-Zeile —
	// der Vorgänger schließt mit ├─ <dieses Label> ─┤ ab. Height/Offset
	// rechnen das über BlockList.heightAt mit (eine Zeile weniger).
	if !b.joinTop {
		// The caption is a fold hit target (HitAction y==0) — die gleiche
		// Hover-Behandlung wie die Footer-Aktion, aber nur wenn SIE getroffen
		// ist (hoverHead), nie gleichzeitig mit dem Footer.
		headSt := actionStyle(b.th, b.hoverHead)
		if sel {
			headSt = ls // auf der Fuellflaeche: Kontrast-Text statt Action-Farbe
		}
		lines = append(lines, bs.Render("┌─ ")+headSt.Render(label)+
			bs.Render(" "+strings.Repeat("─", dashes)+"┐"))
	}

	// Collapsed shows the END of the thinking — the conclusion lives there
	// (visibleRows wraps only that tail, so streaming stays cheap). Der
	// Innenraum wird als EINE Zelle durch den Zeilenstil gerendert — die
	// Fuellung (Theme-bg im Ruhe-Zustand, Akzentfarbe bei Auswahl) geht
	// lueckenlos von Rahmenzeichen zu Rahmenzeichen.
	rows := b.visibleRows(inner)
	for _, ln := range rows {
		gap := inner - runewidth.StringWidth(ln)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+ls.Render(" "+ln+strings.Repeat(" ", gap+1))+bs.Render("│"))
	}

	// Der Boden ist reine Rahmenlinie: im Stack die Trennlinie zur naechsten
	// Karte (├─ <Label> ─┤), sonst der Schluss └┘. Ein zugeklappter Inhalt
	// zeigt sich allein an der Falt-Marke im Kopf (▸) — es gibt keine
	// "+ show more"-Zeile mehr, die den Kasten mitten im Stack vorzeitig
	// geschlossen haette (User-Report: "nur die ersten 2 verbinden").
	if b.joinAsDivider {
		lines = append(lines, stackDivider(bs, b.dividerLabel, width))
	} else {
		lines = append(lines, bs.Render("└"+strings.Repeat("─", width-2)+"┘"))
	}
	return strings.Join(lines, "\n")
}

func (b ThinkBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		key := km.String()
		if !b.focused {
			return b, nil
		}
		nb := b
		switch key {
		case "enter", "c": // Toggle wie bisher
			nb.expanded = !b.expanded
		case "+", "=": // Der Neon-Buchstabe im Label: + klappt AUF
			if !b.expanded {
				nb.expanded = true
			}
		case "-", "_": // und - zu (der Label-Buchstabe ist das Minus)
			if b.expanded {
				nb.expanded = false
			}
		default:
			return b, nil
		}
		return nb, nil
	}
	return b, nil
}
func (b ThinkBlock) Focused() bool   { return b.focused }
func (b ThinkBlock) Focusable() bool { return true }
func (b ThinkBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}
func (b ThinkBlock) SetHover(h bool) Widget {
	nb := b
	nb.hoverHead = h
	return nb
}

// SetHoverAt setzt den Hover zielgenau auf die Kopfzeile — sie ist das
// einzige Falt-Ziel geblieben (▸/▾ im Kopf; die "+ show more"-Zeile ist
// weg, User-Wunsch). Body-Hover highlightet nichts.
func (b ThinkBlock) SetHoverAt(width, x, y int) Widget {
	nb := b
	nb.hoverHead = y == 0 && b.HitAction(width, x, y)
	return nb
}
func (b ThinkBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b ThinkBlock) SetStale(s bool) Widget {
	nb := b
	nb.stale = s
	return nb
}
func (b ThinkBlock) Marked() bool { return b.marked }

// Markdown returns the raw thinking text — plain, without ANSI (FR-29).
func (b ThinkBlock) Markdown() string { return b.text }
func (b ThinkBlock) Resize(width int) Widget {
	return b
}
func (b ThinkBlock) HitAction(width, x, y int) bool {
	if !b.hidden(codeInnerWidth(width)) {
		// Nothing hidden — no fold affordance anywhere, not even the header.
		return false
	}
	// The header caption ("denken ▸ n Zeilen") is the fold affordance —
	// clicking the section title is the natural unfold, and it must NOT
	// toggle the selection mark (FR-30). Der Footer ist reine Rahmenlinie
	// (kein "+ show more" mehr) — kein Klickziel.
	if y != 0 {
		return false
	}
	return x >= 3 && x < 3+runewidth.StringWidth(b.label())
}
func (b ThinkBlock) Copy() (Widget, tea.Cmd) { return b, nil }

// Action: clicking the fold action toggles the block (FR-30) — no focus
// needed, same semantics as pressing enter on the focused block.
func (b ThinkBlock) Action() (Widget, tea.Cmd) {
	nb := b
	nb.expanded = !b.expanded
	return nb, nil
}
func (b ThinkBlock) ID() int { return b.id }
func (b ThinkBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- build -----------------------------------------------------------------

// BuildWidgets turns a structured block tree into interactive widgets.
func BuildWidgets(nodes []render.Node, th *render.Theme, copyFn CopyFunc) []Widget {
	tc := textColorFor(th)
	out := make([]Widget, 0, len(nodes))
	for _, n := range nodes {
		switch n.Kind {
		case render.NodeText:
			tb := NewTextBlock(n.Text, tc)
			if n.List {
				// Nummerierte/Aufgezaehlte Liste als EIN Block mit einer freien
				// Zeile davor und danach (Items bleiben ohne interne Luecken).
				nb := tb
				nb.beforeGap, nb.afterGap = 1, 1
				tb = nb
			}
			out = append(out, tb)
		case render.NodeHeading:
			hb := NewHeadingBlock(n.Text)
			hb.level = n.Level
			if th != nil {
				// Subtile Groessen-Hierarchie h1..h6 (Farbe wird sanft von
				// Action zu Text interpoliert, h1 mit dezenter Unterstreichung)
				// — keine Terminal-Fontgroessen, keine grossen Spaenge.
				hb.style = th.HeadingStyle(n.Level)
			}
			out = append(out, hb)
		case render.NodeCode:
			cb := NewCodeBlock(n.Lang, n.Code, copyFn)
			cb.th = th
			out = append(out, cb)
		case render.NodeTable:
			tb := NewTableBlock(n.Cells, n.Aligns, copyFn)
			tb.th = th
			out = append(out, tb)
		}
	}
	return out
}
