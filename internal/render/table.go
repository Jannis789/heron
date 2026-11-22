package render

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"golang.org/x/term"
)

// maxTableWidth caps the total rendered table width for the string-mode
// renderer (cells wrap to fit). The TUI passes its live viewport width
// instead (dynamic re-flow).
var maxTableWidth = detectWidth()

func detectWidth() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(c)); err == nil && n > 0 {
			return n
		}
	}
	if w, _, err := term.GetSize(0); err == nil && w > 0 {
		return w
	}
	return 100
}

// nodeText collects the plain (unstyled) text under a node.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	inCode := false
	ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if _, ok := c.(*ast.CodeSpan); ok {
				inCode = false
			}
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.CodeSpan:
			// Inline-Code: Backslash-Escapes sind darin literal (CommonMark).
			inCode = true
		case *ast.Text:
			// Text IMMER schreiben (Soft-Break warf frueher den Knotentext
			// weg); der Umbruch selbst wird hier zum Leerzeichen —
			// Ueberschriften/Zellen/Listeneintraege sind einzeilig. Ausserhalb
			// von Code-Spans Backslash-Escapes aufloesen (\* -> *).
			seg := t.Text(src)
			if !inCode {
				seg = unescapeBackslash(seg)
			}
			b.Write(seg)
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.AutoLink:
			// AutoLink traegt die URL in einem privaten Feld (kein Text-Kind
			// im Walk) — ohne Label waere sie komplett verschwunden.
			b.Write(t.Label(src))
		case *extast.TaskCheckBox:
			// Der Parser frisst "[x]"/"[ ]" aus dem Quelltext — ohne
			// Zurueckschreiben verschwanden Checkboxen aus Listen.
			if t.IsChecked {
				b.WriteString("[x] ")
			} else {
				b.WriteString("[ ] ")
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// SGRChain verfolgt die AKTIVEN SGR-Opener eines Strings: \x1b[0m leert die
// Kette, Zwei-Code-Closer (\x1b[22m/23m/24m/27m/29m, 39/49/59) entfernen genau
// ihren Opener, Opener ergaenzen sich. Naives Anhängen (nur die letzte
// Sequenz merken) schlepte geschlossene Paare ("\x1b[1m\x1b[22m") mit und
// oeffnete geschlossene Stile auf Folgeseiten wieder. Genutzt vom Umbruch
// (WrapDisplay/WrapWords), um die aktive Formatierung an Folgelines zu
// reproduzieren (User-Report: "fettgedruckter text wird nicht richtig
// angezeigt, wenn ich im auswahlmode mit up/down bin" — ein umgebrochener
// fett-Absatz verlor ab Zeile 2 sein \x1b[1m), und vom Overlay-Zeichnen
// (styleRange/undercursor) fuer die Wiederherstellung nach dem markierten
// Bereich.
type SGRChain []string

// sgrParams zerlegt die Parameter einer SGR-Sequenz. Composite-Parameter
// (38/48/58 mit 2→r;g;b bzw. 5→Index) bleiben intakt — sonst wuerde das
// 0-Byte im TrueColor "38;2;191;0;255" als Reset missverstanden und die
// Ueberschriften-Kette (\x1b[1;4;38;2;…;4m) wuerde fälschlich geleert.
func sgrParams(seq string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ";")
	out := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if p == "" {
			p = "0"
		}
		out = append(out, p)
		if p == "38" || p == "48" || p == "58" {
			if i+1 < len(parts) && parts[i+1] == "2" {
				i += 4 // 2;r;g;b
			} else if i+1 < len(parts) && parts[i+1] == "5" {
				i += 2 // 5;idx
			}
		}
	}
	return out
}

// sgrCancels liefert die Opener-Parameter, die ein Closer-Param aufhebt
// (22→1/2, 23→3, 24→4, 25→5/6, 27→7, 29→9, 39/49→Farb-Familien, 59→58).
func sgrCancels(closer string) map[string]bool {
	var m map[string]bool
	add := func(vals ...int) {
		if m == nil {
			m = map[string]bool{}
		}
		for _, v := range vals {
			m[strconv.Itoa(v)] = true
		}
	}
	switch closer {
	case "22":
		add(1, 2)
	case "23":
		add(3)
	case "24":
		add(4)
	case "25":
		add(5, 6)
	case "27":
		add(7)
	case "29":
		add(9)
	case "39":
		for n := 30; n <= 37; n++ {
			add(n)
		}
		for n := 90; n <= 97; n++ {
			add(n)
		}
	case "49":
		for n := 40; n <= 47; n++ {
			add(n)
		}
		for n := 100; n <= 107; n++ {
			add(n)
		}
	case "59":
		add(58)
	}
	return m
}

// sgrIsCloser meldet, ob der Param ausschliesslich STILE aufhebt (Zwei-Code-
// Closer bzw. Farb-Defaults) — reine Closer-Sequenzen gehoeren nicht in die
// Kette, sonst wuerde ein geschlossener Stil spaeter wieder geoeffnet.
func sgrIsCloser(p string) bool {
	switch p {
	case "22", "23", "24", "25", "27", "29", "39", "49", "59":
		return true
	}
	return false
}

// Apply nimmt eine vollstaendige SGR-Sequenz (Ende 'm') in die Kette auf.
func (c *SGRChain) Apply(seq string) {
	ps := sgrParams(seq)
	if len(ps) == 0 {
		return
	}
	if ps[0] == "0" {
		*c = (*c)[:0]
		ps = ps[1:]
		if len(ps) == 0 {
			return
		}
	}
	cancelled := map[string]bool{}
	for _, p := range ps {
		for k := range sgrCancels(p) {
			cancelled[k] = true
		}
	}
	if len(cancelled) > 0 {
		kept := (*c)[:0]
		for _, s := range *c {
			drop := false
			for _, p := range sgrParams(s) {
				if cancelled[p] {
					drop = true
					break
				}
			}
			if !drop {
				kept = append(kept, s)
			}
		}
		*c = kept
	}
	for _, p := range ps {
		if !sgrIsCloser(p) && !cancelled[p] {
			*c = append(*c, seq)
			return
		}
	}
	// Reine Closer-Sequenz: die Kette endet hier (kein Opener zu merken).
}

// Join liefert die Kette als wiederzuoeffnende SGR-Folge (leer = plain).
func (c SGRChain) Join() string { return strings.Join(c, "") }

// WrapDisplay splits s into lines of at most w display cells (rune/emoji-aware).
// Hard break — used for code; table cells use wrapWords.
//
// ANSI-bewusst: Escape-Sequenzen werden GANZ uebernommen — sie zaehlen keine
// Zellen und duerfen nie zerschnitten werden (ein halber SGR erscheint im
// Terminal als sichtbarer Muell). Die aktive Formatierung (SGR-Kette) wird
// an Folgeseiten wiederholt, damit ein hart gesplittetes Token weiter in
// seinem Stil bleibt.
func WrapDisplay(s string, w int) []string {
	if w <= 0 || s == "" {
		return []string{s}
	}
	var lines []string
	var cur []rune
	curW := 0
	var chain SGRChain // aktive SGR-Opener (aktiv ab hier)
	rs := []rune(s)
	for i := 0; i < len(rs); {
		if rs[i] == 0x1b {
			j := i + ansiSeqLen(rs, i)
			seq := string(rs[i:j])
			if strings.HasSuffix(seq, "m") {
				chain.Apply(seq)
			}
			cur = append(cur, rs[i:j]...)
			i = j
			continue
		}
		r := rs[i]
		if r == '\n' {
			lines = append(lines, string(cur))
			cur, curW = []rune{}, 0
			i++
			continue
		}
		ww := runewidth.RuneWidth(r)
		if len(cur) > 0 && curW+ww > w {
			lines = append(lines, string(cur))
			// Aktive Formatierung auf der Folgeseite fortsetzen.
			cur, curW = []rune(chain.Join()), 0
		}
		cur = append(cur, r)
		curW += ww
		i++
	}
	lines = append(lines, string(cur))
	return lines
}

// WrapWords wraps s to w display cells at word boundaries; a single token
// wider than w falls back to a hard break. Prose must never split mid-word.
func WrapWords(s string, w int) []string {
	if w <= 0 || s == "" {
		return []string{s}
	}
	var lines []string
	// Aktive SGR-Kette ÜBER den ganzen String (nicht pro Absatz): Inline-
	// Stile wie **fett** spannen über Wortgrenzen — bricht der Umbruch
	// mitten in ihnen, muss die Folgeline die Kette WIEDER OEFFNEN, sonst
	// verliert z. B. ein umgebrochener fett-Absatz ab Zeile 2 sein Bold,
	// während das \x1b[22m-Schlussbyte dort sichtbar verwaist (User-Report:
	// "fettgedruckter text wird nicht richtig angezeigt, wenn ich im
	// auswahlmode mit up/down bin"). SGRChain traegt nur wirklich offene
	// Opener — geschlossene Stile fallen über ihre Zwei-Code-Closer
	// natuerlich heraus, es gibt keine leeren "\x1b[1m\x1b[22m"-Paare auf
	// Folgelines.
	var chain SGRChain
	chainScan := func(word string) {
		rs := []rune(word)
		for i := 0; i < len(rs); {
			if rs[i] == 0x1b {
				j := i + ansiSeqLen(rs, i)
				if seq := string(rs[i:j]); strings.HasSuffix(seq, "m") {
					chain.Apply(seq)
				}
				i = j
				continue
			}
			i++
		}
	}
	cont := false // naechstes Wort beginnt eine Folgeline nach einem Hard-Break
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		var cur string
		var curW int
		flush := func() {
			if cur != "" {
				lines = append(lines, cur)
				cur, curW = "", 0
			}
		}
		for _, word := range words {
			// lipgloss misst Emoji-Praesentation (⚠️/☺️ = 2 Zellen),
			// runewidth nicht — WrapWords faellt sonst eine Zelle zu kurz
			// und die Zeile schiebt sich unter dem Terminalrand durch.
			ww := lipgloss.Width(word)
			if ww > w {
				flush()
				lines = append(lines, WrapDisplay(word, w)...)
				chainScan(word)
				cont = true
				continue
			}
			switch {
			case cont:
				// Folgeline nach Hard-Break: aktive Formatierung fortsetzen
				// (SGR-Prefix kostet keine Zelle — lipgloss.Width sieht es
				// sowieso nicht).
				cur, curW = chain.Join()+word, ww
				cont = false
			case cur == "":
				cur, curW = word, ww
			case curW+1+ww <= w:
				cur += " " + word
				curW += 1 + ww
			default:
				flush()
				// Wrap mitten im Absatz: aktive Formatierung auf die neue
				// Zeile uebernehmen (wie WrapDisplay es am Hard-Break tut).
				cur, curW = chain.Join()+word, ww
			}
			chainScan(word)
		}
		flush()
	}
	return lines
}

func padDisp(s string, w int, a Align) string {
	// lipgloss misst Emoji-Praesentation (⚠️ = 2 Zellen), runewidth nicht —
	// Messung und Padding muessen dieselbe Breite sehen, sonst verschiebt
	// die Zeile die Rahmen (Box-Geometrie vs. Terminal-Realitaet).
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	switch a {
	case AlignRight:
		return strings.Repeat(" ", d) + s
	case AlignCenter:
		l := d / 2
		return strings.Repeat(" ", l) + s + strings.Repeat(" ", d-l)
	default:
		return s + strings.Repeat(" ", d)
	}
}

// wrapCells wraps every cell of a row to its per-column budget (word wrap)
// and haengt eine harte Kante dran: WrapWords misst runewidth, Emoji-
// Praesentation (⚠️) zaehlt im Terminal aber 2 — solche Zeilen werden auf
// das Budget gekuerzt, damit die Spaltenbreite (und damit die Box) haelt.
func wrapCells(row []string, budgets []int) [][]string {
	out := make([][]string, len(budgets))
	for c := range budgets {
		s := ""
		if c < len(row) {
			s = row[c]
		}
		w := budgets[c]
		if w < 1 {
			w = 1
		}
		lines := WrapWords(s, w)
		for i, ln := range lines {
			lines[i] = truncateW(ln, w)
		}
		out[c] = lines
	}
	return out
}

// truncateW schneidet s auf w Anzeige-Zellen (lipgloss-Messung) und
// markiert den Schnitt mit …; Zellen sind kurz, der Scan ist billig.
func truncateW(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	rs := []rune(s)
	for cut := len(rs); cut > 0; cut-- {
		if out := string(rs[:cut]) + "…"; lipgloss.Width(out) <= w {
			return out
		}
	}
	return "…"
}

// DimsLabel builds the compact "R × C" table size label.
func DimsLabel(rows, cols int) string {
	return fmt.Sprintf("%d × %d", rows, cols)
}

// boxWidth returns the total rendered width of a box with the given column
// widths (outer borders, per-column padding, column junctions).
func boxWidth(widths []int) int {
	total := 2
	for _, w := range widths {
		total += w + 2
	}
	total += len(widths) - 1 // junctions between columns
	return total
}

// FooterLine renders the closing bottom border with label and action embedded
// as boxes: ╰─┤ label ├────────────┤ action ├─╯
// actionStyled may carry SGR sequences (focus feedback); actionW is the plain
// display width of the action. st colors the chrome (dim); the action keeps
// its own styling.
func FooterLine(totalW int, label, action, actionStyled string, actionW int, st, labelStyle lipgloss.Style, junctions []int) string {
	// Nur die Action in einem Kasten auf der Rahmenlinie: └──[ COPY ]──┘.
	// Das ist der Basisfall (leeres Label) UND der Degradationspfad, wenn
	// Label + Action das hartes Minimum (8 Zellen Chrome) sprengen — das
	// Label faellt zuerst, die Action bleibt klick-/tastbar (FR-5).
	actionOnly := func() string {
		n := max(totalW-actionW-8, 0)
		return st.Render("└─"+strings.Repeat("─", n)) +
			st.Render("[ ") + actionStyled + st.Render(" ]─┘")
	}
	if strings.TrimSpace(label) == "" {
		return actionOnly()
	}
	labelW := runewidth.StringWidth(label)
	if totalW-labelW-actionW-8 < 0 {
		return actionOnly()
	}
	spaced := totalW-labelW-actionW-12 >= 0
	pad := ""
	if spaced {
		pad = " "
	}
	labelBox := "[" + pad + label + pad + "]"
	extra := 8
	if spaced {
		extra = 12
	}
	n := totalW - labelW - actionW - extra
	if n < 0 {
		n = 0
	}
	dashStart := 2 + len([]rune(labelBox))
	inJunction := func(x int) bool {
		for _, j := range junctions {
			if j == x {
				return true
			}
		}
		return false
	}
	var dash strings.Builder
	for x := dashStart; x < dashStart+n; x++ {
		if inJunction(x) {
			dash.WriteString("┴") // column border meets the footer line
		} else {
			dash.WriteString("─")
		}
	}
	prefix := st.Render("└─") + labelStyle.Render(labelBox) + st.Render(dash.String()+"["+pad)
	suffix := st.Render(pad + "]─┘")
	return prefix + actionStyled + suffix
}

// FooterRail rendert die LEERE Abschlusszeile einer Box: └──────┘ ohne Label
// und ohne Aktions-Kasten — der Grundpfeiler der Progressive-Disclosure-Anzeige
// (die Aktionszeile erscheint nur bei Fokus/Hover/Auswahl). Geometrie bleibt
// exakt die von FooterLine (eine Zeile, gleiche Ecken/Strichstellen), damit
// Scroll-Position und Maus-Hit-Testing einer Box unveraendert bleiben.
func FooterRail(totalW int, st lipgloss.Style, junctions []int) string {
	n := max(totalW-2, 0)
	isJ := make(map[int]bool, len(junctions))
	for _, j := range junctions {
		isJ[j] = true
	}
	var dash strings.Builder
	for x := 1; x <= n; x++ {
		if isJ[x] {
			dash.WriteString("┴")
		} else {
			dash.WriteString("─")
		}
	}
	return st.Render("└" + dash.String() + "┘")
}

// TableFooterRail ist die Tabellen-Variante von FooterRail: die Abschluss-
// zeile kennt die Spalten-Junctions (┴), aber keine Label/Aktion.
func TableFooterRail(widths []int, th *Theme) string {
	return FooterRail(boxWidth(widths), th.BorderStyle(), junctionPositions(widths))
}

// junctionPositions returns the x positions (in cells) of the column
// junctions inside a row rendered with the given widths.
func junctionPositions(widths []int) []int {
	var js []int
	x := 1
	for k := 1; k < len(widths); k++ {
		x += widths[k-1] + 2
		js = append(js, x)
		x++
	}
	return js
}

// tableBody renders the table box down to the last data row; the bottom
// border (with embedded footer) closes it. Columns are separated by dim
// junctions, the header by a double line, data rows by separators — all in
// the theme border color; cells stay plain and wrap at word boundaries.
// Column budgets are proportional to natural content width (sqrt-weighted)
// so wide columns dominate without crushing narrow ones. width is the wrap
// budget (dynamic: pass the live viewport width).
//
// Der Koerper ist eine Pipeline: Spalten kuerzen (truncateCols), Budgets
// verteilen (columnBudgets), Breiten messen (measureColumnWidths), auf
// Footer-Breite wachsen und auffuellen (footerMinWidth/fillWidths), dann
// zeichnen (renderBoxBody).
func tableBody(cells [][]string, aligns []Align, th *Theme, width int) (lines []string, widths []int) {
	if len(cells) == 0 {
		return nil, nil
	}
	cols := len(cells[0])
	for _, r := range cells[1:] {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return nil, nil
	}

	cells, aligns, cols = truncateCols(cells, aligns, cols, width)

	// ERST kuerzen, DANN header/rows ableiten — sonst rendern die beiden
	// noch die Original-Spalten gegen die gekuerzten Budgets.
	header := cells[0]
	rows := cells[1:]

	// Natural (unwrapped) content width per column.
	nat := make([]int, cols)
	for c := 0; c < cols; c++ {
		for _, row := range cells {
			s := ""
			if c < len(row) {
				s = row[c]
			}
			if w := lipgloss.Width(s); w > nat[c] {
				nat[c] = w
			}
		}
	}

	// Proportional budgets: natural widths when they fit, otherwise
	// sqrt-weighted shares of the available space (pi-style proportions).
	avail := width - 2 - 2*cols - (cols - 1)
	if avail < cols {
		avail = cols
	}
	budgets := columnBudgets(nat, avail)

	headerW := wrapCells(header, budgets)
	rowsW := make([][][]string, 0, len(rows))
	for _, r := range rows {
		rowsW = append(rowsW, wrapCells(r, budgets))
	}

	widths = measureColumnWidths(headerW, rowsW, cols)

	// Grow the last column if the footer line would not fit.
	footerMin := footerMinWidth(len(rows), cols, width)
	if total := boxWidth(widths); total < footerMin {
		widths[len(widths)-1] += footerMin - total
	}

	// Fill the available width: distribute surplus evenly across columns so
	// tables span the viewport like pi renders them.
	fillWidths(widths, width)

	return renderBoxBody(headerW, rowsW, len(header) > 0, widths, aligns, th), widths
}

// truncateCols: Spalten, die selbst mit 1 Inhaltszelle nicht mehr in die
// Breite passen (Chrome je Spalte: 2 Padding + 1 Junction, plus 2 Rand),
// fallen von rechts ab — eine …-Spalte markiert den Schnitt. Das
// Kopieren (FR-29) liefert weiterhin ALLE Spalten: Markdown() liest
// die Original-Zellen, nicht die Anzeige-Breiten.
func truncateCols(cells [][]string, aligns []Align, cols, width int) ([][]string, []Align, int) {
	if width < 16 {
		width = 16
	}
	maxCols := cols
	for maxCols > 1 && 4*maxCols+1 > width {
		maxCols--
	}
	if maxCols >= cols {
		return cells, aligns, cols
	}
	keep := maxCols - 1
	trunc := make([][]string, len(cells))
	for i, row := range cells {
		n := min(keep, len(row))
		r := append([]string{}, row[:n]...)
		if i == 0 {
			r = append(r, "…")
		} else {
			r = append(r, "")
		}
		trunc[i] = r
	}
	if len(aligns) > keep {
		aligns = append(append([]Align{}, aligns[:keep]...), AlignDefault)
	}
	return trunc, aligns, maxCols
}

// columnBudgets verteilt avail Anzeige-Zellen auf die Spalten: naturale
// Breiten, wenn sie zusammen passen, sonst sqrt-gewichtete Anteile am
// Platz. Jede Spalte behaelt mindestens 1 Zelle Inhalt.
func columnBudgets(nat []int, avail int) []int {
	budgets := make([]int, len(nat))
	natSum := 0
	for _, n := range nat {
		natSum += n
	}
	if natSum <= avail {
		copy(budgets, nat)
		return budgets
	}
	wSum := 0.0
	for _, n := range nat {
		wSum += math.Sqrt(float64(n))
	}
	assigned := 0
	widest := 0
	for c, n := range nat {
		b := int(math.Round(math.Sqrt(float64(n)) / wSum * float64(avail)))
		if b < 1 {
			b = 1
		}
		budgets[c] = b
		assigned += b
		if n > nat[widest] {
			widest = c
		}
	}
	if rem := avail - assigned; rem > 0 {
		budgets[widest] += rem // Ueberschuss an die breiteste Spalte
	}
	// Negative Reste duerfen keine Spalte unter 1 druecken (Inhalt muss
	// rein) und duerfen die Summe nicht ueber avail heben — sonst
	// ueberlaeuft die Box strukturell die Zielbreite.
	for rem := avail - assigned; rem < 0; {
		reduced := false
		for c := range budgets {
			if rem == 0 {
				break
			}
			if budgets[c] > 1 {
				budgets[c]--
				rem++
				reduced = true
			}
		}
		if !reduced {
			break
		}
	}
	for c, b := range budgets {
		if b < 1 {
			budgets[c] = 1
		}
	}
	return budgets
}

// measureColumnWidths misst nach dem Wrap die breiteste Zeile je Spalte.
func measureColumnWidths(headerW [][]string, rowsW [][][]string, cols int) []int {
	widths := make([]int, cols)
	for c := 0; c < cols; c++ {
		wmax := 0
		for _, ln := range headerW[c] {
			if w := lipgloss.Width(ln); w > wmax {
				wmax = w
			}
		}
		for _, row := range rowsW {
			for _, ln := range row[c] {
				if w := lipgloss.Width(ln); w > wmax {
					wmax = w
				}
			}
		}
		widths[c] = wmax
	}
	return widths
}

// footerMinWidth ist die Ziel-Untergrenze fuer die Boxbreite, damit die
// Footer-Zeile (Dims-Label + COPY-Action) noch passt. Niemals ueber die
// Zielbreite wachsen: der Footer degradiert selbst (FooterLine laesst das
// Label zuerst fallen), die Box aber nie.
func footerMinWidth(rows, cols, width int) int {
	footerMin := runewidth.StringWidth(DimsLabel(rows, cols)) + len("COPY") + 12
	if footerMin > width {
		footerMin -= 4 // drop the inner box padding instead of exceeding the cap
	}
	if footerMin > width {
		footerMin = width
	}
	return footerMin
}

// fillWidths verteilt ueberschuessige Breite gleichmaessig auf die Spalten,
// damit die Tabelle den Viewport spaennt wie pi; der Rest geht ans Ende.
func fillWidths(widths []int, width int) {
	if total := boxWidth(widths); width > total {
		extra := (width - total) / len(widths)
		for c := range widths {
			widths[c] += extra
		}
		if rem := (width - total) % len(widths); rem > 0 {
			widths[len(widths)-1] += rem
		}
	}
}

// renderBoxBody zeichnet Kopf- und Datenzeilen in die Box: Doppellinie
// unter dem Header (falls einer existiert), dim Separatoren zwischen den
// Datenzeilen; Zellen mit Padding und spaltenweiser Ausrichtung.
func renderBoxBody(headerW [][]string, rowsW [][][]string, hasHeader bool, widths []int, aligns []Align, th *Theme) []string {
	cols := len(widths)
	bs := th.BorderStyle()
	fill := th.TableFill()
	alignOf := func(c int) Align {
		if c < len(aligns) {
			return aligns[c]
		}
		return AlignDefault
	}
	sep := func(left, mid, right, hor string) string {
		var b strings.Builder
		b.WriteString(bs.Render(left))
		for c, w := range widths {
			b.WriteString(bs.Render(strings.Repeat(hor, w+2)))
			if c < len(widths)-1 {
				b.WriteString(bs.Render(mid))
			}
		}
		b.WriteString(bs.Render(right))
		return b.String()
	}
	rowLines := func(cells [][]string, header bool) []string {
		maxLines := 0
		for _, ls := range cells {
			if len(ls) > maxLines {
				maxLines = len(ls)
			}
		}
		var out []string
		for i := 0; i < maxLines; i++ {
			var b strings.Builder
			b.WriteString(bs.Render("│"))
			for c := 0; c < cols; c++ {
				ln := ""
				if i < len(cells[c]) {
					ln = cells[c][i]
				}
				cellStyle := fill
				if header {
					cellStyle = th.HeaderStyle()
				}
				b.WriteString(cellStyle.Render(" " + padDisp(ln, widths[c], alignOf(c)) + " "))
				b.WriteString(bs.Render("│"))
			}
			out = append(out, b.String())
		}
		return out
	}
	var lines []string
	lines = append(lines, sep("┌", "┬", "┐", "─"))
	if hasHeader {
		lines = append(lines, rowLines(headerW, true)...)
		// Double-line header separation so the column header stays recognizable.
		lines = append(lines, sep("╞", "╪", "╡", "═"))
	}
	for i, row := range rowsW {
		lines = append(lines, rowLines(row, false)...)
		// Dim separator between data rows so line ends stay visible.
		if i < len(rowsW)-1 {
			lines = append(lines, sep("├", "┼", "┤", "─"))
		}
	}
	return lines
}

// BoxWidth returns the total rendered width of a box with the given column
// widths (outer borders, per-column padding, column junctions).
func BoxWidth(widths []int) int { return boxWidth(widths) }

// ActionRange returns the [start,end) cell positions of the action box in a
// footer line of totalW cells (for mouse hit-testing).
func ActionRange(totalW int, label string, actionW int) (int, int) {
	// Labelless footer (FR-30 think block): "└─" + dashes + "[ action ]─┘".
	// The action box spans actionW+4 cells and ends two cells before the
	// corner (" ]─┘").
	if strings.TrimSpace(label) == "" {
		return totalW - actionW - 6, totalW - 2
	}
	labelW := runewidth.StringWidth(label)
	spaced := totalW-labelW-actionW-12 >= 0
	if spaced {
		return totalW - 8 - actionW, totalW - 4
	}
	return totalW - 5 - actionW, totalW - 3
}

// TableBodyLines renders the table body (no footer line) plus column widths,
// wrapping cells to fit width (dynamic re-flow: pass the live width).
func TableBodyLines(cells [][]string, aligns []Align, th *Theme, width int) (lines []string, widths []int) {
	return tableBody(cells, aligns, th, width)
}

// CenteredActionLine renders a closing border with the action box centered:
// ╰─────[ + mehr anzeigen ]─────╯ — dashes on both sides, so the box never
// touches the corners (FR-30 think block).
func CenteredActionLine(totalW int, action, actionStyled string, actionW int, st lipgloss.Style) string {
	box := actionW + 4 // "[ " + action + " ]"
	n := totalW - box - 2
	if n < 0 {
		// Schmaler als der Action-Kasten: ohne Chrome direkt auf dem
		// Rahmen (Geometrie bleibt via CenteredActionRange synchron).
		n = 0
		if pad := totalW - actionW - 2; pad >= 0 {
			left := pad / 2
			return st.Render("└"+strings.Repeat("─", left)) + actionStyled +
				st.Render(strings.Repeat("─", pad-left)+"┘")
		}
		return st.Render("└") + actionStyled + st.Render("┘")
	}
	left := n / 2
	return st.Render("└"+strings.Repeat("─", left)) +
		st.Render("[ ") + actionStyled + st.Render(" ]") +
		st.Render(strings.Repeat("─", n-left)+"┘")
}

// CenteredActionRange returns the [start,end) cell positions of the centered
// action box (mouse hit-testing, FR-30).
func CenteredActionRange(totalW, actionW int) (int, int) {
	box := actionW + 4
	n := totalW - box - 2
	if n < 0 {
		// Ohne Kasten-Chrome beginnt die Action direkt nach der Ecke —
		// dieselbe Degradation wie in CenteredActionLine.
		if pad := totalW - actionW - 2; pad >= 0 {
			return 1 + pad/2, 1 + pad/2 + actionW
		}
		return 1, 1 + actionW
	}
	left := n / 2
	return 1 + left, 1 + left + box
}

// TableFooterLine renders the closing bottom border of a table with the size
// label and the copy action embedded; chrome colored via the theme border.
func TableFooterLine(widths []int, rows, cols int, th *Theme, action, actionStyled string, actionW int) string {
	return FooterLine(boxWidth(widths), DimsLabel(rows, cols), action, actionStyled, actionW, th.BorderStyle(), th.LabelStyle(), junctionPositions(widths))
}

// renderTable renders a goldmark table as a bordered box whose bottom border
// embeds the size label and copy action. String mode: the width cap is
// maxTableWidth (COLUMNS/terminal at startup).
func renderTable(t *extast.Table, src []byte, th *Theme) string {
	cells, aligns := tableData(t, src)
	body, widths := tableBody(cells, aligns, th, maxTableWidth)
	if len(body) == 0 {
		return ""
	}
	rows := len(cells) - 1
	if rows < 0 {
		rows = 0
	}
	lines := append(body, TableFooterLine(widths, rows, len(widths), th, "COPY", "COPY", len("COPY")))
	return strings.Join(lines, "\n") + "\n"
}
