package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- /provider (FR-45) ------------------------------------------------------

// TestProviderPickerFlow: /provider oeffnet den Picker mit den Labeln der
// annoncierten Provider, die Suche filtert, enter setzt den Provider per
// configSet("provider", value) — und der Footer zeigt danach den Label.
func TestProviderPickerFlow(t *testing.T) {
	r := newRig(t)
	var gotID, gotVal string
	r.m = r.m.WithConfigSet(func(id, val string) error {
		gotID, gotVal = id, val
		return nil
	})
	r.do(infoMsg{model: "glm-5.3", effort: "max", provider: "zai_coding_plan",
		providerOptions: []OptionRow{
			{Value: "zai_coding_plan", Name: "Z.AI (GLM)"},
			{Value: "anthropic", Name: "Anthropic"},
			{Value: "custom_nous", Name: "Nous Research"},
		}})

	typeRunes(r, "/provider")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if !r.m.pickerOpen || r.m.pickerKind != "provider" {
		t.Fatalf("provider picker must open, open=%v kind=%q", r.m.pickerOpen, r.m.pickerKind)
	}
	rows := r.m.filteredProviderRows()
	if len(rows) != 3 {
		t.Fatalf("3 provider rows, got %d", len(rows))
	}
	// Der AKTIVE Provider steht zuerst — eigener Bestand sofort griffbereit.
	if rows[0].Value != "zai_coding_plan" {
		t.Fatalf("active provider must lead the list, got %+v", rows[0])
	}

	// Suche filtert ueber Name UND Wert.
	typeRunes(r, "nous")
	if rows := r.m.filteredProviderRows(); len(rows) != 1 || rows[0].Value != "custom_nous" {
		t.Fatalf("filter nous = %+v", rows)
	}

	// Enter sendet den VALUE (nicht das Label) als configId "provider".
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if gotID != "provider" || gotVal != "custom_nous" {
		t.Fatalf("configSet(%q, %q), want provider/custom_nous", gotID, gotVal)
	}
	if r.m.pickerOpen {
		t.Fatal("picker must close after enter")
	}
	if r.m.provider != "custom_nous" {
		t.Fatalf("provider label = %q", r.m.provider)
	}
	if lbl := r.m.stateLabel(); !strings.Contains(lbl, "Nous Research") {
		t.Fatalf("footer must show provider label, got %q", lbl)
	}
}

// TestProviderFavoritesFirst: Die in goose EINGERICHTETEN Provider
// (Keys aus config/read "providers") stehen im Picker direkt hinter dem
// aktiven. OHNE Suche zeigt der Picker NUR aktive + eingerichtete
// (Ruhe-Liste), MIT Suche den vollen Katalog — nicht eingerichtete
// Provider bleiben so erreichbar, tauchen aber nicht in der Ruhe-Liste
// auf.
func TestProviderFavoritesFirst(t *testing.T) {
	r := newRig(t)
	var gotVal string
	r.m = r.m.WithConfigSet(func(id, val string) error {
		gotVal = val
		return nil
	}).WithProviderFavorites([]string{"custom_nous", "zai_coding_plan"})
	r.do(infoMsg{provider: "custom_nous",
		providerOptions: []OptionRow{
			{Value: "anthropic", Name: "Anthropic"},
			{Value: "custom_nous", Name: "Nous Research"},
			{Value: "openai", Name: "OpenAI"},
			{Value: "zai_coding_plan", Name: "Z.AI Coding Plan"},
		}})

	// Ruhe-Liste: aktiver + eingerichtete — nichts anderes.
	rows := r.m.filteredProviderRows()
	if len(rows) != 2 || rows[0].Value != "custom_nous" || rows[1].Value != "zai_coding_plan" {
		t.Fatalf("quiet list = active + configured only, got %+v", rows)
	}

	// Suche eroeffnet den VOLLKATALOG ("anthropic" ist nicht
	// eingerichtet, aber auffind- und waehlbar).
	typeRunes(r, "/provider")
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // Picker oeffnen
	if !r.m.pickerOpen || r.m.pickerKind != "provider" {
		t.Fatal("provider picker must open")
	}
	typeRunes(r, "anthropic")
	if rows := r.m.filteredProviderRows(); len(rows) != 1 || rows[0].Value != "anthropic" {
		t.Fatalf("search must reach the full catalog, got %+v", rows)
	}

	// End-to-End: Picker zu, neu oeffnen, EIN runter, enter sendet den
	// eingerichteten Favoriten an set_config_option.
	r.do(tea.KeyMsg{Type: tea.KeyEsc})
	typeRunes(r, "/provider")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.do(tea.KeyMsg{Type: tea.KeyDown})
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if gotVal != "zai_coding_plan" {
		t.Fatalf("one down + enter must pick the configured favorite, got %q", gotVal)
	}
}

// TestProviderFavoritesMsg: Im Regelbetrieb treffen die eingerichteten
// Provider (Quelle: goose _goose/unstable/config/read "providers") als
// providerFavoritesMsg aus dem Stream ein — asynchron zur infoMsg
// derselben Session.
func TestProviderFavoritesMsg(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{provider: "zai_coding_plan",
		providerOptions: []OptionRow{
			{Value: "anthropic", Name: "Anthropic"},
			{Value: "zai_coding_plan", Name: "Z.AI Coding Plan"},
			{Value: "custom_nous", Name: "Nous Research"},
		}})
	r.do(providerFavoritesMsg{ids: []string{"custom_nous"}})
	rows := r.m.filteredProviderRows()
	if len(rows) != 2 || rows[0].Value != "zai_coding_plan" || rows[1].Value != "custom_nous" {
		t.Fatalf("quiet list = active + configured: %+v", rows)
	}
}

// TestProviderQuietListFallback: Ohne Favoriten (Demo-Modus, goose ohne
// config/read) zeigt die Ruhe-Liste den vollen Katalog — aktiver zuerst.
func TestProviderQuietListFallback(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{provider: "openai",
		providerOptions: []OptionRow{
			{Value: "anthropic", Name: "Anthropic"},
			{Value: "openai", Name: "OpenAI"},
			{Value: "zai_coding_plan", Name: "Z.AI Coding Plan"},
		}})
	rows := r.m.filteredProviderRows()
	if len(rows) != 3 || rows[0].Value != "openai" || rows[1].Value != "anthropic" {
		t.Fatalf("no favorites = full catalog, active first: %+v", rows)
	}
}

// ---- Queue + doppeltes ctrl+c (FR-46) --------------------------------------

// TestQueueWhileRunningAndDoubleCtrlC: Prompts waehrend eines laufenden
// Turns landen in der Warteschlange (OHNE User-Block) und gehen nach dem
// Turn-Ende automatisch raus; doppeltes ctrl+c verwirft sie.
func TestQueueWhileRunningAndDoubleCtrlC(t *testing.T) {
	var sent []string
	r := newRig(t)
	r = r.withSender(func(p string) (string, error) {
		sent = append(sent, p)
		return "ok: " + p, nil
	})

	// Turn "laeuft": Erste Frage absetzen und running simulieren.
	typeRunes(r, "erste frage")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if len(sent) != 1 || sent[0] != "erste frage" {
		t.Fatalf("first prompt must send, sent=%v", sent)
	}
	r.m.running = true // Turn haelt (Send ist im rig synchron durch)

	typeRunes(r, "zweite frage")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	typeRunes(r, "dritte frage")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if len(r.m.queued) != 2 {
		t.Fatalf("2 prompts queued, got %d", len(r.m.queued))
	}
	if len(sent) != 1 {
		t.Fatalf("queued prompts must NOT send yet, sent=%v", sent)
	}

	// Doppelt ctrl+c quittiert: Warteschlange weg, sichtbares Feedback.
	r.do(tea.KeyMsg{Type: tea.KeyCtrlC})
	r.do(tea.KeyMsg{Type: tea.KeyCtrlC})
	if len(r.m.queued) != 0 {
		t.Fatalf("double ctrl+c must discard the queue, got %d", len(r.m.queued))
	}
	if view := stripANSI(r.m.chat.View()); !strings.Contains(view, "Warteschlange verworfen (2)") {
		t.Fatalf("queue discard must be visible, chat=%q", view)
	}
}

// TestQueueDrainsAfterTurn: Nach einem sauberen Turn-Ende geht der naechste
// wartende Prompt automatisch raus — User-Block, dann Antwort.
func TestQueueDrainsAfterTurn(t *testing.T) {
	var sent []string
	r := newRig(t)
	r = r.withSender(func(p string) (string, error) {
		sent = append(sent, p)
		return "ok: " + p, nil
	})

	r.m.running = true
	typeRunes(r, "nachgeschoben")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if len(r.m.queued) != 1 {
		t.Fatalf("prompt must queue while running, got %d", len(r.m.queued))
	}

	// Turn-Ende (Antwort der laufenden Frage): Drainage feuert den
	// wartenden Prompt als naechsten Turn.
	r.do(replyMsg{text: "antwort der ersten frage", prompt: "erste frage"})
	if len(sent) != 1 || sent[0] != "nachgeschoben" {
		t.Fatalf("queued prompt must drain after the turn, sent=%v", sent)
	}
	if len(r.m.queued) != 0 {
		t.Fatalf("queue must be empty, got %d", len(r.m.queued))
	}
	// Das rig spult die Kommandokette SYNCHRON durch: Der nachgeschobene
	// Turn ist hier bereits ABGESCHLOSSEN — seine Antwort muss im Chat
	// stehen und running wieder false sein.
	if r.m.running {
		t.Fatal("drained turn must have settled (synchronous rig)")
	}
	view := stripANSI(r.m.chat.View())
	if !strings.Contains(view, "nachgeschoben") || !strings.Contains(view, "ok: nachgeschoben") {
		t.Fatalf("user block AND answer of the drained prompt must be visible: %q", view)
	}
}

// ---- Fehler-Block + Retry (FR-33) ------------------------------------------

// TestErrorBlockRetry: Ein API-Fehler erscheint als ⚠-Karte; "r" auf dem
// fokussierten Block entfernt sie und sendet denselben Prompt erneut.
func TestErrorBlockRetry(t *testing.T) {
	var calls []string
	r := newRig(t)
	r = r.withSender(func(p string) (string, error) {
		calls = append(calls, p)
		if len(calls) == 1 {
			return "", errors.New("api down")
		}
		return "alles gut", nil
	})

	typeRunes(r, "wichtige frage")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if len(calls) != 1 {
		t.Fatalf("first send must run, calls=%v", calls)
	}

	var eb ErrorBlock
	found := false
	for _, it := range r.m.chat.items {
		if b, ok := it.(ErrorBlock); ok {
			eb, found = b, true
		}
	}
	if !found {
		t.Fatalf("api error must become an ErrorBlock, chat=%q", stripANSI(r.m.chat.View()))
	}
	view := stripANSI(eb.View(80))
	if !strings.Contains(view, "⚠ API-Fehler") || !strings.Contains(view, "api down") {
		t.Fatalf("error card must show ⚠ head and message: %q", view)
	}
	if !strings.Contains(view, "r retry") {
		t.Fatalf("error card must offer the retry action: %q", view)
	}

	// Fokus auf die Karte (erster fokussierbarer Block) und r druecken.
	r.do(tea.KeyMsg{Type: tea.KeyTab})
	if !r.m.chat.FocusedError() {
		t.Fatal("tab must focus the error block")
	}
	if h := stripANSI(r.m.footerView()); !strings.Contains(h, "r retry") {
		t.Fatalf("hint bar must offer retry: %q", h)
	}
	r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})

	if len(calls) != 2 || calls[1] != calls[0] {
		t.Fatalf("retry must resend the same prompt, calls=%v", calls)
	}
	for _, it := range r.m.chat.items {
		if _, ok := it.(ErrorBlock); ok {
			t.Fatal("error block must be gone after retry")
		}
	}
	if view := stripANSI(r.m.chat.View()); !strings.Contains(view, "alles gut") {
		t.Fatalf("retry answer must land in the chat: %q", view)
	}
}
