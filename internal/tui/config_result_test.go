package tui

import (
	"errors"
	"strings"
	"testing"
)

// ---- configResultMsg: Silent-no-op-Erkennung (FR-45-Nachfass) ---------------

// Gegen echtes goose 1.55.0 verifiziert (2026-11-21, Raw-Probe): goose
// akzeptiert session/set_config_option fuer den unkonfigurierten Default-
// Provider "goose" ohne JEDE Fehlermeldung — der Provider bleibt, UND das
// Modell faellt still auf den Session-Default zurueck. Der Chat muss das
// sichtbar machen, statt den Picker-Wunsch optimistisch zu uebernehmen.
//
// Nachrichtenreihenfolge im Regelbetrieb: configSet ruft in der Cmd zuerst
// publishConfigResult (sendet infoMsg mit den FRISCHEN configOptions) und
// liefert danach configResultMsg — das Update sieht also infoMsg zuerst.

// TestProviderNoOpReported: Der verworfene Provider-Wechsel meldet sich als
// Block im Chat; Labels bleiben beim echten Stand (inkl. Modell-Reset).
func TestProviderNoOpReported(t *testing.T) {
	r := newRig(t)
	// Session-Zustand: custom_nous + ein vom Default abweichendes Modell.
	r.do(infoMsg{model: "openai/gpt-4o", provider: "custom_nous",
		providerOptions: []OptionRow{{Value: "custom_nous", Name: "Nous Research"},
			{Value: "goose", Name: "Goose (Default)"}}})
	// goose hat den Wechsel nach "goose" verworfen und dabei still das
	// Modell auf den Session-Default zurueckgesetzt — die Antwort-Optionen
	// spiegeln genau das.
	r.do(infoMsg{model: "~z-ai/glm-flash-latest", provider: "custom_nous"})
	r.do(configResultMsg{kind: "provider", value: "goose",
		prevModel: "openai/gpt-4o", prevProvider: "custom_nous"})

	if r.m.provider != "custom_nous" {
		t.Fatalf("provider label must stay at the truth, got %q", r.m.provider)
	}
	if r.m.modelLabel != "~z-ai/glm-flash-latest" {
		t.Fatalf("model label must reflect the silent reset, got %q", r.m.modelLabel)
	}
	view := stripANSI(r.m.chat.View())
	if !strings.Contains(view, "Provider-Wechsel verworfen") || !strings.Contains(view, "goose bleibt bei") {
		t.Fatalf("silent no-op must be reported in the chat: %q", view)
	}
	if !strings.Contains(view, "zurückgesetzt") {
		t.Fatalf("the silent model reset must be mentioned: %q", view)
	}
}

// TestModelNoOpReported: Verwirft goose auch einen Modell-Wechsel still,
// bleibt das Label beim alten Modell und der Chat meldet den Verwurf.
func TestModelNoOpReported(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{model: "glm-5.3"})
	// goose haelt am alten Modell fest (Antwort-Optionen unveraendert).
	r.do(infoMsg{model: "glm-5.3"})
	r.do(configResultMsg{kind: "model", value: "openai/gpt-4o",
		prevModel: "glm-5.3", prevProvider: "custom_nous"})

	if r.m.modelLabel != "glm-5.3" {
		t.Fatalf("model label must stay at the truth, got %q", r.m.modelLabel)
	}
	view := stripANSI(r.m.chat.View())
	if !strings.Contains(view, "Modell-Wechsel verworfen") || !strings.Contains(view, "glm-5.3") {
		t.Fatalf("silent model no-op must be reported: %q", view)
	}
}

// TestConfigResultSuccessStaysQuiet: Ein sauber uebernommener Wechsel
// meldet nichts — der Chat ist kein Log des Normalfalls.
func TestConfigResultSuccessStaysQuiet(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{model: "~z-ai/glm-flash-latest", provider: "custom_nous"})
	// Provider-Wechsel akzeptiert: neue Optionen tragen zai_coding_plan
	// plus dessen Default-Modell.
	r.do(infoMsg{model: "glm-5.3", provider: "zai_coding_plan"})
	r.do(configResultMsg{kind: "provider", value: "zai_coding_plan",
		prevModel: "~z-ai/glm-flash-latest", prevProvider: "custom_nous"})
	if r.m.provider != "zai_coding_plan" || r.m.modelLabel != "glm-5.3" {
		t.Fatalf("labels must follow the accepted change, got %s/%s",
			r.m.provider, r.m.modelLabel)
	}
	if view := stripANSI(r.m.chat.View()); strings.Contains(view, "Wechsel") {
		t.Fatalf("accepted changes must not chat about themselves: %q", view)
	}

	// Modell-Wechsel akzeptiert — ebenfalls still.
	r.do(infoMsg{model: "openai/gpt-4o", provider: "zai_coding_plan"})
	r.do(configResultMsg{kind: "model", value: "openai/gpt-4o",
		prevModel: "glm-5.3", prevProvider: "zai_coding_plan"})
	if r.m.modelLabel != "openai/gpt-4o" {
		t.Fatalf("model label must follow the accepted change, got %q", r.m.modelLabel)
	}
	if view := stripANSI(r.m.chat.View()); strings.Contains(view, "Wechsel") {
		t.Fatalf("accepted model changes must stay quiet: %q", view)
	}
}

// TestConfigResultDerivedValueReported: Liefert goose einen DRITTEN Wert
// (weder Wunsch noch Altstand — Normalisierung/Ableitung), zeigt der Chat
// sichtbar, was wirklich gesetzt wurde; kein optimistisches Fehllabel.
func TestConfigResultDerivedValueReported(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{model: "glm-5.3", provider: "custom_nous"})
	r.do(infoMsg{model: "z-ai/glm-5.3", provider: "custom_nous"})
	r.do(configResultMsg{kind: "model", value: "~z-ai/glm-flash-latest",
		prevModel: "glm-5.3", prevProvider: "custom_nous"})

	if r.m.modelLabel != "z-ai/glm-5.3" {
		t.Fatalf("label must keep the derived truth, got %q", r.m.modelLabel)
	}
	view := stripANSI(r.m.chat.View())
	if !strings.Contains(view, "übernommen statt") || !strings.Contains(view, "z-ai/glm-5.3") {
		t.Fatalf("derived value must be reported: %q", view)
	}
}

// TestInfoMsgKeepsOptionsOnEmpty: infoMsg mit LEEREN Options-Listen (z. B.
// Schema-Drift in einer set_config_option-Antwort) darf die bestehenden
// Picker-Listen nicht ausloeschen — sonst bricht /model direkt nach einem
// Provider-Wechsel (FR-20/45: beide Befehle nacheinander nutzbar).
func TestInfoMsgKeepsOptionsOnEmpty(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{model: "glm-5.3", provider: "custom_zai",
		modelOptions:    []string{"glm-5.3", "glm-5.3-flash"},
		effortOptions:   []string{"low", "max"},
		providerOptions: []OptionRow{{Value: "custom_zai", Name: "Z.AI (GLM)"}}})
	// Antwort ohne options: nur das Label wandert, die Listen halten.
	r.do(infoMsg{model: "~z-ai/glm-flash-latest", provider: "custom_nous"})
	if len(r.m.modelOptions) != 2 || r.m.modelOptions[0] != "glm-5.3" {
		t.Fatalf("model options were wiped: %v", r.m.modelOptions)
	}
	if len(r.m.providerOptions) != 1 {
		t.Fatalf("provider options were wiped: %v", r.m.providerOptions)
	}
	if r.m.modelLabel != "~z-ai/glm-flash-latest" || r.m.provider != "custom_nous" {
		t.Fatalf("labels must still follow the fresh truth: %q/%q",
			r.m.modelLabel, r.m.provider)
	}
}

// TestInfoMsgReplacesOptionsWhenFilled: Eine GEFUELLTE neue Liste ersetzt die
// alte — nach dem Provider-Wechsel zeigt /model die Modelle des NEUEN
// Providers (der eigentliche Sinn der Auffrischung).
func TestInfoMsgReplacesOptionsWhenFilled(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{model: "glm-5.3",
		modelOptions: []string{"glm-5.3", "glm-5.3-flash"}})
	r.do(infoMsg{model: "~z-ai/glm-flash-latest",
		modelOptions: []string{"~z-ai/glm-flash-latest", "openai/gpt-6.2-astra"}})
	if len(r.m.modelOptions) != 2 || r.m.modelOptions[1] != "openai/gpt-6.2-astra" {
		t.Fatalf("model options must follow the new provider: %v", r.m.modelOptions)
	}
}

// TestConfigResultErrStillErrors: Ein RPC-Fehler bleibt der Fehlerblock —
// die No-op-Erkennung aendert daran nichts.
func TestConfigResultErrStillErrors(t *testing.T) {
	r := newRig(t)
	r.do(configResultMsg{kind: "provider", value: "venice",
		err: errors.New("Failed to switch provider: missing required key VENICE_API_KEY")})
	view := stripANSI(r.m.chat.View())
	if !strings.Contains(view, "Fehler: Failed to switch provider") {
		t.Fatalf("rpc error must surface as error text: %q", view)
	}
}
