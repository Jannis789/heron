// cmd/acp-probe spawnt `goose acp` und gibt die unbearbeitete (rohe) Response aus.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"heron/internal/acp"
)

func main() {
	prompt := flag.String("prompt", "Antworte kurz auf Deutsch: sag nur Hallo.", "Prompt an goose")
	gooseBin := flag.String("goose", "goose", "goose binary")
	flag.Parse()
	wd, _ := os.Getwd()

	ctx := context.Background()

	// ACP v2 -> goose: Feld heisst `sessionUpdate`, `content` ist EIN Objekt {type,text}.
	c, err := acp.New(ctx, *gooseBin, func(method string, params json.RawMessage) {
		if method != "session/update" {
			return
		}
		var up struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if err := json.Unmarshal(params, &up); err != nil {
			return
		}
		if up.Update.SessionUpdate == "agent_message_chunk" && up.Update.Content.Type == "text" {
			fmt.Print(up.Update.Content.Text) // RAW, unverarbeitet
		}
	}, nil)
	if err != nil {
		log.Fatalf("goose start: %v", err)
	}
	defer c.Close()

	if _, err := c.Request(ctx, "initialize", map[string]any{"protocolVersion": 2, "clientInfo": map[string]any{}}); err != nil {
		log.Fatalf("initialize: %v", err)
	}

	sres, err := c.Request(ctx, "session/new", map[string]any{"cwd": wd, "mcpServers": map[string]any{}})
	if err != nil {
		log.Fatalf("session/new: %v", err)
	}
	var sn struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(sres, &sn); err != nil || sn.SessionID == "" {
		log.Fatalf("session/new parse: %v (%s)", err, sres)
	}

	res, err := c.Request(ctx, "session/prompt", map[string]any{
		"sessionId": sn.SessionID,
		"prompt":    []map[string]any{{"type": "text", "text": *prompt}},
	})
	fmt.Printf("\n[METADATA stopReason] %s\n", string(res))
	if err != nil {
		log.Fatalf("prompt: %v", err)
	}
}
