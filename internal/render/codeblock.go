package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CodeBlockView renders a fenced code block as a widget with a header and a
// [copy i] box — this is the overridable rendering unit per language tag.
func CodeBlockView(lang, code string, index int) string {
	head := strings.TrimSpace(lang)
	if head == "" {
		head = "code"
	}
	// Hellgrund -> dunkles ANSI-Blau (4) statt hellem (12), sonst unsichtbar.
	headerCol := "12"
	if LightBG() {
		headerCol = "4"
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(headerCol)).Render(
		"┌ " + head + " " + strings.Repeat("─", 3) + " [copy " + fmt.Sprint(index) + "] ┐",
	)
	body := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(code)
	return header + "\n" + body + "\n"
}
