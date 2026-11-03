// cmd/acp-probe spawnt `goose acp` und gibt die unbearbeitete (rohe) Response aus.
// Neben dem Basis-Durchlauf (initialize/session/new/session/prompt) gibt es
// die read-only-Modi für die Session-Übernahme (FR-25/FR-44):
//
//	acp-probe -list           session/list (Sessions im goose-Store, paginiert)
//	acp-probe -load <id>      session/load — der Verlauf replayt als Updates
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"heron/internal/acp"
)

func main() {
	prompt := flag.String("prompt", "Antworte kurz auf Deutsch: sag nur Hallo.", "Prompt an goose")
	gooseBin := flag.String("goose", "goose", "goose binary")
	list := flag.Bool("list", false, "session/list (read-only) statt session/new+prompt")
	load := flag.String("load", "", "session id laden (read-only) statt session/new+prompt")
	flag.Parse()
	wd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ACP v2 -> goose: Feld heisst `sessionUpdate`, `content` ist EIN Objekt {type,text}.
	c, err := acp.New(ctx, *gooseBin, func(method string, params json.RawMessage) {
		if method != "session/update" {
			if method != "" {
				fmt.Printf("[NOTIFY %s] %.300s\n", method, string(params))
			}
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
		switch up.Update.SessionUpdate {
		case "agent_message_chunk":
			fmt.Print(up.Update.Content.Text) // RAW, unverarbeitet
		case "user_message_chunk":
			fmt.Printf("\n[REPLAY user] %s\n", up.Update.Content.Text)
		case "agent_thought_chunk":
			fmt.Printf("\n[REPLAY thought] %.120s…\n", up.Update.Content.Text)
		case "tool_call", "tool_call_update":
			// RAW: Schema-Drift soll hier sichtbar werden — content-Liste,
			// rawInput/rawOutput und _meta live pruefbar (leere Tool-Karten
			// im TUI waren genau ein solcher Drift, 2026-10-31).
			fmt.Printf("\n[TOOL %.60s] %.600s\n", up.Update.SessionUpdate,
				strings.ReplaceAll(string(params), "\n", "\\n"))
		}
	}, nil)
	if err != nil {
		log.Fatalf("goose start: %v", err)
	}
	defer c.Close()

	if ires, err := c.Request(ctx, "initialize", map[string]any{"protocolVersion": 2, "clientInfo": map[string]any{}}); err != nil {
		log.Fatalf("initialize: %v", err)
	} else {
		fmt.Printf("[METADATA initialize] %s\n", string(ires))
	}

	// Read-only-Modi: kein session/new, kein Prompt — goose-Store nur lesen.
	if *list {
		infos, err := acp.ListSessions(ctx, *gooseBin)
		if err != nil {
			log.Fatalf("session/list: %v", err)
		}
		for _, in := range infos {
			fmt.Printf("[session] %s | %s | %s | %s | %d msgs | %s\n",
				in.SessionID, in.UpdatedAt, in.Title, in.Cwd, in.MessageCount, in.ModelID)
		}
		return
	}
	if *load != "" {
		var n int
		sess, err := acp.LoadSession(ctx, *gooseBin, wd, *load, func(it acp.ReplayItem) {
			n++
			switch it.Kind {
			case acp.ReplayUser:
				fmt.Printf("\n[REPLAY user] %s\n", it.Text)
			case acp.ReplayAgent:
				fmt.Printf("\n[REPLAY agent] %.400s\n", it.Text)
			case acp.ReplayTool:
				fmt.Printf("[REPLAY tool] %s %s done=%v\n", it.Tool.CallID, it.Tool.Label(), it.Tool.Done)
			}
		})
		if err != nil {
			log.Fatalf("session/load: %v", err)
		}
		defer sess.Close()
		fmt.Printf("\n[METADATA load] items=%d model=%q effort=%q sessionId=%s\n",
			n, sess.Model(), sess.Effort(), sess.SessionID())
		return
	}

	sres, err := c.Request(ctx, "session/new", map[string]any{"cwd": wd, "mcpServers": map[string]any{}})
	if err != nil {
		log.Fatalf("session/new: %v", err)
	}
	fmt.Printf("[METADATA session/new] %s\n", string(sres))
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
