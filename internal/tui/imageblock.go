
package tui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// ImageBlock zeigt ein Diagramm als Truecolor-Halbblock-Grafik (FR-27):
// plantuml/mermaid liefern ein PNG, das auf Blockbreite als ▀-Zeilen
// gerendert wird. Reiner Text — scrollt daher sauber mit dem Zellgitter
// (der Kitty-Image-Layer kollidierte mit bubbleteas Repaint und wurde
// verworfen). Copy/Markdown liefern den Roh-Quelltext.
//
// Zu grosse Diagramme werden auf die _Fit-Hoehe_ (maxRows) herunterskaliert,
// damit sie auf den Bildschirm passen und nicht laggen; Fokus-Tasten +/-
// zoomen hinein (Detail) / hinaus (Überblick), t schaltet zwischen Grafik
// und Roh-Quelltext (Bonus). Der Block bleibt dabei immer reiner Text und
// ist damit ueber den Pager scrollbar.
type ImageBlock struct {
	id      int
	png     []byte
	ascii   string // Roh-Quelltext (puml/mermaid-Quelle)
	copyFn  CopyFunc
	focused bool
	hover   bool
	marked  bool
	th      *render.Theme

	// fitRows begrenzt die gerenderte Hoehe (Zeilen) auf die Chat-Hoehe,
	// damit sehr grosse Diagramme ohne Lag auf den Schirm passen. 0 = kein
	// Fit (volle Aufloesung).
	fitRows int
	// zoomPct ist ein Zoom-Faktor in Prozent. 0 = Auto-Fit auf fitRows;
	// >0 zoomen (100 = volle Breite/echte Aufloesung, darueber Detail).
	zoomPct int
	// showRaw blendet den Roh-Quelltext statt der Grafik (Bonus-FR).
	showRaw bool
}

func NewImageBlock(png []byte, ascii string, copyFn CopyFunc, th *render.Theme) ImageBlock {
	b := ImageBlock{png: png, ascii: ascii, copyFn: copyFn, th: th}
	if b.png == nil {
		b.showRaw = true // kein Bild => sicher Roh-Text zeigen
	}
	return b
}

// WithFitRows setzt die Zeilen-Obergrenze fuer das Auto-Fit (FR-Zoom).
func (b ImageBlock) WithFitRows(n int) ImageBlock {
	b.fitRows = n
	return b
}

// WithZoom setzt den Zoom explizit (0=Auto-Fit). Fuer Tests.
func (b ImageBlock) WithZoom(pct int) ImageBlock {
	if pct < 0 {
		pct = 0
	}
	b.zoomPct = pct
	return b
}

// artCacheKey identifiziert die gerenderte Halbblock-Grafik eindeutig:
// Block-ID plus Spaltenbreite (die von Breite UND Zoom abhaengt).
type artCacheKey struct {
	id   int
	cols int
}

var artCache sync.Map // artCacheKey -> []string

// colsFor liefert die Spalten fuer die Halbblock-Grafik: die innere Breite,
// skaliert mit dem Zoom (0 = Auto-Fit auf fitRows).
func (b ImageBlock) colsFor(width int) int {
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	if b.zoomPct > 0 {
		cols := inner * b.zoomPct / 100
		if cols < 2 {
			cols = 2
		}
		return cols
	}
	if b.fitRows > 0 {
		w, h, ok := render.PNGDims(b.png)
		if ok && h > 0 {
			// rows(cols) ≈ h*cols/(2w) <= fitRows  =>  cols <= 2w*fitRows/h
			cols := 2 * w * b.fitRows / h
			if cols < 2 {
				cols = 2
			}
			if cols < inner {
				return cols
			}
		}
	}
	return inner
}

// artFor liefert die Halbblock-Zeilen (gecached pro id+cols).
func (b ImageBlock) artFor(width int) []string {
	cols := b.colsFor(width)
	if b.id >= 1 {
		if v, ok := artCache.Load(artCacheKey{b.id, cols}); ok {
			return v.([]string)
		}
	}
	lines := render.PNGToANSI(b.png, cols)
	if lines == nil {
		lines = []string{}
	}
	if b.id >= 1 {
		artCache.Store(artCacheKey{b.id, cols}, lines)
	}
	return lines
}

func (b ImageBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	if b.showRaw {
		return b.rawView(width)
	}
	art := b.artFor(width)
	inner := b.colsFor(width)
	bs := b.th.BorderStyle()
	label := "bild"
	if b.zoomPct > 0 {
		label = "bild " + itoa(b.zoomPct) + "%"
	}
	labelW := runewidth.StringWidth(label)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	lines := make([]string, 0, len(art)+2)
	lines = append(lines, bs.Render("┌─ ")+b.th.ActionStyle().Render(label)+
		bs.Render(" "+strings.Repeat("─", dashes)+"┐"))
	for _, ln := range art {
		gap := inner - runewidth.StringWidth(ln)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+" "+ln+strings.Repeat(" ", gap)+" "+bs.Render("│"))
	}
	lines = append(lines, bs.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return strings.Join(lines, "\n")
}

func (b ImageBlock) Height(width int) int {
	if b.showRaw {
		return b.rawHeight(width)
	}
	return 1 + len(b.artFor(width)) + 1
}

// rawView rendert den Roh-Quelltext in einem einfachen Rahmen (Bonus-FR:
// Wechsel zwischen Grafik und puml/mermaid-Rohquelle).
func (b ImageBlock) rawView(width int) string {
	if width < 16 {
		width = 16
	}
	inner := b.colsFor(width)
	bs := b.th.BorderStyle()
	label := "roh"
	labelW := runewidth.StringWidth(label)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	var body []string
	for _, ln := range textWrapped(b.ascii, inner) {
		body = append(body, bs.Render("│")+" "+ln+strings.Repeat(" ", max(0, inner-runewidth.StringWidth(ln)))+" "+bs.Render("│"))
	}
	lines := make([]string, 0, len(body)+2)
	lines = append(lines, bs.Render("┌─ ")+b.th.ActionStyle().Render(label)+
		bs.Render(" "+strings.Repeat("─", dashes)+"┐"))
	lines = append(lines, body...)
	lines = append(lines, bs.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return strings.Join(lines, "\n")
}

func (b ImageBlock) rawHeight(width int) int {
	if width < 16 {
		width = 16
	}
	return 1 + len(textWrapped(b.ascii, b.colsFor(width))) + 1
}

func (b ImageBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && b.focused {
		switch km.String() {
		case "+", "=":
			// Zoom hinein durch die Stufen: 0(auto) -> 50 -> 100 -> 150 -> 200
			return b.WithZoom(nextZoom(b.zoomPct, true)), nil
		case "-", "_":
			// Zoom hinaus zurueck Richtung Auto-Fit (0).
			return b.WithZoom(nextZoom(b.zoomPct, false)), nil
		case "t":
			nb := b
			nb.showRaw = !nb.showRaw
			return nb, nil
		}
	}
	return b, nil
}
func (b ImageBlock) Focused() bool   { return b.focused }
func (b ImageBlock) Focusable() bool { return true }
func (b ImageBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}
func (b ImageBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	return nb
}
func (b ImageBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	return nb
}
func (b ImageBlock) Marked() bool { return b.marked }

// Markdown liefert den Roh-Quelltext (FR-29 yank bleibt nutzbar).
func (b ImageBlock) Markdown() string { return b.ascii }
func (b ImageBlock) Resize(int) Widget { return b }
func (b ImageBlock) HitAction(int, int, int) bool { return false }

// Copy kopiert den Roh-Quelltext in die Zwischenablage (FR-5-Pfad).
func (b ImageBlock) Copy() (Widget, tea.Cmd) {
	if b.copyFn == nil {
		return b, nil
	}
	return b, b.copyFn(b.ascii)
}
func (b ImageBlock) ID() int { return b.id }
func (b ImageBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// zoomSteps sind die Zoom-Stufen; 100 = volle (naturale) Aufloesung.
// Da Halbblock-Bilder nie breiter als die Terminal-Breite sein koennen, ist
// der sinnvolle Zoom das _Herunterskalieren_: - verkleinert zu grosse
// Diagramme (Ueberblick), + zoemt zurueck zur vollen Aufloesung.
var zoomSteps = []int{100, 75, 50, 25, 10}

// nextZoom bewegt current auf der zoomSteps-Leiter. current==0 bedeutet
// Auto-Fit; "+" springt von dort direkt zur vollen Aufloesung.
func nextZoom(current int, up bool) int {
	if up && current == 0 {
		return 100 // Auto-Fit -> naturale Aufloesung
	}
	if !up && current == 0 {
		return 75 // Auto-Fit -> Ueberblick (Verkleinern)
	}
	idx := -1
	for i, s := range zoomSteps {
		if s == current {
			idx = i
		}
	}
	if idx < 0 {
		return current
	}
	if up {
		if idx > 0 {
			return zoomSteps[idx-1] // verkleinert -> voller
		}
		return current // bereits voll (kann Terminal nicht ueberschreiten)
	}
	if idx+1 < len(zoomSteps) {
		return zoomSteps[idx+1] // voller -> verkleinert
	}
	return current
}

// itoa is a small int->decimal helper (keeps imageblock.go free of fmt).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
