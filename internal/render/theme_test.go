package render

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func init() { lipgloss.SetColorProfile(termenv.ANSI256) }

func TestRenderTextDefaultIsBoldLavenderWhite(t *testing.T) {
	th, err := DefaultTheme()
	if err != nil {
		t.Fatalf("DefaultTheme: %v", err)
	}
	out, err := RenderText("Hallo Welt\n", th)
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	if !strings.Contains(out, "Hallo") || !strings.Contains(out, "Welt") {
		t.Fatalf("text fehlt: %q", out)
	}
	if !strings.Contains(out, "\x1b[1") {
		t.Fatalf("fett fehlt: %q", out)
	}
	if !strings.Contains(out, "38;5;189") {
		t.Fatalf("lavender-white(189) fehlt: %q", out)
	}
}

func TestThemeColorsFromJSONAndSwap(t *testing.T) {
	red, err := LoadTheme(strings.NewReader(`{"name":"rot","text":{"color":"196","bold":true}}`))
	if err != nil {
		t.Fatalf("LoadTheme rot: %v", err)
	}
	green, err := LoadTheme(strings.NewReader(`{"name":"gruen","text":{"color":"112","bold":false}}`))
	if err != nil {
		t.Fatalf("LoadTheme gruen: %v", err)
	}
	outRed, _ := RenderText("A\n", red)
	outGreen, _ := RenderText("A\n", green)
	if !strings.Contains(outRed, "38;5;196") {
		t.Fatalf("rot fehlt: %q", outRed)
	}
	if !strings.Contains(outGreen, "38;5;112") {
		t.Fatalf("gruen fehlt: %q", outGreen)
	}
	if strings.Contains(outGreen, "38;5;196") {
		t.Fatalf("theme-swap fehlgeschlagen: %q", outGreen)
	}
}

func TestTableChromeIsLavenderWithDarkFill(t *testing.T) {
	th, err := DefaultTheme()
	if err != nil {
		t.Fatalf("DefaultTheme: %v", err)
	}
	// Assert exact truecolor codes regardless of the profile set in init().
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.ANSI256)

	md := "| A | B |\n|---| --- |\n| 1 | 2 |\n"
	out, err := RenderText(md, th)
	if err != nil {
		t.Fatalf("RenderText table: %v", err)
	}
	// Bold lavender frame (#B4BEFE) …
	if !strings.Contains(out, "\x1b[1") {
		t.Fatalf("bold chrome fehlt: %q", out)
	}
	// #B4BEFE quantizes to 179;190;254 through lipgloss' color conversion.
	if !strings.Contains(out, "38;2;179;190;254") {
		t.Fatalf("lavender border fehlt: %q", out)
	}
	// … on a dark #141419 fill behind the whole table …
	if !strings.Contains(out, "48;2;20;20;25") {
		t.Fatalf("table background fill fehlt: %q", out)
	}
	// … but no colored cell text.
	if strings.Contains(out, "38;2;255;255;85") {
		t.Fatalf("cell text color verboten: %q", out)
	}
	for _, want := range []string{"┌", "│", "╞", "═", "╪", "└", "┘", "[", "]", "COPY", "1", "2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q fehlt: %q", want, out)
		}
	}
}

// TestHeadingScaleSubtle: die "Groesse" von h1..h6 ist eine subtile
// Prominenz-Hierarchie — alle fett, Farbe gleitet sanft von Action (h1)
// zu Text (h6), nur h1 unterstrichen (dezenter Abschnitts-Anker). Keine
// Font-Groessen, keine grossen Spaenge.

// hexDist misst den RGB-Abstand zweier Farben (Kontrast-Kontrolle).
func hexDist(a, b string) float64 {
	pa, pb := parseHex(a), parseHex(b)
	dx, dy, dz := float64(pa[0]-pb[0]), float64(pa[1]-pb[1]), float64(pa[2]-pb[2])
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func TestHeadingScaleSubtle(t *testing.T) {
	th, err := DefaultTheme()
	if err != nil {
		t.Fatalf("DefaultTheme: %v", err)
	}
	if got := th.HeadingColor(1); got != th.FocusColor() {
		t.Fatalf("h1 color = %q, want focus %q", got, th.FocusColor())
	}
	// Mehr Kontrast als vorher: h1 (Focus) muss klar von h6 (Text) abstehen.
	if d := hexDist(th.HeadingColor(1), th.HeadingColor(6)); d < 100 {
		t.Fatalf("Kontrast h1..h6 zu gering (d=%.0f)", d)
	}
	if got := th.HeadingColor(6); got != th.Text.Color {
		t.Fatalf("h6 color = %q, want body-text %q", got, th.Text.Color)
	}
	// H1..H6 liefern einen echten Verlauf (kein Klumpen gleicher Farben).
	seen := map[string]bool{}
	for l := 1; l <= 6; l++ {
		seen[th.HeadingColor(l)] = true
	}
	if len(seen) != 6 {
		t.Fatalf("heading gradient nicht monoton: %d verschiedene Farben", len(seen))
	}
	// Alle Stufen fett; Unterstreichung nur bei h1. Robust ueber
	// UnsetUnderline statt fragiler ANSI-Substrings.
	if !strings.Contains(th.HeadingStyle(1).Render("Titel"), "\x1b[1") {
		t.Fatal("h1 ist nicht fett")
	}
	h1 := th.HeadingStyle(1)
	if h1.Render("T") == h1.UnsetUnderline().Render("T") {
		t.Fatal("h1 fehlt die dezente Unterstreichung")
	}
	for _, l := range []int{2, 3, 6} {
		hs := th.HeadingStyle(l)
		if hs.Render("T") != hs.UnsetUnderline().Render("T") {
			t.Fatalf("h%d darf nicht unterstrichen sein", l)
		}
	}
}
