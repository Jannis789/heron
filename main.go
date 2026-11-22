package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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

	// publishConfiguredFallback ist der Weg fuer aelteres goose ohne
	// providers/list: config/read (key "providers") nennt die Keys des
	// providers:-Blocks, sonst bleibt alles wie bisher.
	publishConfiguredFallback := func(s *acp.Session) {
		own, err := s.ConfiguredProviders(context.Background())
		if err == nil && len(own) > 0 {
			stream.ProviderFavorites(own)
		}
	}

	// publishInventory kitzelt das Provider-Inventar aus goose
	// (_goose/unstable/providers/list) und fuettert daraus BEIDE Picker:
	// die Provider-Zeilen mit den Katalog-Namen (abgekündigte ohne eigene
	// Konfiguration fliegen raus), die Favoriten = die EINGERICHTETEN —
	// und, falls die configOptions von session/new keine Modelle nennen,
	// die Modellliste des aktiven Providers aus dem Inventar. Fehler
	// bleiben stumm: dann sind die configOptions wie bisher die einzige
	// Quelle. Gegen echtes goose 1.55.0 probed (87 Provider, Custom-
	// Provider mit exakt deren Modellen).
	//
	// Favoriten-Quelle (2026-11-22, gegen goose 1.55.0 probed): NICHT das
	// configured-Flag des Inventars — es markiert 17 von 88 Providern als
	// "configured", darunter sämtliche Schlüssellosen (lmstudio, local,
	// claude-code, atomic_chat, aws_bedrock …), die nie eingerichtet
	// wurden. Wahrheit ist stattdessen: der providers:-Block aus
	// config/read PLUS die eigenen Custom-Provider (ProviderType "Custom"
	// — jede Datei in ~/.config/goose/custom_providers/ war bewusste
	// Handarbeit). inventoryFavorites() vereint beide.
	publishInventory := func(s *acp.Session) {
		inv, err := s.ProviderInventory(context.Background())
		if err != nil || len(inv) == 0 {
			publishConfiguredFallback(s)
			return
		}
		cur := s.Provider()
		rows := make([]tui.OptionRow, 0, len(inv))
		var curModels []string
		for _, e := range inv {
			if e.Deprecated && e.ProviderID != cur && !e.Configured {
				continue
			}
			rows = append(rows, tui.OptionRow{Value: e.ProviderID, Name: e.Name})
			if e.ProviderID == cur {
				for _, m := range e.Models {
					curModels = append(curModels, m.ID)
				}
			}
		}
		own, _ := s.ConfiguredProviders(context.Background())
		stream.ProviderFavorites(inventoryFavorites(inv, own))
		models := s.ModelOptions()
		if len(models) == 0 {
			models = curModels
		}
		stream.Info("", "", "", models, s.EffortOptions(), rows)
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
		// /compress ist herons Alias fuer gooses Builtin /compact: Der
		// Verlauf wird als echter LLM-Turn zusammengefasst (die Antwort
		// ist die Komprimierung; ein session/compact-RPC existiert nicht).
		if prompt == "/compress" {
			prompt = "/compact"
		}
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
					publishInventory(s)
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
				publishInventory(s)
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
		publishInventory(s)
		return nil
	}

	// ---- Skills + MCP-Extensions (/skills, /mcp) --------------------
	// /skills: Quelle ist `goose skills list` (goose hat keine ACP-Methode
	// dafür). /mcp: Liste aus gooses config.yaml; das Umschalten wirkt
	// sofort in der laufenden Session (ExtensionManager) UND wird in die
	// config.yaml geschrieben (dauerhaft ab dem nächsten Start).

	// listExtensions liest den extensions:-Block über die Session.
	listExtensions := func() ([]tui.ExtRow, error) {
		s, err := ensureSession()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		exts, err := s.Extensions(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]tui.ExtRow, 0, len(exts))
		for _, e := range exts {
			if !e.IsMCP() {
				continue // nur echte MCP-Server — Platform-Extensions sind goose-intern
			}
			rows = append(rows, tui.ExtRow{Key: e.Key, Label: e.Label(), Type: e.Type,
				Enabled: e.Enabled, Cmd: e.Cmd, EnvKeys: e.EnvKeys})
		}
		if len(rows) == 0 {
			return nil, nil
		}
		return rows, nil
	}
	toggleExtension := func(key string, on bool) error {
		s, err := ensureSession()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.SetExtensionEnabled(ctx, key, on); err != nil {
			return err
		}
		cfgPath, err := acp.DefaultConfigPath()
		if err != nil {
			return err
		}
		return acp.SetExtensionEnabledInFile(cfgPath, key, on)
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
		WithSkillsList(func() ([]tui.SkillRow, error) { return tui.GooseSkills(*gooseBin) }).
		WithExtensions(listExtensions, toggleExtension).
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
