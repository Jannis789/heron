package tui

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	id       int    // stabile Block-ID (FR-3)
	kittyID  int    // Terminal-Bild-ID 1..250, codierbar in \e[38;5;N m
	svg      []byte // Vektor-Quelle (plantuml -tsvg): bei Resize neu rasern
	pngPath  string
	png      []byte
	pngW     int
	pngH     int
	maxRows  int // Zeilen-Obergrenze (Anteil des Chat-Fensters), 0 = frei
	useImg   bool
	caps     TermCaps
	ascii    string
	fallback CodeBlock
	copyFn   CopyFunc
	focused  bool
	hover    bool
	marked   bool
	th       *render.Theme
}

// imgCache haelt Prefix + Platzhalterzeilen je (cols, rows): View ist
// dadurch O(1) pro Frame — kein Neu-Aufbau, kein Neu-Kodieren (Metrik 1).
// txBuilt/txCount steuern, dass der Transmit-Escape (a=T,t=f) nur kurz bei
// Erscheinen des Blocks gesendet wird: bubbletea kann gerenderte Frames
// vor dem Flush verwerfen, deshalb ein kurzes Zeitfenster statt strikt
// einmal — aber NIE pro Scroll-Schritt (das Terminal muesste die PNG bei
// jedem Schritt neu lesen/dekodieren -> 1fps, gemessen im Ghostty).
type imgCache struct {
	cols, rows int
	inline     bool   // timg-Hauptpfad: nur der Direkt-Escape, keine Platzhalter
	own        bool   // pngPath wurde von renderCache gerastert (Temp-Datei)
	src        string // pngPath des Blocks, aus dem der Raster gebaut wurde
	pngPath    string // effektive Transmit-Datei (bei SVG: die gerasterte)
	prefix     string
	lines      []string
	txBuilt    time.Time
	txCount    int
}

// imgCaches haelt die Raster paketweit: ImageBlock ist ein Wertetyp — ein
// Cache-Feld auf der Struktur wuerde bei jedem Update-Kopieren verworfen
// und der Raster (inkl. Transmit-Fenster) JEDEN Frame neu gebaut. Der
// Transmit-Escape ging daraufhin bei jedem Scroll-Schritt erneut raus und
// das Terminal hat die PNG bei jedem Schritt neu dekodiert (1fps, gemessen).
type imgCacheKey struct {
	blockID, kittyID, cols, rows int
}

var (
	imgCacheMu sync.Mutex
	imgCaches  = map[imgCacheKey]*imgCache{}
)

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
	if b.useImg && len(png) > 0 {
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

// imageScale: Default-Faktor, um den ein Diagramm ueber seine intrinsische
// Vektorbreite hinaus gezeigt wird. 2x = Retina-Puffer — scharf (1:1-Pixel)
// und groesser als nativ, ohne ein 251-px-Diagramm auf 3015 px zu strecken.
const imageScale = 2

// imageScaleFactor liest den Anzeige-Faktor (HERON_IMAGE_SCALE, Default 2).
// Das ist ein Geschmacks-Konflikt, den der Nutzer entscheidet, nicht der
// Code: 1 = kompakt (Text bleibt NEBEN dem Bild sichtbar), 2 = groesser und
// trotzdem 1:1-scharf (portrait Diagramme fuellen dann das Fenster), 3+ =
// ueberdimensioniert. Per Env ohne Rebuild abstimmbar.
var imageScaleOnce sync.Once
var imageScaleVal int

func imageScaleFactor() int {
	imageScaleOnce.Do(func() {
		imageScaleVal = imageScale
		if s := os.Getenv("HERON_IMAGE_SCALE"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 8 {
				imageScaleVal = n
			}
		}
	})
	return imageScaleVal
}

// colsFor begrenzt die Darstellung auf die Blockbreite — UND auf die
// intrinsische Bildgroesse x imageScale. Ohne diese Obergrenze wurde ein
// kleines Diagramm auf die ganze Fensterbreite gestreckt: 6-Megapixel-
// Raster, der Schirm voll Bild (kein Text sichtbar), 158 kB Output pro
// Frame (laggy). Mit Obergrenze rastert renderCache auf ~0x Terminal-
// Skalierung (scharf) und das Bild laesst Platz fuer Text (FR-27 Metrik 2).
func (b ImageBlock) colsFor(width int) int {
	c := width - 2
	if c < 10 {
		c = 10
	}
	cellW := b.caps.CellW
	if cellW <= 0 {
		cellW = 9 // gleicher Fallback wie rowsForTerm
	}
	if b.pngW > 0 {
		maxCols := b.pngW * imageScaleFactor() / cellW
		if maxCols < 10 {
			maxCols = 10
		}
		if c > maxCols {
			c = maxCols
		}
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
	_, rows := b.dims(width)
	return rows
}

// dims liefert (cols, rows) unter Beachtung ALLER Grenzen: Fensterbreite,
// intrinsische Breite x imageScale UND die Zeilen-Obergrenze des Chat-
// Fensters. Trifft die Zeilengrenze, werden die Spalten zurueckgerechnet —
// der Aspekt bleibt exakt (Terminal skaliert 0x, Metrik 2), statt dass ein
// Portrait-Diagramm (377x598 -> 59 Zeilen bei 59 Zeilen Fenster) das ganze
// Fenster fuellt und fuer Text kein Platz mehr bleibt.
func (b ImageBlock) dims(width int) (int, int) {
	cols := b.colsFor(width)
	rows := rowsForTerm(b.pngW, b.pngH, cols, b.caps.CellW, b.caps.CellH)
	if b.maxRows <= 0 || rows <= b.maxRows {
		return cols, rows
	}
	cellW, cellH := b.caps.CellW, b.caps.CellH
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 9, 20
	}
	capped := b.maxRows * b.pngW * cellH / (b.pngH * cellW)
	if capped < 1 {
		capped = 1
	}
	if capped < cols {
		cols = capped
	}
	rows = rowsForTerm(b.pngW, b.pngH, cols, b.caps.CellW, b.caps.CellH)
	if rows > b.maxRows {
		rows = b.maxRows // Rundungsrest; das Placement fuegt aspektgetreu ein
	}
	return cols, rows
}

// SetMaxRows begrenzt die Zeilenzahl auf einen Anteil des Chat-Fensters,
// damit neben dem Bild immer Platz fuer Text bleibt (heightCap in
// blocklist.go).
func (b ImageBlock) SetMaxRows(n int) Widget {
	nb := b
	nb.maxRows = n
	return nb
}

// imgRowPct: Anteil der Fensterhoehe, den ein Bild belegen darf (Prozent).
// 60% ist die Vorgabe — bei 59 Zeilen sind das 35 Zeilen Bild, 24 Zeilen
// Text. HERON_IMAGE_ROWS uebersteuert ohne Rebuild.
const imgRowPct = 60

var imgRowOnce sync.Once
var imgRowVal int

func imgRowPctVal() int {
	imgRowOnce.Do(func() {
		imgRowVal = imgRowPct
		if s := os.Getenv("HERON_IMAGE_ROWS"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n >= 10 && n <= 100 {
				imgRowVal = n
			}
		}
	})
	return imgRowVal
}

// virtualPlacement baut den kombinierten Uebertragungs-/Placement-Escape:
// a=T (transmit + virtuelles Placement, Spec: "can be combined with a=T"),
// t=f (Terminal liest die PNG-Datei selbst), U=1, c/r = Zellraster,
// s/v = Pixelmasse, p=<id> macht das Placement deterministisch —
// Wiederholung ersetzt statt zu vervielfachen (Resize bleibt billig).
// transmitDirect ist der timg-HAUPTPFAD (src/kitty-canvas.cc): a=T ohne
// U=1 und ohne p= — laut timg-Quelle sind Unicode-Platzhalter nur "a hack
// around tmux refusing to work with graphics". Das Terminal platziert das
// Bild am Cursor selbst in den Zellraster (c/r/s/v geben die Groesse), der
// Cursor rueckt danach. Keine exotischen U+10EEEE-Cluster mehr im Text —
// genau deshalb stehen bei timg Bild und Text stabil nebeneinander.
func (b ImageBlock) transmitDirect(cols, rows int, path string, pw, ph int) string {
	if b.kittyID < 1 || b.kittyID > kittyIDMax || path == "" {
		return ""
	}
	payload := base64.StdEncoding.EncodeToString([]byte(path))
	// C=1: der Cursor bewegt sich nach dem Platzieren NICHT — bubbleteas
	// eigene \r\n + die Auffuellzeilen steuern das Layout, und die
	// Zeilen-Abrechnung des Renderers stimmt exakt mit den Terminal-Zeilen
	// ueberein (Spec: nach a=T rueckt der Cursor sonst um c Spalten und
	// r Zeilen — das wuerde die Abrechnung verstellen).
	return fmt.Sprintf("\x1b_Ga=T,f=100,t=f,i=%d,q=2,C=1,c=%d,r=%d,s=%d,v=%d,m=0;%s\x1b\\",
		b.kittyID, cols, rows, pw, ph, payload)
}

func (b ImageBlock) virtualPlacement(cols, rows int, path string, pw, ph int) string {
	// ID-Grenze 2^24: die Platzhalterzellen kodieren die ID in 24-Bit-
	// Truecolor-Farbe (s. placeholderRow, Referenz timg) — nicht mehr auf
	// 1..250 begrenzt wie mit e[38;5;N.
	if b.kittyID < 1 || b.kittyID > kittyIDMax || path == "" {
		return ""
	}
	payload := base64.StdEncoding.EncodeToString([]byte(path))
	// q=2: Terminal-Antworten komplett unterdruecken (Spec). Ohne q antwortet
	// das Terminal auf JEEDEN a=T mit ESC_Gi=N,p=N;OK ESC\ auf stdin —
	// bubbletea kann APC-Antworten nicht parsen, sie landeten als Muell-
	// Tasten auf dem Bildschirm und desynchronisierten daneben auch die
	// SGR-Maus-Reports ([<65;37;69M-Fragmente).
	return fmt.Sprintf("\x1b_Ga=T,f=100,t=f,i=%d,p=%d,U=1,q=2,c=%d,r=%d,s=%d,v=%d,m=0;%s\x1b\\",
		b.kittyID, b.kittyID, cols, rows, pw, ph, payload)
}

// placeholderRow baut Zeile r des Rasters: Vordergrundfarbe kodiert die
// Bild-ID, dann je Zelle U+10EEEE + Zeilen- + Spalten-Diakritikon. Das
// Spec-Beispiel (Bild 42, 2x2) ist: \e[38;5;42m\U10EEEE\U0305\U0305 ... —
// jede Zelle identifiziert ihren Rasterplatz selbst, deshalb zeigt das
// Terminal beim Scrollen automatisch den richtigen Bildausschnitt.
// kittyIDMax ist die groesste Bild-ID: 24 Bit passen in die Truecolor-FG
// (Referenz timg src/kitty-canvas.cc: "\e[38:2:%u:%u:%um" + MSB-Diakritikon).
const kittyIDMax = 1<<24 - 1

// placeholderRow baut Zeile r des Rasters: die Vordergrundfarbe kodiert die
// Bild-ID, dann je Zelle U+10EEEE + Zeilen- + Spalten-Diakritikon. Jede Zelle
// adressiert ihren Rasterplatz selbst, deshalb zeigt das Terminal beim
// Scrollen automatisch den richtigen Bildausschnitt (Spec: Unicode
// placeholders).
//
// Truecolor statt 256-Farbe: \e[38;5;N kann nur IDs bis 255, und ein Chat
// ueberschreitet das (jede Konvertierung braucht eine freie ID). timg kodiert
// die ID seit Jahren als 24-Bit-Truecolor mit optionalem MSB-Diakritikon —
// von dort uebernommen.
func placeholderRow(kittyID, r, cols int) string {
	var sb strings.Builder
	sb.WriteString("\x1b[38:2:")
	sb.WriteString(strconv.Itoa((kittyID >> 16) & 0xff))
	sb.WriteString(":")
	sb.WriteString(strconv.Itoa((kittyID >> 8) & 0xff))
	sb.WriteString(":")
	sb.WriteString(strconv.Itoa(kittyID & 0xff))
	sb.WriteString("m")
	rd := diacritic(r)
	msb := (kittyID >> 24) & 0xff
	for c := 0; c < cols; c++ {
		sb.WriteString(placeholderChar)
		sb.WriteRune(rd)
		sb.WriteRune(diacritic(c))
		if msb != 0 {
			sb.WriteRune(diacritic(int(msb)))
		}
	}
	sb.WriteString("\x1b[39m")
	return sb.String()
}

// renderCache liefert den Raster fuer (kittyID, cols, rows) aus der
// paketweiten Map — so ueberlebt er Update-Kopien (Wertetyp) und damit
// auch das Transmit-Fenster.
func (b ImageBlock) renderCache(cols, rows int) (*imgCache, bool) {
	key := imgCacheKey{b.id, b.kittyID, cols, rows}
	imgCacheMu.Lock()
	defer imgCacheMu.Unlock()
	if c := imgCaches[key]; c != nil && c.inline == inlineActive.Load() && c.src == b.pngPath && (c.inline || len(c.lines) == rows) {
		return c, true
	}
	path, pw, ph := b.pngPath, b.pngW, b.pngH
	own := false
	if b.svg != nil {
		// Vektorpfad: auf die exakte Zielgroesse rasern — cols*cellW x
		// rows*cellH, also 0x Terminal-Skalierung (Metrik 2). Kappung bei
		// 8192 px/s Achse schuetzt vor absurden Fensterlingen.
		cellW, cellH := b.caps.CellW, b.caps.CellH
		if cellW <= 0 || cellH <= 0 {
			cellW, cellH = 9, 20
		}
		tw, th := cols*cellW, rows*cellH
		if tw > 8192 {
			tw = 8192
		}
		if th > 8192 {
			th = 8192
		}
		rt0 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		png, err := render.ResvgToPNG(ctx, b.svg, tw, th)
		cancel()
		perfRastered(float64(time.Since(rt0).Microseconds()) / 1000.0)
		if err != nil {
			imageLogf("svg-raster block=%d ziel=%dx%d fehlgeschlagen: %v", b.id, tw, th, err)
			return nil, false
		}
		p, ferr := writeKittyFile(png)
		if ferr != nil {
			imageLogf("svg-raster temp-datei block=%d: %v", b.id, ferr)
			return nil, false
		}
		// resvg passt die Zeichnung aspektgetreu in die Box; die echten
		// Pixel koennen kleiner sein (gemessen 875 statt 918 Breite).
		// s=/v= tragen die ECHTEN Masse, damit das Terminal sie korrekt in
		// das Placement-Rechteck (c,r) einpasst — die Spec fuegt das Bild
		// dort aspekterhaltend ein, also bleibt es bei ~0x Skalierung und
		// einem minimalen Rand. Die Zellgeometrie (cols,rows) bleibt von
		// dims() unabhaengig, damit Cache-Key und Zeilenzahl konsistent sind.
		path = p
		if aw, ah, aok := pngDimsFromBytes(png); aok {
			pw, ph = aw, ah
		} else {
			pw, ph = tw, th
		}
		own = true
	}
	inl := inlineActive.Load()
	c := &imgCache{cols: cols, rows: rows, inline: inl, own: own, src: b.pngPath, pngPath: path, txBuilt: time.Now()}
	if inl {
		// timg-Hauptpfad: Escape auf Zeile 0, rows-1 LEERE Zeilen danach.
		// Der Cursor bewegt sich nicht (C=1), also landen die Leerzeilen
		// IN der Bildflaeche — unsichtbar, und EraseLineRight loescht laut
		// Spec KEINE Grafiken ("commands to erase text must have no effect
		// on graphics"). So zaehlt bubbletea genau r Zeilen und der
		// Cursor-up-Repaint geht nicht daneben.
		c.prefix = b.transmitDirect(cols, rows, path, pw, ph)
		c.lines = make([]string, rows)
		c.lines[0] = c.prefix
	} else {
		c.prefix = b.virtualPlacement(cols, rows, path, pw, ph)
		c.lines = make([]string, rows)
		for r := 0; r < rows; r++ {
			c.lines[r] = placeholderRow(b.kittyID, r, cols)
		}
	}
	// Raster-Aufruestung: alte Eintraege DESSELBEN Blocks entfernen (andere
	// Groesse nach Resize) — sonst haeuften sich bei jedem Resize neue
	// Temp-Dateien an.
	for k, old := range imgCaches {
		if k.blockID == b.id {
			if old.own && old.pngPath != "" {
				os.Remove(old.pngPath)
			}
			delete(imgCaches, k)
		}
	}
	imgCaches[key] = c
	imageLogf("image raster block=%d kitty=%d c=%d r=%d (cell=%dx%d) ziel=%dx%d tx=%d B path=%s",
		b.id, b.kittyID, cols, rows, b.caps.CellW, b.caps.CellH, pw, ph, len(c.prefix), path)
	return c, true
}

// ViewRange rendert die Bildzeilen [from,to) — Transmit-Escape vor der
// ersten gelieferten Zeile. BlockList ruft das mit dem sichtbaren Fenster,
// damit hohe Bilder auch uebertragen werden, wenn ihr Anfang heraus-
// gescrollt ist (Follow-Modus: neue Bloecke erscheinen unten).
func (b ImageBlock) ViewRange(width, from, to int) string {
	if !b.useImg || b.kittyID < 1 || (b.pngPath == "" && b.svg == nil) {
		return b.fallback.View(width)
	}
	if inlineActive.Load() {
		// timg-Hauptpfad: eine Zeile, nur der Escape (im tx-Fenster),
		// sonst leer — das Bild ist schon uebertragen und steht im Fluss.
		cols, rows := b.dims(width)
		c, ok := b.renderCache(cols, rows)
		if !ok {
			return b.fallback.View(width)
		}
		// Zeile 0 traegt den Escape (statisch — der Zeilen-Diff
		// ueberspringt identische Zeilen, kein Re-Transmit pro Frame).
		if c.txCount == 0 {
			c.txCount = 1
			imageLogf("image tx-direct block=%d kitty=%d c=%d r=%d", b.id, b.kittyID, cols, rows)
		}
		if from >= rows || to > rows {
			return ""
		}
		if from < 0 {
			from = 0
		}
		if to < from {
			to = from
		}
		return strings.Join(c.lines[from:to], "\n")
	}
	cols, rows := b.dims(width)
	if from < 0 {
		from = 0
	}
	if to > rows {
		to = rows
	}
	if from >= to {
		return ""
	}
	c, ok := b.renderCache(cols, rows)
	if !ok {
		return b.fallback.View(width)
	}
	out := make([]string, 0, to-from)
	for r := from; r < to; r++ {
		out = append(out, c.lines[r])
	}
	// Transmit nur kurz nach Erscheinen des Rasters senden (Flush-Fenster,
	// max. 5x) — NICHT bei jedem Scroll-Schritt: a=T,t=f liesse das
	// Terminal die PNG sonst wiederholt neu lesen und dekodieren, was im
	// Ghostty die Scroll-FPS auf ~1 kollabieren liess (Metrik 1+3).
	if c.txCount < 5 && time.Since(c.txBuilt) < 750*time.Millisecond {
		out[0] = c.prefix + out[0]
		c.txCount++
		imageLogf("image tx block=%d kitty=%d (%d. mal, fenster)", b.id, b.kittyID, c.txCount)
	}
	return strings.Join(out, "\n")
}

// View rendert das komplette Bild (Blockanfang sichtbar, z. B. reveal).
func (b ImageBlock) View(width int) string { return b.ViewRange(width, 0, 1<<30) }

func (b ImageBlock) Height(width int) int {
	if !b.useImg {
		return b.fallback.Height(width)
	}
	if inlineActive.Load() {
		// timg-Modell: die ECHTEN Zeilen — C=1 haelt den Cursor, die
		// Leerzeilen-Auffuellung deckt die Bildflaeche, also stimmt die
		// Zeilen-Abrechnung des Renderers exakt.
		return b.rowsFor(width)
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
	return nb
}

// SetSVG haelt die Vektor-Quelle (plantuml -tsvg) und setzt die Bildmasse
// auf die intrinsische SVG-Groesse: sie bestimmen das Seitenverhaeltnis
// (rowsFor), und renderCache rastert auf die EXAKTE Terminal-Zielgroesse —
// das Terminal skaliert 0x (Metrik 2). Bei Resize wird aus dem SVG neu
// gerastert (Millisekunden) statt neu konvertiert (~500 ms Java).
func (b ImageBlock) SetSVG(svg []byte) Widget {
	nb := b
	nb.svg = svg
	if w, h, ok := render.SvgDims(svg); ok {
		nb.pngW, nb.pngH = w, h
	}
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
	imgCacheMu.Lock()
	imgCaches = map[imgCacheKey]*imgCache{}
	imgCacheMu.Unlock()
	// Im Inline-Modell (timg) bleiben die Bilder im Scrollback — ein
	// a=d,d=a wuerde sie dort loeschen, wo der Nutzer sie noch sieht.
	if used && !inlineActive.Load() {
		os.Stdout.WriteString("\x1b_Ga=d,d=a,q=2\x1b\\")
		imageLogf("cleanup: alle kitty-bilder geloescht, %d temp-dateien entfernt", len(files))
	}
	for _, f := range files {
		os.Remove(f)
	}
}
