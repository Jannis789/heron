package render

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

// highlightStyle is the chroma style used for fenced code. Catppuccin Mocha
// is the same family as the border and action colours of the default theme
// (themes/default.json), so highlighted code sits in the same palette.
const highlightStyle = "catppuccin-mocha"

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
	style := styles.Get(highlightStyle)
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
func HighlightLines(lang, source string, w int) []string {
	return WrapANSI(Highlight(lang, source), w)
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
		out = append(out, strings.Split(ansi.Wrap(ln, w, ""), "\n")...)
	}
	return out
}
