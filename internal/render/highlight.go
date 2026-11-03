package render

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

// lightBG merkt sich einen HELLEN Terminal-Hintergrund (OSC 11, FR-27):
// dann wird die dunkle Chroma-Palette verwendet, sonst ist der Code
// hell-auf-hell unsichtbar (gemessen: #CDD6F4-Serie auf hellem Grund).
var lightBG atomic.Bool

// SetLightBG schaltet die Palette fuer einen hellen Terminal-Hintergrund um.
func SetLightBG(on bool) { lightBG.Store(on) }

// LightBG berichtet, ob ein heller Terminal-Hintergrund erkannt wurde.
func LightBG() bool { return lightBG.Load() }

// highlightStyleFor liefert die Chroma-Palette passend zur Terminal-Helligkeit.
func highlightStyleFor() string {
	if lightBG.Load() {
		return "catppuccin-latte"
	}
	return "catppuccin-mocha"
}

// Highlight renders source as ANSI-coloured text for a fence tag. An unknown
// tag falls back to plain text, so a mistyped language never breaks the chat.
// The result keeps line breaks; wrapping is the caller's job.
func Highlight(lang, source string) string {
	if strings.TrimSpace(source) == "" {
		return source
	}
	lexer := lexers.Get(strings.ToLower(strings.TrimSpace(lang)))
	if lexer == nil {
		lexer = lexers.Analyse(source)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	style := styles.Get(highlightStyleFor())
	if style == nil {
		style = styles.Fallback
	}
	it, err := lexer.Tokenise(nil, source)
	if err != nil {
		return source
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, it); err != nil {
		return source
	}
	return strings.TrimRight(buf.String(), "\n")
}

// HighlightLines highlights source and returns it already wrapped to w cells
// per line. It returns exactly one entry per rendered row, which is what the
// code block needs to size its frame and its footer correctly.
//
// Das Ergebnis ist gecacht: Height() und View() der CodeBloecke rufen diese
// Funktion JEDESMAL pro Frame auf, und lexers.Get() macht intern ein
// filepath.Match ueber die Muster aller ~250 Chroma-Lexer — ein CPU-Profil
// des Demos wies 91% der Zeit genau dort nach (path/filepath.matchChunk).
// Quellcode aendert sich nie, also ist (lang, source, w) ein stabiler Key.
// Die zurueckgegebene Slice ist geteilt und darf nicht veraendert werden.
type hlKey struct {
	lang string
	src  string
	w    int
}

var hlCache = struct {
	sync.RWMutex
	m map[hlKey][]string
}{m: map[hlKey][]string{}}

func HighlightLines(lang, source string, w int) []string {
	key := hlKey{strings.ToLower(strings.TrimSpace(lang)), source, w}
	hlCache.RLock()
	v, ok := hlCache.m[key]
	hlCache.RUnlock()
	if ok {
		return v
	}
	lines := WrapANSI(Highlight(lang, source), w)
	hlCache.Lock()
	if len(hlCache.m) > 8192 {
		hlCache.m = map[hlKey][]string{} // Hartes Limit gegen Auswuchs
	}
	hlCache.m[key] = lines
	hlCache.Unlock()
	return lines
}

// WrapANSI wraps ANSI-coloured text to w cells per row, one entry per
// rendered row. Counting runes would split escape sequences, so the wrapping
// itself is delegated to the ANSI-aware helper.
func WrapANSI(s string, w int) []string {
	if s == "" {
		return []string{""}
	}
	if w <= 0 {
		return strings.Split(s, "\n")
	}
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		for _, wl := range strings.Split(ansi.Wrap(ln, w, ""), "\n") {
			// x/ansi.Wrap behaelt Spaces VOR dem Umbruch in der Zeile
			// (gemessen: 17 Zellen bei w=16) — jede Zeile bekommt eine
			// harte ANSI-sichere Kante, der Rahmen haelt sonst nicht.
			out = append(out, ansi.Truncate(wl, w, ""))
		}
	}
	return out
}
