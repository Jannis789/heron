package acp

import "testing"

func TestParseSessionInfoReadsModelAndEffort(t *testing.T) {
	// Real shape of goose 1.53's session/new response (trimmed).
	const sres = `{"sessionId":"20261010_5","modes":{"currentModeId":"auto"},` +
		`"configOptions":[` +
		`{"id":"provider","currentValue":"zai_coding_plan"},` +
		`{"id":"mode","currentValue":"auto"},` +
		`{"id":"model","currentValue":"glm-5.3"},` +
		`{"id":"thinking_effort","currentValue":"max"}]}`

	si := parseSessionInfo([]byte(sres))
	if si.sessionID != "20261010_5" {
		t.Fatalf("sessionID = %q, want 20261010_5", si.sessionID)
	}
	if si.model != "glm-5.3" {
		t.Fatalf("model = %q, want glm-5.3", si.model)
	}
	if si.effort != "max" {
		t.Fatalf("effort = %q, want max", si.effort)
	}
}

// TestParseUsageReadsGoosePayload: the token accounting of a finished turn
// arrives in the session/prompt response next to stopReason (FR-23).
func TestParseUsageReadsGoosePayload(t *testing.T) {
	// Real shape of goose 1.53's session/prompt response.
	const res = `{"stopReason":"end_turn","usage":{"totalTokens":4471,"inputTokens":4465,"outputTokens":6}}`
	u := parseUsage([]byte(res))
	if u.InputTokens != 4465 || u.OutputTokens != 6 || u.TotalTokens != 4471 {
		t.Fatalf("usage = %+v", u)
	}
	if u := parseUsage([]byte(`{"stopReason":"end_turn"}`)); u != (Usage{}) {
		t.Fatalf("missing usage must stay zero: %+v", u)
	}
}

func TestParseSessionInfoToleratesMinimalPayload(t *testing.T) {
	si := parseSessionInfo([]byte(`{"sessionId":"abc"}`))
	if si.sessionID != "abc" || si.model != "" || si.effort != "" {
		t.Fatalf("unexpected info: %+v", si)
	}
	si = parseSessionInfo([]byte(`not json`))
	if si.sessionID != "" {
		t.Fatalf("garbage must not yield a session id: %q", si.sessionID)
	}
}

// TestApplyConfigMirrorsSetConfigOption: die set_config_option-Antwort
// traegt dieselben configOptions wie session/new (goose 1.55.0 probed) —
// der Session-Cache muss daraus den NEUEN Stand uebernehmen, sonst
// schreibt publishModelInfo (laeuft bei JEDEM Prompt in main.go) den
// Altstand zurueck: Der Footer fiel nach dem naechsten Prompt aufs alte
// Modell zurueck und der /model-Picker bot nach einem Provider-Wechsel
// die Modellliste des VORHERIGEN Providers an (FR-45).
func TestApplyConfigMirrorsSetConfigOption(t *testing.T) {
	s := &Session{sessionID: "s1", model: "glm-5.3", effort: "max",
		modelOptions: []string{"glm-4.7", "glm-5.3"},
		provider:     "custom_zai",
		providerOptions: []ProviderOption{
			{Value: "custom_zai", Name: "Z.AI (GLM)"},
		}}

	// Echte Form der Antwort: Provider-Wechsel custom_zai -> custom_nous
	// (Modellliste frischt auf, effort faellt auf "off").
	const res = `{"configOptions":[` +
		`{"id":"provider","currentValue":"custom_nous","options":[{"value":"custom_nous","name":"Nous Research"}]},` +
		`{"id":"mode","currentValue":"auto","options":[{"value":"auto","name":"auto"}]},` +
		`{"id":"model","currentValue":"~z-ai/glm-flash-latest","options":[{"value":"~z-ai/glm-flash-latest","name":"GLM Flash Latest"}]},` +
		`{"id":"thinking_effort","currentValue":"off","options":[{"value":"off","name":"off"}]}]}`
	s.applyConfig(parseSessionInfo([]byte(res)))

	if got := s.Model(); got != "~z-ai/glm-flash-latest" {
		t.Fatalf("model cache not refreshed: %q", got)
	}
	if got := s.Provider(); got != "custom_nous" {
		t.Fatalf("provider cache not refreshed: %q", got)
	}
	if got := s.Effort(); got != "off" {
		t.Fatalf("effort cache not refreshed: %q", got)
	}
	if got := s.ModelOptions(); len(got) != 1 || got[0] != "~z-ai/glm-flash-latest" {
		t.Fatalf("model options not refreshed: %v", got)
	}
	if got := s.ProviderOptions(); len(got) != 1 || got[0].Value != "custom_nous" {
		t.Fatalf("provider options not refreshed: %v", got)
	}

	// Schema-Drift: leere Antwortteile duerfen den Stand nicht leeren.
	s.applyConfig(sessionInfo{})
	if s.Model() != "~z-ai/glm-flash-latest" || s.Provider() != "custom_nous" || s.Effort() != "off" {
		t.Fatalf("drift wiped the cache: model=%q provider=%q effort=%q",
			s.Model(), s.Provider(), s.Effort())
	}
	if len(s.ModelOptions()) != 1 || len(s.ProviderOptions()) != 1 {
		t.Fatalf("drift wiped the option lists: %v / %v", s.ModelOptions(), s.ProviderOptions())
	}

	// applyConfig darf die Session-ID nie anfassen — set_config_option
	// antwortet ohne sessionId (wie session/load).
	s.applyConfig(parseSessionInfo([]byte(`{"sessionId":"","configOptions":[{"id":"model","currentValue":"glm-4.7"}]}`)))
	if s.SessionID() != "s1" {
		t.Fatalf("sessionID must never change via applyConfig: %q", s.SessionID())
	}
	if s.Model() != "glm-4.7" {
		t.Fatalf("partial refresh failed: %q", s.Model())
	}
}
