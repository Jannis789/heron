package render

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

//go:embed themes/default.json
var defaultThemeJSON []byte

// Style is an ANSI rendering setting; color values come from the theme JSON.
type Style struct {
	Color  string `json:"color"`
	Bold   bool   `json:"bold"`
	Italic bool   `json:"italic"`
}

// Theme defines the rendering scheme (swappable via JSON). Background is the
// base color of the whole TUI screen (and of the table fill).
type Theme struct {
	Name       string `json:"name"`
	Text       Style  `json:"text"`
	Border     Style  `json:"border"`
	Action     Style  `json:"action"`
	Focus      Style  `json:"focus"`
	Pan        Style  `json:"pan"`
	Background string `json:"background"`
}

// LoadTheme reads a theme from JSON.
func LoadTheme(r io.Reader) (*Theme, error) {
	var th Theme
	if err := json.NewDecoder(r).Decode(&th); err != nil {
		return nil, fmt.Errorf("theme json: %w", err)
	}
	return &th, nil
}

// DefaultTheme returns the embedded default.json.
func DefaultTheme() (*Theme, error) {
	return LoadTheme(bytes.NewReader(defaultThemeJSON))
}

// Light liefert eine Kopie des Themes mit DUNKLEN Farben fuer HELLE
// Terminal-Hintergruende (OSC-11-Erkennung, FR-27): die Dark-Palette
// (#D9D4EC etc.) ist auf hellem Grund hell-auf-hell unsichtbar.
func (t *Theme) Light() *Theme {
	c := *t
	c.Text.Color = "#3B4252"
	c.Border.Color = "#5E81AC"
	c.Action.Color = "#6C47C8"
	c.Focus.Color = "#7C3AED" // Neon-Violett abgedunkelt: lesbar auf Weiss
	c.Pan.Color = "#B45309"   // Amber abgedunkelt: lesbar auf Weiss
	c.Background = "#FFFFFF"
	return &c
}

// DefaultFocusColor ist Neon-Violett: die EINE Akzentfarbe fuer alles
// Tastaktive — der Hotkey-Buchstabe im Action-Label (C bei COPY, + beim
// Aufklappen) UND der Rahmen des per Tab fokussierten Blocks. Fehlt
// "focus" im Theme-JSON, greift dieser Fallback (alte Themes laufen weiter).
const DefaultFocusColor = "#BF00FF"

// FocusColor liefert die Tastatur-Akzentfarbe mit Fallback — auch fuer
// nil-Themes und Theme-JSONs ohne "focus"-Feld.
func (t *Theme) FocusColor() string {
	if t == nil || t.Focus.Color == "" {
		return DefaultFocusColor
	}
	return t.Focus.Color
}

// FocusBorderStyle ist der Rahmen-Stil des per Tab fokussierten Blocks:
// Neon-Violett statt der Ruhefarbe — die BORDER ist das Fokus-Signal,
// nicht mehr eine Reverse-Action im Footer.
func (t *Theme) FocusBorderStyle() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.FocusColor()))
	if t != nil && t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// DefaultPanColor ist Amber: die Rahmen-Farbe des Pan-&-Zoom-Modus —
// bewusst KEIN Violett, damit der Unterschied zum Fokus (Select-Modus)
// auf den ersten Blick klar bleibt. Fehlt "pan" im Theme-JSON, greift
// dieser Fallback (alte Themes laufen weiter).
const DefaultPanColor = "#FFB300"

// PanColor liefert die Pan-&-Zoom-Akzentfarbe mit Fallback — auch fuer
// nil-Themes und Theme-JSONs ohne "pan"-Feld.
func (t *Theme) PanColor() string {
	if t == nil || t.Pan.Color == "" {
		return DefaultPanColor
	}
	return t.Pan.Color
}

// PanBorderStyle ist der Rahmen-Stil des Pan-&-Zoom-Modus: Amber — die
// Pfeiltasten steuern das Bild, nicht die Fokus-Kette; der Rahmen
// verkuendet das, bevor man eine Taste drückt.
func (t *Theme) PanBorderStyle() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.PanColor()))
	if t != nil && t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// StaleColor ist die Farbe der VERGANGENEN Auswahl (FR-43): Beim Start
// einer NEUEN Auswahl (oder wenn der Fokus eine laufende verlaesst) bleibt
// die alte sichtbar — ihr Rahmen weicht farblich von der AKTUELLEN Auswahl
// ab (User-Wunsch: Rahmenzeichen unterscheidbar), damit nie zwei gleich
// farbene Auswahlen konkurrieren. Sie ist der Focus-Akzent, Richtung
// Terminal-Hintergrund verblichen (und nicht fett) — verwandt, aber klar
// schwächer als das Neon der aktuellen Auswahl.
func (t *Theme) StaleColor() string {
	fc := DefaultFocusColor
	bg := "#000000"
	if t != nil {
		fc = t.FocusColor()
		if t.Background != "" {
			bg = t.Background
		}
	}
	return LerpHex(fc, bg, 0.55)
}

// StaleBorderStyle ist der Rahmen-Stil der vergangenen Auswahl: verblichene
// Akzentfarbe, nicht fett (nil-theme safe).
func (t *Theme) StaleBorderStyle() lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(lipgloss.Color(t.StaleColor()))
	if t != nil && t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// TextStyle returns the lipgloss style for plain text of this theme.
func (t *Theme) TextStyle() lipgloss.Style {
	s := lipgloss.NewStyle()
	if t == nil {
		return s
	}
	if t.Text.Color != "" {
		s = s.Foreground(lipgloss.Color(t.Text.Color))
	}
	if t.Text.Bold {
		s = s.Bold(true)
	}
	if t.Text.Italic {
		s = s.Italic(true)
	}
	return s
}

// BorderStyle returns the frame style: bold + theme color + background fill
// (nil-theme safe: renders plain).
func (t *Theme) BorderStyle() lipgloss.Style {
	s := lipgloss.NewStyle()
	if t == nil {
		return s
	}
	if t.Border.Bold {
		s = s.Bold(true)
	}
	if t.Border.Color != "" {
		s = s.Foreground(lipgloss.Color(t.Border.Color))
	}
	if t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// HeadingStyle liefert den Stil einer Ueberschrift der Stufe level (1..6).
// Ein Terminal kann keine Font-Groessen — "Groesse" wird daher ueber
// Prominenz dargestellt: alle Ueberschriften sind fett, die Farbe wandert
// sanft von der Action-Farbe (h1, deutlich) zur Text-Farbe (h6, fast wie
// FliessText) — subtile Hierarchie, keine grossen Spaenge (FR: angefragt).
func (t *Theme) HeadingStyle(level int) lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if t == nil {
		return s
	}
	s = s.Foreground(lipgloss.Color(t.HeadingColor(level)))
	if level <= 1 {
		s = s.Underline(true) // dezenter Abschnitts-Anker fuer h1
	}
	return s
}

// HeadingColor interpoliert die Ueberschriftsfarbe der Stufe level: h1
// = Action-Farbe, h6 = Text-Farbe (siehe HeadingStyle).
func (t *Theme) HeadingColor(level int) string {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	// h1 startet an der saturierten FOCUS-Farbe (Neon, deutlich vom
	// Fliesstext abgesetzt), h6 an der Text-Farbe — mehr Kontrast als die
	// alte Action-zu-Text-Strecke (beide fast gleich hell).
	from := DefaultFocusColor
	to := from
	if t != nil {
		from = t.FocusColor()
		if t.Text.Color != "" {
			to = t.Text.Color
		}
	}
	// lerpHex(f: 0=a,1=b): level 1 -> f=0 (Focus), level 6 -> f=1 (Text)
	return lerpHex(from, to, float64(level-1)/5.0)
}

// LerpHex interpoliert zwei #RRGGBB-Farben linear (f: 0=a, 1=b) — exportiert
// fuer den Neon-Eingaberahmen (tui), der seine Zellfarben daraus mischt.
func LerpHex(a, b string, f float64) string {
	return lerpHex(a, b, f)
}

// lerpHex interpoliert zwei #RRGGBB-Farben linear (f: 0=a, 1=b).
func lerpHex(a, b string, f float64) string {
	pa, pb := parseHex(a), parseHex(b)
	lerp := func(x, y int) int {
		v := float64(x) + (float64(y-x) * f)
		c := int(v)
		if c < 0 {
			return 0
		}
		if c > 255 {
			return 255
		}
		return c
	}
	return "#" + hex2(lerp(pa[0], pb[0])) + hex2(lerp(pa[1], pb[1])) + hex2(lerp(pa[2], pb[2]))
}

// parseHex liest drei RGB-Byte aus #RRGGBB; unparsbare Kanaele bleiben 0.
func parseHex(s string) [3]int {
	var out [3]int
	if len(s) >= 7 && s[0] == '#' {
		for i := 0; i < 3; i++ {
			if v, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8); err == nil {
				out[i] = int(v)
			}
		}
	}
	return out
}

func hex2(v int) string {
	return fmt.Sprintf("%02X", v)
}

// HeaderStyle is the always-bold column-header cell style.
func (t *Theme) HeaderStyle() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if t != nil && t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// LabelStyle is the de-emphasized footer label style (faint frame color).
func (t *Theme) LabelStyle() lipgloss.Style {
	s := lipgloss.NewStyle().Faint(true)
	if t == nil {
		return s
	}
	if t.Border.Color != "" {
		s = s.Foreground(lipgloss.Color(t.Border.Color))
	}
	if t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// ActionStyle colors the footer action (e.g. COPY).
func (t *Theme) ActionStyle() lipgloss.Style {
	s := lipgloss.NewStyle()
	if t == nil {
		return s
	}
	if t.Action.Color != "" {
		s = s.Foreground(lipgloss.Color(t.Action.Color))
	}
	if t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// TableFill is the background-only style that fills a table block (cells and
// padding) with the theme background.
func (t *Theme) TableFill() lipgloss.Style {
	s := lipgloss.NewStyle()
	if t != nil && t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// BackgroundStyle is the whole-screen background style.
func (t *Theme) BackgroundStyle() lipgloss.Style {
	s := lipgloss.NewStyle()
	if t != nil && t.Background != "" {
		s = s.Background(lipgloss.Color(t.Background))
	}
	return s
}

// BackgroundSeq returns the raw SGR prefix that sets the theme background
// ("" if unset), suitable for re-establishing the fill after inner resets.
func (t *Theme) BackgroundSeq() string {
	if t == nil || t.Background == "" {
		return ""
	}
	return strings.TrimSuffix(t.BackgroundStyle().Render(""), "\x1b[0m")
}
