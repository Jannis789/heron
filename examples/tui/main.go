// Command tui runs an interactive heron chat view seeded with a markdown
// file — hover and click COPY to copy blocks (OSC52) without a goose session.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/clipboard"
	"heron/internal/render"
	"heron/internal/tui"
)

func main() {
	path := "examples/reply.md"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
		os.Exit(1)
	}
	th, err := render.DefaultTheme()
	if err != nil {
		fmt.Fprintf(os.Stderr, "theme: %v\n", err)
		os.Exit(1)
	}
	copyFn := func(text string) tea.Cmd {
		return func() tea.Msg {
			clipboard.Copy(text)
			return nil
		}
	}
	m := tui.New(nil, th, copyFn).SeedMarkdown(string(src))
	if err := tui.RunModel(m); err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}
}
