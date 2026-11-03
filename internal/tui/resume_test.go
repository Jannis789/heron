package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// resumeRig builds a rig with resume callbacks recording the started ids:
// two sessions, "s2" the newer one (rows arrive newest first).
func resumeRig(t *testing.T) (*rig, *[]string) {
	r := newRig(t)
	var started []string
	rows := []SessionRow{
		{ID: "s2", Label: "Neueste · 31.10. 18:00", Detail: "heron · 26 msgs · glm-5.3"},
		{ID: "s1", Label: "Ältere · 31.10. 17:00", Detail: "media-svr · 2 msgs"},
	}
	r.m = r.m.
		WithResumeList(func() ([]SessionRow, error) { return rows, nil }).
		WithResumeStart(func(id string) error { started = append(started, id); return nil })
	return r, &started
}

// TestResumeOpensSessionPicker (FR-44): /resume lists the stored sessions as
// a picker; the search line is there, the newest row first.
func TestResumeOpensSessionPicker(t *testing.T) {
	r, _ := resumeRig(t)
	typeRunes(r, "/resume")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if !r.m.pickerOpen || r.m.pickerKind != "resume" {
		t.Fatal("/resume did not open the session picker")
	}
	if len(r.m.pickerOptions()) != 2 {
		t.Fatalf("picker rows = %d, want 2", len(r.m.pickerOptions()))
	}
	if !strings.Contains(stripANSI(r.m.View()), "Neueste") {
		t.Fatal("session rows missing in view")
	}
}

// TestResumeEnterStartsAndReplays (FR-44): enter starts the marked session;
// the chat and counters reset and the replayed history lands as blocks
// (user line + final rendered answer).
func TestResumeEnterStartsAndReplays(t *testing.T) {
	r, started := resumeRig(t)
	typeRunes(r, "/resume")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // cursor sits on the newest row

	if len(*started) != 1 || (*started)[0] != "s2" {
		t.Fatalf("started = %v, want [s2]", *started)
	}
	if !r.m.chat.Empty() {
		t.Fatal("chat not cleared before the replay")
	}
	if r.m.totalIn != 0 || r.m.totalOut != 0 {
		t.Fatal("token counters not reset")
	}
	// Der Replay (session/load) füllt den Chat:
	r.do(replayMsg{kind: replayUser, text: "frage von damals"})
	r.do(replayMsg{kind: replayAgent, text: "antwort **von damals**"})
	r.contains("frage von damals")
	r.contains("von damals") // rendered, not raw markdown fences
}

// TestResumePickerSearchFilters (FR-44): typing filters the session rows
// like the model list (FR-20), enter starts the filtered row.
func TestResumePickerSearchFilters(t *testing.T) {
	r, started := resumeRig(t)
	typeRunes(r, "/resume")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.do(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ältere")}) // query filters

	if len(r.m.pickerOptions()) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(r.m.pickerOptions()))
	}
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if len(*started) != 1 || (*started)[0] != "s1" {
		t.Fatalf("started = %v, want [s1]", *started)
	}
}

// TestResumeBlockedWhileRunning (FR-44): a running turn blocks the load,
// same rule as /new (FR-20).
func TestResumeBlockedWhileRunning(t *testing.T) {
	r, started := resumeRig(t)
	r.m.running = true
	typeRunes(r, "/resume")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if len(*started) != 0 {
		t.Fatal("resume ran while a turn was in flight")
	}
	r.contains("Ein Turn läuft noch")
}

// TestResumeListErrorReports (FR-44): a failed listing reports in the chat
// instead of opening an empty picker.
func TestResumeListErrorReports(t *testing.T) {
	r := newRig(t)
	r.m = r.m.WithResumeList(func() ([]SessionRow, error) {
		return nil, errTest{"goose weg"}
	})
	typeRunes(r, "/resume")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if r.m.pickerOpen {
		t.Fatal("picker opened despite the listing error")
	}
	r.contains("Resume-Liste fehlgeschlagen")
}

// TestResumeListEmptyReports: no stored sessions — say so, don't open a
// picker with zero rows.
func TestResumeListEmptyReports(t *testing.T) {
	r := newRig(t)
	r.m = r.m.WithResumeList(func() ([]SessionRow, error) { return nil, nil })
	typeRunes(r, "/resume")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if r.m.pickerOpen {
		t.Fatal("picker opened with zero rows")
	}
	r.contains("keine früheren Sessions")
}

// TestResumeStartErrorReports: a failed session/load reports in the chat.
func TestResumeStartErrorReports(t *testing.T) {
	r := newRig(t)
	r.m = r.m.
		WithResumeList(func() ([]SessionRow, error) {
			return []SessionRow{{ID: "s1", Label: "x"}}, nil
		}).
		WithResumeStart(func(id string) error { return errTest{"session/load boom"} })
	typeRunes(r, "/resume")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	r.contains("Resume fehlgeschlagen")
}

// TestNewSessionResetsPrintedUpTo: /new clears the chat — the inline print
// cursor must reset too, or inline mode never prints again (regression).
func TestNewSessionResetsPrintedUpTo(t *testing.T) {
	r := newRig(t).seed("# hallo")
	r.m.inline = true
	r.m.printedUpTo = len(r.m.chat.items) // all printed
	r.do(newSessionMsg{})

	if !r.m.chat.Empty() {
		t.Fatal("/new did not clear the chat")
	}
	if r.m.printedUpTo != 0 {
		t.Fatalf("printedUpTo = %d, want 0", r.m.printedUpTo)
	}
}

// errTest ist ein einfacher Fehlertyp für Stub-Callbacks.
type errTest struct{ msg string }

func (e errTest) Error() string { return e.msg }
