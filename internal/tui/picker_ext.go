package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ExtRow ist EINE Zeile des /mcp-Pickers: ein konfigurierter MCP-Server aus
// gooses config.yaml samt aktuellem Enable-Zustand. Die TUI hält bewusst
// einen eigenen Typ (statt acp.Extension), damit sie nicht am acp-Paket
// hängt — main.go mappt die Felder.
type ExtRow struct {
	Key     string // Schlüssel im config.yaml (extension_name der Tools)
	Label   string // Anzeigename
	Type    string // stdio / sse / streamable_http / platform
	Enabled bool
	Cmd     string   // stdio: Kommandozeile (gekürzt für die Zeile)
	EnvKeys []string // gesetzte Umgebungsvariablen (nur Namen!)
}

// skillsLoadedMsg trägt das Ergebnis von `goose skills list`.
type skillsLoadedMsg struct {
	rows []SkillRow
	err  error
}

// extLoadedMsg trägt die konfigurierten Extensions aus config/read.
type extLoadedMsg struct {
	rows []ExtRow
	err  error
}

// extToggledMsg quittiert ein Umschalten im /mcp-Picker.
type extToggledMsg struct {
	label string
	key   string
	on    bool
	err   error
}

// WithSkillsList verdrahtet die Skill-Quelle (`goose skills list`).
func (m Model) WithSkillsList(fn func() ([]SkillRow, error)) Model {
	m.skillsList = fn
	return m
}

// WithExtensions verdrahtet Liste + Umschalten der MCP-Extensions.
func (m Model) WithExtensions(list func() ([]ExtRow, error), toggle func(key string, on bool) error) Model {
	m.extList = list
	m.extToggle = toggle
	return m
}

// filteredSkillRows wendet die Picker-Suche auf Name UND Herkunft an.
func (m Model) filteredSkillRows() []SkillRow {
	return pickerMatch(m.skillRows, m.pickerQuery, func(r SkillRow) string { return r.Name + " " + r.Location })
}

// filteredExtRows: Suche über Name/Key/Typ.
func (m Model) filteredExtRows() []ExtRow {
	return pickerMatch(m.extRows, m.pickerQuery, func(r ExtRow) string { return r.Label + " " + r.Key + " " + r.Type })
}

// cursorSkill liefert die Zeile unter dem Picker-Cursor.
func (m Model) cursorSkill() (SkillRow, bool) {
	return pickerRowAt(m.filteredSkillRows(), m.pickerCursor)
}

// cursorExt liefert die MCP-Zeile unter dem Picker-Cursor.
func (m Model) cursorExt() (ExtRow, bool) {
	return pickerRowAt(m.filteredExtRows(), m.pickerCursor)
}

// toggleSkill hakt die Zeile unter dem Cursor ab (Leertaste).
func (m *Model) toggleSkill() {
	row, ok := m.cursorSkill()
	if !ok {
		return
	}
	if m.skillSel == nil {
		m.skillSel = map[string]bool{}
	}
	if m.skillSel[row.Name] {
		delete(m.skillSel, row.Name)
		return
	}
	m.skillSel[row.Name] = true
}

// selectedSkills: die angehakten Skills in LISTEN-Reihenfolge (stabil, nicht
// in Klick-Reihenfolge — der Prompt soll reproduzierbar aussehen). Ist
// nichts angehakt, gilt die Zeile unter dem Cursor: der Ein-Klick-Fall
// bleibt damit ein Druck.
func (m Model) selectedSkills() []string {
	var out []string
	for _, r := range m.skillRows { // VOLLE Liste: ein Häkchen überlebt den Filter
		if m.skillSel[r.Name] {
			out = append(out, r.Name)
		}
	}
	if len(out) == 0 {
		if row, ok := m.cursorSkill(); ok {
			out = append(out, row.Name)
		}
	}
	return out
}

// skillPrompt baut den Prompt-Prefix: der Agent lädt den Skill selbst über
// load_skill — heron kann ihm die Instruktionen NICHT vorab in den Kontext
// schieben (ein tools/call-Ergebnis landet bei heron, nicht beim Modell).
func skillPrompt(names []string) string {
	if len(names) == 0 {
		return ""
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "\"" + n + "\""
	}
	if len(names) == 1 {
		return "Nutze den Skill " + quoted[0] + ": "
	}
	return "Nutze die Skills " + strings.Join(quoted, ", ") + ": "
}

// applySkills übernimmt die Auswahl in das Eingabefeld und schließt den
// Picker: Warteschlange und History bekommen den fertigen Text erst beim
// Absenden.
func (m Model) applySkills() (tea.Model, tea.Cmd) {
	names := m.selectedSkills()
	m.closePicker()
	m.skillSel = nil
	if len(names) == 0 {
		return m, nil
	}
	m.input.SetValue(skillPrompt(names))
	m.input.CursorEnd()
	m.syncInputSize()
	return m, nil
}

// mcpKey behandelt Leertaste/enter im /mcp-Picker: beide schalten die Zeile
// unter dem Cursor um (kein Übernehmen — der Picker bleibt offen, damit man
// mehrere Server nacheinander schalten kann).
func (m Model) mcpKey() (tea.Model, tea.Cmd) {
	row, ok := m.cursorExt()
	if !ok {
		return m, nil
	}
	if m.extToggle == nil {
		m.chat = m.chat.Append(NewTextBlock("Kein Backend für Extensions (Demo-Modus).", "1"))
		m.closePicker()
		return m, m.flushPrinted()
	}
	fn := m.extToggle
	on := !row.Enabled
	return m, func() tea.Msg {
		return extToggledMsg{label: row.Label, key: row.Key, on: on, err: fn(row.Key, on)}
	}
}

// applyExtToggled schreibt die Quittung in den Chat und zieht den Zustand in
// der Zeile nach — der Picker bleibt offen.
func (m Model) applyExtToggled(msg extToggledMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.chat = m.chat.Append(NewTextBlock(fmt.Sprintf("%s: %v", msg.label, msg.err), "1"))
		return m, m.flushPrinted()
	}
	for i := range m.extRows {
		if m.extRows[i].Key == msg.key {
			m.extRows[i].Enabled = msg.on
		}
	}
	state := "deaktiviert"
	if msg.on {
		state = "aktiviert"
	}
	m.chat = m.chat.Append(NewTextBlock(fmt.Sprintf("%s %s — sofort wirksam, dauerhaft ab dem nächsten Start.", msg.label, state), "1"))
	return m, m.flushPrinted()
}
