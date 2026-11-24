package main

// backend bündelt das goose-Session-Wiring hinter dem TUI: Session-Lifecycle
// (Prewarm, Resume, /new), Config-Optionen, Provider-Inventar, Skills- und
// Extension-Listen. Vorher lebten diese Closures inline in main(); als
// Methoden sind sie einzeln lesbar, testbar und die Mutex-Disziplin um die
// Session (NFR-2) an einem Ort versammelt.

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"heron/internal/acp"
	"heron/internal/prefs"
	"heron/internal/tui"
)

type backend struct {
	gooseBin string
	wd       string
	stream   *tui.Stream
	events   *acp.Events
	// pinned merkt, welche Flags der Nutzer explizit gesetzt hat
	// (-model/-effort) — deren Auto-Erkennung bleibt dann stumm.
	pinned map[string]bool
	// prefs persistiert den zuletzt genutzten Modell/Effort-Stand global
	// und je Session-ID: neue Sessions starten darauf, /resume holt den
	// Stand der damaligen Session nach.
	prefs *prefs.Store

	// sess wird vom Prompt-Goroutine und von Ctrl+C im Event-Loop
	// beruehrt: jeder Zugriff geht durch sessMu (NFR-2: keine Data Races).
	sessMu sync.Mutex
	sess   *acp.Session
}

// newBackend verdrahtet die Stream-Events: Alles, was die ACP-Reader-
// Goroutine meldet, wandert ueber den Stream in die TUI (FR-2).
func newBackend(gooseBin, wd string, stream *tui.Stream, pinned map[string]bool) *backend {
	b := &backend{gooseBin: gooseBin, wd: wd, stream: stream, pinned: pinned, prefs: prefs.Open()}
	b.events = &acp.Events{
		OnChunk:   func(text string) { stream.Chunk(text) },
		OnThought: func(text string) { stream.Thought(text) },
		OnTool: func(e acp.ToolEvent) {
			stream.Tool(toolUpdate(e))
		},
		OnUsage: func(u acp.Usage) { stream.Usage(u.InputTokens, u.OutputTokens) },
	}
	return b
}

// toolUpdate mappt ein acp.ToolEvent auf die Struktur des TUI-Streams —
// Prompt-Versand und session/load-Replay nutzen dieselbe Abbildung.
func toolUpdate(e acp.ToolEvent) tui.ToolUpdate {
	return tui.ToolUpdate{
		CallID: e.CallID, Label: e.Label(), Kind: e.Kind, Text: e.Text,
		Command: e.Command, Stdout: e.Stdout, Stderr: e.Stderr,
		Exit: e.ExitCode, HasOutput: e.HasOutput, Live: e.Live,
		Done: e.Done, Failed: e.IsError,
	}
}

// optionRows mapt ACP-Provider-Optionen auf die Picker-Zeilen der TUI.
func optionRows(opts []acp.ProviderOption) []tui.OptionRow {
	rows := make([]tui.OptionRow, len(opts))
	for i, po := range opts {
		rows[i] = tui.OptionRow{Value: po.Value, Name: po.Name}
	}
	return rows
}

// session liefert die laufende goose-Session (oder nil) unter dem Mutex.
func (b *backend) session() *acp.Session {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	return b.sess
}

// ensureSession startet die goose-Session beim ersten Zugriff lazy und
// liefert sie danach aus dem Cache.
func (b *backend) ensureSession() (*acp.Session, error) {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	if b.sess != nil {
		return b.sess, nil
	}
	s, err := acp.NewSession(context.Background(), b.gooseBin, b.wd)
	if err != nil {
		return nil, err
	}
	b.sess = s
	b.applyPrefs(s)
	return b.sess, nil
}

// applyPrefs spielt den GLOBAL gespeicherten "zuletzt genutzt"-Stand auf
// eine frische goose-Session (User-Wunsch 2026-11-23: neue Sessions
// starten immer auf dem letzten Modell/Effort, nicht auf goose-Defaults).
// Explizite -model/-effort-Flags bleiben vorrangig (pinned) — und ein
// stiller No-op von goose (FR-45-Nachfass) ist folgenlos: applyOption
// publiziert nur, was goose wirklich übernommen hat, gespeichert wird
// hier bewusst nichts (ein abgelehnter Wert darf den letzten Stand nicht
// überschreiben — die nächste Session probiert ihn erneut).
func (b *backend) applyPrefs(s *acp.Session) {
	if b.prefs == nil {
		return
	}
	model, effort := b.prefs.Last()
	if b.pinned["model"] {
		model = ""
	}
	if b.pinned["effort"] {
		effort = ""
	}
	b.applyOption(s, "model", model)
	b.applyOption(s, "thinking_effort", effort)
}

// applySessionPrefs holt den je Session-ID gespeicherten Stand nach einem
// /resume nach: goose meldet den Stand der geladenen Session zwar in den
// configOptions, aber nur was es zuverlässig restauriert — weicht der
// gemerkte Stand ab, setzen wir ihn explizit (pro Session speichern).
func (b *backend) applySessionPrefs(s *acp.Session) {
	if b.prefs == nil {
		return
	}
	model, effort := b.prefs.Session(s.SessionID())
	if b.pinned["model"] {
		model = ""
	}
	if b.pinned["effort"] {
		effort = ""
	}
	b.applyOption(s, "model", model)
	b.applyOption(s, "thinking_effort", effort)
}

// applyOption setzt EINE Config-Option, wenn der Zielwert vom gemeldeten
// Stand abweicht, und publiziert die erfrischten configOptions (Picker und
// Footer stehen danach nicht auf Altbestand — derselbe Vertrag wie beim
// manuellen Wechsel über configSet).
func (b *backend) applyOption(s *acp.Session, id, want string) {
	if want == "" {
		return
	}
	var cur string
	switch id {
	case "model":
		cur = s.Model()
	case "thinking_effort":
		cur = s.Effort()
	default:
		return
	}
	if cur == want {
		return
	}
	if res, err := s.SetConfigOption(context.Background(), id, want); err == nil {
		b.publishConfigResult(res)
	}
}

// publishModelInfo: The prompt box shows what goose really runs (ACP
// configOptions) — unless the user pinned a value via -model/-effort.
func (b *backend) publishModelInfo(s *acp.Session) {
	mdl, eff := s.Model(), s.Effort()
	if b.pinned["model"] {
		mdl = ""
	}
	if b.pinned["effort"] {
		eff = ""
	}
	// Provider ist nie gepinnt — er gehoert zur Session (FR-45).
	if mdl == "" && eff == "" && s.Provider() == "" {
		return
	}
	b.stream.Info(mdl, eff, s.Provider(), s.ModelOptions(), s.EffortOptions(), optionRows(s.ProviderOptions()))
}

// publishConfiguredFallback ist der Weg fuer aelteres goose ohne
// providers/list: config/read (key "providers") nennt die Keys des
// providers:-Blocks, sonst bleibt alles wie bisher.
func (b *backend) publishConfiguredFallback(s *acp.Session) {
	own, err := s.ConfiguredProviders(context.Background())
	if err == nil && len(own) > 0 {
		b.stream.ProviderFavorites(own)
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
func (b *backend) publishInventory(s *acp.Session) {
	inv, err := s.ProviderInventory(context.Background())
	if err != nil || len(inv) == 0 {
		b.publishConfiguredFallback(s)
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
	b.stream.ProviderFavorites(inventoryFavorites(inv, own))
	models := s.ModelOptions()
	if len(models) == 0 {
		models = curModels
	}
	b.stream.Info("", "", "", models, s.EffortOptions(), rows)
}

// publishConfigResult wertet die ERFRISCHTEN configOptions aus einer
// set_config_option-Antwort aus: Ein Provider-Wechsel meldet die NEUE
// Modellliste — Picker und Footer stehen danach nicht auf Altbestand.
func (b *backend) publishConfigResult(res []byte) {
	mdl, eff, prov, mdls, effs, provs := acp.ParseConfig(res)
	if mdl == "" && eff == "" && prov == "" && len(mdls) == 0 && len(effs) == 0 && len(provs) == 0 {
		return
	}
	b.stream.Info(mdl, eff, prov, mdls, effs, optionRows(provs))
}

// listInfos liefert die gespeicherten Sessions — ueber die laufende
// Session, sonst als eigener goose-Prozess (session/list vor dem
// ersten Prompt).
func (b *backend) listInfos() ([]acp.SessionInfo, error) {
	if s := b.session(); s != nil {
		return s.ListSessions(context.Background())
	}
	return acp.ListSessions(context.Background(), b.gooseBin)
}

// replayCollect leitet den session/load-Replay in den Stream (FR-44):
// Nutzerzeilen, final gerenderte Antworten, Tool-Karten — dieselbe
// Abbildung wie im Live-Betrieb.
func (b *backend) replayCollect(it acp.ReplayItem) {
	switch it.Kind {
	case acp.ReplayUser:
		b.stream.ReplayUser(it.Text)
	case acp.ReplayAgent:
		b.stream.ReplayAgent(it.Text)
	case acp.ReplayTool:
		b.stream.Tool(toolUpdate(it.Tool))
	}
}

// startResume laedt die gewaehlte Session in einem frischen goose-Prozess
// und replayt den Verlauf als Blöcke in den Chat.
func (b *backend) startResume(id string) error {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	if b.sess != nil {
		b.sess.Close()
		b.sess = nil
	}
	s, err := acp.LoadSession(context.Background(), b.gooseBin, b.wd, id, b.replayCollect)
	if err != nil {
		return err
	}
	b.sess = s
	b.applySessionPrefs(s)
	b.publishModelInfo(s)
	return nil
}

// resumeRows mappt goose's Sessionliste auf die Picker-Zeilen: Titel ·
// lokale Zeit, dahinter gedimmt Ordner · Nachrichten · Modell.
func (b *backend) resumeRows() ([]tui.SessionRow, error) {
	infos, err := b.listInfos()
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
func (b *backend) resumeNewest() error {
	infos, err := b.listInfos()
	if err != nil {
		return err
	}
	for _, in := range infos {
		if in.Cwd == b.wd {
			return b.startResume(in.SessionID)
		}
	}
	return fmt.Errorf("keine frühere Session in diesem Ordner")
}

// send stellt den Prompt an die (lazy gestartete) Session und streamt die
// Antwort ueber die Events.
func (b *backend) send(prompt string) (string, error) {
	s, err := b.ensureSession()
	if err != nil {
		return "", err
	}
	b.publishModelInfo(s)
	// /compress ist herons Alias fuer gooses Builtin /compact: Der
	// Verlauf wird als echter LLM-Turn zusammengefasst (die Antwort
	// ist die Komprimierung; ein session/compact-RPC existiert nicht).
	if prompt == "/compress" {
		prompt = "/compact"
	}
	return s.PromptStream(context.Background(), prompt, b.events)
}

// prewarm bereitet die Session direkt nach dem Start vor: Bei cont=true
// übernimmt sie die letzte Session desselben Ordners (FR-25, Verlauf
// replayt als Blöcke; Fehlschlag: neue Session + Hinweis), sonst startet
// sie eine frische — die Eingabebox zeigt dann sofort das echte Modell.
// done schliesst, sobald der Versuch settled (main wartet beim Exit
// begrenzt darauf); Fehler bleiben stumm — die Fallback-Labels bleiben.
func (b *backend) prewarm(cont bool, done chan struct{}) {
	if cont {
		go func() {
			defer close(done)
			if err := b.resumeNewest(); err != nil {
				b.stream.Notice("Resume fehlgeschlagen: " + err.Error() + " — neue Session.")
				if s, err := b.ensureSession(); err == nil {
					b.publishModelInfo(s)
					b.publishInventory(s)
				}
			}
		}()
		return
	}
	go func() {
		defer close(done)
		if s, err := b.ensureSession(); err == nil {
			b.publishModelInfo(s)
			b.publishInventory(s)
		}
	}()
}

// cancel: Ctrl+C during a turn aborts it asynchronously (FR-7): we notify
// the agent and wait for the prompt request to settle, never kill
// mid-write.
func (b *backend) cancel() {
	if s := b.session(); s != nil {
		_ = s.Cancel(context.Background())
	}
}

// configSet: The model/effort popup changes session config options
// through the live session — same mutex discipline as send (NFR-2). Ein
// erfolgreicher Wechsel wird zugleich persistiert (persistConfig): global
// als "zuletzt genutzt" für alle künftigen Sessions und je Session-ID für
// ein späteres /resume.
func (b *backend) configSet(id, value string) error {
	s := b.session()
	if s == nil {
		return fmt.Errorf("keine goose-session")
	}
	res, err := s.SetConfigOption(context.Background(), id, value)
	if err == nil {
		b.publishConfigResult(res)
		b.persistConfig(s)
	}
	return err
}

// persistConfig merkt den AKTUELLEN Stand nach einem Config-Wechsel —
// SetConfigOption spiegelt die Antwort bereits in den Session-Cache.
// Gespeichert wird, was goose wirklich übernommen hat: ein stiller No-op
// (FR-45-Nachfass) persistiert den alten Stand statt des abgelehnten
// Wunschs, und ein Provider-Wechsel mit still zurückgesetztem Modell
// persistiert genau dieses. Auch der Provider selbst fließt in den
// globalen Last-Stand ein, wenn goose ihn meldet.
func (b *backend) persistConfig(s *acp.Session) {
	if b.prefs == nil {
		return
	}
	b.prefs.SetLast(s.Model(), s.Effort())
	b.prefs.SetSession(s.SessionID(), s.Model(), s.Effort())
}

// resetSession: /new starts a fresh session: close the old goose
// subprocess and spawn a new one — the cleanest cut, no shared history
// state (FR-20). Modell/Effort übernehmen wir vom gespeicherten letzten
// Stand: /new bleibt damit auf der Wahl der vorherigen Session.
func (b *backend) resetSession() error {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	if b.sess != nil {
		b.sess.Close()
		b.sess = nil
	}
	s, err := acp.NewSession(context.Background(), b.gooseBin, b.wd)
	if err != nil {
		return err
	}
	b.sess = s
	b.applyPrefs(s)
	b.publishModelInfo(s)
	b.publishInventory(s)
	return nil
}

// ---- Skills + MCP-Extensions (/skills, /mcp) --------------------
// /skills: Quelle ist `goose skills list` (goose hat keine ACP-Methode
// dafür). /mcp: Liste aus gooses config.yaml; das Umschalten wirkt
// sofort in der laufenden Session (ExtensionManager) UND wird in die
// config.yaml geschrieben (dauerhaft ab dem nächsten Start).

// listExtensions liest den extensions:-Block über die Session.
func (b *backend) listExtensions() ([]tui.ExtRow, error) {
	s, err := b.ensureSession()
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

// toggleExtension schaltet den MCP-Server in der laufenden Session und
// persistiert ihn in gooses config.yaml.
func (b *backend) toggleExtension(key string, on bool) error {
	s, err := b.ensureSession()
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
