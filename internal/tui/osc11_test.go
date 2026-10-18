package tui

import (
	"os"
	"strings"
	"testing"
)

// TestOsc11BgParse: OSC-11-Antworten (Ghostty-Form rgb:RRRR/GGGG/BBBB und
// #RRGGBB-Form) muessen die LightBG-Erkennung treiben.
func TestOsc11BgParse(t *testing.T) {
	os.Unsetenv("HERON_BG")
	light := "\x1b]11;rgb:ffff/ffff/ffff\x1b\\"
	dark := "\x1b]11;rgb:0a0a/1414/1919\x1b\\"
	hexForm := "\x1b]11;#FFFFFF\x1b\\"
	c1 := parseTermCaps(light)
	if !c1.LightBG {
		t.Fatalf("weisser Grund (rgb:ffff...) muss LightBG=true sein")
	}
	c2 := parseTermCaps(dark)
	if c2.LightBG {
		t.Fatalf("dunkler Grund (rgb:0a0a/1414/1919) muss LightBG=false sein")
	}
	c3 := parseTermCaps(hexForm)
	if !c3.LightBG {
		t.Fatalf("#FFFFFF muss LightBG=true sein")
	}
}

// TestHermesBgOverride: HERON_BG=light|dark muss jede Antwort uebersteuern.
func TestLightBgOverride(t *testing.T) {
	os.Setenv("HERON_BG", "light")
	if !parseTermCaps("").LightBG {
		t.Fatalf("HERON_BG=light muss LightBG=true erzwingen")
	}
	os.Setenv("HERON_BG", "dark")
	if parseTermCaps("\x1b]11;rgb:ffff/ffff/ffff\x1b\\").LightBG {
		t.Fatalf("HERON_BG=dark muss LightBG=false erzwingen")
	}
	os.Unsetenv("HERON_BG")
	_ = strings.TrimSpace
}
