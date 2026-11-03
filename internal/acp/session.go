package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Session wraps initialize + session/new for one goose subprocess and answers
// prompts with the assembled agent_message_chunk text.
type Session struct {
	client        *Client
	sessionID     string
	model         string   // model goose runs (ACP configOptions), may be empty
	effort        string   // thinking effort (ACP configOptions), may be empty
	modelOptions  []string // selectable models goose announced
	effortOptions []string // selectable effort levels goose announced
	// provider: aktueller ACP-Provider (configOptions "provider") und die
	// waehlbaren Alternativen — der Wechsel laeuft ueber set_config_option
	// und liefert die ERFRISCHTEN configOptions zurueck (neue Modellliste!).
	provider        string
	providerOptions []ProviderOption
}

// Model reports the model goose announced for this session (configOptions
// "model"); empty when the agent does not say.
func (s *Session) Model() string { return s.model }

// SessionID reports the id goose assigned to this session — the same id a
// later /resume loads back (FR-44).
func (s *Session) SessionID() string { return s.sessionID }

// Effort reports the thinking effort goose announced (configOptions
// "thinking_effort"); empty when the agent does not say.
func (s *Session) Effort() string { return s.effort }

// Provider reports the AI provider goose announced (configOptions
// "provider"); empty when the agent does not say.
func (s *Session) Provider() string { return s.provider }

// ProviderOptions lists the selectable providers goose announced; may be
// empty. Name is the human-readable label, Value the config value.
func (s *Session) ProviderOptions() []ProviderOption { return s.providerOptions }

// ModelOptions lists the selectable models goose announced; may be empty.
func (s *Session) ModelOptions() []string { return s.modelOptions }

// EffortOptions lists the selectable effort levels goose announced; may be
// empty.
func (s *Session) EffortOptions() []string { return s.effortOptions }

// SetConfigOption changes a session config option (e.g. "model",
// "thinking_effort") and returns the raw result with the refreshed
// configOptions list.
func (s *Session) SetConfigOption(ctx context.Context, id, value string) (json.RawMessage, error) {
	return s.client.Request(ctx, "session/set_config_option", map[string]any{
		"sessionId": s.sessionID,
		"configId":  id,
		"value":     value,
	})
}

// ConfiguredProviders liefert die IDs der Provider, die der Nutzer IN
// GOOSE eingerichtet hat — goose liest seine eigene Konfiguration
// (_goose/unstable/config/read, key "providers": der providers:-Block
// mit enabled/model/configured je Provider) und nennt die Keys. Das ist
// die goose-eigene Antwort auf "welche Provider existieren", nicht das
// Provider-Inventar: goose KATALOGISIERT ~90 Provider (Built-ins, davon
// keyless wie lmstudio "configured"), EINGERICHTET sind nur die hier
// genannten.
func (s *Session) ConfiguredProviders(ctx context.Context) ([]string, error) {
	res, err := s.client.Request(ctx, "_goose/unstable/config/read", map[string]any{"key": "providers"})
	if err != nil {
		return nil, err
	}
	return parseConfiguredProviders(res), nil
}

// parseConfiguredProviders zieht die Keys aus der value-Map der
// config/read-Antwort; Schema-Drift bleibt leer.
func parseConfiguredProviders(res []byte) []string {
	var raw struct {
		Value map[string]json.RawMessage `json:"value"`
	}
	if json.Unmarshal(res, &raw) != nil || raw.Value == nil {
		return nil
	}
	out := make([]string, 0, len(raw.Value))
	for k := range raw.Value {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ProviderOption ist eine waehlbare Provider-Auspraegung aus den ACP
// configOptions: Value ist der Konfigurationswert (Ziel von
// session/set_config_option), Name das lesbare Label ("Z.AI (GLM)").
type ProviderOption struct {
	Value string
	Name  string
}

// sessionInfo bundles what heron reads out of the session/new response —
// und, dank identischem configOptions-Layout, auch aus der Antwort von
// session/set_config_option (ParseConfig).
type sessionInfo struct {
	sessionID       string
	model           string
	effort          string
	provider        string
	modelOptions    []string
	effortOptions   []string
	providerOptions []ProviderOption
}

// parseUsage reads the token usage from a session/prompt response. Zero
// values on schema drift — the UI hides the segment instead of lying.
func parseUsage(res []byte) Usage {
	var raw struct {
		Usage struct {
			TotalTokens  int `json:"totalTokens"`
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(res, &raw) != nil {
		return Usage{}
	}
	return Usage{
		InputTokens:  raw.Usage.InputTokens,
		OutputTokens: raw.Usage.OutputTokens,
		TotalTokens:  raw.Usage.TotalTokens,
	}
}

// parseSessionInfo extracts the session id plus the config options goose
// announces. Anything missing stays empty — schema drift cannot break
// session creation.
func parseSessionInfo(sres []byte) sessionInfo {
	var si sessionInfo
	var raw struct {
		SessionID     string `json:"sessionId"`
		ConfigOptions []struct {
			ID           string `json:"id"`
			CurrentValue string `json:"currentValue"`
			Options      []struct {
				Value string `json:"value"`
				Name  string `json:"name"`
			} `json:"options"`
		} `json:"configOptions"`
	}
	if json.Unmarshal(sres, &raw) != nil {
		return si
	}
	si.sessionID = raw.SessionID
	for _, co := range raw.ConfigOptions {
		switch co.ID {
		case "model":
			si.model = co.CurrentValue
			for _, o := range co.Options {
				si.modelOptions = append(si.modelOptions, o.Value)
			}
		case "thinking_effort":
			si.effort = co.CurrentValue
			for _, o := range co.Options {
				si.effortOptions = append(si.effortOptions, o.Value)
			}
		case "provider":
			si.provider = co.CurrentValue
			for _, o := range co.Options {
				name := o.Name
				if name == "" {
					name = o.Value
				}
				si.providerOptions = append(si.providerOptions, ProviderOption{Value: o.Value, Name: name})
			}
		}
	}
	return si
}

// ParseConfig extrahiert Modell/Effort/Provider (inkl. Auswahlmoeglich-
// keiten) aus einer session/new- ODER session/set_config_option-Antwort:
// Nach einem Provider-Wechsel meldet goose die ERFRISCHTEN configOptions —
// v.a. die Modellliste des neuen Providers — genau hier.
func ParseConfig(res []byte) (model, effort, provider string, models, efforts []string, providers []ProviderOption) {
	si := parseSessionInfo(res)
	return si.model, si.effort, si.provider, si.modelOptions, si.effortOptions, si.providerOptions
}

// Events receives incremental session updates while a prompt is in flight.
// Callbacks run on the ACP reader goroutine: they must not block and must not
// touch UI state directly — publish into the tea runtime instead.
type Events struct {
	// OnChunk receives agent_message_chunk text (the answer being streamed).
	OnChunk func(string)
	// OnThought receives agent_thought_chunk text (thinking stream).
	OnThought func(string)
	// OnTool reports tool activity (tool_call/tool_call_update): the event
	// carries the call identity, a human-readable label, streamed content
	// text and the done state — heron rendert daraus die Karte im Chat.
	OnTool func(ToolEvent)
	// OnUsage reports the token accounting of the finished turn (the
	// session/prompt response carries it next to stopReason, FR-23).
	OnUsage func(Usage)
}

// Usage carries the token accounting of one finished turn.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// NewSession connects a fresh `goose acp` subprocess and creates a session.
func NewSession(ctx context.Context, gooseBin, cwd string) (*Session, error) {
	c, err := New(ctx, gooseBin, nil, nil)
	if err != nil {
		return nil, err
	}
	if _, err := c.Request(ctx, "initialize", map[string]any{"protocolVersion": 2, "clientInfo": map[string]any{}}); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	sres, err := c.Request(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": map[string]any{}})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("session/new: %w", err)
	}
	si := parseSessionInfo(sres)
	if si.sessionID == "" {
		c.Close()
		return nil, fmt.Errorf("session/new parse: %v", sres)
	}
	return &Session{client: c, sessionID: si.sessionID, model: si.model, effort: si.effort,
		modelOptions: si.modelOptions, effortOptions: si.effortOptions,
		provider: si.provider, providerOptions: si.providerOptions}, nil
}

// update mirrors the session/update payload. ACP puts tool metadata directly
// under "update"; unknown fields are ignored so schema drift cannot break the
// stream.
//
// Gegen echtes goose 1.55.0 verifiziert (2026-10-31, Raw-Probe): Message-
// Chunks tragen content als EIN Objekt {type,text}, Tool-Calls dagegen als
// LISTE — und goose schachtelt doppelt: [{"type":"content","content":
// {"type":"text","text":…}}]. Die Argumente stehen in rawInput (Objekt, z.B.
// {"command":…}), das Resultat in rawOutput {exit_code,stdout,stderr}, und
// während des Laufs streamen _meta.toolNotification.live_output-Chunks.
// Deshalb ist Content hier json.RawMessage — die Form parseb ich je
// SessionUpdate-Art tolerant (chunkText/parseToolContent), statt ein
// Schema zu erzwingen, dessen Typfehler das GANZE Update verwerfen wuerde.
type update struct {
	Update struct {
		SessionUpdate string          `json:"sessionUpdate"`
		ToolCallID    string          `json:"toolCallId"`
		Content       json.RawMessage `json:"content"`
		Title         string          `json:"title"`
		Name          string          `json:"name"`
		Kind          string          `json:"kind"`
		Status        string          `json:"status"`
		RawInput      json.RawMessage `json:"rawInput"`
		RawOutput     rawOutput       `json:"rawOutput"`
		Meta          updateMeta      `json:"_meta"`
	} `json:"update"`
}

// rawOutput ist das strukturierte Tool-Resultat (goose-Erweiterung neben dem
// ACP-Standard): exit_code/stdout/stderr statt flachem Text.
type rawOutput struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// DiffFile ist eine Datei-Aenderung aus einem diff-Content-Block (ACP v1:
// oldText null = neu angelegte Datei). Grundlage fuer das Diff-Rendering.
type DiffFile struct {
	Path    string
	OldText *string
	NewText string
}

// updateMeta: die goose-spezifischen Zusatzdaten unter _meta — der Tool-Name
// (toolCall.toolName, z.B. "shell" — goose sendet KEIN ACP-kind!) und die
// Live-Ausgabe laufender Kommandos (toolNotification.live_output).
type updateMeta struct {
	Goose struct {
		ToolCall struct {
			ExtensionName string `json:"extensionName"`
			ToolName      string `json:"toolName"`
		} `json:"toolCall"`
	} `json:"goose"`
	ToolNotification *struct {
		Type   string `json:"type"` // "live_output"
		Params struct {
			Chunks []struct {
				Output string `json:"output"`
				Stream string `json:"stream"` // stdout|stderr
			} `json:"chunks"`
		} `json:"params"`
	} `json:"toolNotification"`
}

// chunkText liest den Text eines Message-Chunk-Contents ({type,text} als
// Objekt). Kein Text-Block → leer (der Aufrufer ueberspringt dann).
func chunkText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var c struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &c) != nil || c.Type != "text" {
		return ""
	}
	return c.Text
}

// contentBlock ist EIN Element der Tool-Content-Liste. goose schachtelt
// doppel (type:"content" + innerer Block), der ACP-Standard direkt.
type contentBlock struct {
	Type    string          `json:"type"` // content|text|diff|…
	Content json.RawMessage `json:"content"`
	Text    string          `json:"text"`
	IsError bool            `json:"isError"`
	Path    string          `json:"path"`
	OldText *string         `json:"oldText"`
	NewText string          `json:"newText"`
}

// parseToolContent verarbeitet die Content-Liste eines Tool-Calls: die
// Text-Bloecke ( konkateniert), diff-Bloecke und der isError-Marker.
// Tolerant gegen beide Schachtelungen UND das alte Einzelobjekt (Fake).
func parseToolContent(raw json.RawMessage) (text string, diffs []DiffFile, isError bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	elems := []json.RawMessage{raw}
	if raw[0] == '[' {
		elems = nil
		if json.Unmarshal(raw, &elems) != nil {
			return "", nil, false
		}
	}
	var sb strings.Builder
	for _, el := range elems {
		var blk contentBlock
		if json.Unmarshal(el, &blk) != nil {
			continue
		}
		// goose-Doppelverschachtelung: [{"type":"content","content":{…}}]
		if blk.Type == "content" && len(blk.Content) > 0 {
			var inner contentBlock
			if json.Unmarshal(blk.Content, &inner) != nil {
				continue
			}
			inner.Content = nil
			blk = inner
		}
		isError = isError || blk.IsError
		switch blk.Type {
		case "text":
			sb.WriteString(blk.Text)
		case "diff":
			diffs = append(diffs, DiffFile{Path: blk.Path, OldText: blk.OldText, NewText: blk.NewText})
		}
	}
	return sb.String(), diffs, isError
}

// buildToolEvent wandelt ein geparsedes tool_call(_update)-Update in das
// ToolEvent der UI — eine Stelle, live (PromptStream) UND Replay geteilt.
func buildToolEvent(up update) ToolEvent {
	e := ToolEvent{
		CallID: up.Update.ToolCallID,
		Title:  up.Update.Title,
		Kind:   up.Update.Kind,
		Status: up.Update.Status,
	}
	// goose liefert den Tool-Namen in _meta (name-Feld bleibt leer).
	if e.Name = up.Update.Name; e.Name == "" {
		e.Name = up.Update.Meta.Goose.ToolCall.ToolName
	}
	// goose sendet kein ACP-kind: aus dem Tool-Namen ableiten (_meta).
	if e.Kind == "" {
		e.Kind = kindForTool(up.Update.Meta.Goose.ToolCall.ToolName)
	}
	// rawInput: goose liefert die Argumente als Objekt — Shell-Kommandos
	// als {"command":…}, sonst kompaktes JSON als Fallback.
	if len(up.Update.RawInput) > 2 { // nicht "{}" oder null
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(up.Update.RawInput, &in) == nil && in.Command != "" {
			e.Command = in.Command
		} else {
			e.Command = string(up.Update.RawInput)
		}
	}
	// rawOutput: das finale Resultat (exit_code/stdout/stderr).
	if up.Update.RawOutput.ExitCode != 0 || up.Update.RawOutput.Stdout != "" || up.Update.RawOutput.Stderr != "" {
		e.HasOutput = true
		e.ExitCode = up.Update.RawOutput.ExitCode
		e.Stdout = up.Update.RawOutput.Stdout
		e.Stderr = up.Update.RawOutput.Stderr
	}
	text, diffs, isErr := parseToolContent(up.Update.Content)
	e.Text, e.Diffs = text, diffs
	// Live-Ausgabe laufender Kommandos (_meta.toolNotification.live_output).
	if tn := up.Update.Meta.ToolNotification; tn != nil && tn.Type == "live_output" {
		var sb strings.Builder
		for _, ch := range tn.Params.Chunks {
			sb.WriteString(ch.Output)
		}
		e.Live = sb.String()
	}
	// Status: goose setzt completed/failed nur auf dem finalen Update —
	// in_progress-Frames sind zwischendurch (Karte bleibt laufend).
	e.Done = up.Update.Status == "completed" || up.Update.Status == "done" ||
		up.Update.Status == "failed"
	e.IsError = isErr || up.Update.Status == "failed"
	return e
}

// kindForTool mappt goose-Tool-Namen auf ACP-Kategorien (goose sendet kein
// kind — ohne Mapping faelle jede Karte auf das generische Icon zurueck).
func kindForTool(toolName string) string {
	switch toolName {
	case "shell":
		return "execute"
	case "apply_patch", "editor", "text_editor":
		return "edit"
	}
	return ""
}

// ToolEvent ist eine Tool-Aktivitaet des Agenten (session/update tool_call
// bzw. tool_call_update). CallID identifiziert den Call ueber den Stream
// hinweg — heron aktualisiert DIESELBE Karte im Chat, statt eine neue zu
// starten. Text ist der Inhalt, soweit goose ihn als Text-Content liefert
// (Kommando, Argumente, Ausgabe). Kind ist die ACP-Kategorie (execute/
// fetch/think/edit/create/other — Icon der Karte), IsError markiert ein
// gescheitertes Tool (Karte zeigt ✗ statt ✓).
type ToolEvent struct {
	CallID string
	Title  string
	Name   string // Tool-Name aus _meta (z.B. "shell") bzw. ACP name
	Kind   string
	Status string
	// Die echten goose-Felder: Command (rawInput), das finale Resultat
	// (rawOutput: ExitCode/Stdout/Stderr, HasOutput markiert das finale
	// Update) und der Live-Chunk laufender Kommandos (live_output).
	Command   string
	Stdout    string
	Stderr    string
	ExitCode  int
	HasOutput bool
	Live      string
	// Diffs traegt die diff-Content-Bloecke (Datei-Aenderungen) — Grundlage
	// fuer das kommende Diff-Rendering (FR-32).
	Diffs   []DiffFile
	Text    string
	Done    bool
	IsError bool
}

// Label picks the most human-readable name available for a tool call.
// goose kuerzt den Titel ("shell · ls -la …") — steht das volle Kommando
// als $-Zeile im Karten-Koerper, reicht der Tool-Name als Titel.
func (e ToolEvent) Label() string {
	switch {
	case e.Command != "" && e.Name != "":
		return e.Name
	case e.Title != "":
		return e.Title
	case e.Name != "":
		return e.Name
	case e.Kind != "":
		return e.Kind
	}
	return "tool"
}

// PromptStream sends a prompt, forwards every session/update to ev while the
// request runs (live streaming), and returns the assembled message text.
func (s *Session) PromptStream(ctx context.Context, text string, ev *Events) (string, error) {
	var sb strings.Builder
	s.client.onNotify = func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var up update
		if err := json.Unmarshal(params, &up); err != nil {
			return
		}
		switch up.Update.SessionUpdate {
		case "agent_message_chunk":
			txt := chunkText(up.Update.Content)
			if txt == "" {
				return
			}
			sb.WriteString(txt)
			if ev != nil && ev.OnChunk != nil {
				ev.OnChunk(txt)
			}
		case "agent_thought_chunk":
			txt := chunkText(up.Update.Content)
			if txt == "" {
				return
			}
			if ev != nil && ev.OnThought != nil {
				ev.OnThought(txt)
			}
		case "tool_call", "tool_call_update":
			if ev != nil && ev.OnTool != nil {
				ev.OnTool(buildToolEvent(up))
			}
		}
	}
	defer func() { s.client.onNotify = nil }()
	res, err := s.client.Request(ctx, "session/prompt", map[string]any{
		"sessionId": s.sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
	})
	if err != nil {
		return sb.String(), fmt.Errorf("prompt: %w", err)
	}
	// The response carries stopReason + usage (FR-23): publish the token
	// accounting when the backend reports it.
	if ev != nil && ev.OnUsage != nil {
		if u := parseUsage(res); u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0 {
			ev.OnUsage(u)
		}
	}
	return sb.String(), nil
}

// Prompt sends a prompt and returns the assembled agent message text.
func (s *Session) Prompt(ctx context.Context, text string) (string, error) {
	return s.PromptStream(ctx, text, nil)
}

// Cancel asks the agent to abort the running turn (session/cancel). The
// prompt request stays in flight until the agent honours it — stop() is
// asynchronous by design, we never kill the turn mid-write.
func (s *Session) Cancel(ctx context.Context) error {
	_ = ctx
	return s.client.Notify("session/cancel", map[string]any{"sessionId": s.sessionID})
}

// Close shuts down the goose subprocess.
func (s *Session) Close() {
	if s.client != nil {
		s.client.Close()
	}
}

// ---- Sessions übernehmen (FR-25/FR-44): list + load ------------------------
// Gegen echtes goose 1.55.0 verifiziert (acp-probe -list/-load): initialize
// meldet agentCapabilities.loadSession=true und sessionCapabilities.list,
// session/list antwortet mit {sessions,nextCursor} (50 je Seite, opaque
// Cursor-Parameter), session/load replays den Verlauf als session/update
// (user_message_chunk, agent_thought_chunk, agent_message_chunk, tool_*).

// SessionInfo is one stored session as goose reports it (session/list — a
// goose extension next to the ACP standard). MessageCount/ProviderID/
// ModelID come from _meta and may be zero/empty.
type SessionInfo struct {
	SessionID    string
	Cwd          string
	Title        string
	UpdatedAt    string // as reported (RFC3339)
	MessageCount int
	ProviderID   string
	ModelID      string

	updated time.Time // sort key, zero when unparseable
}

// parseSessionList extracts one session/list page: the infos sorted newest
// first plus the opaque nextCursor (empty = last page).
func parseSessionList(res []byte) ([]SessionInfo, string) {
	var raw struct {
		NextCursor string `json:"nextCursor"`
		Sessions   []struct {
			SessionID string `json:"sessionId"`
			Cwd       string `json:"cwd"`
			Title     string `json:"title"`
			UpdatedAt string `json:"updatedAt"`
			Meta      struct {
				MessageCount int    `json:"messageCount"`
				ProviderID   string `json:"providerId"`
				ModelID      string `json:"modelId"`
			} `json:"_meta"`
		} `json:"sessions"`
	}
	if json.Unmarshal(res, &raw) != nil {
		return nil, ""
	}
	out := make([]SessionInfo, 0, len(raw.Sessions))
	for _, s := range raw.Sessions {
		info := SessionInfo{
			SessionID:    s.SessionID,
			Cwd:          s.Cwd,
			Title:        s.Title,
			UpdatedAt:    s.UpdatedAt,
			MessageCount: s.Meta.MessageCount,
			ProviderID:   s.Meta.ProviderID,
			ModelID:      s.Meta.ModelID,
		}
		if t, err := time.Parse(time.RFC3339, s.UpdatedAt); err == nil {
			info.updated = t
		}
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].updated.Equal(out[j].updated) {
			return out[i].updated.After(out[j].updated)
		}
		return out[i].SessionID > out[j].SessionID
	})
	return out, raw.NextCursor
}

// maxListPages caps the session/list pagination — 5 pages × 50 sessions
// are plenty for the resume picker and bound the wait (NFR-2).
const maxListPages = 5

// listSessions pages through goose's stored sessions, newest first.
func (c *Client) listSessions(ctx context.Context) ([]SessionInfo, error) {
	var out []SessionInfo
	cursor := ""
	for page := 0; ; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		res, err := c.Request(ctx, "session/list", params)
		if err != nil {
			return nil, err
		}
		infos, next := parseSessionList(res)
		out = append(out, infos...)
		if next == "" || page >= maxListPages {
			return out, nil
		}
		cursor = next
	}
}

// ListSessions reports goose's stored sessions over the live connection —
// no extra subprocess while a session exists.
func (s *Session) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	return s.client.listSessions(ctx)
}

// ListSessions spawns a short-lived goose acp, lists the stored sessions
// and closes it again. Used when no live session exists (e.g. before the
// first prompt or after a failed startup).
func ListSessions(ctx context.Context, gooseBin string) ([]SessionInfo, error) {
	c, err := New(ctx, gooseBin, nil, nil)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if _, err := c.Request(ctx, "initialize", map[string]any{"protocolVersion": 2, "clientInfo": map[string]any{}}); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	return c.listSessions(ctx)
}

// ReplayKind classifies a replayed history element (session/load, FR-44).
type ReplayKind int

const (
	ReplayUser ReplayKind = iota
	ReplayAgent
	ReplayTool
)

// ReplayItem is one whole element of the loaded conversation: a user
// message, the agent answer or a tool call. Consecutive message chunks of
// one kind merge into one item — one goose message becomes one heron block.
type ReplayItem struct {
	Kind ReplayKind
	Text string
	Tool ToolEvent
}

// replayAccumulator turns the replay session/update notifications into
// whole ReplayItems. Message chunks of one kind accumulate (a kind change
// or tool event flushes); tool events pass through immediately — the UI
// upserts their cards by callID on the FR-2 path. flush emits pending text
// — LoadSession calls it once the load settled.
func replayAccumulator(collect func(ReplayItem)) (handle func(method string, params json.RawMessage), flush func()) {
	var kind ReplayKind
	var sb strings.Builder
	flush = func() {
		if sb.Len() > 0 {
			collect(ReplayItem{Kind: kind, Text: sb.String()})
			sb.Reset()
		}
	}
	handle = func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var up update
		if json.Unmarshal(params, &up) != nil {
			return
		}
		switch up.Update.SessionUpdate {
		case "user_message_chunk":
			if kind != ReplayUser {
				flush()
				kind = ReplayUser
			}
			sb.WriteString(chunkText(up.Update.Content))
		case "agent_message_chunk":
			if kind != ReplayAgent {
				flush()
				kind = ReplayAgent
			}
			sb.WriteString(chunkText(up.Update.Content))
		case "tool_call", "tool_call_update":
			flush()
			collect(ReplayItem{Kind: ReplayTool, Tool: buildToolEvent(up)})
		}
	}
	return handle, flush
}

// LoadSession connects a fresh `goose acp` subprocess and loads an existing
// session: the agent replays the stored conversation as session/update
// notifications, which the collector receives as whole ReplayItems in chat
// order. The returned Session is live — prompts continue the loaded
// conversation (FR-44).
func LoadSession(ctx context.Context, gooseBin, cwd, sessionID string, collect func(ReplayItem)) (*Session, error) {
	c, err := New(ctx, gooseBin, nil, nil)
	if err != nil {
		return nil, err
	}
	if _, err := c.Request(ctx, "initialize", map[string]any{"protocolVersion": 2, "clientInfo": map[string]any{}}); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	var flush func()
	if collect != nil {
		var handle func(string, json.RawMessage)
		handle, flush = replayAccumulator(collect)
		c.onNotify = handle
	}
	res, err := c.Request(ctx, "session/load", map[string]any{
		"sessionId":  sessionID,
		"cwd":        cwd,
		"mcpServers": map[string]any{},
	})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("session/load: %w", err)
	}
	if flush != nil {
		flush() // pending text am Replay-Ende nicht verlieren
	}
	// The load response carries modes + configOptions (model/effort of the
	// loaded session) but no sessionId — the session keeps the requested id.
	si := parseSessionInfo(res)
	return &Session{client: c, sessionID: sessionID, model: si.model, effort: si.effort,
		modelOptions: si.modelOptions, effortOptions: si.effortOptions,
		provider: si.provider, providerOptions: si.providerOptions}, nil
}
