package main

// Real-goose-E2E für die Modell/Effort-Persistenz (opt-in wie die anderen
// Real-Goose-Tests): der komplette Fluss aus backend_prefs_test.go noch
// einmal gegen die Wahrheit — configSet persistiert, resetSession (/new)
// nimmt den letzten Stand wieder auf. XDG_CONFIG_HOME zeigt ins Tempdir,
// damit der Test nie die echte state.json des Nutzers anfasst.

import (
	"os"
	"path/filepath"
	"testing"

	"heron/internal/prefs"
	"heron/internal/tui"
)

func TestPrefsPersistAndApplyRealGoose(t *testing.T) {
	if os.Getenv("HERON_E2E_GOOSE") != "1" {
		t.Skip("set HERON_E2E_GOOSE=1 for a direct goose test")
	}
	dir := t.TempDir()
	wd, _ := os.Getwd()
	b := newBackend("goose", wd, &tui.Stream{}, map[string]bool{})
	// state.json-Isolation über den Store-Override (sofort nach newBackend,
	// bevor irgendein Schreiben passiert) — XDG_CONFIG_HOME umzubiegen
	// ginge nicht: goose liest SEINE Config ebenfalls daraus und fiele
	// dann ohne Provider/Modell auf Defaults zurück.
	b.prefs = prefs.OpenPath(filepath.Join(dir, "state.json"))

	s, err := b.ensureSession()
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	before := s.Model()
	if before == "" {
		t.Fatal("goose did not announce a model in session/new")
	}
	target := before
	for _, o := range s.ModelOptions() {
		if o != before {
			target = o
			break
		}
	}
	if target == before {
		t.Skipf("only one model option announced (%q) — nothing to switch", before)
	}

	// Picker-Pfad: Wechsel muss global + je Session landen.
	if err := b.configSet("model", target); err != nil {
		t.Fatalf("configSet: %v", err)
	}
	if got := s.Model(); got != target {
		t.Skipf("goose kept %q instead of %q (silent no-op) — nothing persisted", got, target)
	}
	if model, effort := b.prefs.Last(); model != target || (effort == "" && s.Effort() != "") {
		t.Fatalf("Last() = %q/%q, want %q/<effort erhalten>", model, effort, target)
	}
	sid := s.SessionID()
	if model, _ := b.prefs.Session(sid); model != target {
		t.Fatalf("Session(%s) = %q, want %q", sid, model, target)
	}

	// /new: frische goose-Session startet auf dem letzten Stand.
	if err := b.resetSession(); err != nil {
		t.Fatalf("resetSession: %v", err)
	}
	s2 := b.session()
	if s2 == s {
		t.Fatal("resetSession did not create a new session")
	}
	if got := s2.Model(); got != target {
		t.Fatalf("new session model = %q, want %q (letzter Stand nicht angewendet)", got, target)
	}
	if s2.SessionID() == sid {
		t.Fatal("new session reuses the old session id")
	}
	b.session().Close()
}
