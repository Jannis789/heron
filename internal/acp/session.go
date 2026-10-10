package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Session wraps initialize + session/new for one goose subprocess and answers
// prompts with the raw (unprocessed) agent_message_chunk text.
type Session struct {
	client    *Client
	sessionID string
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
	var sn struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(sres, &sn); err != nil || sn.SessionID == "" {
		c.Close()
		return nil, fmt.Errorf("session/new parse: %v", sres)
	}
	return &Session{client: c, sessionID: sn.SessionID}, nil
}

// Prompt sends a prompt and returns the raw agent_message_chunk text.
func (s *Session) Prompt(ctx context.Context, text string) (string, error) {
	var sb strings.Builder
	s.client.onNotify = func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var up struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if err := json.Unmarshal(params, &up); err != nil {
			return
		}
		if up.Update.SessionUpdate == "agent_message_chunk" && up.Update.Content.Type == "text" {
			sb.WriteString(up.Update.Content.Text)
		}
	}
	if _, err := s.client.Request(ctx, "session/prompt", map[string]any{
		"sessionId": s.sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
	}); err != nil {
		return sb.String(), fmt.Errorf("prompt: %w", err)
	}
	return sb.String(), nil
}

// Close shuts down the goose subprocess.
func (s *Session) Close() {
	if s.client != nil {
		s.client.Close()
	}
}
