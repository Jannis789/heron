package tui

import (
	"strconv"
	"strings"
	"testing"

	"heron/internal/render"
)

// benchList baut eine realistische Historie: n Prosa-/Code-Bloecke plus
// einen grossen Tool-Output am Ende (wie ein langer Chat).
func benchList(b *testing.B, n int, toolLines int) BlockList {
	b.Helper()
	if _, err := render.DefaultTheme(); err != nil {
		b.Fatal(err)
	}
	l := NewBlockList().SetSize(120, 40)
	for i := 0; i < n; i++ {
		var w Widget
		switch i % 3 {
		case 0:
			w = NewTextBlock(strings.Repeat("Ein Satz Fliesstext mit einigen Woertern. ", 6), "")
		case 1:
			w = NewCodeBlock("go", strings.Repeat("fmt.Println(\"zeile\")\n", 20), nil)
		default:
			w = NewTextBlock("## Ueberschrift "+strings.Repeat("x", 20), "")
		}
		l = l.Append(w)
	}
	if toolLines > 0 {
		out := strings.Repeat("ausgabe-zeile mit etwas text\n", toolLines)
		l = l.Append(NewTextBlock(out, ""))
	}
	return l.ScrollEnd()
}

func BenchmarkListViewLongChat(b *testing.B) {
	l := benchList(b, 300, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.View()
	}
}

func BenchmarkListViewHugeToolOutput(b *testing.B) {
	l := benchList(b, 300, 20000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.View()
	}
}

func BenchmarkListFocusVisibleLongChat(b *testing.B) {
	l := benchList(b, 500, 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.FocusVisible()
	}
}

// BenchmarkListScaling zeigt, wie sich Frame- und Tasten-Kosten mit der
// Chat-Historie verhalten (Ziel: die Werte flachen ab, statt linear zu
// wachsen — "lazy" heisst hier: nur Sichtbares rendern, nur Geaendertes
// kopieren).
func BenchmarkListScaling(b *testing.B) {
	for _, n := range []int{300, 1000, 3000} {
		b.Run("view"+strconv.Itoa(n), func(b *testing.B) {
			l := benchList(b, n, 0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = l.View()
			}
		})
		b.Run("focus"+strconv.Itoa(n), func(b *testing.B) {
			l := benchList(b, n, 0)
			a, c := len(l.items)-1, len(l.items)-2
			l = l.setFocus(a)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					l = l.setFocus(c)
				} else {
					l = l.setFocus(a)
				}
			}
		})
	}
}

func BenchmarkFocusStepLongChat(b *testing.B) {
	l := benchList(b, 1000, 0)
	a, c := len(l.items)-1, len(l.items)-2
	l = l.setFocus(a)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			l = l.setFocus(c)
		} else {
			l = l.setFocus(a)
		}
	}
}

func BenchmarkClearMarksLongChat(b *testing.B) {
	l := benchList(b, 1000, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l = l.toggleMarkIdx(len(l.items) - 1)
		l = l.clearMarks()
	}
}

func BenchmarkActivityViewHugeCommand(b *testing.B) {
	th, _ := render.DefaultTheme()
	cmd := strings.Repeat("grep -rn muster ./src && xargs build \\\n  --flag mit wert\n", 400)
	a := NewActivityTree(th).Tool(ToolUpdate{CallID: "c", Label: "bash", Kind: "execute", Command: cmd})
	a = a.ToggleTools()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View(100, 15)
	}
}
