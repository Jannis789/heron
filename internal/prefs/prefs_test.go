package prefs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLastRoundtrip: SetLast landet auf der Platte und überlebt ein
// erneutes Open (der eigentliche Zweck: heron-Neustart startet neue
// Sessions auf dem letzten Modell/Effort).
func TestLastRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := OpenPath(path)
	s.SetLast("glm-5.3", "high")

	again := OpenPath(path)
	model, effort := again.Last()
	if model != "glm-5.3" || effort != "high" {
		t.Fatalf("Last() = %q/%q, want glm-5.3/high", model, effort)
	}
}

// TestSetLastKeepsEmptyParts: leere Felder lassen den bisherigen Wert
// unangetastet — ein Provider-Wechsel, der nur das Modell meldet, darf
// den gemerkten Effort nicht leeren.
func TestSetLastKeepsEmptyParts(t *testing.T) {
	s := OpenPath(filepath.Join(t.TempDir(), "state.json"))
	s.SetLast("glm-5.3", "high")
	s.SetLast("gpt-5.2", "")
	model, effort := s.Last()
	if model != "gpt-5.2" || effort != "high" {
		t.Fatalf("Last() = %q/%q, want gpt-5.2/high", model, effort)
	}
}

// TestSessionRoundtrip: der je Session-ID gemerkte Stand überlebt den
// Neustart — /resume holt genau ihn nach.
func TestSessionRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	OpenPath(path).SetSession("sess-1", "glm-5.3", "low")

	model, effort := OpenPath(path).Session("sess-1")
	if model != "glm-5.3" || effort != "low" {
		t.Fatalf("Session(sess-1) = %q/%q, want glm-5.3/low", model, effort)
	}
	if model, effort := OpenPath(path).Session("unbekannt"); model != "" || effort != "" {
		t.Fatalf("Session(unbekannt) = %q/%q, want leer", model, effort)
	}
}

// TestSessionLRUPrune: über maxSessions hinaus werden die ÄLTESTEN
// Einträge verworfen — die Datei wächst nicht mit jeder /new-Session.
func TestSessionLRUPrune(t *testing.T) {
	s := OpenPath(filepath.Join(t.TempDir(), "state.json"))
	for i := 0; i <= maxSessions; i++ {
		s.SetSession(idOf(i), "m", "e")
	}
	if len(s.f.Order) != maxSessions {
		t.Fatalf("Order hat %d Einträge, want %d", len(s.f.Order), maxSessions)
	}
	if _, ok := s.f.Sessions[idOf(0)]; ok {
		t.Fatal("ältester Eintrag (id 0) wurde nicht geprunt")
	}
	if _, ok := s.f.Sessions[idOf(maxSessions)]; !ok {
		t.Fatal("neuester Eintrag fehlt")
	}
	// Erneutes Setzen einer vorhandenen ID rückt sie ans Ende und rettet
	// sie über das nächste Prünen hinweg (echte LRU, nicht FIFO).
	s.SetSession(idOf(1), "m", "e")
	s.SetSession("neu", "m", "e")
	if _, ok := s.f.Sessions[idOf(1)]; !ok {
		t.Fatal("kürzlich genutzte Session wurde fälschlich geprunt")
	}
}

// TestCorruptFileStartsFresh: eine kaputte JSON ergibt einen leeren Store
// statt eines Fehlers — Persistenz darf heron nie blockieren.
func TestCorruptFileStartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := OpenPath(path)
	if model, effort := s.Last(); model != "" || effort != "" {
		t.Fatalf("Last() = %q/%q, want leer", model, effort)
	}
	// Und der Store bleibt schreibbar:
	s.SetLast("m", "e")
	if model, _ := OpenPath(path).Last(); model != "m" {
		t.Fatalf("nach Reparatur Last() = %q, want m", model)
	}
}

// TestMissingDirIsCreated: save legt fehlende Verzeichnisse selbst an.
func TestMissingDirIsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tief", "verschachtelt", "state.json")
	OpenPath(path).SetLast("m", "e")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state.json nicht angelegt: %v", err)
	}
}

// idOf macht reproduzierbare Session-IDs (Reihenfolge = Alter).
func idOf(i int) string {
	return "sess-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
}
