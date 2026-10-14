package tui

import (
	"fmt"
	"testing"
	"time"

	"heron/internal/render"
)

// BenchmarkBlockListView misst die Kosten von BlockList.all()/View() ueber
// die echten render-test-Bloecke (50 Bloecke inkl. Codeblocks). Zeigt, ob
// Nicht-Bild-Widgets (Chroma etc.) die ~66ms View-Zeit treiben.
func buildRenderTestList(b *testing.B) BlockList {
	b.Helper()
	src := "# t\n\nAbsatz mit Text.\n\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(1) }\n```\n\n```python\nx = 1\nfor i in range(3):\n    print(i, \"a\u00e4\")\n```\n\n```shell\necho hi | grep -c h || true\n```\n\nText dazwischen.\n\n"
	// x5 wiederholen -> ~50 Bloecke wie im echten Seed.
	full := ""
	for i := 0; i < 5; i++ {
		full += fmt.Sprintf("## Abschnitt %d\n%s", i, src)
	}
	nodes, err := render.Parse(full)
	if err != nil {
		b.Fatal(err)
	}
	th, err := render.DefaultTheme()
	if err != nil {
		b.Fatal(err)
	}
	l := NewBlockList().Append(BuildWidgets(nodes, th, nil)...)
	return l.SetSize(120, 40)
}

func BenchmarkBlockListView(b *testing.B) {
	l := buildRenderTestList(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.View()
	}
}

// BenchmarkBlockListViewImages: dieselbe Liste, aber die Codeblocks sind zu
// ImageBlocks konvertiert (wie applyDiagrams es tut) — die Live-Konfiguration.
func BenchmarkBlockListViewImages(b *testing.B) {
	b.Cleanup(KittyCleanup)
	l := buildRenderTestList(b)
	caps := TermCaps{CellW: 9, CellH: 20}
	kid := 0
	for i, it := range l.items {
		cb, ok := it.(CodeBlock)
		if !ok {
			continue
		}
		kid++
		img := NewImageBlock(realPNG(549, 371), "roh", true, caps, nil, nil)
		img = img.SetKittyID(kid).(ImageBlock)
		img = img.SetID(cb.id).(ImageBlock)
		l.items[i] = img
	}
	l = l.ScrollEnd()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.View()
	}
}

func BenchmarkBlockListAllOnce(b *testing.B) {
	l := buildRenderTestList(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.all()
	}
}

// TestViewDurationSmoketest loggt die reale Dauer eines View-Aufrufs.
func TestViewDurationSmoketest(t *testing.T) {
	l := buildRenderTestList(&testing.B{})
	t0 := time.Now()
	l.View()
	d := time.Since(t0)
	t.Logf("ein View: %v (%d Bloecke)", d, len(l.items))
	if d > 30*time.Millisecond {
		t.Fatalf("View zu langsam: %v", d)
	}
}
