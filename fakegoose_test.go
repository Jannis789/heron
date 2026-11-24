package main

// Fake-goose für Backend-Tests: das Test-Binary re-exekutiert sich selbst
// (os.Executable) mit HERON_FAKE_GOOSE=1 und spricht dann newline-delimited
// JSON-RPC auf stdio — dasselbe Framing wie internal/acp.Client. So sind
// die Persistenz-Flows (applyPrefs/configSet/resetSession/startResume)
// deterministisch testbar, ganz ohne goose-Installation; gegen echtes
// goose läuft derselbe Fluss als Opt-in (prefs_e2e_test.go).
//
// Der Fake ist bewusst minimal, aber ehrlich in den Stellen, auf die es
// ankommt: session/new kündigt default-model/medium an,
// session/set_config_option spiegelt den übernommenen Stand in den
// configOptions zurück (oder verweigert still — HERON_FAKE_REFUSE, der
// FR-45-Fall), session/load meldet den "restaurierten" Altstand.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"heron/internal/prefs"
	"heron/internal/tui"
)

// TestMain fängt den Fake-Modus ab, BEVOR irgendein Test läuft: mit
// HERON_FAKE_GOOSE=1 ist dieses Binary der ACP-Agent, nicht der Testlauf.
func TestMain(m *testing.M) {
	if os.Getenv("HERON_FAKE_GOOSE") == "1" {
		if err := fakeGooseMain(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeGooseMain ist der Einstieg des Fake-Agents. Es endet mit EOF auf
// stdin (der Client schließt die Pipe) — kein Fehler, sondern das Ende.
func fakeGooseMain() error {
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	enc := json.NewEncoder(os.Stdout)
	refuse := os.Getenv("HERON_FAKE_REFUSE") == "1"
	var counter int64
	// state: was der Fake aktuell "läuft"; ein neuer subprocess startet
	// immer auf default-model/medium — genau wie goose ohne heron.
	state := struct{ model, effort string }{"default-model", "medium"}
	for {
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&msg); err != nil {
			return nil // stdin zu → sauberes Ende
		}
		if msg.ID == nil {
			continue // Notifications brauchen keine Antwort
		}
		var result any = map[string]any{}
		switch msg.Method {
		case "session/new":
			state.model, state.effort = "default-model", "medium"
			result = map[string]any{
				"sessionId":     "fake-s-" + strconv.FormatInt(atomic.AddInt64(&counter, 1), 10),
				"configOptions": fakeConfigOptions(state.model, state.effort),
			}
		case "session/load":
			// Der Fake "restauriert" immer den Altstand default-model —
			// applySessionPrefs muss den gemerkten Stand dagegen nachsetzen.
			state.model, state.effort = "default-model", "medium"
			result = map[string]any{"configOptions": fakeConfigOptions(state.model, state.effort)}
		case "session/set_config_option":
			var p struct {
				ConfigId string `json:"configId"`
				Value    string `json:"value"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if !refuse {
				switch p.ConfigId {
				case "model":
					state.model = p.Value
				case "thinking_effort":
					state.effort = p.Value
				}
			}
			result = map[string]any{"configOptions": fakeConfigOptions(state.model, state.effort)}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	}
}

// fakeConfigOptions baut den ACP-configOptions-Block im goose-1.55-Layout
// (id/currentValue/options) — parseSessionInfo liest genau diese Form.
func fakeConfigOptions(model, effort string) []map[string]any {
	return []map[string]any{
		{"id": "model", "currentValue": model, "options": []map[string]any{
			{"value": "default-model", "name": "Default Model"},
			{"value": "glm-5.3", "name": "GLM 5.3"},
		}},
		{"id": "thinking_effort", "currentValue": effort, "options": []map[string]any{
			{"value": "low"}, {"value": "medium"}, {"value": "high"},
		}},
	}
}

// fakeBackend baut einen Backend gegen den Fake-goose und biegt die
// state.json ins Tempdir — newBackend öffnet sonst die echte Nutzer-
// state.json, und Tests dürfen sie nie anfassen. Der zurückgegebene Pfad
// ermöglicht den "heron-Neustart" im selben Test.
func fakeBackend(t *testing.T, pinned map[string]bool) (*backend, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	t.Setenv("HERON_FAKE_GOOSE", "1") // der Fake erbt ihn beim Spawn
	b := newBackend(exe, t.TempDir(), &tui.Stream{}, pinned)
	path := filepath.Join(t.TempDir(), "state.json")
	b.prefs = prefs.OpenPath(path)
	return b, path
}
