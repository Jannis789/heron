package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// BenchmarkLipglossWidthPlaceholder misst die Breitenberechnung einer
// echten Platzhalterzeile (118 Zellen U+10EEEE + je 2 Diakritika) — der
// Verdaechchtige hinter 66ms View-Zeit (40 Zeilen/Frame).
func BenchmarkLipglossWidthPlaceholder(b *testing.B) {
	line := placeholderRow(3, 1, 118)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lipgloss.Width(line)
	}
}

// BenchmarkLipglossWidthText als Vergleich: gewoehnliche Textzeile.
func BenchmarkLipglossWidthText(b *testing.B) {
	line := "normale Chat-Zeile mit etwas Text und Umlauten äöü — 118 Zeichen breit ungefaehr, egal."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lipgloss.Width(line)
	}
}
