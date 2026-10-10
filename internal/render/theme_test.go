package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func init() { lipgloss.SetColorProfile(termenv.ANSI256) }

func TestRenderTextDefaultIsBoldYellow(t *testing.T) {
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
	if !strings.Contains(out, "38;5;220") {
		t.Fatalf("gelb(220) fehlt: %q", out)
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
