package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
		OnTool: func(e acp.ToolEvent) {
			stream.Tool(tui.ToolUpdate{
				CallID: e.CallID, Label: e.Label(), Kind: e.Kind, Text: e.Text,
				Command: e.Command, Stdout: e.Stdout, Stderr: e.Stderr,
				Exit: e.ExitCode, HasOutput: e.HasOutput, Live: e.Live,
				Done: e.Done, Failed: e.IsError,
			})
		},
		OnUsage: func(u acp.Usage) { stream.Usage(u.InputTokens, u.OutputTokens) },
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
		// Provider ist nie gepinnt — er gehoert zur Session (FR-45).
		if mdl == "" && eff == "" && s.Provider() == "" {
			return
		}
		provOpts := make([]tui.OptionRow, len(s.ProviderOptions()))
		for i, po := range s.ProviderOptions() {
			provOpts[i] = tui.OptionRow{Value: po.Value, Name: po.Name}
		}
		stream.Info(mdl, eff, s.Provider(), s.ModelOptions(), s.EffortOptions(), provOpts)
	}

	// publishProviderFavorites holt die EINGERICHTETEN Provider aus goose
	// selbst (_goose/unstable/config/read, key "providers"): goose nennt
	// genau die Keys des providers:-Blocks — bei ~90 katalogisierten
	// Providern (davon keyless wie lmstudio "configured") sind das die
	// einzigen, die der Nutzer wirklich eingerichtet hat. Der
	// /provider-Picker zeigt nur sie (plus den aktiven), der Vollkatalog
	// bleibt über die Suche erreichbar. Fehler bleiben stumm: ohne
	// Favoriten zeigt der Picker wie bisher die volle Liste.
	publishProviderFavorites := func(s *acp.Session) {
		own, err := s.ConfiguredProviders(context.Background())
		if err == nil && len(own) > 0 {
			stream.ProviderFavorites(own)
		}
	}

	// publishConfigResult wertet die ERFRISCHTEN configOptions aus einer
	// set_config_option-Antwort aus: Ein Provider-Wechsel meldet die NEUE
	// Modellliste — Picker und Footer stehen danach nicht auf Altbestand.
	publishConfigResult := func(res []byte) {
		mdl, eff, prov, mdls, effs, provs := acp.ParseConfig(res)
		if mdl == "" && eff == "" && prov == "" && len(mdls) == 0 && len(effs) == 0 && len(provs) == 0 {
			return
		}
		provOpts := make([]tui.OptionRow, len(provs))
		for i, po := range provs {
			provOpts[i] = tui.OptionRow{Value: po.Value, Name: po.Name}
		}
		stream.Info(mdl, eff, prov, mdls, effs, provOpts)
	}

	// Resume (FR-44): Listing und Start als Callbacks ins TUI — dieselbe
	// Naht wie /new. session/list liefert die gespeicherten Sessions,
	// session/load lädt die gewählte in einem frischen goose-Prozess und
	// replayt den Verlauf als Blöcke in den Chat.
	listInfos := func() ([]acp.SessionInfo, error) {
		if s := session(); s != nil {
			return s.ListSessions(context.Background())
		}
		return acp.ListSessions(context.Background(), *gooseBin)
	}
	replayCollect := func(it acp.ReplayItem) {
		switch it.Kind {
		case acp.ReplayUser:
			stream.ReplayUser(it.Text)
		case acp.ReplayAgent:
			stream.ReplayAgent(it.Text)
		case acp.ReplayTool:
			e := it.Tool
			stream.Tool(tui.ToolUpdate{
				CallID: e.CallID, Label: e.Label(), Kind: e.Kind, Text: e.Text,
				Command: e.Command, Stdout: e.Stdout, Stderr: e.Stderr,
				Exit: e.ExitCode, HasOutput: e.HasOutput, Live: e.Live,
				Done: e.Done, Failed: e.IsError,
			})
		}
	}
	startResume := func(id string) error {
		sessMu.Lock()
		defer sessMu.Unlock()
		if sess != nil {
			sess.Close()
			sess = nil
		}
		s, err := acp.LoadSession(context.Background(), *gooseBin, wd, id, replayCollect)
		if err != nil {
			return err
		}
		sess = s
		publishModelInfo(s)
		return nil
	}
	// resumeRows mappt goose's Sessionliste auf die Picker-Zeilen: Titel ·
	// lokale Zeit, dahinter gedimmt Ordner · Nachrichten · Modell.
	resumeRows := func() ([]tui.SessionRow, error) {
		infos, err := listInfos()
		if err != nil {
			return nil, err
		}
		rows := make([]tui.SessionRow, 0, len(infos))
		for _, in := range infos {
			title := in.Title
			if title == "" {
				title = in.SessionID
			}
			when := in.UpdatedAt
			if t, err := time.Parse(time.RFC3339, in.UpdatedAt); err == nil {
				when = t.Local().Format("02.01. 15:04")
			}
			dir := in.Cwd
			if base := filepath.Base(in.Cwd); base != "" && base != "." && base != "/" {
				dir = base
			}
			detail := dir
			if in.MessageCount > 0 {
				detail += " · " + strconv.Itoa(in.MessageCount) + " msgs"
			}
			if in.ModelID != "" {
				detail += " · " + in.ModelID
			}
			rows = append(rows, tui.SessionRow{ID: in.SessionID, Label: title + " · " + when, Detail: detail})
		}
		return rows, nil
	}
	// resumeNewest wählt die neueste Session dieses Ordners (FR-25) —
	// listInfos sortiert bereits newest-first.
	resumeNewest := func() error {
		infos, err := listInfos()
		if err != nil {
			return err
		}
		for _, in := range infos {
			if in.Cwd == wd {
				return startResume(in.SessionID)
			}
		}
		return fmt.Errorf("keine frühere Session in diesem Ordner")
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
	} else if *cont {
		// FR-25: heron -c übernimmt die letzte Session desselben Ordners —
		// der Verlauf replayt als Blöcke. Fehlschlag: neue Session + Hinweis.
		go func() {
			defer close(prewarmDone)
			if err := resumeNewest(); err != nil {
				stream.Notice("Resume fehlgeschlagen: " + err.Error() + " — neue Session.")
				if s, err := ensureSession(); err == nil {
					publishModelInfo(s)
					publishProviderFavorites(s)
				}
			}
		}()
	} else {
		// Prewarm the session right after launch: the prompt box then shows
		// the real model/effort without waiting for the first prompt.
		// Failure is silent — the fallback labels stay.
		go func() {
			defer close(prewarmDone)
			if s, err := ensureSession(); err == nil {
				publishModelInfo(s)
				publishProviderFavorites(s)
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
		res, err := s.SetConfigOption(context.Background(), id, value)
		if err == nil {
			publishConfigResult(res)
		}
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
		publishProviderFavorites(s)
		return nil
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
	model := tui.New(sender, th, copyFn).WithStream(stream).WithCancel(cancel).
		WithConfigSet(configSet).WithNewSession(resetSession).
		WithResumeList(resumeRows).WithResumeStart(startResume).
		WithContextWindow(*contextWindow).WithTermCaps(caps).
		WithWorkspace(cwd).WithModel(*modelFlag).WithEffort(*effortFlag)
	var seedText string
	if *seedFile != "" {
		b, err := os.ReadFile(*seedFile)
		if err != nil {
			log.Fatalf("seed %s: %v", *seedFile, err)
		}
		seedText = string(b)
		model = model.SeedMarkdown(seedText)
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
