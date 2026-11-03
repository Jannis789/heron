package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestToolEventParse: tool_call/tool_call_update parsen callID, Titel und
// Text-Inhalt — die Karte im Chat identifiziert der Call ueber die ID
// (In-place-Update statt Duplikat). Legacy-Form (Fake): content als
// EIN Objekt, Text direkt.
func TestToolEventParse(t *testing.T) {
	raw := `{"update":{"sessionUpdate":"tool_call","toolCallId":"call-9",
		"title":"bash","content":{"type":"text","text":"go test ./..."}}}`
	var u update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e := buildToolEvent(u)
	if e.CallID != "call-9" || e.Title != "bash" || e.Text != "go test ./..." {
		t.Fatalf("callID/Title/Text nicht geparsed: %+v", e)
	}
	if e.Done || e.IsError {
		t.Fatalf("Start-Event ohne Status ist weder done noch error: %+v", e)
	}
}

// TestToolEventRealGooseFrames: die ECHTEN goose-1.55-Frames (Raw-Probe
// 2026-10-31) — rawInput als Objekt, Tool-Name nur in _meta, finale
// Updates mit doppelt verschachtelter content-LISTE und strukturiertem
// rawOutput. Genau dieser Drift liess die Karten leer: die Liste passte
// nicht ins alte Einzelobjekt-Schema und warf das GANZE Update weg.
func TestToolEventRealGooseFrames(t *testing.T) {
	// Start-Frame: Kommando in rawInput, kein kind, Name nur in _meta.
	start := `{"update":{"sessionUpdate":"tool_call","toolCallId":"c1",
		"title":"shell · ls -la /tmp && cat hello.txt…",
		"rawInput":{"command":"ls -la /tmp && cat hello.txt"},
		"_meta":{"goose":{"messageId":"m1","toolCall":{"extensionName":"developer","toolName":"shell"}}}}}`
	var u update
	if err := json.Unmarshal([]byte(start), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e := buildToolEvent(u)
	if e.Command != "ls -la /tmp && cat hello.txt" {
		t.Fatalf("rawInput.command nicht geparsed: %+v", e)
	}
	if e.Name != "shell" || e.Kind != "execute" {
		t.Fatalf("Tool-Name/Kind-Ableitung aus _meta: got %q/%q", e.Name, e.Kind)
	}
	if e.Label() != "shell" {
		t.Fatalf("Label: mit Kommando im Body reicht der Tool-Name, got %q", e.Label())
	}
	if e.Done {
		t.Fatal("Start-Frame ist nicht done")
	}

	// Finaler Frame: content als Liste (doppelt verschachtelt) + rawOutput.
	fin := `{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1",
		"status":"failed",
		"content":[{"type":"content","content":{"type":"text","text":"total 4\ncat: hello.txt: No such file\n\nCommand exited with code 1"}}],
		"rawOutput":{"exit_code":1,"stdout":"total 4\n","stderr":"cat: hello.txt: No such file"}}}`
	var u2 update
	if err := json.Unmarshal([]byte(fin), &u2); err != nil {
		t.Fatalf("unmarshal des finalen Frames: %v", err)
	}
	e2 := buildToolEvent(u2)
	if !e2.Done || !e2.IsError {
		t.Fatalf("status failed => done+error: %+v", e2)
	}
	if !e2.HasOutput || e2.ExitCode != 1 || e2.Stdout != "total 4\n" ||
		e2.Stderr != "cat: hello.txt: No such file" {
		t.Fatalf("rawOutput nicht geparsed: %+v", e2)
	}
	if !strings.Contains(e2.Text, "Command exited with code 1") {
		t.Fatalf("content-Liste (verschachtelt) nicht entpackt: %q", e2.Text)
	}
}

// TestToolEventLiveChunk: waehrend des Laufs streamt goose die Ausgabe als
// _meta.toolNotification.live_output-Chunks (stdout/stderr gemischt) —
// Live traegt den Chunk DIESES Updates, done bleibt aus.
func TestToolEventLiveChunk(t *testing.T) {
	raw := `{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1",
		"status":"in_progress",
		"_meta":{"toolNotification":{"type":"live_output","params":{
			"chunks":[{"output":"total 4\n","stream":"stdout"},
			          {"output":"cat: x\n","stream":"stderr"}],
			"sequence":2,"truncated":false}}}}}`
	var u update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e := buildToolEvent(u)
	if e.Live != "total 4\ncat: x\n" {
		t.Fatalf("live_output-Chunk nicht konkateniert: %q", e.Live)
	}
	if e.Done || e.IsError {
		t.Fatalf("in_progress ist weder done noch error: %+v", e)
	}
}

// TestToolEventDiffContent: diff-Bloecke in der content-Liste werden als
// DiffFile geparsed (oldText null = neue Datei) — Grundlage FR-32.
func TestToolEventDiffContent(t *testing.T) {
	raw := `{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c2",
		"status":"completed",
		"content":[{"type":"diff","path":"/p/a.go","oldText":"alt","newText":"neu"},
		           {"type":"diff","path":"/p/new.go","newText":"frisch"}]}}`
	var u update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e := buildToolEvent(u)
	if len(e.Diffs) != 2 {
		t.Fatalf("2 diff-Bloecke erwartet: %+v", e.Diffs)
	}
	if e.Diffs[0].Path != "/p/a.go" || e.Diffs[0].OldText == nil || *e.Diffs[0].OldText != "alt" ||
		e.Diffs[0].NewText != "neu" {
		t.Fatalf("diff[0] = %+v", e.Diffs[0])
	}
	if e.Diffs[1].OldText != nil {
		t.Fatalf("ohne oldText ist die Datei neu (nil): %+v", e.Diffs[1])
	}
	if !e.Done {
		t.Fatal("status completed => done")
	}
}

// TestToolEventLabel: Titel > Name > Kind > "tool" — das lesbarste Label
// gewinnt (Karten-Caption). Steht das volle Kommando im Body (Command),
// kuerzt goose den Titel nur ("shell · ls …") — dann reicht der Tool-Name.
func TestToolEventLabel(t *testing.T) {
	for _, tc := range []struct {
		ev   ToolEvent
		want string
	}{
		{ToolEvent{Title: "bash", Name: "shell_exec"}, "bash"},
		{ToolEvent{Name: "shell_exec"}, "shell_exec"},
		{ToolEvent{Kind: "execute"}, "execute"},
		{ToolEvent{}, "tool"},
		{ToolEvent{Command: "ls -la", Name: "shell", Title: "shell · ls -la …"}, "shell"},
	} {
		if got := tc.ev.Label(); got != tc.want {
			t.Fatalf("Label(%+v) = %q, want %q", tc.ev, got, tc.want)
		}
	}
}

// TestToolEventParseKindAndError: kind/status/isError mappen aus dem
// Update-Payload — die Karte im Chat waehlt danach Icon ($/↯/…) und
// Fehler-Zustand (✗). isError im Content ODER status "failed" zaehlt.
func TestToolEventParseKindAndError(t *testing.T) {
	raw := `{"update":{"sessionUpdate":"tool_call_update","toolCallId":"call-9",
		"title":"curl","kind":"fetch","status":"failed",
		"content":{"type":"text","text":"connection refused","isError":true}}}`
	var u update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e := buildToolEvent(u)
	if e.Kind != "fetch" || e.Status != "failed" {
		t.Fatalf("Kind/Status nicht geparsed: %+v", e)
	}
	if !e.IsError || !e.Done {
		t.Fatalf("status failed => error+done: %+v", e)
	}
}

// TestParseConfigProvider: die configOptions einer session/new-Antwort
// tragen den Provider als Select mit value/name-Paaren — ParseConfig (auch
// fuer set_config_option-Antworten) liefert CurrentValue, Label und Liste.
func TestParseConfigProvider(t *testing.T) {
	res := []byte(`{"sessionId":"s1","configOptions":[
		{"id":"provider","type":"select","currentValue":"zai_coding_plan","options":[
			{"value":"goose","name":"Goose (Default)"},
			{"value":"custom_nous","name":"Nous Research"},
			{"value":"raw_id"}]},
		{"id":"model","currentValue":"glm-5.3","options":[{"value":"glm-5.3"},{"value":"glm-4.7"}]}]}`)
	model, effort, provider, models, _, providers := ParseConfig(res)
	if model != "glm-5.3" || effort != "" || provider != "zai_coding_plan" {
		t.Fatalf("model/effort/provider = %q/%q/%q", model, effort, provider)
	}
	if len(models) != 2 || models[0] != "glm-5.3" {
		t.Fatalf("models = %v", models)
	}
	if len(providers) != 3 {
		t.Fatalf("providers = %+v", providers)
	}
	// Name gewinnt, fehlender Name faellt auf den Value zurueck.
	if providers[1].Name != "Nous Research" || providers[1].Value != "custom_nous" {
		t.Fatalf("provider row = %+v", providers[1])
	}
	if providers[2].Name != "raw_id" || providers[2].Value != "raw_id" {
		t.Fatalf("nameless row must fall back to value: %+v", providers[2])
	}
}
