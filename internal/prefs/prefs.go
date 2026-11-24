// Package prefs persistiert herons "zuletzt genutzt"-Stand von Modell und
// Thinking-Effort — GLOBAL (neue Sessions starten nie auf goose-Defaults,
// wenn heron schon eine Wahl kennt) und JE GOOSE-SESSION-ID (ein /resume
// holt den Stand der damaligen Session nach, selbst wenn goose ihn in den
// configOptions nicht zuverlässig restauriert). Ablageort:
// os.UserConfigDir()/heron/state.json.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// maxSessions begrenzt den Per-Session-Speicher (LRU): goose-Session-IDs
// sind UUID-artig, ungefiltert wüchse die Datei mit jeder /new-Session.
const maxSessions = 32

// Session ist der je goose-Session-ID gemerkte Stand; leere Felder heißen
// "unangetastet lassen" (dieselbe Toleranz wie acp.applyConfig).
type Session struct {
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// file ist das JSON-Format auf der Platte. Order hält die LRU-Reihenfolge
// der Session-IDs — Map-Iteration ist in Go zufällig, das Prünen braucht
// aber einen definiertesten "ältesten" Kandidaten.
type file struct {
	Model    string             `json:"model,omitempty"`
	Effort   string             `json:"effort,omitempty"`
	Sessions map[string]Session `json:"sessions,omitempty"`
	Order    []string           `json:"order,omitempty"`
}

// Store hält den persistierten Stand im Prozess. Alle Methoden sind
// goroutine-safe: configSet läuft in tea.Cmd-Goroutinen, Session-Start im
// Prewarm-/Prompt-Goroutine — ohne Mutex wäre das ein Data Race (NFR-2).
type Store struct {
	mu   sync.Mutex
	path string
	f    file
}

// Path liefert den Ablageort der state.json (Test-Hook: OpenPath).
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "heron", "state.json"), nil
}

// Open lädt den Standard-Store. Fehlendes UserConfigDir: ein Store ohne
// Pfad — Merken läuft dann nur im Speicher, Speichern bleibt stumm.
func Open() *Store {
	p, err := Path()
	if err != nil {
		return &Store{}
	}
	return OpenPath(p)
}

// OpenPath lädt den Store von einem expliziten Pfad (Tests). Eine fehlende
// oder kaputte Datei ergibt einen frischen Store — Persistenz darf den
// Start von heron niemals blockieren.
func OpenPath(path string) *Store {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		return s // nicht vorhanden: frisch beginnen
	}
	_ = json.Unmarshal(b, &s.f) // korrupt: frisch beginnen
	if s.f.Sessions == nil {
		s.f.Sessions = map[string]Session{}
	}
	return s
}

// Last meldet den global zuletzt genutzten Stand ("" = unbekannt).
func (s *Store) Last() (model, effort string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Model, s.f.Effort
}

// Session meldet den für eine goose-Session-ID gemerkten Stand; eine
// unbekannte ID liefert leer.
func (s *Store) Session(id string) (model, effort string) {
	if id == "" {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.f.Sessions[id]
	if !ok {
		return "", ""
	}
	return e.Model, e.Effort
}

// SetLast merkt den globalen letzten Stand; leere Felder lassen den
// bisherigen Wert unangetastet (Schema-Drift darf nichts leeren).
func (s *Store) SetLast(model, effort string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if model != "" {
		s.f.Model = model
	}
	if effort != "" {
		s.f.Effort = effort
	}
	s.save()
}

// SetSession merkt den Stand einer goose-Session-ID und rückt sie ans Ende
// der LRU-Ordnung; über maxSessions hinaus wird die älteste verworfen.
func (s *Store) SetSession(id, model, effort string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f.Sessions == nil {
		s.f.Sessions = map[string]Session{}
	}
	e := s.f.Sessions[id]
	if model != "" {
		e.Model = model
	}
	if effort != "" {
		e.Effort = effort
	}
	s.f.Sessions[id] = e
	// LRU: ID aus der Ordnung streichen und hinten anhängen.
	for i, x := range s.f.Order {
		if x == id {
			s.f.Order = append(s.f.Order[:i], s.f.Order[i+1:]...)
			break
		}
	}
	s.f.Order = append(s.f.Order, id)
	for len(s.f.Order) > maxSessions {
		oldest := s.f.Order[0]
		s.f.Order = s.f.Order[1:]
		delete(s.f.Sessions, oldest)
	}
	s.save()
}

// save schreibt die Datei atomar (tmp + rename — ein Absturz mitten im
// Schreiben kann keine halbe JSON hinterlassen). Best effort: Fehler
// bleiben stumm, ein nicht persistierter Stand ist ärgerlich, darf heron
// aber niemals werfen.
func (s *Store) save() {
	if s.path == "" {
		return
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	b, err := json.MarshalIndent(s.f, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}
