package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/acp"
	"heron/internal/clipboard"
	"heron/internal/render"
	"heron/internal/tui"
)

func main() {
	gooseBin := flag.String("goose", "goose", "goose binary (launched with 'acp' as ACP agent)")
	themeFile := flag.String("theme", "", "path to a theme JSON (default: embedded default.json)")
	demo := flag.Bool("demo", false, "canned reply instead of a goose session (TUI testing)")
	seedFile := flag.String("seed", "", "markdown file to seed into the chat on startup")
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
	realSender := func(prompt string) (string, error) {
		if sess == nil {
			s, err := acp.NewSession(context.Background(), *gooseBin, wd)
			if err != nil {
				return "", err
			}
			sess = s
		}
		return sess.Prompt(context.Background(), prompt)
	}
	sender := realSender
	if *demo {
		sender = demoSender
	}

	model := tui.New(sender, th, copyFn)
	if *seedFile != "" {
		b, err := os.ReadFile(*seedFile)
		if err != nil {
			log.Fatalf("seed %s: %v", *seedFile, err)
		}
		model = model.SeedMarkdown(string(b))
	}

	if err := tui.RunModel(model); err != nil {
		log.Fatal(err)
	}
	if sess != nil {
		sess.Close()
	}
}

// demoSender answers with canned markdown (text, fenced code, table) so the
// whole render path can be exercised without a goose installation.
func demoSender(prompt string) (string, error) {
	var b strings.Builder
	b.WriteString("Du fragst: **" + prompt + "**\n\n")
	b.WriteString("Das ist eine Demowiederung von heron ohne goose-Subprozess.\n\n")
	b.WriteString("```go\npackage main\n\nfunc main() {\n    println(hallo)\n}\n```\n\n")
	b.WriteString("| Komponente | Status | Note |\n|---|---|---|\n")
	b.WriteString("| Render | ok | Codeblock + Tabelle |\n")
	b.WriteString("| Copy | ok | Maus oder Tab |\n")
	b.WriteString("| Session | demo | kein goose nötig |\n")
	return b.String(), nil
}
