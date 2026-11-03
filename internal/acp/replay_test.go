package acp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestParseSessionListSortsNewestFirst (FR-44): goose's session/list page —
// real-shaped JSON (acp-probe -list) — must map to SessionInfos sorted
// newest first and carry the opaque cursor through.
func TestParseSessionListSortsNewestFirst(t *testing.T) {
	res := []byte(`{"sessions":[
		{"sessionId":"20261031_34","cwd":"/home/j/w/media-svr","title":"Test message",
		 "updatedAt":"2026-10-31T17:08:39+00:00",
		 "_meta":{"messageCount":2,"createdAt":"2026-10-31T17:08:26Z","providerId":"custom_nous","modelId":"glm-flash"}},
		{"sessionId":"20261031_36","cwd":"/home/j/w/heron","title":"Heron /resume",
		 "updatedAt":"2026-10-31T17:21:06+00:00",
		 "_meta":{"messageCount":26,"providerId":"custom_nous","modelId":"~z-ai/glm-flash-latest"}},
		{"sessionId":"20261031_27","cwd":"/home/j/w/nixos","title":"Fehler",
		 "updatedAt":"2026-10-31T20:00:00+02:00",
		 "_meta":{"messageCount":19}}
	],"nextCursor":"eyJzb3J0X2F0IjoiLi4uIn0"}`)
	infos, next := parseSessionList(res)
	if next != "eyJzb3J0X2F0IjoiLi4uIn0" {
		t.Fatalf("nextCursor = %q", next)
	}
	if len(infos) != 3 {
		t.Fatalf("n sessions = %d, want 3", len(infos))
	}
	// Newest first — 20:00+02:00 IST 18:00Z und damit die neueste:
	// gemischte Zeitzonen (Z vs +02:00) zählen real.
	if infos[0].SessionID != "20261031_27" || infos[1].SessionID != "20261031_36" || infos[2].SessionID != "20261031_34" {
		t.Fatalf("order wrong: %v", []string{infos[0].SessionID, infos[1].SessionID, infos[2].SessionID})
	}
	first := infos[0]
	if first.Cwd != "/home/j/w/nixos" || first.Title != "Fehler" || first.MessageCount != 19 {
		t.Fatalf("fields not mapped: %+v", first)
	}
	if infos[1].ModelID != "~z-ai/glm-flash-latest" || infos[1].ProviderID != "custom_nous" {
		t.Fatalf("_meta not mapped: %+v", infos[1])
	}
}

// TestParseSessionListGarbage: schema drift must yield an empty page, not a
// crash — the picker shows "keine Sessions" instead of failing hard.
func TestParseSessionListGarbage(t *testing.T) {
	infos, next := parseSessionList([]byte(`{"error":"nope"`))
	if len(infos) != 0 || next != "" {
		t.Fatalf("garbage parsed to %+v next=%q", infos, next)
	}
}

// updateMsg builds a session/update notification payload like goose sends it.
func updateMsg(name, text string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": name,
			"content":       map[string]any{"type": "text", "text": text},
		},
	})
	return b
}

// TestReplayAccumulatorMergesChunks (FR-44): consecutive message chunks of
// one kind merge into ONE item (one goose message = one heron block); a
// kind change or tool event flushes; foreign methods are ignored.
func TestReplayAccumulatorMergesChunks(t *testing.T) {
	var items []ReplayItem
	acc, flush := replayAccumulator(func(it ReplayItem) { items = append(items, it) })

	acc("other/method", []byte(`{}`)) // ignorieren
	acc("session/update", updateMsg("user_message_chunk", "frage "))
	acc("session/update", updateMsg("user_message_chunk", "eins"))
	acc("session/update", updateMsg("agent_message_chunk", "antwort"))
	acc("session/update", updateMsg("agent_message_chunk", " zwei"))
	toolCall, _ := json.Marshal(map[string]any{"update": map[string]any{
		"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "ls", "kind": "execute", "status": "completed",
	}})
	acc("session/update", toolCall)
	acc("session/update", updateMsg("agent_message_chunk", " nach dem tool"))
	flush() // das Load-Ende flush'd den letzten pending Text

	if len(items) != 4 {
		t.Fatalf("n items = %d, want 4: %+v", len(items), items)
	}
	if items[0].Kind != ReplayUser || items[0].Text != "frage eins" {
		t.Fatalf("item0 = %+v", items[0])
	}
	if items[1].Kind != ReplayAgent || items[1].Text != "antwort zwei" {
		t.Fatalf("item1 = %+v", items[1])
	}
	if items[2].Kind != ReplayTool || items[2].Tool.CallID != "t1" || items[2].Tool.Title != "ls" || !items[2].Tool.Done {
		t.Fatalf("item2 = %+v", items[2])
	}
	if items[3].Kind != ReplayAgent || items[3].Text != " nach dem tool" {
		t.Fatalf("item3 = %+v", items[3])
	}
}

// TestReplayAccumulatorFlushesAtEnd: text pending when the load settles must
// flush on the trailing flush call, not vanish — and a second flush must not
// duplicate it.
func TestReplayAccumulatorFlushesAtEnd(t *testing.T) {
	var items []ReplayItem
	acc, flush := replayAccumulator(func(it ReplayItem) { items = append(items, it) })
	acc("session/update", updateMsg("agent_message_chunk", "offen"))
	flush()
	flush() // idempotent
	if len(items) != 1 || items[0].Text != "offen" {
		t.Fatalf("items = %+v", items)
	}
}

// TestSessionLoadRealGoose (FR-44, e2e): goose must load a session — the
// history replays as ReplayItems and the loaded session is live. The test
// builds its OWN throwaway session first (one tiny prompt): session/load
// rewrites the stored cwd, so foreign sessions must not be touched. Load
// itself is read-only: no prompt, no LLM call.
func TestSessionLoadRealGoose(t *testing.T) {
	if os.Getenv("HERON_E2E_GOOSE") != "1" {
		t.Skip("set HERON_E2E_GOOSE=1 for a direct goose test")
	}
	wd, _ := os.Getwd()
	mk, err := NewSession(context.Background(), "goose", wd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := mk.Prompt(context.Background(), "Antworte mit genau einem Wort: marker44."); err != nil {
		mk.Close()
		t.Fatalf("Prompt: %v", err)
	}
	id := mk.SessionID()
	mk.Close()

	var items []ReplayItem
	sess, err := LoadSession(context.Background(), "goose", wd, id,
		func(it ReplayItem) { items = append(items, it) })
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	defer sess.Close()
	if sess.SessionID() != id {
		t.Fatalf("session id = %q, want %q", sess.SessionID(), id)
	}
	if len(items) < 2 || items[0].Kind != ReplayUser {
		t.Fatalf("replay items = %+v, want user message first", items)
	}
	found := false
	for _, it := range items {
		if it.Kind == ReplayAgent && strings.Contains(it.Text, "marker44") {
			found = true
		}
	}
	if !found {
		t.Fatalf("agent replay missing the answer: %+v", items)
	}
	t.Logf("replay: %d items, model=%q effort=%q", len(items), sess.Model(), sess.Effort())
}

// TestListSessionsEphemeralRealGoose (FR-44, e2e): the package-level listing
// must work without any live session (short-lived subprocess).
func TestListSessionsEphemeralRealGoose(t *testing.T) {
	if os.Getenv("HERON_E2E_GOOSE") != "1" {
		t.Skip("set HERON_E2E_GOOSE=1 for a direct goose test")
	}
	infos, err := ListSessions(context.Background(), "goose")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	for _, in := range infos {
		if in.SessionID == "" || in.Cwd == "" {
			t.Fatalf("session without id/cwd: %+v", in)
		}
		if !strings.Contains(in.UpdatedAt, "T") {
			t.Fatalf("updatedAt not RFC3339: %q", in.UpdatedAt)
		}
	}
}

// TestReplayToolErrorFlag: ein tool_call_update mit isError bleibt im
// Replay-Item als IsError haengen (die Karte im Chat zeigt ✗ statt ✓).
func TestReplayToolErrorFlag(t *testing.T) {
	var items []ReplayItem
	acc, flush := replayAccumulator(func(it ReplayItem) { items = append(items, it) })
	fail, _ := json.Marshal(map[string]any{"update": map[string]any{
		"sessionUpdate": "tool_call_update", "toolCallId": "t9", "title": "curl", "kind": "fetch",
		"status":  "failed",
		"content": map[string]any{"type": "text", "text": "refused", "isError": true},
	}})
	acc("session/update", fail)
	flush()
	if len(items) != 1 || !items[0].Tool.IsError || !items[0].Tool.Done || items[0].Tool.Kind != "fetch" {
		t.Fatalf("replay item = %+v", items)
	}
}
