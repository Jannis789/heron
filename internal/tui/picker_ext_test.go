package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// skillsRig: /skills mit zwei Skills aus einem festen Katalog.
func skillsRig(t *testing.T) *rig {
	r := newRig(t)
	r.m = r.m.WithSkillsList(func() ([]SkillRow, error) {
		return []SkillRow{
			{Name: "solid", Desc: "Use this skill when writing code", Location: "/nix/store/…/solid"},
			{Name: "web-search", Desc: "Search the web and extract page content", Location: "builtin://skills/web-search"},
		}, nil
	})
	return r
}

// TestSkillsPickerMultiSelect: Leertaste hakt mehrere Skills ab, enter
// übernimmt ALLE in EINEN Prompt-Prefix (User-Wunsch 22.11.).
func TestSkillsPickerMultiSelect(t *testing.T) {
	r := skillsRig(t)
	typeRunes(r, "/skills")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})

	if !r.m.pickerOpen || r.m.pickerKind != "skills" {
		t.Fatalf("/skills öffnet keinen Skill-Picker (kind=%q)", r.m.pickerKind)
	}
	if v := stripANSI(r.m.View()); !strings.Contains(v, "[ ]") || !strings.Contains(v, "solid") {
		t.Fatalf("Checkbox-Zeilen fehlen:\n%s", v)
	}
	r.do(tea.KeyMsg{Type: tea.KeySpace}) // solid abhaken
	r.do(tea.KeyMsg{Type: tea.KeyDown})
	r.do(tea.KeyMsg{Type: tea.KeySpace}) // web-search abhaken
	if v := stripANSI(r.m.View()); !strings.Contains(v, "[x] solid") || !strings.Contains(v, "[x] web-search") {
		t.Fatalf("Häkchen nicht sichtbar:\n%s", v)
	}
	r.do(tea.KeyMsg{Type: tea.KeySpace}) // web-search wieder abhaken
	r.do(tea.KeyMsg{Type: tea.KeySpace})
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // übernehmen

	if r.m.pickerOpen {
		t.Fatal("enter muss den Picker schließen")
	}
	want := `Nutze die Skills "solid", "web-search": `
	if got := r.m.input.Value(); got != want {
		t.Fatalf("Prompt-Prefix = %q, want %q", got, want)
	}
}

// TestSkillsPickerEnterWithoutCheckUsesCursor: ohne Häkchen übernimmt enter
// die Zeile unter dem Cursor — der Ein-Skill-Fall bleibt ein Druck.
func TestSkillsPickerEnterWithoutCheckUsesCursor(t *testing.T) {
	r := skillsRig(t)
	typeRunes(r, "/skills")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.do(tea.KeyMsg{Type: tea.KeyDown}) // web-search
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if got := r.m.input.Value(); got != `Nutze den Skill "web-search": ` {
		t.Fatalf("Prefix = %q", got)
	}
}

// TestSkillsPickerFilter: tippen filtert; das Häkchen hängt am NAMEN, nicht
// an der Zeilennummer (Filter ändert die Indizes).
func TestSkillsPickerFilter(t *testing.T) {
	r := skillsRig(t)
	typeRunes(r, "/skills")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.do(tea.KeyMsg{Type: tea.KeySpace}) // solid
	typeRunes(r, "web")
	if len(r.m.pickerOptions()) != 1 {
		t.Fatalf("Filter ließ %d Zeilen, want 1", len(r.m.pickerOptions()))
	}
	r.do(tea.KeyMsg{Type: tea.KeySpace}) // web-search
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if got := r.m.input.Value(); got != `Nutze die Skills "solid", "web-search": ` {
		t.Fatalf("Prefix = %q", got)
	}
}

// mcpRig: /mcp mit einem aktiven und einem inaktiven Server.
func mcpRig(t *testing.T) (*rig, *[]string) {
	r := newRig(t)
	var calls []string
	rows := []ExtRow{
		{Key: "sonarqube", Label: "SonarQube", Type: "stdio", Enabled: true, Cmd: "/run/current-system/sw/bin/sonarqube-mcp"},
		{Key: "coral", Label: "Coral", Type: "stdio", Enabled: false, Cmd: "coral-mcp"},
	}
	r.m = r.m.WithExtensions(
		func() ([]ExtRow, error) { return rows, nil },
		func(key string, on bool) error {
			calls = append(calls, key)
			for i := range rows {
				if rows[i].Key == key {
					rows[i].Enabled = on
				}
			}
			return nil
		})
	return r, &calls
}

// TestMCPPickerToggles: space schaltet den Server unter dem Cursor um,
// der Picker bleibt offen und quittiert im Chat.
func TestMCPPickerToggles(t *testing.T) {
	r, calls := mcpRig(t)
	typeRunes(r, "/mcp")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	if !r.m.pickerOpen || r.m.pickerKind != "mcp" {
		t.Fatalf("/mcp öffnet keinen Extension-Picker (kind=%q)", r.m.pickerKind)
	}
	if v := stripANSI(r.m.View()); !strings.Contains(v, "● SonarQube") || !strings.Contains(v, "○ Coral") {
		t.Fatalf("Zustände fehlen:\n%s", v)
	}
	r.do(tea.KeyMsg{Type: tea.KeySpace}) // SonarQube aus
	if len(*calls) != 1 || (*calls)[0] != "sonarqube" {
		t.Fatalf("Toggle-Aufrufe = %v", *calls)
	}
	if !r.m.pickerOpen {
		t.Fatal("der Picker muss nach dem Umschalten offen bleiben")
	}
	if v := stripANSI(r.m.activityView() + r.m.View()); !strings.Contains(v, "SonarQube deaktiviert") {
		t.Fatalf("keine Quittung im Chat:\n%s", v)
	}
	r.do(tea.KeyMsg{Type: tea.KeyDown})
	r.do(tea.KeyMsg{Type: tea.KeyEnter}) // Coral an
	if len(*calls) != 2 || (*calls)[1] != "coral" {
		t.Fatalf("Toggle-Aufrufe = %v", *calls)
	}
	if v := stripANSI(r.m.View()); !strings.Contains(v, "● Coral") {
		t.Fatalf("Zustand nach dem Umschalten nicht sichtbar:\n%s", v)
	}
	r.do(tea.KeyMsg{Type: tea.KeyEscape})
	if r.m.pickerOpen {
		t.Fatal("esc muss den Picker schließen")
	}
}

// TestMCPToggleErrorShowsInChat: ein Backend-Fehler (z. B. config.yaml nicht
// schreibbar) landet als Fehlerblock im Chat statt den Picker zu zerschießen.
func TestMCPToggleErrorShowsInChat(t *testing.T) {
	r := newRig(t)
	r.m = r.m.WithExtensions(
		func() ([]ExtRow, error) { return []ExtRow{{Key: "coral", Label: "Coral", Type: "stdio"}}, nil },
		func(string, bool) error { return errors.New("permission denied") })
	typeRunes(r, "/mcp")
	r.do(tea.KeyMsg{Type: tea.KeyEnter})
	r.do(tea.KeyMsg{Type: tea.KeySpace})
	v := stripANSI(r.m.View())
	if !strings.Contains(v, "permission denied") {
		t.Fatalf("Fehler fehlt im View:\n%s", v)
	}
}

// TestParseSkillsTable: die echte Tabellenform von `goose skills list`.
func TestParseSkillsTable(t *testing.T) {
	out := `Name            | Description                                        | Description tokens | Content tokens | Location
goose-doc-guide | Reference goose documentation to create, config... | 57                 | 817            | builtin://skills/goose-doc-guide
solid           | Use this skill when writing code, implementing ... | 54                 | 2371           | /nix/store/x/skills/solid
web-search      | Search the web and extract page content using D... | 47                 | 546            | builtin://skills/web-search
`
	rows := parseSkillsTable(out)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].Name != "goose-doc-guide" || rows[0].Location != "builtin://skills/goose-doc-guide" {
		t.Fatalf("erste Zeile falsch: %+v", rows[0])
	}
	if !strings.HasPrefix(rows[1].Desc, "Use this skill when writing code") {
		t.Fatalf("Beschreibung falsch: %q", rows[1].Desc)
	}
	if rows[2].Name != "web-search" {
		t.Fatalf("dritte Zeile falsch: %+v", rows[2])
	}
}
