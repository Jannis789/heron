package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// The prompt box hugs one row and grows as the user types, without ever
// eating more than a share of the screen.
const (
	inputMinHeight = 1
	inputMaxHeight = 8
	// inputChromeRows are the two frame rows of the prompt box.
	inputChromeRows = 2
)

// syncInputSize grows the prompt box with its content and keeps the chat
// viewport sized to whatever is left over. Height follows the SOFT-WRAPPED
// row count: the textarea already word-wraps long lines at its width, so
// the box must grow as soon as a line is full — not only after explicit
// ctrl+j newlines.
func (m *Model) syncInputSize() {
	w := m.width
	if w < 24 {
		w = 80
	}
	inner := w - 4
	var rows []string
	for _, ln := range strings.Split(m.input.Value(), "\n") {
		rows = append(rows, render.WrapWords(ln, inner)...)
	}
	lines := len(rows)
	if lines < inputMinHeight {
		lines = inputMinHeight
	}
	if lines > inputMaxHeight {
		lines = inputMaxHeight
	}
	if lines != m.input.Height() {
		m.input.SetHeight(lines)
	}
	// The textarea viewport follows the cursor only downward: an insert
	// that pushes the cursor past a too-small box scrolls it and it never
	// scrolls back (repositionView has no upward correction for a grown
	// window) — the first rows stayed hidden. When the whole value fits the
	// box and the cursor is at the end (typing/pasting flow), reset the
	// viewport via SetValue; the cursor lands back at the end. Mid-text
	// cursors are left alone, an over-height box scrolls intentionally.
	//
	// atEnd-Check (Regression 2026-10-31): Der alte Vergleich hielt
	// CharOffset (nur INNERHALB der Wrap-Zeile, inkl. Nachlauf-Whitespace)
	// gegen die Laenge der GANZEN logischen Zeile — bei EINER umgebrochenen
	// Zeile (Standard-Tippfall!) ging er nie wahr, ihr Anfang blieb
	// abgeschnitten. Wasserdicht: Cursor in der letzten harten Zeile
	// (Line/LineCount) und am deren Ende — verbrauchte Runen der Zeile =
	// StartColumn+ColumnOffset (beide LineInfo-Aeste tragen m.col ein).
	if v := m.input.Value(); v != "" && len(rows) <= lines {
		hard := strings.Split(v, "\n")
		last := hard[len(hard)-1]
		li := m.input.LineInfo()
		atEnd := m.input.Line() == m.input.LineCount()-1 &&
			li.StartColumn+li.ColumnOffset >= len([]rune(last))
		if atEnd {
			m.input.SetValue(v)
		}
	}
	m.relayout()
}

// relayout recomputes the chat viewport so the whole screen fits exactly:
// title, status line, the prompt box as it renders right now, a gap, and the
// chat takes whatever is left. Using the rendered height — not the configured
// one — keeps the bottom border of the box from being clipped.
func (m *Model) relayout() {
	inputRows := len(strings.Split(m.inputView(), "\n"))
	used := 3 + inputRows // title, box, gap, footer
	if m.workingLine() != "" {
		used++ // the "AI is working" row above the box while a turn runs
	}
	h := m.height - used
	if h < 1 {
		h = 1
	}
	if m.inline {
		// timg-Modell: alle Zeilen rendern (kein Slice), Terminal scrollt
		// nativ — die Fensterhoehe bleibt gesetzt, damit der Zeilen-Cap
		// der Bilder weiter greift.
		m.chat = m.chat.SetSize(m.width, h).SetInline(true)
		return
	}
	m.chat = m.chat.SetSize(m.width, h)
}

// inputView renders the prompt area: a framed box that hugs the typed text,
// with the send hint embedded in the bottom border — the same chrome the
// tables and code blocks use.
// rewritePromptRow swaps the one background code the focused textarea style
// paints its cursor line with. bubbles v1 keeps that style unexported, so it
// cannot be themed directly — and plain ANSI black glares against the theme
// background. Everything else (grey placeholder, lavender frame) is left as
// the textarea produced it.
func rewritePromptRow(ln string, th *render.Theme) string {
	if th == nil {
		return ln
	}
	return strings.ReplaceAll(ln, "\x1b[40m", th.BackgroundSeq())
}

// nixIcon prefixes the workspace path — the Nerd Font NixOS glyph.
const nixIcon = "󱄅"

// Neon-Verlauf des Eingaberahmens, solange ein Turn laeuft (Lila -> Magenta).
// Nur die RAHMEN-ZEICHEN tragen ihn; Inhalt, Label und Fusszeile bleiben
// ruhig. Wie bei crushs Regenbogen-Spinner (internal/ui/anim) ist der
// Verlauf ein PING-PONG-Band (Lila -> Magenta -> Lila) ueber den ganzen
// Box-Umfang, dessen Phase pro Spinner-Takt weiterrueckt: Das Farbband
// fliesst im Uhrzeigersinn um die Box.
const (
	// neonFrom: Cornflower-artiges Blauviolett (#6C9EFF) — bewusst weit von
	// Magenta weg, damit das Band beim Umkreisen sichtbar durch das Spektrum
	// laeuft (Neon-Lila #BF00FF lag zu dicht am Endton: zu wenig Kontrast).
	neonFrom = "#6C9EFF"
	neonTo   = "#FF00CC" // Neon-Magenta

	// neonPhaseStep: Umfangs-Zellen pro Spinner-Takt — 18 = dreimal die
	// vorherige Geschwindigkeit (insgesamt 9x gegenueber Fassung 1).
	neonPhaseStep = 18
	// neonFadeStep: 0.5 blendet in zwei Takten (~180 ms) ein/aus —
	// 1.0 waere kein Fade mehr, nur ein Sprung.
	neonFadeStep float32 = 0.5
)

// neonBand liefert die Farbwerte (Hex) des Bandes an allen Umfangs-
// Positionen, bereits mit dem Fade-Level gegen die Ruhefarbe geblendet:
// 0 = Ruhe (gar nicht aufgerufen), 1 = volles Band, dazwischen weiche
// Uebergangsphasen — der Rahmen cross-fadet statt zu springen.
func (m Model) neonBand(per int, baseHex string) []string {
	band := make([]string, per)
	for i := 0; i < per; i++ {
		t := float64(i) / float64(per)
		f := t * 2
		if f > 1 {
			f = 2 - f // Ping-Pong: hin und zurueck, geschlossen am Umfang
		}
		hex := render.LerpHex(neonFrom, neonTo, f)
		if m.neonLevel < 1 {
			hex = render.LerpHex(baseHex, hex, float64(m.neonLevel))
		}
		band[i] = hex
	}
	return band
}

// neonLine faerbt die Rahmen-Glyphen einer Rahmenzeile mit styleAt(i) — i
// zaehlt nur Rahmen-Zellen der Zeile (der Aufrufer uebersetzt sie in die
// Umfangs-Position); Nicht-Rahmen-Glyphen wie das Workspace-Label behalten bs.
func neonLine(s string, bs lipgloss.Style, styleAt func(i int) lipgloss.Style) string {
	rs := []rune(s)
	var sb strings.Builder
	c := 0
	for _, r := range rs {
		switch r {
		case '┌', '┐', '└', '╯', '─', '│':
			sb.WriteString(styleAt(c).Render(string(r)))
			c++
		default:
			sb.WriteString(bs.Render(string(r)))
		}
	}
	return sb.String()
}

// topLabel is the workspace the session runs in — nothing else. The app name
// already lives in the title row.
func (m Model) topLabel() string {
	if m.cwd == "" {
		return ""
	}
	return nixIcon + " " + m.cwd
}

// stateLabel is the bottom-left footer: model and effort — the same in every
// state (FR-23, minimal: no token/price dump, no elapsed/abort chrome).
func (m Model) stateLabel() string {
	model := m.modelLabel
	if model == "" {
		model = "goose"
	}
	effort := m.effort
	if effort == "" {
		effort = "medium"
	}
	if prov := m.providerLabel(); prov != "" {
		return prov + " · " + model + " · " + effort
	}
	return model + " · " + effort
}

// providerLabel loest den Provider-Wert ueber die annoncierten Optionen zum
// lesbaren Namen auf ("zai_coding_plan" → "Z.AI (GLM)"); ohne Treffer oder
// Optionen bleibt der Rohwert, ohne Provider bleibt die Zeile wie bisher.
func (m Model) providerLabel() string {
	if m.provider == "" {
		return ""
	}
	for _, o := range m.providerOptions {
		if o.Value == m.provider {
			return o.Name
		}
	}
	return m.provider
}

func (m Model) inputView() string {
	w := m.width
	if w < 24 {
		w = 80
	}
	th := m.theme
	// Monochrome lavender chrome: one hue for frame, label and hints. The
	// former accent colour and the inner background fill made the box read as
	// two colours and left a visible patch around the placeholder.
	// inputFocus (Fokus-Kette, arrow down vom letzten Block): Neon-Rahmen —
	// dasselbe Signal wie bei den Chat-Bloecken.
	bs := borderStyleFor(th, m.inputFocus, false)
	// Neon-Band auf den Rahmenzeichen: Ein Ping-Pong-Verlauf Cornflower ->
	// Magenta -> Cornflower ueber den GESAMTEN Box-Umfang (im Uhrzeigersinn:
	// oben hin, rechts runter, unten zurueck, links hoch). neonPhase schiebt
	// das Band pro Spinner-Takt neonPhaseStep Zellen weiter, neonLevel
	// blendet es beim Turnstart ein und nach der Antwort wieder aus —
	// glow greift solange, nicht nur waehrend m.running.
	// FR-39 "hoechstens EIN Neon-Rahmen": Haelt die Fokus-Kette den Rahmen
	// im Chat (Block-Fokus ODER Auswahl), ruht das Band am Eingabefeld —
	// die magenta Neon-Zeichen sind unique, der laufende Turn meldet sich
	// ueber die Hint-Leiste (ctrl+c abort).
	glow := m.neonLevel > 0 && !m.chat.Focused() && m.chat.markedCount() == 0
	rows := strings.Split(m.input.View(), "\n")
	nRows := len(rows)
	per := 2 * (w + nRows)
	baseHex := "#B4BEFE"
	if th != nil {
		baseHex = th.Border.Color
		if m.inputFocus {
			baseHex = th.FocusColor()
		}
	}
	band := m.neonBand(per, baseHex)
	ph := m.neonPhase % per
	at := func(pos int) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(band[(pos+ph)%per]))
	}
	frameLine := func(s string, posOf func(i int) int) string {
		if !glow {
			return bs.Render(s)
		}
		return neonLine(s, bs, func(i int) lipgloss.Style { return at(posOf(i)) })
	}
	// Umfangs-Positionen: oben links->rechts, rechts oben->unten, unten
	// rechts->links, links unten->oben — ein geschlossener Uhrzeigersinn.
	topPos := func(i int) int { return i }
	rightPos := func(k int) int { return w + k }
	bottomPos := func(i int) int { return w + nRows + (w - 1 - i) }
	leftPos := func(k int) int { return 2*w + nRows + (nRows - 1 - k) }
	// The prompt row itself gets the theme background, so the box interior is
	// filled and does not show the terminal's default background.
	fill := th.TableFill()
	inner := w - 4

	label := m.topLabel()
	lines := make([]string, 0, 8)
	if label == "" {
		lines = append(lines, frameLine("┌"+strings.Repeat("─", w-2)+"┐", topPos))
	} else {
		dashes := w - 5 - runewidth.StringWidth(label)
		if dashes < 0 {
			dashes = 0
		}
		lines = append(lines, frameLine("┌─ "+label+" "+strings.Repeat("─", dashes)+"┐", topPos))
	}

	// Model/effort picker (FR-20): the box expands into the option list.
	if th != nil && m.pickerOpen {
		ls := th.LabelStyle()
		active := m.modelLabel
		if m.pickerKind == "effort" {
			active = m.effort
		}
		row := func(s string) string {
			gap := w - 4 - lipgloss.Width(s)
			if gap < 0 {
				gap = 0
			}
			return bs.Render("│") + " " + s + strings.Repeat(" ", gap) + " " + bs.Render("│")
		}
		// /model and /resume carry a search line above the list; /effort
		// is short enough to browse without one (FR-20/FR-44).
		if m.pickerKind == "model" || m.pickerKind == "resume" {
			if m.pickerQuery == "" {
				lines = append(lines, row("> "+ls.Render("search")))
			} else {
				lines = append(lines, row("> "+runewidth.Truncate(m.pickerQuery, inner-3, "…")))
			}
		}
		options := m.pickerOptions()
		if len(options) == 0 {
			lines = append(lines, row("  "+ls.Render("no options")))
		}
		rows := m.pickerVisibleRows()
		end := min(m.pickerScroll+rows, len(options))
		// Resume rows render their detail dimmed behind the label — the
		// rows list and the labels come from the same filter, so the
		// indexes stay aligned (FR-44).
		var resumeRows []SessionRow
		if m.pickerKind == "resume" {
			resumeRows = m.filteredResumeRows()
		}
		for idx := m.pickerScroll; idx < end; idx++ {
			name := options[idx]
			prefix := "    "
			if idx == m.pickerCursor {
				prefix = "  ▸ "
			}
			// Resume: newest session starts at the top, there is no
			// "active" marker — and long rows truncate to the box.
			if m.pickerKind == "resume" {
				rowText := name
				if idx < len(resumeRows) && resumeRows[idx].Detail != "" {
					rowText += " " + resumeRows[idx].Detail
				}
				lines = append(lines, row(prefix+runewidth.Truncate(rowText, inner-5, "…")))
				continue
			}
			// The active option keeps its ✓ even while the cursor is
			// somewhere else — scrolling must not hide what is live (FR-20).
			suffix := ""
			if name == active {
				suffix = " ✓"
			}
			lines = append(lines, row(prefix+name+suffix))
		}
		lines = append(lines, bs.Render("└"+strings.Repeat("─", w-2)+"╯"))
		return strings.Join(lines, "\n")
	}

	for i, raw := range rows {
		ln := rewritePromptRow(raw, th)
		// Slash mode (FR-20) renders its own row: typed text + dim ghost —
		// shell-style autocomplete on the same line. The textarea row stays
		// out of the way, so no cursor cell or styled padding can clash.
		if i == 0 && th != nil && m.slashActive() {
			if rem := m.slashGhostRemainder(); rem != "" {
				val := m.input.Value()
				budget := inner - runewidth.StringWidth(val)
				if budget > 0 {
					ln = rewritePromptRow(val, th) + th.LabelStyle().Faint(true).Render(runewidth.Truncate(rem, budget, ""))
				}
			}
		}
		gap := inner - lipgloss.Width(ln)
		if gap < 0 {
			gap = 0
		}
		// Seitliche Rahmenzellen: im Glow auf ihrer Umfangs-Position im
		// fliessenden Neon-Band.
		l, r := bs.Render("│"), bs.Render("│")
		if glow {
			l = at(leftPos(i)).Render("│")
			r = at(rightPos(i)).Render("│")
		}
		// One fill across content + padding keeps the background continuous
		// instead of a seam between the text and the empty cells.
		lines = append(lines, l+" "+
			fill.Render(ln+strings.Repeat(" ", gap))+" "+r)
	}

	// Untere Rahmenlinie: leer (crush-style) — das Copy-UI lebt in der
	// zentralen Hint-Leiste unten (app.footerView); im laufenden Turn Neon.
	lines = append(lines, frameLine("└"+strings.Repeat("─", w-2)+"╯", bottomPos))
	return strings.Join(lines, "\n")
}

// ---- Copy-Ziel + Aktion (Hint-Leiste unten, Taste c) -----------------------

// copyTarget ist das Ziel der Copy-Aktion: die Auswahl (markierter Block,
// FR-29) oder — ohne Auswahl — der fokussierte Block.
func (m Model) copyTarget() (Widget, int, bool) {
	l := m.chat
	if l.markedCount() > 0 {
		for i, it := range l.items {
			if l.marks[it.ID()] {
				return it, i, true
			}
		}
	}
	if l.Focused() {
		return l.items[l.focus], l.focus, true
	}
	return nil, -1, false
}

// copySelection fuehrt die Copy-Aktion aus (Taste c): kopiert das Ziel als
// rohes Markdown und blendet den Flash ("✓ kopiert" in der Hint-Leiste)
// bis zur naechsten Eingabe. Ziel ist eine Ueberschrift, wird der GANZE
// Abschnitt bis zur naechsten Ueberschrift kopiert.
func (m Model) copySelection() (Model, tea.Cmd) {
	// Shift-Auswahl hat Vorrang: Nur ihr Bereich wird kopiert.
	if m.chat.selActive() {
		if m.copyFn == nil {
			return m, nil
		}
		m.copiedFlash = true
		return m, m.copyFn(m.chat.selMarkdown())
	}
	it, idx, ok := m.copyTarget()
	if !ok {
		return m, nil
	}
	if headingAt(it) {
		if m.copyFn == nil {
			return m, nil
		}
		return m, m.copyFn(m.chat.sectionMarkdown(idx))
	}
	_, cmd := it.Copy()
	if cmd == nil {
		// FR-40: Auch reine Textabsaetze sind eigene Segmente und copy-bar
		// — TextBlock.Copy() hat kein eigenes copyFn, der Weg geht hier.
		if m.copyFn == nil {
			return m, nil
		}
		return m, m.copyFn(it.Markdown())
	}
	m.copiedFlash = true
	return m, cmd
}
