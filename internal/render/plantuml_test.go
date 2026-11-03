package render

import (
	"strings"
	"testing"
)

// TestWithPlantUMLTheme: das Standard-Theme mimeograph wird jedem PlantUML-
// Quelltext eingespritzt — direkt nach @startuml; ein explizites !theme des
// Nutzers gewinnt; ohne @startuml wird vorangestellt; HERON_PUML_THEME
// uebersteuert das Standard-Theme.
func TestWithPlantUMLTheme(t *testing.T) {
	t.Setenv("HERON_PUML_THEME", "") // deterministisch: Standard-Theme
	sheet := "\nskinparam BackgroundColor transparent"
	got := withPlantUMLTheme("@startuml\nA -> B\n@enduml\n")
	want := "@startuml\n!theme " + plantumlDefaultTheme + sheet + "\nA -> B\n@enduml\n"
	if got != want {
		t.Fatalf("injection after @startuml failure:\n got %q\nwant %q", got, want)
	}

	// Ein explizites User-Theme wird nicht ueberschrieben.
	own := "@startuml\n!theme cerulean\nA\n@enduml\n"
	if g := withPlantUMLTheme(own); g != own {
		t.Fatalf("explicit !theme was overridden: %q", g)
	}

	// Ohne @startuml: Direktive voranstellen.
	no := "A -> B\n@enduml\n"
	if g := withPlantUMLTheme(no); !strings.HasPrefix(g, "!theme "+plantumlDefaultTheme+sheet+"\n") {
		t.Fatalf("no-@startuml prepend failure: %q", g)
	}

	// Env-Uebersteuerung (z.B. ein Dark-Theme).
	t.Setenv("HERON_PUML_THEME", "cyborg-outline")
	if g := withPlantUMLTheme("@startuml\nA\n@enduml\n"); !strings.Contains(g, "!theme cyborg-outline") && !strings.Contains(g, "BackgroundColor transparent") {
		t.Fatalf("HERON_PUML_THEME override or transparent not applied: %q", g)
	}
}

// TestPlantUMLDefaultThemeIsMimeograph sichert den angefragten Standard ab.
func TestPlantUMLDefaultThemeIsMimeograph(t *testing.T) {
	if plantumlDefaultTheme != "mimeograph" {
		t.Fatalf("default theme = %q, want mimeograph", plantumlDefaultTheme)
	}
}
