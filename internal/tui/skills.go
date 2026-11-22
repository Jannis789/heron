package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SkillRow ist EIN Skill aus `goose skills list`: Name, (gekürzte)
// Beschreibung und Herkunft. goose hat dafür KEINE ACP-Methode
// (_goose/unstable/skills/list → Method not found, config/read "skills" →
// null) — der CLI-Befehl ist die einzige autoritative Quelle und kennt auch
// die builtin-Skills, die im goose-Binary stecken (~220 ms).
type SkillRow struct {
	Name     string
	Desc     string
	Location string
}

// skillsTimeout begrenzt den CLI-Aufruf: ein hängender goose-Prozess darf
// den Picker nicht blockieren.
const skillsTimeout = 5 * time.Second

// GooseSkills führt `goose skills list` aus und parst die Tabelle.
func GooseSkills(gooseBin string) ([]SkillRow, error) {
	if strings.TrimSpace(gooseBin) == "" {
		gooseBin = "goose"
	}
	ctx, cancel := context.WithTimeout(context.Background(), skillsTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, gooseBin, "skills", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("goose skills list: %w", err)
	}
	return parseSkillsTable(string(out)), nil
}

// parseSkillsTable liest die Tabelle von `goose skills list`:
//
//	Name            | Description        | Description tokens | Content tokens | Location
//	goose-doc-guide | Reference goose …  | 57                 | 817            | builtin://skills/goose-doc-guide
//
// Die Beschreibung ist in der Tabelle gekürzt ("…"); das reicht für den
// Picker, den Volltext liefert später load_skill.
func parseSkillsTable(out string) []SkillRow {
	var rows []SkillRow
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		fields := strings.Split(ln, "|")
		if len(fields) < 5 {
			continue // Kopfzeile oder Rahmen
		}
		name := strings.TrimSpace(fields[0])
		if name == "" || strings.EqualFold(name, "Name") {
			continue
		}
		if strings.HasPrefix(name, "-") { // Trennzeile
			continue
		}
		rows = append(rows, SkillRow{
			Name:     name,
			Desc:     strings.TrimSpace(fields[1]),
			Location: strings.TrimSpace(fields[4]),
		})
	}
	return rows
}
