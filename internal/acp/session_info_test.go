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
