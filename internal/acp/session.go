package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
}

// Model reports the model goose announced for this session (configOptions
// "model"); empty when the agent does not say.
func (s *Session) Model() string { return s.model }

// Effort reports the thinking effort goose announced (configOptions
// "thinking_effort"); empty when the agent does not say.
func (s *Session) Effort() string { return s.effort }

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

// sessionInfo bundles what heron reads out of the session/new response.
type sessionInfo struct {
	sessionID     string
	model         string
	effort        string
	modelOptions  []string
	effortOptions []string
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
			CurrentValue string   `json:"currentValue"`
			Options      []struct {
				Value string `json:"value"`
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
		}
	}
	return si
}

// Events receives incremental session updates while a prompt is in flight.
// Callbacks run on the ACP reader goroutine: they must not block and must not
// touch UI state directly — publish into the tea runtime instead.
type Events struct {
	// OnChunk receives agent_message_chunk text (the answer being streamed).
	OnChunk func(string)
	// OnThought receives agent_thought_chunk text (thinking stream).
	OnThought func(string)
	// OnTool reports tool activity: the tool label and whether it finished.
	OnTool func(name string, done bool)
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
		modelOptions: si.modelOptions, effortOptions: si.effortOptions}, nil
}

// update mirrors the session/update payload. ACP puts tool metadata directly
// under "update"; unknown fields are ignored so schema drift cannot break the
// stream.
type update struct {
	Update struct {
		SessionUpdate string `json:"sessionUpdate"`
		Content       struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Title  string `json:"title"`
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	} `json:"update"`
}

// toolLabel picks the most human-readable name available for a tool call.
func toolLabel(u update) string {
	switch {
	case u.Update.Title != "":
		return u.Update.Title
	case u.Update.Name != "":
		return u.Update.Name
	case u.Update.Kind != "":
		return u.Update.Kind
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
			if up.Update.Content.Type != "text" {
				return
			}
			sb.WriteString(up.Update.Content.Text)
			if ev != nil && ev.OnChunk != nil {
				ev.OnChunk(up.Update.Content.Text)
			}
		case "agent_thought_chunk":
			if up.Update.Content.Type != "text" {
				return
			}
			if ev != nil && ev.OnThought != nil {
				ev.OnThought(up.Update.Content.Text)
			}
		case "tool_call", "tool_call_update":
			if ev != nil && ev.OnTool != nil {
				done := up.Update.SessionUpdate == "tool_call_update" ||
					up.Update.Status == "completed" || up.Update.Status == "done"
				ev.OnTool(toolLabel(up), done)
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
