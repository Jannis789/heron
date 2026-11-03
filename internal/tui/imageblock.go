package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

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
	// Roh-Quelltext des Diagramms (Bonus-FR): BlockList reicht t an den
	// fokussierten Block, showSrc schaltet zwischen Bild (Standard) und
	// Quelltext-Ansicht um. srcBlock ist die Code-Ansicht des Quelltexts —
	// voller CodeBlock: Rahmen, Zeilen-Label, COPY-Action, Fokus-Stil.
	src      string
	srcBlock CodeBlock
	showSrc  bool
	// stale: Block ist Teil der VERGANGENEN Auswahl (FR-43) — Rahmen in
	// der verblichenen Sekundaer-Farbe statt Neon; propagiert an Quelltext-
	// und Fallback-Ansicht (wie SetMarked).
	stale bool
	// hoverBox ist der zielgenaue Hover im Footer: -1 keiner, 0 der
	// Switch-Kasten ([ TEXT ]/[ IMAGE ]), 1 COPY — blockweiter Hover
	// leuchtete frueher ALLE Kaesten gleichzeitig auf.
	hoverBox int
	// panZoom haelt den Pan-&-Zoom-Modus (FR-27 Erweiterung): panning
	// schaltet die Pfeiltasten vom Fokus- auf Bild-Steuerung um, zoom ist
	// Prozent (100 = Default-Aufloesung), panRow/panCol die linke obere
	// Ecke des sichtbaren Fensters im VIRTUELLEN Raster. Der Kasten auf
	// dem Schirm bleibt GLEICH GROSS — das virtuelle Placement wächst,
	// die Platzhalter adressieren ihr Fenster darin: zu grosse Bilder
	// werden angeschnitten (Crop), nie waechst die Box aus dem Schirm.
	panning bool
	zoom    int // Prozent, 100 = Default; 0 wird als 100 gelesen
	panRow  int
	panCol  int
	// lastWidth ist die Breite des letzten Resize — Update (Pfeil-Tasten)
	// braucht die Fenster-Masse fuer Schrittweite und Clamp, bekommt aber
	// keine Breite herein. 0 = noch nie gesetzt (Fallback 80).
	lastWidth int
	// lang ist die Diagramm-Sprache ("plantuml") fuer die Kopfzeile — Bild-
	// und Quelltext-Ansicht tragen DIESELBE Kopfzeile, damit der t-Switch
	// als Moduswechsel im selben Block lesbar ist.
	lang    string
	copyFn  CopyFunc
	focused bool
	hover   bool
	marked  bool
	th      *render.Theme
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
	png        []byte // gerasterte Pixel (Inline-Base64 wie timg)
	inline     bool   // timg-Hauptpfad: nur der Direkt-Escape, keine Platzhalter
	own        bool   // pngPath wurde von renderCache gerastert (Temp-Datei)
	wasSrc     bool   // letzter showSrc-Zustand: Umschalten zurueck aufs Bild
	// oeffnet das Transmit-Fenster erneut (t-Switch) — ohne das blieben
	// nach Scroll/Partials leere Platzhalter, wenn das Terminal das Bild
	// verworfen hat.
	src     string // pngPath des Blocks, aus dem der Raster gebaut wurde
	pngPath string // effektive Transmit-Datei (bei SVG: die gerasterte)
	prefix  string
	lines   []string
	txBuilt time.Time
	txCount int
	pw, ph  int         // echte Pixelmasse des uebertragenen PNGs
	decoded image.Image // Fenster-Crop: dekodiertes VOLLraster (Pan-Schritt-Reuse in windowRaster)
}

// imgCaches haelt die Raster paketweit: ImageBlock ist ein Wertetyp — ein
// Cache-Feld auf der Struktur wuerde bei jedem Update-Kopieren verworfen
// und der Raster (inkl. Transmit-Fenster) JEDEN Frame neu gebaut. Der
// Transmit-Escape ging daraufhin bei jedem Scroll-Schritt erneut raus und
// das Terminal hat die PNG bei jedem Schritt neu dekodiert (1fps, gemessen).
type imgCacheKey struct {
	blockID, kittyID, cols, rows int // virtuelles Placement (Zoom-Raster)
	wCols, wRows                 int // sichtbares Fenster = Kasten
	panRow, panCol               int // Fenster oben links im virtuellen Raster
}

var (
	imgCacheMu sync.Mutex
	imgCaches  = map[imgCacheKey]*imgCache{}
)

// markImgCacheWasSrc markiert jeden Raster eines Blocks als "Quelltext war
// aktiv" — der naechste Bild-Render oeffnet das Transmit-Fenster einmal neu
// (t-Switch: ohne das koennten nach Scroll/Partials leere Platzhalter
// stehen, wenn das Terminal das Bild verworfen hat).
func markImgCacheWasSrc(blockID, kittyID int) {
	imgCacheMu.Lock()
	defer imgCacheMu.Unlock()
	for k, c := range imgCaches {
		if k.blockID == blockID && (kittyID < 1 || k.kittyID == kittyID) {
			c.wasSrc = true
		}
	}
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
		lang:     "diagram",
		// Kein Kasten ist initial GEHOVERT: -1 ist "keiner", erst der
		// Navigator setzt ihn zielgenau (SetHover/SetHoverAt). Der Go-
		// Nullwert 0 wuerde den TEXT-Switch-Kasten sofort weiss+unter-
		// strichen aufleuchten lassen, lange bevor die Maus ihn beruehrt.
		hoverBox: -1,
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

// colsFor begrenzt die Darstellung auf die Blockbreite — auf die
// intrinsische Bildgroesse x imageScale — UND auf eine Zoom-Reserve. Ohne
// diese Obergrenzen wurde ein kleines Diagramm auf die ganze Fensterbreite
// gestreckt (6-Megapixel-Raster, kein Text sichtbar, laggy), und ein FILL-
// BREITES Bild haette beim Zoomen direkt die Diakritika-Grenze (siehe
// zoomHeadroomPct). Mit Obergrenze rastert renderCache auf ~0x Terminal-
// Skalierung (scharf) und das Bild laesst Platz fuer Text (FR-27 Metrik 2).
//
// zoomHeadroomPct ist die Default-Breitenreserve fuer breite Diagramme:
// Ohne sie fuellt ein fill-TERMINAL-Bild den Rahmen schon bei 100%, und
// JEDE Zoom-Stufe waere sofort Ausschnitt (Crop) statt sichtbarem Wachs-
// tum der Box. Mit Reserve waechst die Box beim Zoomen erst einmal mit
// (bis die Rahmenkante kommt), bevor der Ueberschuss ge-croppt wird — auf
// Kosten einer minimal kleineren Default-Anzeige nur fuer solcherart
// breite Bilder. Die alte Diakritika-Begruendung ist seit dem Fenster-Crop
// entfallen (das virtuelle Raster ist unbegrenzt), die Reserve bleibt als
// Zoom-Gefuehl.
const zoomHeadroomPct = 190

func (b ImageBlock) colsFor(width int) int {
	// width-4: der Bildkoerper sitzt im Rahmen (│ + Leerzeichen links und
	// rechts) — dieselbe Innenbreite wie CodeBlock-Zeilen.
	c := width - 4
	if c < 10 {
		c = 10
	}
	// KEINE Zoom-Reserve mehr: die Box nutzt im Rahmen des Protokolls die
	// maximale verfuegbare Breite (intrinsische Bildgroesse x imageScale
	// sowie die Diakritika-Adresse). Eine Reserve (frueher ~156 Zellen) liess
	// in breiten Terminals rechtsmassig viel Leerraum.
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
	// Harte Protokoll-Grenze: Ein Kitty-Platzhalter adressiert Zeile/Spalte
	// mit einem Diakritikon aus der Tabelle (diacriticAddrCap). Eine Box
	// breiter als 297 Zellen liesse sich nicht mehr eindeutig adressieren
	// (Kollisionen -> Artefakte), also klemmt colsFor hier auch die Default-
	// Breite (nicht nur zoomWindow die Zoom-Box). Terminal breiter als
	// ~300 Zellen: das Bild fuellt bis zu 297 Spalten, der Rest bleibt frei
	// (nur der Inline/timg-Pfad ohne Diakritika kann breiter werden).
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
	// Hoehen-Cap fuer NATUERLICHE Fit-Breite: ein hochkantes Bild wird
	// schmaler dargestellt, damit es ins Zeilenlimit passt. Das ist die
	// 100%-Grundbreite — beim Zoomen waechst die Box dann proportional mit
	// der Breite (Bildbreite verdoppelt sich), bis zur Protokoll-/Rahmen-
	// grenze (Wunschszenario des Nutzers).
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
	return nb.normalizePan() // schrumpfender Kasten schraenkt das Pan-Fenster ein
}

// --- Pan & Zoom (FR-27 Erweiterung) ---------------------------------------
//
// Modell: Der Kasten (Rahmen) bleibt in seiner Default-Groesse — dims()
// begrenzt ihn auf Fensterbreite und Zeilen-Anteil, er kann also NIE aus
// dem Schirm laufen. Zoom vergroessert das VIRTUELLE Placement (c/r im
// Transmit-Escape): das Terminal skaliert das Bild in das groessere
// Zellraster, die sichtbaren Platzhalter adressieren nur noch ihr Fenster
// (panRow/panCol) darin — der Rest ist abgeschnitten. Jede Platzhalter-
// Zelle traegt ihre absolute (Zeile, Spalte) im virtuellen Raster, das
// Terminal zeigt also automatisch den richtigen Ausschnitt (gleiches
// Prinzip wie das teilweise Sichtbar-Scrollen).

// zoomStep ist die Zoom-Stufe in Prozent pro +/-, zoomMin/zoomMax die
// Grenzen. zoomMin 100: der Default IST die passt-auf-den-Schirm-Aufloesung,
// kleiner macht das Bild nur unschaerfer, ohne Platz zu schaffen.
const (
	zoomStep = 25
	zoomMin  = 100
	zoomMax  = 400
)

// diacriticAddrCap ist die Anzahl der adressierbaren Raster-Plaetze des
// Kitty-Platzhalter-Schemas (rowcolumn-diacritics.txt): ein Platzhalter
// kodiert Zeile/Spalte mit EINEM Diakritikon je Achse (U+10EEEE + Zeile +
// Spalte). diacritic(i) klemmt i >= len(kittyDiacritics) ans Tabellenende —
// ab dort adressieren MEHRERE Zellen dieselbe Zeile/Spalte, und die Raster-
// Runs des Terminals brechen -> Bildartefakte. Seit der Fenster-Crop-
// Umstellung (renderCache) adressieren die Platzhalter nur noch das
// FENSTER (hoechstens so gross wie Terminal bzw. Zeilen-Cap), deshalb
// klemmt die Grenze heute die FENSTER-Groesse (zoomWindow) — das virtuelle
// Raster dahinter ist unbegrenzt, der Zoom laeuft bis zoomMax.
func diacriticAddrCap() int {
	if n := len(kittyDiacritics); n > 1 {
		return n
	}
	return 1
}

// Panning meldet, ob der Block im Pan-&-Zoom-Modus ist — BlockList leitet
// daraufhin die Pfeiltasten an den Block statt in die Fokus-Kette.
func (b ImageBlock) Panning() bool { return b.panning }

// panZoom returns the effective zoom in percent (0 field value = 100).
func (b ImageBlock) zoomPct() int {
	if b.zoom < zoomMin {
		return zoomMin
	}
	return b.zoom
}

// effZoomPct ist der WIRKLICH wirksame Zoom, den der ⤢-Indikator zeigt:
// der gespeicherte Wert, geklemmt an zoomMax. Seit dem Fenster-Crop
// (renderCache uebertraegt nur den sichtbaren Ausschnitt) ist der Zoom
// breitenunabhaengig wirksam — vorher klemmte hier die Diakritika-Grenze
// von dims(width), und ein veralteter frameWidth liess den Indikator
// Wachstum behaupten, das das Raster nicht mehr hergab. Der Parameter
// width bleibt als Anknuepfungspunkt (Aufrufer hat ihn ohnehin).
func (b ImageBlock) effZoomPct(width int) int {
	_ = width
	if z := b.zoomPct(); z < zoomMax {
		return z
	}
	return zoomMax
}

// zoomVirtual liefert die Zellgroesse (c/r im Transmit-Escape), in die das
// Bild bei Zoom z skaliert wird: relativ zur Default-Groesse dims(), also
// UNIFORM (Aspekt bleibt exakt) — vCols/vRows wachsen mit dem Zoom, auch
// wenn sie die Box ueberragen. Keine Diakritika-Klemmung mehr: ueberragt
// das virtuelle Raster die Box, liefert renderCache nur den Fensteraussch-
// nitt als eigenes Bild (Platzhalter adressieren fensterlokal).
func (b ImageBlock) zoomVirtual(width, z int) (int, int) {
	fc, fr := b.dims(width)
	if z > zoomMax {
		z = zoomMax
	}
	return fc * z / 100, fr * z / 100
}

// zoomWindow liefert die sichtbare BOX (die Zellen, die der Rahmen um-
// schliesst) bei Zoom z. Sie WAECHST mit dem Bild mit — jede Achse bis an
// ihre eigene Grenze (Rahmeninnenkante width-4, Zeilen-Obergrenze maxRows,
// Diakritika-Adresse) —, fuellt also beim Zoomen die MAXIMALE Ausschnitts-
// flaeche, die der Rahmen zulaesst. Erst wenn das virtuelle Placement die
// Box ueberragt (Zoom-Ueberschuss), liefert renderCache nur den Fenster-
// ausschnitt als eigenes Bild; der Ausschnitt liegt dabei proportional IM
// Originalbild (cropRect) — die Breite kann also nie ueber die originale
// Pixelbreite wachsen, und die Skalierung des Ausschnitts in die Box ist
// uniform (keine Streckung, pixel-Aspekt == Box-Aspekt, s.
// TestZoomCropNeverExceedsOriginal).
// Ohne Pan-Modus ist die Box identisch zu dims() (zoom 100 = ganzes Bild).
func (b ImageBlock) zoomWindow(width, z int) (int, int) {
	fc, fr := b.dims(width)
	if !b.panning {
		return fc, fr
	}
	if z > zoomMax {
		z = zoomMax
	}
	addr := diacriticAddrCap()
	wc := fc * z / 100
	if wc < 1 {
		wc = 1
	}
	if mx := width - 4; wc > mx {
		wc = mx
	}
	if wc < 1 {
		wc = 1
	}
	wr := fr * z / 100
	if wr < 1 {
		wr = 1
	}
	if b.maxRows > 0 && wr > b.maxRows {
		wr = b.maxRows
	}
	if wr > addr {
		wr = addr
	}
	return wc, wr
}

// frameWidth liefert die Breite, mit der der Block zuletzt layoutet wurde
// (Resize), sonst 80 — Update-Helfer (Schrittweite, Clamp) brauchen sie.
func (b ImageBlock) frameWidth() int {
	if b.lastWidth < 16 {
		return 80
	}
	return b.lastWidth
}

// zoomLayout liefert (vCols, vRows, wCols, wRows, panRow, panCol) fuer den
// aktuellen Zoom und die Fensterbreite width: vCols×vRows ist das
// virtuelle Placement (Bildraster), wCols×wRows die sichtbare Box; pan ist
// immer GANZ im Ueberschuss eingespannt (das Bild kann so nie aus der Box
// laufen, die Box nie aus dem Schirm).
func (b ImageBlock) zoomLayout(width int) (vCols, vRows, wCols, wRows, panRow, panCol int) {
	z := b.zoomPct()
	vCols, vRows = b.zoomVirtual(width, z)
	wCols, wRows = b.zoomWindow(width, z)
	panRow = clamp(b.panRow, 0, max(vRows-wRows, 0))
	panCol = clamp(b.panCol, 0, max(vCols-wCols, 0))
	return
}

// clampPan liefert das eingespannte Fenster (panRow, panCol) fuer die
// virtuelle Groesse vCols×vRows und die Box wCols×wRows.
func (b ImageBlock) clampPan(vCols, vRows, wCols, wRows int) (int, int) {
	return clamp(b.panRow, 0, max(vRows-wRows, 0)), clamp(b.panCol, 0, max(vCols-wCols, 0))
}

// normalizePan spannt panRow/panCol nach Zoom-/Fensteraenderung wieder ins
// virtuelle Raster ein.
func (b ImageBlock) normalizePan() ImageBlock {
	_, _, _, _, b.panRow, b.panCol = b.zoomLayout(b.frameWidth())
	return b
}

// togglePan oeffnet/schliesst den Pan-&-Zoom-Modus. Beim Schliessen kehrt
// das Bild auf Default-Aufloesung und -Position zurueck (zoom 100, pan 0)
// — der Cache baut den Default-Raster daraufhin neu.
func (b ImageBlock) togglePan() (Widget, tea.Cmd) {
	if !b.imgActive() || b.showSrc {
		return b, nil
	}
	nb := b
	nb.panning = !nb.panning
	if !nb.panning {
		nb.zoom = 100
		nb.panRow, nb.panCol = 0, 0
	}
	return nb, nil
}

// zoomBy vergroessert/verkleinert Bild UND Box um eine Stufe (Bild und
// Rahmen wachsen gemeinsam, das Bild bleibt zentriert) und haelt die
// FENSTERMITTE im Bild — der betrachtete Ausschnitt bleibt (soweit die
// Raender es erlauben) stehen. Erst wenn die Box am Rahmen limitiert ist,
// wird weiter ge-zoomed und dabei ge-croppt.
func (b ImageBlock) zoomBy(d int) (Widget, tea.Cmd) {
	nb := b
	old := nb.zoomPct()
	// Einzig zoomMax stoppt den Zoom: Seit dem Fenster-Crop (renderCache)
	// wird bei ueberschuessigem virtuellen Raster nur der sichtbare
	// Ausschnitt als eigenes Bild uebertragen — die Platzhalter adressieren
	// fensterlokal, die Diakritika-Grenze bindet allein die Box
	// (zoomWindow). Der Footer zeigt denselben Wert (effZoomPct).
	nb.zoom = clamp(old+d, zoomMin, zoomMax)
	if nb.zoom == old {
		return nb, nil
	}
	// Zoom laesst das Fenster ANKERORIENTIERT (noch nicht veraendert) — ein
	// reines Zoomen verschiebt den Ausschnitt also NIE von selbst in einen
	// Innenbereich des Bildes. Innenbereiche (Window-Offset unter den
	// Platzhaltern) sind der fragilste Fall der Kitty-Adressierung: sobald
	// der Nutzer von selbst puffert (Pfeiltasten), wuerde ein automatisches
	// Zentrieren die Rahmen in jedem Zoom-Schritt verschieben und sichtbare
	// "Hall-of-Mirrors"-Artefakte beguenstigen (beobachtet ab ~225%).
	// panRow/panCol bleiben also stehen und werden nur in die (ggf. groessere)
	// virtuelle Raster eingeklemmt; wer zentrieren will, panned von Hand.
	return nb.normalizePan(), nil
}

// panBy verschiebt die Box im virtuellen Raster (dx/dy in Richtungs-
// einheiten). Die Schrittweite waechst mit der Box (1/8 der Kanten,
// mindestens 1 Zelle) — bei 400% waere Einzelschritt-Zeilenkarat muehsam.
func (b ImageBlock) panBy(dx, dy int) (Widget, tea.Cmd) {
	nb := b
	_, _, wCols, wRows, _, _ := nb.zoomLayout(nb.frameWidth())
	sy := max(wRows/8, 1)
	sx := max(wCols/8, 1)
	nb.panRow += dy * sy
	nb.panCol += dx * sx
	return nb.normalizePan(), nil
}

// clamp haelt v in [lo,hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
// transmitDirectChunks ist der timg-Hauptpfad, BYTE-GETREU nachgebaut aus
// einem echten PTY-Mitschnitt von timg 1.6.3 (-pk):
//
//	\x1b_Ga=T,i=ID,q=2,f=100,m=1;<chunk>\x1b\   (erster Chunk)
//	\x1b_Gq=2,m=1;<chunk>\x1b\                  (Fortsetzung)
//	\x1b_Gq=2,m=0;<chunk>\x1b\                  (letzter Chunk)
//
// KEIN t=f (Datei-Medium), KEIN C=1, keine c/r/s/v-Keys: jedes zusaetzliche
// Key ist ein Kandidat fuer stille Ablehnung (q=2 unterdrueckt die Fehler-
// antwort!). timg beweist seit Jahren, dass genau diese Form laeuft. Die
// Anzeige-Groesse kommt aus den Pixeln des PNG — die Skalierung macht
// resvg VOR der Uebertragung.
func transmitDirectChunks(kittyID int, png []byte, cols, rows int) string {
	if kittyID < 1 || kittyID > kittyIDMax || len(png) == 0 {
		return ""
	}
	enc := base64.StdEncoding.EncodeToString(png)
	var bld strings.Builder
	first := true
	for {
		chunk := enc
		if len(chunk) > 4096 {
			chunk = chunk[:4096]
		}
		enc = enc[len(chunk):]
		more := 0
		if len(enc) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&bld, "\x1b_Ga=T,i=%d,c=%d,r=%d,q=2,f=100,m=%d;%s\x1b\\", kittyID, cols, rows, more, chunk)
			first = false
		} else {
			fmt.Fprintf(&bld, "\x1b_Gq=2,m=%d;%s\x1b\\", more, chunk)
		}
		if len(enc) == 0 {
			break
		}
	}
	return bld.String()
}

// sshSession meldet, ob heron in einer SSH-Session laeuft: heron und
// Terminal-Emulator sitzen dann auf verschiedenen Maschinen. SSH_TTY wird
// von sshd bei interaktiven PTY-Sessions gesetzt, SSH_CONNECTION auch ohne
// PTY — beide zusammen decken die ueblichen Laeufe ab (mosh/tmux sind
// bewusst NICHT abgedeckt: sie geben APC-Sequenzen ohnehin nicht durch).
func sshSession() bool {
	return os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != ""
}

// transmitPlaceholderChunks ist der SSH-Zwilling von virtualPlacement:
// identische Keys (i,p,U=1,q=2,c,r,s,v,f=100) und das gleiche Platzhalter-
// Layout — aber statt t=f (Datei-Medium, Terminal liest die PNG selbst)
// wandern die Pixeldaten in Base64-Chunks (m=1/m=0) im Escape-Stream.
// t=f funktioniert nur, wenn heron und Terminal auf der SELBEN Maschine
// laufen; in einer SSH-Session existiert der Pfad nur auf der Remote-
// Maschine, das lokale Terminal kann ihn nicht oeffnen — und q=2
// unterdrueckt die Fehlerantwort, der Ausfall bleibt komplett still
// (beobachtet: probe graphics=true, raster tx=100 B, kein Bild).
func transmitPlaceholderChunks(kittyID int, png []byte, cols, rows, pw, ph int) string {
	if kittyID < 1 || kittyID > kittyIDMax || len(png) == 0 {
		return ""
	}
	enc := base64.StdEncoding.EncodeToString(png)
	var bld strings.Builder
	first := true
	for {
		chunk := enc
		if len(chunk) > 4096 {
			chunk = chunk[:4096]
		}
		enc = enc[len(chunk):]
		more := 0
		if len(enc) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&bld, "\x1b_Ga=T,i=%d,p=%d,U=1,q=2,c=%d,r=%d,s=%d,v=%d,f=100,m=%d;%s\x1b\\",
				kittyID, kittyID, cols, rows, pw, ph, more, chunk)
			first = false
		} else {
			fmt.Fprintf(&bld, "\x1b_Gq=2,m=%d;%s\x1b\\", more, chunk)
		}
		if len(enc) == 0 {
			break
		}
	}
	return bld.String()
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
// placeholders). Die Adressierung ist FENSTERLOKAL (0..rows-1): seit dem
// Fenster-Crop ist das uebertragene Bild immer genau das sichtbare Fenster
// — im Default (zoom 100, pan 0) ist das identisch zum alten absoluten
// Schema, im Zoom adressieren die Platzhalter nur noch ihr kleines Fenster
// und das virtuelle Raster dahinter ist unbegrenzt.
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
	cap := len(kittyDiacritics)
	for c := 0; c < cols; c++ {
		sb.WriteString(placeholderChar)
		sb.WriteRune(rd)
		if c < cap {
			// In der Tabelle: Zeile + Spalte (und ggf. msb) explizit.
			sb.WriteRune(diacritic(c))
			if msb != 0 {
				sb.WriteRune(diacritic(int(msb)))
			}
		} else {
			// Spalte ueber die Diakritika-Tabellengrenze hinaus: das Spalten-
			// Diakritikon wird WEGGELASSEN; die Spezifikation-Vererbungsregel
			// (nur Zeilen-Diakritikon, gleiche Zeile/Farbe wie links) erzeugt
			// Spalte = links+1 → beliebig breit (keine 297er-Decke; die war
			// ein heron-Detail: immer diacritic(c), das fuer c>=cap klemmt).
		}
	}
	sb.WriteString("\x1b[39m")
	return sb.String()
}

// renderCache liefert den Raster fuer das virtuelle Placement (vCols×vRows)
// mit dem sichtbaren Fenster (cols×rows bei panRow/panCol) aus der paket-
// weiten Map — so ueberlebt er Update-Kopien (Wertetyp) und damit auch das
// Transmit-Fenster. Ohne Pan-&-Zoom-Überschuss ist das virtuelle Raster
// gleich dem Fenster (vCols==cols, vRows==rows, pan=0) — der Default-Pfad
// bleibt byte-identisch. MIT Überschuss uebernimmt windowRenderCache: nur
// der Fensterausschnitt wird als eigenes Bild übertragen (Crop), pro
// Pan-Schritt neu — das dekodierte Vollraster wird dabei wiederverwendet
// (windowRaster), Pfeiltasten bleiben ohne Neu-Rastern flott.
func (b ImageBlock) renderCache(vCols, vRows, cols, rows, panRow, panCol int) (*imgCache, bool) {
	key := imgCacheKey{b.id, b.kittyID, vCols, vRows, cols, rows, panRow, panCol}
	imgCacheMu.Lock()
	defer imgCacheMu.Unlock()
	if c := imgCaches[key]; c != nil && c.inline == inlineActive.Load() && c.src == b.pngPath && (c.inline || len(c.lines) == rows) {
		return c, true
	}
	// FENSTER-MODUS: Das virtuelle Raster ueberragt die Box (Zoom) oder das
	// Fenster ist verschoben (Pan) → es wird nur der AUSSCHNITT als eigenes
	// Bild uebertragen (Platzhalter fensterlokal), nicht das ganze Bild in
	// ein Raster, das die Diakritika-Adresse sprengt. Der Default-Pfad
	// (v==Fenster, pan=0) bleibt unberuehrt und byte-identisch.
	if vCols != cols || vRows != rows || panRow != 0 || panCol != 0 {
		return b.windowRenderCache(key, vCols, vRows, cols, rows, panRow, panCol)
	}
	path, pw, ph := b.pngPath, b.pngW, b.pngH
	own := false    // pngPath ist eine eigene (gerasterte) Temp-Datei
	reused := false // Pan-Schritt: Raster eines Vorlaeuifers uebernommen
	var rasterPng []byte
	if b.svg != nil {
		// RASTER-REUSE: gleiche virtuelle Rastermasse (z. B. nach t-Switch
		// zurueck aufs Bild oder Inline-Toggle) → Raster (Temp-Datei,
		// Pixel, Masse) vom existierenden Cache uebernehmen; der Transmit
		// im Terminal gilt weiterhin (gleiches Placement) — reused haelt
		// unten das Transmit-Fenster zu (txCount=5).
		// (Reine PAN-Schritte laufen seit dem Fenster-Crop ueber
		// windowRenderCache — sie croppen den Ausschnitt selbst.)
		for k, old := range imgCaches {
			if k.blockID == b.id && k.kittyID == b.kittyID && k.cols == vCols && k.rows == vRows && old.own {
				path, pw, ph, rasterPng = old.pngPath, old.pw, old.ph, old.png
				own, reused = true, true
				break
			}
		}
	}
	if b.svg != nil && !own {
		// Vektorpfad: auf die exakte Zielgroesse rasern — vCols*cellW x
		// vRows*cellH (das VIRTUELLE Placement!), also 0x Terminal-
		// Skalierung (Metrik 2) auch im Zoom. Kappung bei 8192 px/s Achse
		// schuetzt vor absurden Fensterlingen.
		cellW, cellH := b.caps.CellW, b.caps.CellH
		if cellW <= 0 || cellH <= 0 {
			cellW, cellH = 9, 20
		}
		tw, th := vCols*cellW, vRows*cellH
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
		rasterPng = png
		if aw, ah, aok := pngDimsFromBytes(png); aok {
			pw, ph = aw, ah
		} else {
			pw, ph = tw, th
		}
		own = true
	}
	inl := inlineActive.Load()
	c := &imgCache{cols: vCols, rows: vRows, png: rasterPng, inline: inl, own: own, src: b.pngPath, pngPath: path, pw: pw, ph: ph, txBuilt: time.Now()}
	if reused && rasterPng != nil {
		// Pan-Schritt mit uebernommenem Raster: das Terminal kennt das
		// Placement schon — Transmit-Fenster bleibt zu (txCount=5),
		// nur die Platzhalter adressieren ihr neues Fenster.
		c.txCount = 5
	}
	if inl {
		// timg-Hauptpfad: Escape auf Zeile 0, rows-1 LEERE Zeilen danach.
		// Der Cursor bewegt sich nicht (C=1), also landen die Leerzeilen
		// IN der Bildflaeche — unsichtbar, und EraseLineRight loescht laut
		// Spec KEINE Grafiken ("commands to erase text must have no effect
		// on graphics"). So zaehlt bubbletea genau r Zeilen und der
		// Cursor-up-Repaint geht nicht daneben.
		src := c.png
		if src == nil {
			src = b.png
		}
		c.prefix = transmitDirectChunks(b.kittyID, src, vCols, vRows)
		if !own {
			c.lines = make([]string, rows)
			c.lines[0] = c.prefix
		}
	} else {
		src := rasterPng
		if src == nil {
			src = b.png
		}
		if !reused {
			if sshSession() {
				// SSH-Pfad: t=f kann nicht funktionieren (PNG liegt auf der
				// Remote-Maschine) — Pixeldaten als Base64-Chunks senden.
				// Layout bleibt das Platzhalter-Modell, nur das Medium
				// wechselt; tx steigt von ~100 B auf ~4/3 der PNG-Groesse.
				c.prefix = transmitPlaceholderChunks(b.kittyID, src, vCols, vRows, pw, ph)
			} else {
				c.prefix = b.virtualPlacement(vCols, vRows, path, pw, ph)
			}
		}
	}
	// Platzhalter adressieren das FENSTER im virtuellen Raster — beim
	// Default (pan=0, v==frame) identisch zum bisherigen Raster. Der
	// timg-Pfad hat KEINE Platzhalter: nur der Escape auf Zeile 0 (beim
	// Pan-Reuse bleibt selbst der weg — das Terminal kennt das Bild).
	c.lines = make([]string, rows)
	if inl {
		if !reused {
			c.lines[0] = c.prefix
		}
	} else {
		for r := 0; r < rows; r++ {
			c.lines[r] = placeholderRow(b.kittyID, r, cols)
		}
	}
	// Raster-Aufruestung: alte Eintraege DESSELBEN Blocks entfernen (andere
	// Groesse nach Resize/Zoom) — sonst haeuften sich bei jedem Resize neue
	// Temp-Dateien an. Die Temp-Datei eines wiederverwendeten Rasters
	// wandert in den neuen Eintrag und wird NICHT geloescht.
	for k, old := range imgCaches {
		if k.blockID == b.id {
			if old.own && old.pngPath != "" && old.pngPath != path {
				os.Remove(old.pngPath)
			}
			delete(imgCaches, k)
		}
	}
	imgCaches[key] = c
	mod := "t=f"
	if inl {
		mod = "timg"
	} else if sshSession() {
		mod = "ssh-inline"
	}
	imageLogf("image raster block=%d kitty=%d mod=%s c=%d r=%d fenster=%dx%d pan=%d,%d reuse=%v (cell=%dx%d) ziel=%dx%d tx=%d B path=%s",
		b.id, b.kittyID, mod, vCols, vRows, cols, rows, panRow, panCol, own, b.caps.CellW, b.caps.CellH, pw, ph, len(c.prefix), path)
	return c, true
}

// windowRenderCache baut den Cache-Eintrag im FENSTER-MODUS: Das virtuelle
// Raster (vCols×vRows) ueberragt die sichtbare Box (cols×rows bei panRow/
// panCol) — statt das ganze Bild in ein Zellraster zu legen, das die
// Diakritika-Adresse (297 Plaetze je Achse) sprengt, wird nur der sichtbare
// AUSSCHNITT als eigenes Bild uebertragen: Placement c/r = Fenster, s/v =
// Crop-Pixel, Platzhalter adressieren fensterlokal (0..rows-1). Der Zoom
// ist damit allein durch zoomMax begrenzt — je mehr Ueberschuss, desto
// groesser der Pixel-Ausschnitt pro Zelle (Heranzoomen). Die Escape-Formen
// sind byte-identisch zum Default-Pfad, nur die Werte sind Ausschnitt-
// Werte: kein neues Key, das ein Terminal still ablehnen koennte (q=2!).
// Aufrufer (renderCache) haelt imgCacheMu.
func (b ImageBlock) windowRenderCache(key imgCacheKey, vCols, vRows, cols, rows, panRow, panCol int) (*imgCache, bool) {
	raster := b.windowRaster(vCols, vRows)
	if raster == nil {
		return nil, false
	}
	rb := raster.Bounds()
	cr := cropRect(rb.Dx(), rb.Dy(), vCols, vRows, cols, rows, panRow, panCol)
	// Wunschszenario 10:N: Der proportional gewaehlte Ausschnitt braucht
	// exakt den Pixel-Aspekt der sichtbaren Box, sonst skaliert das
	// Terminal ihn aspekterhaltend zentriert und laesst rechts/frei einen
	// Schlitz (rechts ~Dutzende leere Zellen bei mittlerem Zoom).
	// aspectFillCrop klemmt den Ausschnitt auf den Box-Aspekt (Hoehe der
	// Freiheitsgrad) — die Box ist damit randlos gefuellt, max. Breite =
	// Rahmenbreite, der Rest liegt links/rechts per Pan im Bild.
	cellW, cellH := int(b.caps.CellW), int(b.caps.CellH)
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 9, 20
	}
	cr = aspectFillCrop(cr, rb.Dx(), rb.Dy(), cols, rows, cellW, cellH)
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropImage(raster, cr)); err != nil {
		imageLogf("crop-encode block=%d: %v", b.id, err)
		return nil, false
	}
	p, ferr := writeKittyFile(buf.Bytes())
	if ferr != nil {
		imageLogf("crop temp-datei block=%d: %v", b.id, ferr)
		return nil, false
	}
	inl := inlineActive.Load()
	c := &imgCache{
		cols: vCols, rows: vRows,
		png:     buf.Bytes(),
		decoded: raster, // Pan-Schritt-Reuse: gleiches virtuelles Raster
		inline:  inl,
		own:     true,
		src:     b.pngPath,
		pngPath: p,
		pw:      cr.Dx(), ph: cr.Dy(),
		txBuilt: time.Now(),
	}
	// Transmit mit FENSTER-Geometrie (c/r = Box, s/v = Crop-Pixel) — in
	// allen drei Medien dieselben Formen wie im Default-Pfad.
	switch {
	case inl:
		c.prefix = transmitDirectChunks(b.kittyID, c.png, cols, rows)
	case sshSession():
		c.prefix = transmitPlaceholderChunks(b.kittyID, c.png, cols, rows, c.pw, c.ph)
	default:
		c.prefix = b.virtualPlacement(cols, rows, p, c.pw, c.ph)
	}
	c.lines = make([]string, rows)
	if inl {
		c.lines[0] = c.prefix
	} else {
		for r := 0; r < rows; r++ {
			c.lines[r] = placeholderRow(b.kittyID, r, cols)
		}
	}
	// Alte Eintraege desselben Blocks raeumen (Temp-Dateien + dekodierter
	// Vollraster): jede Zoom-/Pan-Stufe erzeugt genau einen neuen Eintrag,
	// der Vorgaenger (mit decoded fuer den Pan-Reuse) faellt nach dem
	// Weiterreichen an windowRaster hier weg.
	for k, old := range imgCaches {
		if k.blockID == b.id {
			if old.own && old.pngPath != "" && old.pngPath != p {
				os.Remove(old.pngPath)
			}
			delete(imgCaches, k)
		}
	}
	imgCaches[key] = c
	mod := "crop t=f"
	if inl {
		mod = "crop timg"
	} else if sshSession() {
		mod = "crop ssh-inline"
	}
	imageLogf("image raster block=%d kitty=%d mod=%s virtuell=%dx%d fenster=%dx%d pan=%d,%d crop=%dx%d raster=%dx%d tx=%d B path=%s",
		b.id, b.kittyID, mod, vCols, vRows, cols, rows, panRow, panCol, cr.Dx(), cr.Dy(), rb.Dx(), rb.Dy(), len(c.prefix), p)
	return c, true
}

// windowRaster liefert das dekodierte VOLLraster des virtuellen Placements:
// SVG-Bloecke werden auf vCols×vRows Zellpixel gerastert (resvg, 8192-px-
// Kappung je Achse — darueber hinaus wird der Ausschnitt nur weicher, nie
// falsch), PNG-Bloecke nutzen ihre native Datei (das Terminal hat frueher
// das ganze Bild ins virtuelle Raster skaliert, jetzt croppen wir selbst).
// Der Decode ist teuer genug, um ihn ueber PAN-SCHRITTE (gleiches
// virtuelles Raster, anderes Fenster) wiederzuverwenden — der Scan findet
// den dekodierten Vorgaenger im Paket-Cache. Aufrufer haelt imgCacheMu.
func (b ImageBlock) windowRaster(vCols, vRows int) image.Image {
	for k, old := range imgCaches {
		if k.blockID == b.id && k.kittyID == b.kittyID && k.cols == vCols && k.rows == vRows && old.decoded != nil {
			return old.decoded
		}
	}
	var raw []byte
	if b.svg != nil {
		cellW, cellH := b.caps.CellW, b.caps.CellH
		if cellW <= 0 || cellH <= 0 {
			cellW, cellH = 9, 20
		}
		tw, th := vCols*cellW, vRows*cellH
		if tw > 8192 {
			tw = 8192
		}
		if th > 8192 {
			th = 8192
		}
		rt0 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		data, err := render.ResvgToPNG(ctx, b.svg, tw, th)
		cancel()
		perfRastered(float64(time.Since(rt0).Microseconds()) / 1000.0)
		if err != nil {
			imageLogf("svg-raster block=%d ziel=%dx%d fehlgeschlagen: %v", b.id, tw, th, err)
			return nil
		}
		raw = data
	} else {
		raw = b.png
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		imageLogf("raster-decode block=%d: %v", b.id, err)
		return nil
	}
	return img
}

// cropRect rechnet das Fenster (panRow/panCol, cols×rows Zellen) in ein
// Pixel-Rechteck im Raster pw×ph um. Der Raster deckt das virtuelle
// vCols×vRows-Zellraster PROPORTIONAL ab (bei SVG kann resvg ihn
// aspektbedingt minimal kleiner machen als die Zielpixel) — der Crop
// trifft so dieselbe Stelle, die das Vollbild-Placement zeigen wuerde, und
// bleibt immer im Raster: zoomLayout klemmt pan+window nie ueber virtual,
// Intersect plus Mindestgroesse 1px sind reine Vorsicht.
func cropRect(pw, ph, vCols, vRows, cols, rows, panRow, panCol int) image.Rectangle {
	if vCols < 1 {
		vCols = 1
	}
	if vRows < 1 {
		vRows = 1
	}
	x0 := panCol * pw / vCols
	y0 := panRow * ph / vRows
	x1 := (panCol + cols) * pw / vCols
	y1 := (panRow + rows) * ph / vRows
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	r := image.Rect(x0, y0, x1, y1).Intersect(image.Rect(0, 0, pw, ph))
	if r.Dx() < 1 || r.Dy() < 1 {
		return image.Rect(0, 0, 1, 1)
	}
	return r
}

// aspectFillCrop klemmt den proportionalen Fenster-Crop cr exakt auf den
// physikalischen Aspekt der Box (cols×rows Zellen mit cellW×cellH) — das
// Wunschszenario 10:N: der sichtbare Ausschnitt behaelt seine natuerliche
// Position (Pan-Mitte), seine BREITE bleibt der natuerliche Wert (max = die
// Rahmenbreite ist der Rahmen selbst; der TUI kann nie breiter als sein
// Fenster sein), und die HOEHE (der freie Freiheitsgrad) wird so gewaehlt,
// dass der Pixel-Aspekt den Box-Aspekt trifft — das Terminal skaliert den
// Ausschnitt damit randlos in die Box (keine Rechts-Luecke, keine leeren
// Zeilen). Bleibt der Aspekt nicht erreichbar (Bildrand), gewinnt der
// Kandidat mit der kleinsten Registrierdifferenz; der Ausschnitt bleibt
// immer im Bild.
func aspectFillCrop(cr image.Rectangle, pw, ph, cols, rows, cellW, cellH int) image.Rectangle {
	w, h := cr.Dx(), cr.Dy()
	if w < 1 || h < 1 || cols < 1 || rows < 1 {
		return cr
	}
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 9, 20
	}
	aBox := float64(cols*cellW) / float64(rows*cellH)
	aNat := float64(w) / float64(h)
	if d := aNat - aBox; d > -0.01 && d < 0.01 {
		return cr
	}
	cx, cy := float64(cr.Min.X)+float64(w)/2, float64(cr.Min.Y)+float64(h)/2
	clamp := func(w2, h2 float64) image.Rectangle {
		if w2 < 1 {
			w2 = 1
		}
		if h2 < 1 {
			h2 = 1
		}
		x0 := cx - w2/2
		y0 := cy - h2/2
		if x0 < 0 {
			x0 = 0
		}
		if y0 < 0 {
			y0 = 0
		}
		x1, y1 := x0+w2, y0+h2
		if x1 > float64(pw) {
			x1 = float64(pw)
			x0 = x1 - w2
			if x0 < 0 {
				x0 = 0
			}
		}
		if y1 > float64(ph) {
			y1 = float64(ph)
			y0 = y1 - h2
			if y0 < 0 {
				y0 = 0
			}
		}
		return image.Rect(int(x0), int(y0), int(x1), int(y1))
	}
	// Kandidat A: Breite behalten, Hoehe dem Box-Aspekt nachziehen.
	a := clamp(float64(w), float64(w)/aBox)
	// Kandidat B: Hoehe behalten, Breite dem Box-Aspekt nachziehen.
	b := clamp(float64(h)*aBox, float64(h))
	aErr := math.Abs(float64(a.Dx())/float64(a.Dy()) - aBox)
	bErr := math.Abs(float64(b.Dx())/float64(b.Dy()) - aBox)
	if bErr < aErr {
		return b
	}
	return a
}

// subImager ist das SubImage-Interface der Standard-Bildtypen (NRGBA, RGBA,
// Paletted, Gray, ...): cropImage schneidet daraus ohne Pixelkopie.
type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// cropImage schneidet r aus img: bevorzugt verlustfrei per SubImage (null
// Kopie), sonst (exotisches Format) per draw.Draw-Kopie. Das Ergebnis wird
// als eigenes Fenster-Bild encodiert und uebertragen.
func cropImage(img image.Image, r image.Rectangle) image.Image {
	if si, ok := img.(subImager); ok {
		return si.SubImage(r)
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst
}

// --- Chrome fuer den Text/Bild-Switch ------------------------------------
//
// Bild- und Quelltext-Ansicht teilen sich Kopf- und Fußzeile: gleiche
// Diagramm-Sprache, gleiche Blocknummer, gleicher Rahmen — nur der Koerper
// und die Footer-Actions wechseln ([ TEXT ] <-> [ IMAGE ]). So liest sich
// der t-Switch als Moduswechsel IM SELBEN Block statt als Austausch zweier
// fremder Bloecke.

// imgActive: Bild-Modus aktiv (Terminal-Grafik, Bild-ID und Quelle da)?
func (b ImageBlock) imgActive() bool {
	return b.useImg && b.kittyID > 0 && (b.pngPath != "" || b.svg != nil)
}

// footBox ist eine Footer-Action: Klartext (bestimmt die Kastenbreite)
// plus vorstylte Variante (Fokus reverse, Hover weiß+unterstrichen —
// dieselbe Behandlung wie COPY, s. actionStyle).
type footBox struct {
	label  string
	styled string
}

// footLayout ist die Zellgeometrie einer Footer-Zeile. Rendern (View) und
// Treffer-Erkennung (ActionAt) leiten sich aus DEMSELBEN Layout ab — kein
// Drift zwischen Dargestelltem und Klickbereich moeglich.
type footLayout struct {
	labelBox string // "[ label ]", leer wenn ohne Label oder zu schmal
	dashes   int
	ranges   [][2]int // [from,to) je Action-Kasten, in Zellen
}

// layoutFooter rechnet └─[ label ]───…───[ ACT1 ]─[ ACT2 ]─┘ aus. Alle
// Zeichen sind einzeilig, die Arithmetik ist exakt. Bei zu schmaler Zeile
// faellt zuerst das Label weg, die Actions bleiben.
func layoutFooter(width int, label string, boxes []footBox) footLayout {
	var ly footLayout
	lw := runewidth.StringWidth(label)
	sum := 0
	for _, bx := range boxes {
		sum += runewidth.StringWidth(bx.label) + 4
	}
	sep := len(boxes)
	if sep > 0 {
		sep--
	}
	dashes := width - 8 - lw - sum - sep
	if dashes < 0 && lw > 0 {
		// Label faellt; der Chrome ist dann nur noch "└─"+"─┘" (4 Zellen) —
		// NICHT die vollen 8, sonst wuerde der Footer breitenabhaengig 4
		// Zellen zu kurz (Exact-Width-Vertrag).
		lw = 0
		label = ""
		dashes = width - 4 - sum - sep
	}
	if dashes < 0 {
		dashes = 0
	}
	ly.dashes = dashes
	if lw > 0 {
		ly.labelBox = "[ " + label + " ]"
	}
	x := 2 + lw + 4 + dashes
	for _, bx := range boxes {
		w := runewidth.StringWidth(bx.label) + 4
		ly.ranges = append(ly.ranges, [2]int{x, x + w})
		x += w + 1 // Trenn-Strich zwischen Kaesten
	}
	return ly
}

// renderFooter setzt das Layout in Farbe: Rahmen/Striche im Border-Stil,
// Label im Label-Stil, Actions in ihrem eigenen Stil.
func renderFooter(th *render.Theme, ly footLayout, boxes []footBox, bs lipgloss.Style) string {
	st := bs
	ls := lipgloss.NewStyle()
	if th != nil {
		ls = th.LabelStyle()
	}
	var sb strings.Builder
	sb.WriteString(st.Render("└─"))
	if ly.labelBox != "" {
		sb.WriteString(ls.Render(ly.labelBox))
	}
	sb.WriteString(st.Render(strings.Repeat("─", ly.dashes)))
	for i, bx := range boxes {
		sb.WriteString(st.Render("[ "))
		sb.WriteString(bx.styled)
		sb.WriteString(st.Render(" ]"))
		if i < len(boxes)-1 {
			sb.WriteString(st.Render("─"))
		}
	}
	sb.WriteString(st.Render("─┘"))
	return sb.String()
}

// footerActions ist der gemeinsam genutzte Footer-Aufbau: Rendern
// (footBoxes/footerView) und Klick (ActionAt) leiten sich aus DEMSELBEN
// Ergebnis ab — kein Drift zwischen Anzeige und Hitbox. panIdx ist der
// Index des Pan-&-Zoom-Kastens (-1 = nicht angeboten).
type footerActions struct {
	label   string
	boxes   []footBox
	panIdx  int // Index des Pan-&-Zoom-Einstiegs (-1 = nicht angeboten)
	quitIdx int // Index des Zoom-Exit-Kastens (-1 = nicht angeboten)
	copyIdx int // Index des COPY-Kastens (-1 = nicht angeboten)
}

// footerFits prueft, ob Label + Kaesten in die Footer-Zeile passen — mit
// demselben Fallback wie layoutFooter (Label darf zuerst fallen).
func footerFits(width int, label string, boxes []footBox) bool {
	lw := runewidth.StringWidth(label)
	sum := 0
	for _, bx := range boxes {
		sum += runewidth.StringWidth(bx.label) + 4
	}
	sep := len(boxes) - 1
	if sep < 0 {
		sep = 0
	}
	if width-8-lw-sum-sep >= 0 {
		return true
	}
	return width-8-sum-sep >= 0
}

// panFootBox ist der Pan-&-Zoom-EINSTIEGS-Kasten — nur im (nicht gezoomten)
// Bild-Modus: Enter oeffnet den Modus ([ ⏎ PAN & ZOOM ]). Die Scale (⤢ %)
// ist danach eine reine INFO und steht links im Label, nicht als Kasten.
// footerActions liefert den INFO-Inhalt der Abschlusszeile — und zwar NUR
// Information (Zeilenzahl, Aufloesung, Zoom-Stand). Es gibt KEINE Aktionen
// und KEINE Klick-Kaesten mehr im Block-Footer: alle Aktionen (c copy,
// t text<->bild, enter/esc pan & zoom, +/-, Pfeile) stehen ausschliesslich
// in der zentralen Aktions-/Hint-Leiste ganz unten (app.footerView).
func (b ImageBlock) footerActions(width int) footerActions {
	fa := footerActions{}
	if b.showSrc {
		fa.label = labelAt(len(b.srcBlock.bodyLines(width)))
		return fa
	}
	if b.imgActive() {
		res := fmt.Sprintf("%d×%d px", b.pngW, b.pngH)
		if b.panning {
			fa.label = res + " · ⤢ " + strconv.Itoa(b.effZoomPct(width)) + "%"
		} else {
			fa.label = res
		}
		return fa
	}
	fa.label = labelAt(len(b.fallback.bodyLines(width)))
	return fa
}

// footBoxes liefert Label und Actions je Ansichtsmodus (s. footerActions).
func (b ImageBlock) footBoxes(width int) (string, []footBox) {
	fa := b.footerActions(width)
	return fa.label, fa.boxes
}

// borderStyle liefert den Rahmen-Stil des Bildblocks: Amber im Pan-&-Zoom-
// Modus (die Pfeiltasten steuern das Bild, nicht die Fokus-Kette — der
// Rahmen verkuendet das, bevor man eine Taste drueckt), sonst Fokus-Stil —
// bzw. die verblichene Sekundaer-Farbe der VERGANGENEN Auswahl (FR-43).
func (b ImageBlock) borderStyle() lipgloss.Style {
	if b.panning && b.imgActive() && !b.showSrc && b.th != nil {
		return b.th.PanBorderStyle()
	}
	return borderStyleFor(b.th, b.focused || b.marked, b.stale)
}

// footerView rendert die Fußzeile komplett (Layout + Stil).
func (b ImageBlock) footerView(width int) string {
	bs := b.borderStyle()
	if !(b.focused || b.hover || b.marked) {
		// Progressive Disclosure: ohne Fokus/Hover/Auswahl schliesst die Box
		// mit einer leeren Bahn; die Info-Zeile erscheint erst, wenn sie
		// relevant wird. Geometrie (eine Zeile) bleibt gleich.
		return bs.Render("└" + strings.Repeat("─", max(width-2, 0)) + "┘")
	}
	label, boxes := b.footBoxes(width)
	return renderFooter(b.th, layoutFooter(width, label, boxes), boxes, bs)
}

// imgPad zentriert ein cols Zellen breites Bild im Rahmen-Inneren
// (width-4) — fuehrende/abschliessende Leerzeichen je Bildzeile.
func imgPad(width, cols int) (lead, trail int) {
	inner := width - 4
	if inner < cols {
		return 0, 0
	}
	lead = (inner - cols) / 2
	return lead, inner - cols - lead
}

// frameImg umrahmt eine Bildzeile (Platzhalter-Raster oder Inline-Escape):
// Border links, linke Ausrichtung, Border rechts. KEIN lipgloss auf dem
// Inhalt — die Platzhalter-Zeichen (U+10EEEE + Diakritika) messen sich mit
// Unicode-Breiten falsch; die Zellzahl `cols` ist die einzige Wahrheit.
func (b ImageBlock) frameImg(ln string, lead, trail int, st lipgloss.Style) string {
	return st.Render("│") + " " + strings.Repeat(" ", lead) + ln + strings.Repeat(" ", trail) + " " + st.Render("│")
}

// ViewRange rendert die Blockzeilen [from,to): Kopfzeile, gerahmte und
// linksbuendige Bildzeilen (Transmit-Escape auf der ersten sichtbaren
// BILDZeile), Fußzeile mit [ TEXT ]/[ COPY ]. BlockList ruft das mit dem
// sichtbaren Fenster, damit hohe Bilder auch angeschnitten uebertragen
// werden, wenn ihr Anfang herausgescrollt ist (Follow-Modus).
func (b ImageBlock) ViewRange(width, from, to int) string {
	if b.showSrc {
		return b.srcViewRange(width, from, to)
	}
	if !b.imgActive() {
		return b.asciiViewRange(width, from, to)
	}
	if width < 16 {
		width = 16
	}
	// Pan & Zoom: die BOX (wCols×wRows = der Rahmen) waechst mit dem Zoom
	// mit, bis sie an die Rahmeninnenkante / Zeilen-Obergrenze stoesst; erst
	// DANN ueberragt das virtuelle Placement (vCols×vRows) die Box: renderCache
	// uebertraegt in dem Fall nur den FENSTERAUSSCHNITT als eigenes Bild
	// (Placement = Fenster, Platzhalter fensterlokal) — die Box verlaesst
	// nie den Schirm, und der Zoom ist nicht durch die Diakritika-faehige
	// Zellzahl (~298) begrenzt.
	vCols, vRows, cols, rows, panRow, panCol := b.zoomLayout(width)
	if from < 0 {
		from = 0
	}
	if to > rows+2 {
		to = rows + 2
	}
	if from >= to {
		return ""
	}
	c, ok := b.renderCache(vCols, vRows, cols, rows, panRow, panCol)
	if !ok {
		return b.asciiViewRange(width, from, to)
	}
	st := b.borderStyle()
	var out []string
	if from == 0 {
		out = append(out, headerLine(b.th, b.lang, b.id, width, st))
	}
	imgFrom, imgTo := from-1, to-1
	if imgFrom < 0 {
		imgFrom = 0
	}
	if imgTo > rows {
		imgTo = rows
	}
	if imgFrom < imgTo {
		body := make([]string, imgTo-imgFrom)
		copy(body, c.lines[imgFrom:imgTo])
		// t-Switch zurueck aufs Bild: Transmit-Fenster EINMAL erneut oeffnen.
		// Das Bild liegt meist noch im Terminal-Speicher und die Platzhalter
		// re-displayen es — aber nach Scroll/Partials kann das Terminal es
		// verworfen haben, dann staenden leere Platzhalter im Rahmen.
		if c.wasSrc && !b.showSrc {
			c.txCount = 0
			c.txBuilt = time.Now()
		}
		c.wasSrc = b.showSrc
		if inlineActive.Load() {
			// timg-Pfad: Prefix steckt dauerhaft in Zeile 0 — nur loggen.
			if c.txCount == 0 {
				c.txCount = 1
				imageLogf("image tx-direct block=%d kitty=%d c=%d r=%d", b.id, b.kittyID, cols, rows)
			}
		} else if c.txCount < 5 && time.Since(c.txBuilt) < 750*time.Millisecond {
			// Transmit nur kurz nach Erscheinen des Rasters senden (Flush-
			// Fenster, max. 5x) — NICHT bei jedem Scroll-Schritt.
			body[0] = c.prefix + body[0]
			c.txCount++
			imageLogf("image tx block=%d kitty=%d (%d. mal, fenster)", b.id, b.kittyID, c.txCount)
		}
		lead, trail := imgPad(width, cols)
		for _, ln := range body {
			out = append(out, b.frameImg(ln, lead, trail, st))
		}
	}
	if to >= rows+2 {
		out = append(out, b.footerView(width))
	}
	return strings.Join(out, "\n")
}

// View rendert das komplette Bild (Blockanfang sichtbar, z. B. reveal).
func (b ImageBlock) View(width int) string { return b.ViewRange(width, 0, 1<<30) }

func (b ImageBlock) Height(width int) int {
	if b.showSrc {
		return b.srcBlock.Height(width)
	}
	if !b.imgActive() {
		return b.fallback.Height(width)
	}
	// Kopf- + Fußzeile des Switch-Chrome kommen hinzu. Die Hoehe folgt der
	// BOX (zoomWindow): waechst das Bild beim Zoomen, waechst auch der
	// Rahmen mit — die Blocklist re-layoutet das dann automatisch je Frame.
	_, rows := b.zoomWindow(width, b.zoomPct())
	return rows + 2
}

// Update wertet Tasten des fokussierten Blocks — BlockList reicht enter/c
// und +,-,t durch (Bonus-FR): t schaltet zwischen Bild (Standard) und
// rohem Quelltext um. In der Quelltext-Ansicht geht alles ausser t an den
// CodeBlock: enter/c kopieren dort den QUELLTEXT samt Bestaetigungs-Flash,
// der Rahmen zeigt Fokus und die COPY-Action wie bei jedem Codeblock.
// +/- (Zoom) sind noch ohne Funktion — Bewegung/Zoom im Bild kommt spaeter.
func (b ImageBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return b, nil
	}
	// Pan & Zoom (nur Bild-Modus): die Pfeiltasten steuern das Bild, +/-
	// zoomen das virtuelle Raster, esc/enter kehren zurueck — der Kasten
	// bleibt dabei immer gleich gross, zu grosse Bilder werden im Fenster
	// angeschnitten. Beim Verlassen stellt sich die Default-Aufloesung
	// und -position wieder her.
	if b.panning && !b.showSrc {
		switch k.String() {
		case "esc", "enter":
			return b.togglePan()
		case "+", "=":
			return b.zoomBy(zoomStep)
		case "-", "_":
			return b.zoomBy(-zoomStep)
		case "up":
			return b.panBy(0, -1)
		case "down":
			return b.panBy(0, 1)
		case "left":
			return b.panBy(-1, 0)
		case "right":
			return b.panBy(1, 0)
		case "c":
			return b.Copy() // Copy bleibt auch im Pan-Modus erreichbar
		}
		return b, nil // Rest verschlucken: Pfeil-Tasten gehoeren dem Bild
	}
	switch k.String() {
	case "t":
		// FR-38: EINE Taste toggelt in BEIDE Richtungen (t->t = vorheriger
		// Zustand); die Zusatztaste i ist entfallen.
		return b.toggleSrc()
	case "c":
		// Copy im Bild-Modus: die ASCII-Grafik (Markdown-Payload des Blocks).
		// Im Quelltext-Modus greift unten die Weiterleitung an den CodeBlock,
		// die den QUELLTEXT samt Bestaetigungs-Flash kopiert.
		if !b.showSrc {
			return b.Copy()
		}
	case "enter":
		// Bild-Modus: enter oeffnet Pan & Zoom (der ⏎-Kasten im Footer ist
		// dieselbe Aktion klickbar). Copy bleibt c; in der Quelltext-Ansicht
		// faellt enter unten an den CodeBlock (kopiert den Quelltext).
		if !b.showSrc && b.imgActive() {
			return b.togglePan()
		}
	}
	if b.showSrc {
		w, cmd := b.srcBlock.Update(msg)
		if cb, ok := w.(CodeBlock); ok {
			nb := b
			nb.srcBlock = cb
			return nb, cmd
		}
	}
	return b, nil
}
func (b ImageBlock) Focused() bool   { return b.focused }
func (b ImageBlock) Focusable() bool { return true }
func (b ImageBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	nb.srcBlock.focused = f
	if !f {
		// Den Pan-Modus mitnehmen heisst, Pfeiltasten waeren ohne sicht-
		// bares Ziel aktiv — Fokus weg ist Modus weg, inkl. Default-
		// Restaurierung (zoom/pan zurueck).
		if nb.panning {
			nb.panning = false
			nb.zoom = 100
			nb.panRow, nb.panCol = 0, 0
		}
	}
	return nb
}
func (b ImageBlock) SetHover(h bool) Widget {
	nb := b
	nb.hover = h
	nb.hoverBox = -1
	return nb
}

// SetHoverAt setzt den Hover zielgenau auf den getroffenen Footer-Kasten:
// [ TEXT ]/[ IMAGE ] oder [ COPY ] — die Geometrie liefert layoutFooter,
// derselbe Code, der die Zeile rendert (kein Drift Anzeige vs. Hitbox).
func (b ImageBlock) SetHoverAt(width, x, y int) Widget {
	if width < 16 {
		width = 16
	}
	nb := b
	nb.hover = true
	nb.hoverBox = -1
	if y != b.Height(width)-1 {
		return nb
	}
	label, boxes := b.footBoxes(width)
	ly := layoutFooter(width, label, boxes)
	for i, r := range ly.ranges {
		if x >= r[0] && x < r[1] {
			nb.hoverBox = i
			break
		}
	}
	return nb
}
func (b ImageBlock) SetMarked(m bool) Widget {
	nb := b
	nb.marked = m
	nb.srcBlock.marked = m
	nb.fallback.marked = m
	return nb
}
func (b ImageBlock) Marked() bool { return b.marked }

func (b ImageBlock) SetStale(s bool) Widget {
	nb := b
	nb.stale = s
	nb.srcBlock.stale = s
	nb.fallback.stale = s
	return nb
}

// Markdown liefert den Markdown-Payload des Blocks (FR-29): Bei Diagrammen
// MIT Quelltext (PlantUML/mermaid-Zaun) der gezaeumte QUELLTEXT — nicht die
// ASCII-Grafik; nur Blöcke ohne Quelle (reines PNG) fallen auf die ASCII
// zurueck.
func (b ImageBlock) Markdown() string {
	if b.src != "" {
		lang := b.lang
		if lang == "" {
			lang = "text"
		}
		return "```" + lang + "\n" + b.src + "\n```"
	}
	return b.ascii
}
func (b ImageBlock) Resize(width int) Widget {
	// lastWidth merken: Update-Helfer (Zoom-Anker, Pan-Schritt, Clamp)
	// brauchen die Fenster-Masse, bekommen aber keine Breite herein.
	// Zugleich das Pan-Fenster neu einspannen — der Kasten kann durch
	// Resize schrumpfen, dann schrumpfen auch die Pan-Grenzen.
	nb := b
	nb.lastWidth = width
	// Gespeicherten Zoom nachklemmen: zoomMax ist seit dem Fenster-Crop die
	// einzige Grenze (die Diakritika-Klemmung band frueher auch hier die
	// Breite hinein) — ein alter Speicherstand ueber zoomMax gehoert
	// bereinigt, damit zoomPct()/Indikator und Render-Pfad nie auseinander-
	// laufen.
	if nb.zoom > zoomMax {
		nb.zoom = zoomMax
	}
	return nb.normalizePan()
}

// HitAction: Klicks laeuft ueber ActionAt (mehrere Kaesten); der alte
// Einziel-Pfad trifft hier nie zu.
func (b ImageBlock) HitAction(int, int, int) bool { return false }

// ActionAt ist der Klick-Dispatcher der Footer-Kaesten: [ TEXT ]/
// [ IMAGE ] schaltet die Ansicht (wie t), [ ⏎ PAN & ZOOM ] oeffnet/
// schliesst den Pan-Modus (wie enter), [ COPY ] kopiert den sichtbaren
// Inhalt — im Quelltext-Modus den QUELLTEXT (mit srcBlock-Flash), sonst
// die ASCII-Grafik. Die Geometrie liefert layoutFooter, derselbe Code,
// der die Fußzeile rendert.
func (b ImageBlock) ActionAt(width, x, y int) (Widget, tea.Cmd, bool) {
	if width < 16 {
		width = 16
	}
	if y != b.Height(width)-1 {
		return b, nil, false
	}
	fa := b.footerActions(width)
	ly := layoutFooter(width, fa.label, fa.boxes)
	hit := -1
	for i, r := range ly.ranges {
		if x >= r[0] && x < r[1] {
			hit = i
			break
		}
	}
	if hit < 0 {
		return b, nil, false
	}
	if hit == fa.panIdx || hit == fa.quitIdx {
		w, cmd := b.togglePan()
		return w, cmd, true
	}
	if b.src != "" && hit == 0 {
		w, cmd := b.toggleSrc()
		return w, cmd, true
	}
	if b.showSrc {
		w, cmd := b.srcBlock.Copy()
		if cb, ok := w.(CodeBlock); ok {
			nb := b
			nb.srcBlock = cb
			return nb, cmd, true
		}
		return b, nil, false
	}
	w, cmd := b.Copy()
	return w, cmd, true
}

// toggleSrc dreht den Moduswechsel um (t-Taste und TEXT/IMAGE-Klick).
// Ein offener Pan-&-Zoom-Modus wird dabei geschlossen (inkl. Default-
// Restaurierung) — die Quelltext-Ansicht hat kein Bild zu steuern.
func (b ImageBlock) toggleSrc() (Widget, tea.Cmd) {
	if b.src == "" {
		return b, nil
	}
	nb := b
	nb.showSrc = !b.showSrc
	nb.panning = false
	nb.zoom = 100
	nb.panRow, nb.panCol = 0, 0
	if nb.showSrc {
		// Alle Raster dieses Blocks als wasSrc markieren — der Umschalt
		// zurueck aufs Bild oeffnet das Transmit-Fenster dann genau
		// einmal neu (s. ViewRange). Direkt hier, weil ein schnelles
		// t-t ohne Render dazwischen sonst nicht erkannt wuerde.
		markImgCacheWasSrc(b.id, b.kittyID)
	}
	return nb, nil
}

// Copy liefert den Copy-Payload als Cmd — aufgerufen vom Eingabefeld-Chip
// und alt+<n>, ohne COPIED-Zustand im Block (der Chip zeigt das Feedback,
// s. Model.copiedFlash). Im Bild-Modus ist der Payload das MARKDOWN (der
// gezaeumte Diagramm-Quelltext; ohne Quelle die ASCII-Grafik), in der
// Quelltext-Ansicht (t-Switch) kopiert der CodeBlock den Quelltext — wie
// jeder CodeBlock roh, ohne Zaun.
func (b ImageBlock) Copy() (Widget, tea.Cmd) {
	if b.showSrc {
		return b.srcBlock.Copy()
	}
	if b.copyFn == nil {
		return b, nil
	}
	return b, b.copyFn(b.Markdown())
}

// ---- Zeichen-Modus (FR-42): Caret im Diagramm-QUELLTEXT --------------------
//
// Der ImageBlock ist NUR in der Quelltext-Ansicht (t-Switch, showSrc)
// buchstaben-faehig: Das gerenderte Bild hat keine Zeichenzellen — der
// Cursor spawnt dort nie (nextCharBlock ueberspringt den Block). Nach dem
// Umschalten auf den Zaun-Quelltext laeuft er aber wie durch jeden Code-
// block hinein: srcBlock liefert Zellen-EXAKT dieselben Zeilen, die
// srcViewRange rendert (headerLine davor = CharPad 1, codeBodyLine-Einzug
// links = CharLeft 2) — Teil-Auswahl von Diagramm-Quelle PLUS Folgetext
// wird so moeglich, ohne die Block-Auswahl zu verlassen (t greift dort,
// s. BlockList.handleKey).
func (b ImageBlock) CharLines(width int) []string {
	if !b.showSrc {
		return nil
	}
	return b.srcBlock.CharLines(width)
}

// CharPad: Kopfzeile vor der ersten Quelltext-Zeile (wie CodeBlock).
func (b ImageBlock) CharPad(width int) int { return 1 }

// CharLeft: Rahmen plus Padding-Zelle vor dem Quelltext (codeBodyLine).
func (b ImageBlock) CharLeft(width int) int { return 2 }

func (b ImageBlock) ID() int { return b.id }
func (b ImageBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	nb.srcBlock.id = i
	return nb
}

// SetKittyID setzt die Terminal-Bild-ID (1..250; nur dieser Bereich ist
// in \e[38;5;N m codierbar). Der Zeilen-Cache verkoerpert die ID.
func (b ImageBlock) SetKittyID(i int) Widget {
	nb := b
	nb.kittyID = i
	return nb
}

// SetSource haelt den rohen Diagramm-Quelltext fuer den t-Switch (Bonus-FR):
// applyDiagrams ruft das direkt nach der Konvertierung — bis dahin war der
// Zaun-Quelltext nur im fluechtigen CodeBlock und ging beim Uebergang ins
// Bild verloren (Markdown()/Copy() lieferten die ASCII-Grafik). Der
// srcBlock erbt ID/Fokus/Hover/Markierung, damit Kopieren und Rahmen stimmen.
func (b ImageBlock) SetSource(lang, src string) Widget {
	nb := b
	nb.src = strings.TrimSuffix(src, "\n")
	nb.lang = lang
	cb := NewCodeBlock(lang, nb.src, b.copyFn)
	cb.th = b.th
	cb.id = b.id
	cb.focused = b.focused
	cb.marked = b.marked
	nb.srcBlock = cb
	return nb
}

// srcViewRange rendert die Quelltext-Ansicht mit DEMSELBEN Chrome wie das
// Bild: identische Kopfzeile (Sprache + Blocknummer), Koerper im Code-Stil,
// Fußzeile mit [ IMAGE ]+[ COPY ]. ViewRange-Semantik wie beim Bild, damit
// BlockList.all() den Block auch angeschnitten rendert.
func (b ImageBlock) srcViewRange(width, from, to int) string {
	if width < 16 {
		width = 16
	}
	body := b.srcBlock.bodyLines(width)
	h := len(body) + 2
	if from < 0 {
		from = 0
	}
	if to > h {
		to = h
	}
	if from >= to {
		return ""
	}
	var out []string
	if from == 0 {
		out = append(out, headerLine(b.th, b.lang, b.id, width, b.borderStyle()))
	}
	bf, bt := from-1, to-1
	if bf < 0 {
		bf = 0
	}
	if bt > len(body) {
		bt = len(body)
	}
	if bf < bt {
		inner := codeInnerWidth(width)
		bs := b.borderStyle()
		fill := b.th.TableFill()
		for _, ln := range body[bf:bt] {
			out = append(out, codeBodyLine(ln, inner, bs, fill))
		}
	}
	if to >= h {
		out = append(out, b.footerView(width))
	}
	return strings.Join(out, "\n")
}

// asciiViewRange: ASCII-Fallback, ebenfalls mit Switch-Chrome ([ TEXT ]),
// wenn eine Quelle existiert — der Moduswechsel ist auch ohne Terminal-
// Grafik sichtbar und klickbar.
func (b ImageBlock) asciiViewRange(width, from, to int) string {
	if b.src == "" {
		return b.fallback.View(width) // ohne Quelle: klassischer Codeblock
	}
	if width < 16 {
		width = 16
	}
	body := b.fallback.bodyLines(width)
	h := len(body) + 2
	if from < 0 {
		from = 0
	}
	if to > h {
		to = h
	}
	if from >= to {
		return ""
	}
	var out []string
	if from == 0 {
		out = append(out, headerLine(b.th, b.lang, b.id, width, b.borderStyle()))
	}
	bf, bt := from-1, to-1
	if bf < 0 {
		bf = 0
	}
	if bt > len(body) {
		bt = len(body)
	}
	if bf < bt {
		inner := codeInnerWidth(width)
		bs := b.borderStyle()
		fill := b.th.TableFill()
		for _, ln := range body[bf:bt] {
			out = append(out, codeBodyLine(ln, inner, bs, fill))
		}
	}
	if to >= h {
		out = append(out, b.footerView(width))
	}
	return strings.Join(out, "\n")
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
