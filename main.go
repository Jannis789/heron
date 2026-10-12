package main

import (
	"context"
	"flag"
	"fmt"
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
	modelFlag := flag.String("model", "", "model label override (default: what goose reports, else \"goose\")")
	effortFlag := flag.String("effort", "", "effort label override (default: what goose reports, else \"medium\")")
	seedFile := flag.String("seed", "", "markdown file to seed into the chat on startup")
	contextWindow := flag.Int("context-window", 200000, "context window tokens for the footer bar")
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
	// it starts; messages published before that are buffered and flushed on
	// attach.
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
		OnUsage:   func(u acp.Usage) { stream.Usage(u.InputTokens, u.OutputTokens) },
	}

	// The prompt box shows what goose really runs (ACP configOptions) —
	// unless the user pinned a value via -model/-effort.
	pinned := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { pinned[f.Name] = true })
	publishModelInfo := func(s *acp.Session) {
		mdl, eff := s.Model(), s.Effort()
		if pinned["model"] {
			mdl = ""
		}
		if pinned["effort"] {
			eff = ""
		}
		if mdl == "" && eff == "" {
			return
		}
		stream.Info(mdl, eff, s.ModelOptions(), s.EffortOptions())
	}

	sender := func(prompt string) (string, error) {
		s, err := ensureSession()
		if err != nil {
			return "", err
		}
		publishModelInfo(s)
		return s.PromptStream(context.Background(), prompt, events)
	}
	// prewarmDone closes once the startup session attempt settled; main
	// waits (bounded) for it before the final Close.
	prewarmDone := make(chan struct{})
	if *demo {
		sender = demoStreamingSender(stream)
	} else {
		// Prewarm the session right after launch: the prompt box then shows
		// the real model/effort without waiting for the first prompt.
		// Failure is silent — the fallback labels stay.
		go func() {
			defer close(prewarmDone)
			if s, err := ensureSession(); err == nil {
				publishModelInfo(s)
			}
		}()
	}

	// Ctrl+C during a turn aborts it asynchronously (FR-7): we notify the
	// agent and wait for the prompt request to settle, never kill mid-write.
	cancel := func() {
		if s := session(); s != nil {
			_ = s.Cancel(context.Background())
		}
	}

	// The model/effort popup changes session config options through the live
	// session — same mutex discipline as the sender (NFR-2).
	configSet := func(id, value string) error {
		s := session()
		if s == nil {
			return fmt.Errorf("keine goose-session")
		}
		_, err := s.SetConfigOption(context.Background(), id, value)
		return err
	}

	// /new starts a fresh session: close the old goose subprocess and spawn
	// a new one — the cleanest cut, no shared history state (FR-20).
	resetSession := func() error {
		sessMu.Lock()
		defer sessMu.Unlock()
		if sess != nil {
			sess.Close()
			sess = nil
		}
		s, err := acp.NewSession(context.Background(), *gooseBin, wd)
		if err != nil {
			return err
		}
		sess = s
		publishModelInfo(s)
		return nil
	}

	// Shorten the home prefix for display only — the session keeps the real
	// absolute working directory.
	cwd := wd
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + strings.TrimPrefix(cwd, home)
	}

	model := tui.New(sender, th, copyFn).WithStream(stream).WithCancel(cancel).
		WithConfigSet(configSet).WithNewSession(resetSession).
		WithContextWindow(*contextWindow).
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
	// If the prewarm is still mid-handshake, give it a moment so the session
	// is closed properly instead of leaking the subprocess.
	select {
	case <-prewarmDone:
	case <-time.After(2 * time.Second):
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
		stream.Thought("demo: thinking of a reply …")
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
	b.WriteString("You ask: **" + prompt + "**\n\n")
	b.WriteString("This is a demo run of heron without a goose subprocess.\n\n")
	b.WriteString("```go\npackage main\n\nfunc main() {\n    println(hello)\n}\n```\n\n")
	b.WriteString("| Component | Status | Note |\n|---|---|---|\n")
	b.WriteString("| Render | ok | code block + table |\n")
	b.WriteString("| Copy | ok | mouse or tab |\n")
	b.WriteString("| Session | demo | no goose needed |\n")
	return b.String()
}
