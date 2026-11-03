package render

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// ANSI-SGR-Sequenzen fuer Inline-Formatierung. Geschlossen wird immer mit dem
// Attribut-ZWEI-Code ([22m/23m/24m/27m/29m), NICHT mit [0m — das
// wuerde die Basis-Farbe des Blocks loeschen.
const (
	ansiItalicOpen  = "[3m"
	ansiItalicClose = "[23m"
	ansiStrikeOpen  = "[9m"
	ansiStrikeClose = "[29m"
	ansiCodeOpen    = "[7m" // Inline-Code: Reverse-Video (man-Page-Look)
	ansiCodeClose   = "[27m"
	ansiUnderOpen   = "[4m"
	ansiUnderClose  = "[24m"
)

// inlineEnter schreibt die Oeffnungs-Sequenz eines Inline-Formatknotens in b:
// Emphasis (Level 2 = fett, sonst kursiv), Strikethrough, CodeSpan und <u>.
// <u>/</u> sind getrennte RawHTML-Blätter, daher wird die Unterstreichung
// direkt beim Enter geoeffnet bzw. geschlossen.
func inlineEnter(b *strings.Builder, n ast.Node, src []byte) {
	switch t := n.(type) {
	case *ast.Emphasis:
		b.WriteString(emOpen(t.Level))
	case *extast.Strikethrough:
		b.WriteString(ansiStrikeOpen)
	case *ast.CodeSpan:
		b.WriteString(ansiCodeOpen)
	case *ast.RawHTML:
		raw := string(t.Text(src))
		if strings.HasPrefix(raw, "<u") {
			b.WriteString(ansiUnderOpen)
		} else if strings.HasPrefix(raw, "</u") {
			b.WriteString(ansiUnderClose)
		}
	}
}

// inlineExit schliesst ein Inline-Format beim Verlassen des Knotens; RawHTML
// ist ein Blatt und wird nur beim Enter behandelt. Liefert true, wenn der
// Knoten geschlossen wurde.
func inlineExit(b *strings.Builder, n ast.Node) bool {
	switch t := n.(type) {
	case *ast.Emphasis:
		b.WriteString(emClose(t.Level))
	case *extast.Strikethrough:
		b.WriteString(ansiStrikeClose)
	case *ast.CodeSpan:
		b.WriteString(ansiCodeClose)
	default:
		return false
	}
	return true
}
