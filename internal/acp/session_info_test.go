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
