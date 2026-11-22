package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Extension ist EIN Eintrag aus dem extensions:-Block von gooses config.yaml,
// so wie goose ihn über _goose/unstable/config/read (key "extensions")
// selbst meldet. Type ist die goose-Kategorie: "platform" für die
// eingebauten Extension, alles andere ("stdio", "sse", "streamable_http")
// ist ein externer MCP-Server.
type Extension struct {
	Key           string // Schlüssel im config.yaml (extension_name der Tools)
	Name          string // Anzeigename in gooses config
	DisplayName   string
	Type          string
	Description   string
	Enabled       bool
	Bundled       bool
	Cmd           string
	EnvKeys       []string
	ToolCountHint int // -1 = unbekannt
}

// IsMCP: externe MCP-Server (stdio/HTTP) statt goose-interner Platform-
// Extension.
func (e Extension) IsMCP() bool {
	return e.Type != "" && e.Type != "platform"
}

// Label: der Name, den die TUI zeigt (DisplayName → Name → Key).
func (e Extension) Label() string {
	switch {
	case e.DisplayName != "":
		return e.DisplayName
	case e.Name != "":
		return e.Name
	}
	return e.Key
}

// Extensions liest den extensions:-Block aus gooses Config (read-only, kein
// Session-Eingriff). Sortiert: aktive zuerst, dann alphabetisch.
func (s *Session) Extensions(ctx context.Context) ([]Extension, error) {
	res, err := s.client.Request(ctx, "_goose/unstable/config/read", map[string]any{"key": "extensions"})
	if err != nil {
		return nil, fmt.Errorf("config/read extensions: %w", err)
	}
	return parseExtensions(res)
}

func parseExtensions(res []byte) ([]Extension, error) {
	var wire struct {
		Value map[string]struct {
			Name        string            `json:"name"`
			DisplayName string            `json:"display_name"`
			Type        string            `json:"type"`
			Description string            `json:"description"`
			Enabled     bool              `json:"enabled"`
			Bundled     bool              `json:"bundled"`
			Cmd         string            `json:"cmd"`
			Envs        map[string]string `json:"envs"`
		} `json:"value"`
	}
	if err := json.Unmarshal(res, &wire); err != nil {
		return nil, fmt.Errorf("config/read extensions: %w", err)
	}
	out := make([]Extension, 0, len(wire.Value))
	for key, v := range wire.Value {
		e := Extension{Key: key, Name: v.Name, DisplayName: v.DisplayName,
			Type: v.Type, Description: v.Description, Enabled: v.Enabled,
			Bundled: v.Bundled, Cmd: v.Cmd, ToolCountHint: -1}
		for k := range v.Envs {
			e.EnvKeys = append(e.EnvKeys, k)
		}
		sort.Strings(e.EnvKeys)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		return strings.ToLower(out[i].Label()) < strings.ToLower(out[j].Label())
	})
	return out, nil
}

// ManageExtensionsTool ist der voll qualifizierte Name (extension__tool), den
// _goose/unstable/tools/call verlangt — mit dem blanken Namen antwortet
// goose "tool not found".
const ManageExtensionsTool = "extensionmanager__manage_extensions"

// SetExtensionEnabled schaltet eine Extension in der LAUFENDEN Session um
// (gooses ExtensionManager: enable startet den MCP-Server, disable beendet
// ihn; die Tool-Liste ändert sich sofort). Nur in-memory — die Persistenz
// für den nächsten Start schreibt SetExtensionEnabledInFile.
func (s *Session) SetExtensionEnabled(ctx context.Context, key string, enabled bool) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("keine Session")
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	res, err := s.client.Request(ctx, "_goose/unstable/tools/call", map[string]any{
		"sessionId":     s.sessionID,
		"extensionName": "extensionmanager",
		"name":          ManageExtensionsTool,
		"arguments":     map[string]any{"action": action, "extension_name": key},
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, key, err)
	}
	var out struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(res, &out)
	if out.IsError {
		msg := "unbekannter Fehler"
		if len(out.Content) > 0 {
			msg = out.Content[0].Text
		}
		return fmt.Errorf("%s %s: %s", action, key, msg)
	}
	return nil
}

// DefaultConfigPath liefert gooses Config-Datei (~/.config/goose/config.yaml),
// wie `goose info` sie nennt. GOOSE_CONFIG_DIR überschreibt das Verzeichnis
// (goose-Konvention), damit Tests und fremde Profile nicht die echte Datei
// anfassen.
func DefaultConfigPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("GOOSE_CONFIG_DIR")); dir != "" {
		return dir + "/config.yaml", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.config/goose/config.yaml", nil
}

// SetExtensionEnabledInFile schreibt extensions.<key>.enabled dauerhaft in
// gooses config.yaml — NUR diese eine Zeile, zeilenbasiert: die Datei ist
// von Hand gepflegt (Kommentare, Reihenfolge, Einrückung), ein YAML-Roundtrip
// würde sie umformatieren. Fehlt enabled:, wird die Zeile direkt unter dem
// Schlüssel eingefügt.
func SetExtensionEnabledInFile(path, key string, enabled bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	lines := strings.Split(string(b), "\n")

	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "extensions:") {
			start = i
			break
		}
	}
	if start < 0 {
		return fmt.Errorf("%s: kein extensions:-Block", path)
	}

	keyLine := -1
	for i := start + 1; i < len(lines); i++ {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(ln, "  ") { // Blockende (Dedent)
			break
		}
		if strings.HasPrefix(ln, "   ") { // Eigenschaft (4+ Leerzeichen)
			continue
		}
		if strings.TrimRight(ln, " ") == "  "+key+":" {
			keyLine = i
			break
		}
	}
	if keyLine < 0 {
		return fmt.Errorf("%s: Extension %q nicht gefunden", path, key)
	}

	val := "false"
	if enabled {
		val = "true"
	}
	// Innere Blockgrenze: bis zum nächsten 2-Space-Schlüssel bzw. Dedent.
	end := len(lines)
	for i := keyLine + 1; i < len(lines); i++ {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(ln, "    ") {
			end = i
			break
		}
	}
	enabledLine := -1
	for i := keyLine + 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimLeft(lines[i], " "), "enabled:") {
			enabledLine = i
			break
		}
	}
	if enabledLine >= 0 {
		if strings.TrimSpace(lines[enabledLine]) == "enabled: "+val {
			return nil // schon so — Datei nicht anfassen
		}
		lines[enabledLine] = "    enabled: " + val
	} else {
		rest := append([]string{"    enabled: " + val}, lines[keyLine+1:]...)
		lines = append(lines[:keyLine+1], rest...)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), mode)
}
