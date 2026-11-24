package main

// Backend-Tests für die Modell/Effort-Persistenz (User-Wunsch 2026-11-23):
// neue Sessions starten immer auf dem letzten Stand — global gespeichert
// für frische Sessions und /new, je Session-ID für /resume. Alle Tests
// laufen gegen den Fake-goose aus fakegoose_test.go, ganz ohne Installation.

import (
	"os"
	"testing"

	"heron/internal/prefs"
	"heron/internal/tui"
)

// TestNewSessionAppliesGlobalPrefs: eine frische Session (Prewarm-Pfad,
// hier ensureSession) startet auf goose-Defaults — applyPrefs muss den
// global gespeicherten letzten Stand Modell UND Effort nachsetzen.
func TestNewSessionAppliesGlobalPrefs(t *testing.T) {
	b, path := fakeBackend(t, nil)
	prefs.OpenPath(path).SetLast("glm-5.3", "high")
	b.prefs = prefs.OpenPath(path)

	s, err := b.ensureSession()
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	defer b.session().Close()
	if got := s.Model(); got != "glm-5.3" {
		t.Fatalf("Model() = %q, want glm-5.3 (letzter Stand nicht angewendet)", got)
	}
	if got := s.Effort(); got != "high" {
		t.Fatalf("Effort() = %q, want high", got)
	}
}

// TestConfigSetPersistsAcrossRestart: der Picker-Pfad (configSet) schreibt
// global und je Session-ID; ein "heron-Neustart" (frischer Backend, selber
// state.json-Pfad) startet die neue Session auf genau dieser Wahl.
func TestConfigSetPersistsAcrossRestart(t *testing.T) {
	b, path := fakeBackend(t, nil)
	s, err := b.ensureSession()
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if err := b.configSet("model", "glm-5.3"); err != nil {
		t.Fatalf("configSet: %v", err)
	}
	if err := b.configSet("thinking_effort", "low"); err != nil {
		t.Fatalf("configSet: %v", err)
	}
	model, effort := prefs.OpenPath(path).Last()
	if model != "glm-5.3" || effort != "low" {
		t.Fatalf("Last() = %q/%q, want glm-5.3/low", model, effort)
	}
	if model, effort = prefs.OpenPath(path).Session(s.SessionID()); model != "glm-5.3" || effort != "low" {
		t.Fatalf("Session(%s) = %q/%q, want glm-5.3/low", s.SessionID(), model, effort)
	}
	b.session().Close()

	// "Neustart": neuer Backend-Prozess, dieselbe state.json.
	exe, _ := os.Executable()
	b2 := newBackend(exe, b.wd, &tui.Stream{}, nil)
	b2.prefs = prefs.OpenPath(path)
	s2, err := b2.ensureSession()
	if err != nil {
		t.Fatalf("ensureSession nach Neustart: %v", err)
	}
	defer b2.session().Close()
	if got := s2.Model(); got != "glm-5.3" {
		t.Fatalf("Model() nach Neustart = %q, want glm-5.3", got)
	}
	if got := s2.Effort(); got != "low" {
		t.Fatalf("Effort() nach Neustart = %q, want low", got)
	}
}

// TestResetSessionKeepsChoice: /new (resetSession) startet eine frische
// goose-Session, übernimmt aber Modell/Effort vom gespeicherten letzten
// Stand — die Wahl der vorherigen Session überlebt den Schnitt.
func TestResetSessionKeepsChoice(t *testing.T) {
	b, _ := fakeBackend(t, nil)
	if _, err := b.ensureSession(); err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if err := b.configSet("model", "glm-5.3"); err != nil {
		t.Fatalf("configSet: %v", err)
	}
	if err := b.resetSession(); err != nil {
		t.Fatalf("resetSession: %v", err)
	}
	defer b.session().Close()
	if got := b.session().Model(); got != "glm-5.3" {
		t.Fatalf("Model() nach /new = %q, want glm-5.3", got)
	}
}

// TestPinnedFlagsSuppressApply: explizite -model/-effort-Flags (pinned)
// schalten das automatische Nachsetzen AUS — der Nutzer hat entschieden,
// heron greift nicht ein. Der gespeicherte Stand bleibt unangetastet.
func TestPinnedFlagsSuppressApply(t *testing.T) {
	b, path := fakeBackend(t, map[string]bool{"model": true, "effort": true})
	prefs.OpenPath(path).SetLast("glm-5.3", "high")
	b.prefs = prefs.OpenPath(path)

	s, err := b.ensureSession()
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	defer b.session().Close()
	if got := s.Model(); got != "default-model" {
		t.Fatalf("Model() = %q, want default-model (pinned: kein Auto-Apply)", got)
	}
	if got := s.Effort(); got != "medium" {
		t.Fatalf("Effort() = %q, want medium (pinned: kein Auto-Apply)", got)
	}
	if model, effort := b.prefs.Last(); model != "glm-5.3" || effort != "high" {
		t.Fatalf("prefs wurden trotz pinned verändert: %q/%q", model, effort)
	}
}

// TestResumeAppliesSessionPrefs: /resume meldet den restaurierten Altstand
// (der Fake: default-model) — applySessionPrefs setzt den FÜR DIESE SESSION
// gemerkten Stand nach, nicht den globalen (der könnte inzwischen woanders
// sein). Ohne gemerkten Eintrag greift der goose-Stand unverändert.
func TestResumeAppliesSessionPrefs(t *testing.T) {
	b, path := fakeBackend(t, nil)
	// Global inzwischen etwas anderes als die alte Session:
	prefs.OpenPath(path).SetLast("default-model", "medium")
	b.prefs = prefs.OpenPath(path)
	b.prefs.SetSession("alt-77", "glm-5.3", "high")

	if err := b.startResume("alt-77"); err != nil {
		t.Fatalf("startResume: %v", err)
	}
	defer b.session().Close()
	if got := b.session().Model(); got != "glm-5.3" {
		t.Fatalf("Model() nach resume = %q, want glm-5.3 (Session-Stand)", got)
	}
	if got := b.session().Effort(); got != "high" {
		t.Fatalf("Effort() nach resume = %q, want high", got)
	}

	// Unbekannte Session-ID: kein Nachsetzen, kein Überschreiben.
	b2, _ := fakeBackend(t, nil)
	if err := b2.startResume("unbekannt"); err != nil {
		t.Fatalf("startResume: %v", err)
	}
	defer b2.session().Close()
	if got := b2.session().Model(); got != "default-model" {
		t.Fatalf("Model() = %q, want default-model (kein Eingriff)", got)
	}
}

// TestRefusedSwitchPersistsActual: der FR-45-Fall — goose bestätigt den
// Wechsel, behält aber still den alten Stand. persistConfig speichert den
// AKTUELLEN Stand: der abgelehnte Wunsch wird nicht zum "letzten Modell"
// verklärt, mit dem dann jede künftige Session erfolglos anstößt.
func TestRefusedSwitchPersistsActual(t *testing.T) {
	b, path := fakeBackend(t, nil)
	t.Setenv("HERON_FAKE_REFUSE", "1") // Fake nimmt set_config_option nicht an
	if _, err := b.ensureSession(); err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if err := b.configSet("model", "glm-5.3"); err != nil {
		t.Fatalf("configSet: %v", err)
	}
	if got := b.session().Model(); got != "default-model" {
		t.Fatalf("Model() = %q, want default-model (verweigert)", got)
	}
	if model, _ := prefs.OpenPath(path).Last(); model != "default-model" {
		t.Fatalf("Last() = %q, want default-model (tatsächlicher Stand)", model)
	}
	b.session().Close()
}
