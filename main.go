package main

import (
	"context"
	"flag"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/acp"
	"heron/internal/clipboard"
	"heron/internal/render"
	"heron/internal/tui"
)

func main() {
	gooseBin := flag.String("goose", "goose", "goose binary (launched with 'acp' as ACP agent)")
	themeFile := flag.String("theme", "", "path to a theme JSON (default: embedded default.json)")
	flag.Parse()
	wd, _ := os.Getwd()

	var th *render.Theme
	th, err := render.DefaultTheme()
	if err != nil {
		log.Printf("default theme: %v", err)
	}
	if *themeFile != "" {
		f, err := os.Open(*themeFile)
		if err != nil {
			log.Fatalf("open theme: %v", err)
		}
		defer f.Close()
		if th, err = render.LoadTheme(f); err != nil {
			log.Fatalf("read theme: %v", err)
		}
	}

	// Copy runs as a tea.Cmd the runtime executes after Update returns —
	// Update itself stays free of side effects (Elm principle).
	copyFn := func(text string) tea.Cmd {
		return func() tea.Msg {
			clipboard.Copy(text)
			return nil
		}
	}

	var sess *acp.Session
	sender := func(prompt string) (string, error) {
		if sess == nil {
			s, err := acp.NewSession(context.Background(), *gooseBin, wd)
			if err != nil {
				return "", err
			}
			sess = s
		}
		return sess.Prompt(context.Background(), prompt)
	}

	if err := tui.Run(sender, th, copyFn); err != nil {
		log.Fatal(err)
	}
	if sess != nil {
		sess.Close()
	}
}
