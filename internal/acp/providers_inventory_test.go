package acp

import (
	"context"
	"os"
	"testing"
)

// Fixture: gegen echtes goose 1.55.0 probed (2026-11-17, bash-JSON-RPC
// ueber `goose acp`) — auf 3 repraesentative Eintraege gekürzt: ein
// Custom-Provider aus ~/.config/goose/custom_providers mit Modellliste,
// ein ACP-Agent ohne Modelle, ein abgekündigter Builtin.
const inventoryFixture = `{"entries":[
 {"providerId":"custom_zai","providerName":"Z.AI (GLM)","description":"Z.AI Coding-Plan Custom-Provider (Fallback)","defaultModel":"glm-4.7","configured":true,"available":true,"providerType":"Custom","acp":false,"visibleInSetup":true,"deprecated":false,"configKeys":[{"name":"ZAI_API_KEY","required":false,"secret":true,"primary":true}],"setupSteps":[],"supportsRefresh":true,"refreshing":false,"models":[{"id":"glm-4.7","name":"GLM-4.7","family":"glm","contextLimit":204800,"reasoning":true,"recommended":false},{"id":"glm-5.3","name":"GLM-5.3","family":"glm","contextLimit":1000000,"reasoning":true,"recommended":true}]},
 {"providerId":"claude-code","providerName":"Claude Code","description":"ACP adapter","defaultModel":"","configured":false,"available":false,"providerType":"Preferred","acp":true,"deprecated":false,"models":[]},
 {"providerId":"old_thing","providerName":"Old","defaultModel":"m-1","configured":false,"available":false,"providerType":"Builtin","acp":false,"deprecated":true,"models":[]}
]}`

func TestParseProviderInventory(t *testing.T) {
	entries := parseProviderInventory([]byte(inventoryFixture))
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	zai := entries[0]
	if zai.ProviderID != "custom_zai" || zai.Name != "Z.AI (GLM)" || !zai.Configured || !zai.Available {
		t.Fatalf("custom_zai header wrong: %+v", zai)
	}
	if zai.ACP || zai.Deprecated {
		t.Fatalf("custom_zai flags wrong: %+v", zai)
	}
	if len(zai.Models) != 2 {
		t.Fatalf("want 2 models, got %d", len(zai.Models))
	}
	m := zai.Models[1]
	if m.ID != "glm-5.3" || m.ContextLimit != 1000000 || !m.Reasoning || !m.Recommended {
		t.Fatalf("glm-5.3 wrong: %+v", m)
	}
	acpAgent := entries[1]
	if !acpAgent.ACP || acpAgent.Configured || len(acpAgent.Models) != 0 {
		t.Fatalf("claude-code wrong: %+v", acpAgent)
	}
	if !entries[2].Deprecated {
		t.Fatalf("old_thing must be deprecated")
	}
}

// Schema-Drift bleibt leer statt zu brechen — wie parseSessionInfo.
func TestParseProviderInventoryDrift(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":    `{"entries":[]}`,
		"noField":  `{}`,
		"noID":     `{"entries":[{"providerName":"x","models":[]}]}`,
		"garbage":  `not json`,
		"typesOff": `{"entries":"nope"}`,
	} {
		if got := parseProviderInventory([]byte(raw)); got != nil {
			t.Errorf("%s: want nil, got %+v", name, got)
		}
	}
}

// TestProviderInventoryRealGoose kitzelt das Inventar aus echtem goose:
// ohne Inventar und ohne Eintrag mit Modellen ist der Modell-Fallback
// eine Behauptung, kein Feature.
func TestProviderInventoryRealGoose(t *testing.T) {
	if os.Getenv("HERON_E2E_GOOSE") != "1" {
		t.Skip("set HERON_E2E_GOOSE=1 for a direct goose test")
	}
	wd, _ := os.Getwd()
	sess, err := NewSession(context.Background(), "goose", wd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()
	inv, err := sess.ProviderInventory(context.Background())
	if err != nil {
		t.Fatalf("ProviderInventory: %v", err)
	}
	if len(inv) == 0 {
		t.Fatal("goose reported an empty provider inventory")
	}
	withModels := 0
	configured := 0
	for _, e := range inv {
		if len(e.Models) > 0 {
			withModels++
		}
		if e.Configured {
			configured++
		}
	}
	if withModels == 0 || configured == 0 {
		t.Fatalf("inventory lacks usable entries: %d providers, %d with models, %d configured",
			len(inv), withModels, configured)
	}
	t.Logf("inventory: %d providers, %d with models, %d configured", len(inv), withModels, configured)
}
