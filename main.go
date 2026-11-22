package main

import (
	"flag"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
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
	cont := flag.Bool("c", false, "resume the newest session of this directory (FR-25)")
	modelFlag := flag.String("model", "", "model label override (default: what goose reports, else \"goose\")")
	effortFlag := flag.String("effort", "", "effort label override (default: what goose reports, else \"medium\")")
	seedFile := flag.String("seed", "", "markdown file to seed into the chat on startup")
	contextWindow := flag.Int("context-window", 200000, "context window tokens for the footer bar")
	flag.Parse()

	// HERON_PRINT=1: reiner Text-Modus fuer die Fehler-Bisektion — GANZ
	// FRUEH, bevor irgendeine goose-Session/Prewarm startet (der Prewarm
	// hat vorher die Session trotzdem gespawnt; beim Exit brach deren Pipe:
	// "Broken pipe (os error 32)"). Kein TUI, kein Renderer: den Seed
	// einmal gerendert auf stdout, dann Ende.
	if os.Getenv("HERON_PRINT") == "1" {
		if *seedFile == "" {
			log.Fatalf("HERON_PRINT braucht --seed")
		}
		b, err := os.ReadFile(*seedFile)
		if err != nil {
			log.Fatalf("seed %s: %v", *seedFile, err)
		}
		w := 80
		if f := os.Getenv("HERON_PRINT_W"); f != "" {
			if n, err := strconv.Atoi(f); err == nil && n > 40 {
				w = n
			}
		}
		if err := tui.PrintOnly(string(b), w); err != nil {
			log.Fatal(err)
		}
		return
	}
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

	// Das goose-Session-Wiring (Prewarm/Resume, Config-Optionen, Inventar,
	// Extensions) lebt als backend-Methoden in backend.go — main verdrahtet
	// nur noch. pinned merkt die explizit gesetzten Flags: -model/-effort
	// unterdruecken die Auto-Erkennung der Labels.
	pinned := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { pinned[f.Name] = true })
	b := newBackend(*gooseBin, wd, stream, pinned)

	sender := b.send
	prewarmDone := make(chan struct{})
	if *demo {
		sender = demoStreamingSender(stream)
	} else {
		b.prewarm(*cont, prewarmDone)
	}

	// Shorten the home prefix for display only — the session keeps the real
	// absolute working directory.
	cwd := wd
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + strings.TrimPrefix(cwd, home)
	}

	// Term-Faehigkeiten abfragen (Kitty-Graphics/Platzhalter, Zellpixel,
	// Hintergrund-Helligkeit via OSC 11), bevor bubbletea stdin uebernimmt
	// (FR-27). Heller Grund -> dunkle Textpalette, sonst unsichtbar.
	caps := tui.ProbeTerminal()
	render.SetLightBG(caps.LightBG)
	if caps.LightBG {
		th = th.Light()
	}
	model := tui.New(sender, th, copyFn).WithStream(stream).WithCancel(b.cancel).
		WithConfigSet(b.configSet).WithNewSession(b.resetSession).
		WithResumeList(b.resumeRows).WithResumeStart(b.startResume).
		WithSkillsList(func() ([]tui.SkillRow, error) { return tui.GooseSkills(*gooseBin) }).
		WithExtensions(b.listExtensions, b.toggleExtension).
		WithContextWindow(*contextWindow).WithTermCaps(caps).
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
	if s := b.session(); s != nil {
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
		stream.ToolCall("demo-0", "demo-tool", "execute", "ls -la\nLICENSE README.md", false, false)
		for _, piece := range splitChunks(full, 14) {
			stream.Chunk(piece)
			time.Sleep(5 * time.Millisecond)
		}
		stream.ToolCall("demo-0", "demo-tool", "execute", "ls -la\nLICENSE README.md", true, false)
		// Zweite Demo-Karte: fetch-Icon UND Fehler-Zustand (✗) zeigen.
		stream.ToolCall("demo-1", "demo fetch", "fetch",
			"curl -s https://example.com\nconnection refused", true, true)
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

// inventoryFavorites vereint die beiden wahren "eingerichtet"-Quellen für
// die Ruhe-Liste des /provider-Pickers: die Keys des providers:-Blocks
// (goose nennt sie via _goose/unstable/config/read) und die eigenen
// Custom-Provider aus dem Inventar (ProviderType "Custom" — jede Datei
// in custom_providers/ war bewusste Handarbeit). Das configured-Flag des
// Inventars selbst lügt bei Schlüssellosen (lmstudio, local, claude-code,
// atomic_chat …) und bleibt bewusst außen vor — gegen echtes goose 1.55.0
// verifiziert: 17 von 88 als "configured" markiert, wahr sind 4.
func inventoryFavorites(inv []acp.InventoryEntry, own []string) []string {
	set := make(map[string]bool, len(own)+4)
	for _, id := range own {
		set[id] = true
	}
	for _, e := range inv {
		if e.ProviderType == "Custom" {
			set[e.ProviderID] = true
		}
	}
	favs := make([]string, 0, len(set))
	for id := range set {
		favs = append(favs, id)
	}
	sort.Strings(favs)
	return favs
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
