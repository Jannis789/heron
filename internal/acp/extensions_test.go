package acp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cfgFixture = `# Handgepflegte goose-Konfiguration
extensions:
  # SonarQube MCP Server — Kommentar bleibt stehen
  sonarqube:
    name: SonarQube
    cmd: /run/current-system/sw/bin/sonarqube-mcp
    args: []
    enabled: true
    type: stdio
    timeout: 300
    envs:
      SONARQUBE_URL: "http://127.0.0.1:9000"
  tom:
    enabled: true
    type: platform
    name: tom
  coral:
    name: Coral
    cmd: coral-mcp
    enabled: false
    type: stdio
GOOSE_TELEMETRY_ENABLED: false
active_provider: custom_zai
`

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestSetExtensionEnabledInFileDisables: nur die enabled:-Zeile der
// genannten Extension kippt — Kommentare, Reihenfolge und die anderen
// Extensions bleiben byte-identisch (die Datei ist handgepflegt).
func TestSetExtensionEnabledInFileDisables(t *testing.T) {
	p := writeCfg(t, cfgFixture)
	if err := SetExtensionEnabledInFile(p, "sonarqube", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got := string(b)

	if !strings.Contains(got, "  # SonarQube MCP Server — Kommentar bleibt stehen") {
		t.Fatalf("Kommentar verloren:\n%s", got)
	}
	idx := strings.Index(got, "  sonarqube:")
	block := got[idx:]
	if !strings.Contains(block[:strings.Index(block, "  tom:")], "enabled: false") {
		t.Fatalf("sonarqube nicht deaktiviert:\n%s", got)
	}
	if !strings.Contains(got, "  tom:\n    enabled: true") {
		t.Fatalf("tom wurde mitverändert:\n%s", got)
	}
	if !strings.Contains(got, "  coral:\n    name: Coral\n    cmd: coral-mcp\n    enabled: false") {
		t.Fatalf("coral wurde verändert:\n%s", got)
	}
	// Modus bleibt erhalten (0600).
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("Dateimodus = %v", st.Mode().Perm())
	}
}

// TestSetExtensionEnabledInFileIdempotent: steht der Wert schon so, bleibt
// die Datei unangetastet (kein Schreibvorgang, keine Formatänderung).
func TestSetExtensionEnabledInFileIdempotent(t *testing.T) {
	p := writeCfg(t, cfgFixture)
	before, _ := os.ReadFile(p)
	if err := SetExtensionEnabledInFile(p, "tom", true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatalf("Datei wurde ohne Änderung neu geschrieben:\n%s", after)
	}
}

// TestSetExtensionEnabledInFileInserts: fehlt enabled:, wird die Zeile
// direkt unter dem Schlüssel eingefügt (Standard ist in goose "an").
func TestSetExtensionEnabledInFileInserts(t *testing.T) {
	body := "extensions:\n  coral:\n    name: Coral\n    cmd: coral-mcp\n    type: stdio\n"
	p := writeCfg(t, body)
	if err := SetExtensionEnabledInFile(p, "coral", false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	want := "extensions:\n  coral:\n    enabled: false\n    name: Coral\n    cmd: coral-mcp\n    type: stdio\n"
	if string(got) != want {
		t.Fatalf("Einfügen falsch:\n%q\nwant\n%q", got, want)
	}
}

// TestSetExtensionEnabledInFileMissingKey: unbekannte Extension → Fehler,
// Datei unverändert.
func TestSetExtensionEnabledInFileMissingKey(t *testing.T) {
	p := writeCfg(t, cfgFixture)
	before, _ := os.ReadFile(p)
	if err := SetExtensionEnabledInFile(p, "gibtsnicht", false); err == nil {
		t.Fatal("unbekannte Extension muss einen Fehler liefern")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("Datei wurde trotz Fehler verändert")
	}
}

// TestParseExtensions: gooses config/read-Antwort → Extension-Liste,
// MCP-Server erkannt, aktive zuerst.
func TestParseExtensions(t *testing.T) {
	res := []byte(`{"value":{
	  "sonarqube":{"name":"SonarQube","cmd":"/bin/sonarqube-mcp","enabled":true,"type":"stdio","envs":{"B":"2","A":"1"}},
	  "tom":{"enabled":true,"type":"platform","name":"tom","description":"Top Of Mind","bundled":true},
	  "coral":{"name":"Coral","cmd":"coral-mcp","enabled":false,"type":"stdio"}
	}}`)
	exts, err := parseExtensions(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 3 {
		t.Fatalf("exts = %d", len(exts))
	}
	// Aktive zuerst (alphabetisch: SonarQube, tom), dann inaktive.
	if exts[0].Key != "sonarqube" || !exts[0].IsMCP() {
		t.Fatalf("erste Extension falsch: %+v", exts[0])
	}
	if exts[1].Key != "tom" || exts[1].IsMCP() {
		t.Fatalf("platform-Extension muss IsMCP=false sein: %+v", exts[1])
	}
	if exts[2].Key != "coral" || exts[2].Enabled {
		t.Fatalf("inaktive Extension falsch sortiert: %+v", exts[2])
	}
	if strings.Join(exts[0].EnvKeys, ",") != "A,B" {
		t.Fatalf("EnvKeys = %v", exts[0].EnvKeys)
	}
	if exts[0].Label() != "SonarQube" {
		t.Fatalf("Label = %q", exts[0].Label())
	}
}
