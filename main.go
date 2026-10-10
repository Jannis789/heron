package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"sync"
	"time"

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
	modelFlag := flag.String("model", "goose", "model label shown in the prompt box")
	effortFlag := flag.String("effort", "medium", "effort label shown in the prompt box")
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

	// stream is the bridge the backend uses to push live updates into the UI
	// from the ACP reader goroutine (FR-2). The program attaches itself when
	// it starts; publishing before that is a no-op.
	stream := &tui.Stream{}

	// sess is touched by the prompt goroutine and by Ctrl+C in the event
	// loop, so every access goes through sessMu (NFR-2: no data races).
	var (
		sessMu sync.Mutex
		sess   *acp.Session
	)
	ensureSession := func() (*acp.Session, error) {
		sessMu.Lock()
		defer sessMu.Unlock()
		if sess != nil {
			return sess, nil
		}
		s, err := acp.NewSession(context.Background(), *gooseBin, wd)
		if err != nil {
			return nil, err
		}
		sess = s
		return sess, nil
	}
	session := func() *acp.Session {
		sessMu.Lock()
		defer sessMu.Unlock()
		return sess
	}

	events := &acp.Events{
		OnChunk:   func(text string) { stream.Chunk(text) },
		OnThought: func(text string) { stream.Thought(text) },
		OnTool:    func(name string, done bool) { stream.Tool(name, done) },
	}

	sender := func(prompt string) (string, error) {
		s, err := ensureSession()
		if err != nil {
			return "", err
		}
		return s.PromptStream(context.Background(), prompt, events)
	}
	if *demo {
		sender = demoStreamingSender(stream)
	}

	// Ctrl+C during a turn aborts it asynchronously (FR-7): we notify the
	// agent and wait for the prompt request to settle, never kill mid-write.
	cancel := func() {
		if s := session(); s != nil {
			_ = s.Cancel(context.Background())
		}
	}

	// Shorten the home prefix for display only — the session keeps the real
	// absolute working directory.
	cwd := wd
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + strings.TrimPrefix(cwd, home)
	}

	model := tui.New(sender, th, copyFn).WithStream(stream).WithCancel(cancel).
		WithWorkspace(cwd).WithModel(*modelFlag).WithEffort(*effortFlag)
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
	if s := session(); s != nil {
		s.Close()
	}
}

// demoStreamingSender answers with canned markdown and pushes it through the
// live stream in small pieces, so the incremental path can be exercised
// without a goose installation (FR-2 demo).
func demoStreamingSender(stream *tui.Stream) func(string) (string, error) {
	return func(prompt string) (string, error) {
		full := demoMarkdown(prompt)
		stream.Thought("demo: überlege eine Antwort …")
		stream.Tool("demo-tool", false)
		for _, piece := range splitChunks(full, 14) {
			stream.Chunk(piece)
			time.Sleep(5 * time.Millisecond)
		}
		stream.Tool("demo-tool", true)
		return full, nil
	}
}

// splitChunks cuts s into pieces of at most n runes, preferring to break at a
// space so the streamed preview grows word by word.
func splitChunks(s string, n int) []string {
	r := []rune(s)
	var out []string
	for len(r) > 0 {
		if len(r) <= n {
			out = append(out, string(r))
			break
		}
		cut := n
		for i := n; i > n/2; i-- {
			if r[i-1] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
	}
	return out
}

// demoMarkdown is the canned answer used by -demo (text, fence, table).
func demoMarkdown(prompt string) string {
	var b strings.Builder
	b.WriteString("Du fragst: **" + prompt + "**\n\n")
	b.WriteString("Das ist eine Demowiederung von heron ohne goose-Subprozess.\n\n")
	b.WriteString("```go\npackage main\n\nfunc main() {\n    println(hallo)\n}\n```\n\n")
	b.WriteString("| Komponente | Status | Note |\n|---|---|---|\n")
	b.WriteString("| Render | ok | Codeblock + Tabelle |\n")
	b.WriteString("| Copy | ok | Maus oder Tab |\n")
	b.WriteString("| Session | demo | kein goose nötig |\n")
	return b.String()
}
