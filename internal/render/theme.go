package render

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
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
