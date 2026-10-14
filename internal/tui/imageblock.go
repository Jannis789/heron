package tui

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// placeholderChar ist das Kitty-Platzhalterzeichen U+10EEEE (Spec:
// sw.kovidgoyal.net/kitty/graphics-protocol/#unicode-placeholders).
const placeholderChar = "\U0010EEEE"

// kittyDiacritics ist die offizielle Zeilen-/Spalten-Diakritika-Tabelle des
// Kitty-Grafikprotokolls (rowcolumn-diacritics.txt: Kombinierende Zeichen
// der Klasse 230 aus Unicode 6.0.0). Index i kodiert die Zahl i:
// U+0305=0, U+030D=1, U+030E=2, ... Byte-identisch zur Referenz-
// implementierung image.nvim (lua/image/backends/kitty/codes.lua).
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE,
	0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C,
	0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87,
	0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D,
	0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1,
	0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9,
	0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7,
	0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
	0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2,
	0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D,
	0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5,
	0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7,
	0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242,
	0x1D243, 0x1D244,
}

// diacritic liefert das Diakritikon fuer eine Zeilen-/Spaltennummer
// (geclampt auf das Tabellenende — mehr als ~250 Zeilen hat kein Bild).
func diacritic(i int) rune {
	if i < 0 {
		i = 0
	}
	if i >= len(kittyDiacritics) {
		i = len(kittyDiacritics) - 1
	}
	return kittyDiacritics[i]
}

// kittyRegistry verwaltet die Temp-PNG-Dateien (t=f-Transmit) und ob
// ueberhaupt Kitty-Graphics geschrieben wurden — Grundlage fuer Cleanup.
var kittyRegistry = struct {
	sync.Mutex
	files []string
	used  bool
}{}

// ImageBlock zeigt ein Diagramm als PNG ueber das Kitty-Grafikprotokoll
// mit Unicode-Platzhaltern (FR-27) — exakt nach Spezifikation und Referenz
// (image.nvim), nicht nach Rueckgriff:
//
//   - Uebertragung + virtuelles Placement in EINEM kurzen Escape
//     (a=T, t=f): das Terminal liest die PNG-Datei selbst. Kein
//     Base64-Blob im Frame (~150 Byte statt ~15 KB je Bild, Metrik 1).
//   - Die sichtbaren Zellen sind Platzhalter-TEXT: die Vordergrundfarbe
//     kodiert die Bild-ID (\e[38;5;<id>m), U+10EEEE plus Zeilen-/Spalten-
//     Diakritika adressieren jeden Rasterplatz. Weil das normale Text-
//     zellen sind, verschiebt das Terminal das Bild automatisch mit, wenn
//     der Pager scrollt (Metrik 3) — auch teilweise sichtbar (Metrik 2).
//   - ViewRange rendert nur die sichtbaren Zeilen und haelt den Transmit-
//     Escape auf der ERSTEN sichtbaren Zeile: hohe Bilder uebertragen
//     zuverlaessig, auch wenn der Blockanfang aus dem Fenster gescrollt ist.
//   - Aspekt aus echten Zellpixeln (CSI 16 t): rowsForTerm rechnet die
//     Zeilen aus Bildpixeln und CellW/CellH; das virtuelle Placement
//     (c,r) fuegt das Bild seitenverhaeltnisgetreu ein (Metrik 2).
type ImageBlock struct {
	id       int // stabile Block-ID (FR-3)
	kittyID  int // Terminal-Bild-ID 1..250, codierbar in \e[38;5;N m
	pngPath  string
	png      []byte
	pngW     int
	pngH     int
	useImg   bool
	caps     TermCaps
	ascii    string
	fallback CodeBlock
	copyFn   CopyFunc
	focused  bool
	hover    bool
	marked   bool
	th       *render.Theme
	cache    *imgCache
}

// imgCache haelt Prefix + Platzhalterzeilen je (cols, rows): View ist
// dadurch O(1) pro Frame — kein Neu-Aufbau, kein Neu-Kodieren (Metrik 1).
type imgCache struct {
	cols, rows int
	prefix     string
	lines      []string
}

// NewImageBlock baut den Bildblock; Dimensionen aus dem IHDR-Chunk. Bei
// aktiviertem Bildpfad wird das PNG zusaetzlich in einer Temp-Datei
// gesichert (t=f-Transmit: das Terminal liest die Datei selbst).
func NewImageBlock(png []byte, ascii string, useImg bool, caps TermCaps, copyFn CopyFunc, th *render.Theme) ImageBlock {
	w, h, ok := pngDimsFromBytes(png)
	if !ok {
		w, h = 400, 300
	}
	b := ImageBlock{
		png:      png,
		pngW:     w,
		pngH:     h,
		useImg:   useImg,
		caps:     caps,
		ascii:    strings.TrimSuffix(ascii, "\n"),
		fallback: NewCodeBlock("text", ascii, nil),
		copyFn:   copyFn,
		th:       th,
	}
	if b.useImg {
		if p, err := writeKittyFile(png); err == nil {
			b.pngPath = p
		} else {
			imageLogf("image temp-datei fehlgeschlagen: %v (ASCII-Fallback)", err)
			b.useImg = false
		}
	}
	b.fallback.th = th
	return b
}

// writeKittyFile sichert das PNG in einer Temp-Datei und registriert es
// fuer KittyCleanup.
func writeKittyFile(png []byte) (string, error) {
	f, err := os.CreateTemp("", "heron-img-*.png")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(png); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	kittyRegistry.Lock()
	kittyRegistry.files = append(kittyRegistry.files, f.Name())
	kittyRegistry.used = true
	kittyRegistry.Unlock()
	return f.Name(), nil
}

// pngDimsFromBytes liest Breite/Hoehe aus dem IHDR-Chunk (Header only).
func pngDimsFromBytes(b []byte) (int, int, bool) {
	if len(b) < 24 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0, false
	}
	w := int(binary.BigEndian.Uint32(b[16:20]))
	h := int(binary.BigEndian.Uint32(b[20:24]))
	return w, h, w > 0 && h > 0
}

// colsFor begrenzt die Darstellung auf die Blockbreite.
func (b ImageBlock) colsFor(width int) int {
	c := width - 2
	if c < 10 {
		c = 10
	}
	return c
}

// rowsForTerm rechnet die Bildhoehe in Zellen — mit echten Zellpixeln aus
// der Probe (CSI 16 t), sonst 1:2-Schaetzung. Das virtuelle Placement
// (c=cols, r=rows) fuegt das Bild aspektgetreu ein (FR-27, Metrik 2).
func rowsForTerm(pngW, pngH, cols, cellW, cellH int) int {
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 9, 20
	}
	rows := pngH * cols * cellW / (pngW * cellH)
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (b ImageBlock) rowsFor(width int) int {
	return rowsForTerm(b.pngW, b.pngH, b.colsFor(width), b.caps.CellW, b.caps.CellH)
}

// virtualPlacement baut den kombinierten Uebertragungs-/Placement-Escape:
// a=T (transmit + virtuelles Placement, Spec: "can be combined with a=T"),
// t=f (Terminal liest die PNG-Datei selbst), U=1, c/r = Zellraster,
// s/v = Pixelmasse, p=<id> macht das Placement deterministisch —
// Wiederholung ersetzt statt zu vervielfachen (Resize bleibt billig).
func (b ImageBlock) virtualPlacement(cols, rows int) string {
	if b.kittyID < 1 || b.kittyID > 250 || b.pngPath == "" {
		return ""
	}
	payload := base64.StdEncoding.EncodeToString([]byte(b.pngPath))
	return fmt.Sprintf("\x1b_Ga=T,f=100,t=f,i=%d,p=%d,U=1,c=%d,r=%d,s=%d,v=%d,m=0;%s\x1b\\",
		b.kittyID, b.kittyID, cols, rows, b.pngW, b.pngH, payload)
}

// placeholderRow baut Zeile r des Rasters: Vordergrundfarbe kodiert die
// Bild-ID, dann je Zelle U+10EEEE + Zeilen- + Spalten-Diakritikon. Das
// Spec-Beispiel (Bild 42, 2x2) ist: \e[38;5;42m\U10EEEE\U0305\U0305 ... —
// jede Zelle identifiziert ihren Rasterplatz selbst, deshalb zeigt das
// Terminal beim Scrollen automatisch den richtigen Bildausschnitt.
func placeholderRow(kittyID, r, cols int) string {
	var sb strings.Builder
	sb.WriteString("\x1b[38;5;")
	sb.WriteString(strconv.Itoa(kittyID))
	sb.WriteString("m")
	rd := diacritic(r)
	for c := 0; c < cols; c++ {
		sb.WriteString(placeholderChar)
		sb.WriteRune(rd)
		sb.WriteRune(diacritic(c))
	}
	sb.WriteString("\x1b[39m")
	return sb.String()
}

// renderCache baut (und merkt) Prefix + Zeilen fuer (cols, rows).
func (b *ImageBlock) renderCache(cols, rows int) *imgCache {
	if b.cache != nil && b.cache.cols == cols && b.cache.rows == rows {
		return b.cache
	}
	c := &imgCache{cols: cols, rows: rows, prefix: b.virtualPlacement(cols, rows)}
	c.lines = make([]string, rows)
	for r := 0; r < rows; r++ {
		c.lines[r] = placeholderRow(b.kittyID, r, cols)
	}
	b.cache = c
	imageLogf("image raster block=%d kitty=%d c=%d r=%d (cell=%dx%d) tx=%d B path=%s",
		b.id, b.kittyID, cols, rows, b.caps.CellW, b.caps.CellH, len(c.prefix), b.pngPath)
	return c
}

// ViewRange rendert die Bildzeilen [from,to) — Transmit-Escape vor der
// ersten gelieferten Zeile. BlockList ruft das mit dem sichtbaren Fenster,
// damit hohe Bilder auch uebertragen werden, wenn ihr Anfang heraus-
// gescrollt ist (Follow-Modus: neue Bloecke erscheinen unten).
func (b ImageBlock) ViewRange(width, from, to int) string {
	if !b.useImg || b.kittyID < 1 || b.pngPath == "" {
		return b.fallback.View(width)
	}
	cols := b.colsFor(width)
	rows := b.rowsFor(width)
	if from < 0 {
		from = 0
	}
	if to > rows {
		to = rows
	}
	if from >= to {
		return ""
	}
	c := b.renderCache(cols, rows)
	out := make([]string, 0, to-from)
	for r := from; r < to; r++ {
		out = append(out, c.lines[r])
	}
	out[0] = c.prefix + out[0]
	return strings.Join(out, "\n")
}

// View rendert das komplette Bild (Blockanfang sichtbar, z. B. reveal).
func (b ImageBlock) View(width int) string { return b.ViewRange(width, 0, 1<<30) }

func (b ImageBlock) Height(width int) int {
	if !b.useImg {
		return b.fallback.Height(width)
	}
	return b.rowsFor(width)
}

func (b ImageBlock) Update(msg tea.Msg) (Widget, tea.Cmd) { return b, nil }
func (b ImageBlock) Focused() bool                        { return b.focused }
func (b ImageBlock) Focusable() bool                      { return true }
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

// Markdown liefert den ASCII-/Roh-Text (FR-29 yank bleibt nutzbar).
func (b ImageBlock) Markdown() string             { return b.ascii }
func (b ImageBlock) Resize(int) Widget            { return b }
func (b ImageBlock) HitAction(int, int, int) bool { return false }

// Copy kopiert den ASCII-/Roh-Text in die Zwischenablage (FR-5-Pfad).
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

// SetKittyID setzt die Terminal-Bild-ID (1..250; nur dieser Bereich ist
// in \e[38;5;N m codierbar). Der Zeilen-Cache verkoerpert die ID.
func (b ImageBlock) SetKittyID(i int) Widget {
	nb := b
	nb.kittyID = i
	nb.cache = nil
	return nb
}

// KittyCleanup loescht alle uebertragenen Bilder im Terminal (a=d,d=a)
// und entfernt die Temp-PNGs. RunModel ruft das nach Programmende — es
// bleibt kein Bildmuell im Terminal zurueck.
func KittyCleanup() {
	kittyRegistry.Lock()
	used := kittyRegistry.used
	files := kittyRegistry.files
	kittyRegistry.files = nil
	kittyRegistry.used = false
	kittyRegistry.Unlock()
	if used {
		os.Stdout.WriteString("\x1b_Ga=d,d=a\x1b\\")
		imageLogf("cleanup: alle kitty-bilder geloescht, %d temp-dateien entfernt", len(files))
	}
	for _, f := range files {
		os.Remove(f)
	}
}
