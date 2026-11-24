package tui

import (
	"strconv"
	"strings"
	"testing"
)

// Scroll-Erhalt in Prozent (User-Anforderung 2, 2026-11-23): Der Activity-
// Baum (thinking/execute/other) verdraengt die normale Response-Ausgabe,
// wenn der Platz knapp wird — dabei darf die LESPOSITION nicht springen:
// Wer bei 40 % des Chats liest, liest nach Baum-Wachstum weiter bei 40 %,
// und der Bottom-Follow bleibt unten gepinnt (die neuesten Zeilen der
// Antwort bleiben sichtbar, solange nichts anderes scrollt).

// scrollPct misst die Scroll-Position als Anteil (0..1) des scrollbaren
// Bereichs — das Scrollbar-Modell: 0 = Dokumentanfang, 1 = Ende.
func scrollPct(l BlockList) float64 {
	maxOff := max(0, l.totalLines()-l.height)
	if maxOff <= 0 {
		return 0
	}
	return float64(l.offset) / float64(maxOff)
}

// longChat baut einen Blocklist mit deutlich mehr Zeilen als der Viewport.
func longChat(w, h int) BlockList {
	l := NewBlockList()
	for i := 0; i < 60; i++ {
		l = l.Append(NewTextBlock("antwort "+strconv.Itoa(i), ""))
	}
	return l.SetSize(w, h)
}

// TestSetSizeShrinkPreservesScrollPercent: der Baum frisst Chat-Zeilen
// (Höhe schrumpft) — die Lesposition bleibt in Prozent stehen, statt am
// absoluten Offset zu kleben (unten verliert man Zeilen, oben springt der
// Inhalt scheinbar).
func TestSetSizeShrinkPreservesScrollPercent(t *testing.T) {
	l := longChat(80, 12)
	maxOff := l.totalLines() - l.height
	l.offset = maxOff / 2 // Leseposition: 50 %
	l.follow = false
	want := scrollPct(l)

	l = l.SetSize(80, 7) // Baum hat 5 Zeilen genommen
	if l.height != 7 {
		t.Fatalf("height = %d, want 7", l.height)
	}
	got := scrollPct(l)
	if absF(got-want) > 0.02 {
		t.Fatalf("Leseposition gesprungen: vor dem Schrumpfen %.3f, danach %.3f (offset %d, height %d)",
			want, got, l.offset, l.height)
	}
}

// TestSetSizeGrowPreservesScrollPercent: der Baum schrumpft wieder (Turn
// endet, Thinking klappt zu) — derselbe Vertrag in der Gegenrichtung.
func TestSetSizeGrowPreservesScrollPercent(t *testing.T) {
	l := longChat(80, 7)
	maxOff := l.totalLines() - l.height
	l.offset = maxOff / 4 // Leseposition: 25 %
	l.follow = false
	want := scrollPct(l)

	l = l.SetSize(80, 16)
	got := scrollPct(l)
	if absF(got-want) > 0.02 {
		t.Fatalf("Leseposition gesprungen: vor dem Wachsen %.3f, danach %.3f (offset %d, height %d)",
			want, got, l.offset, l.height)
	}
}

// TestSetSizeShrinkKeepsFollowPinnedToBottom: im Bottom-Follow (Standard
// nach dem Senden) muss das Schrumpfen des Chat-Fensters die NEUSTEN
// Zeilen sichtbar halten — vorher schnitt der feste Offset unten ab,
// bis der nächste Stream-Chunk nachrollte; während des reinen Denkens
// (keine Chunks) blieb das Antwortende unsichtbar.
func TestSetSizeShrinkKeepsFollowPinnedToBottom(t *testing.T) {
	l := longChat(80, 12)
	if !l.follow {
		t.Fatal("Ausgang: follow an (frischer Chat haengt unten)")
	}
	if l.offset != l.totalLines()-l.height {
		t.Fatalf("Ausgang: offset %d, want Unterkante %d", l.offset, l.totalLines()-l.height)
	}
	l = l.SetSize(80, 7)
	if maxOff := l.totalLines() - l.height; l.offset != maxOff {
		t.Fatalf("nach Schrumpfen: offset %d, want Unterkante %d — die neuesten Zeilen sind abgeschnitten",
			l.offset, maxOff)
	}
	// Sichtprüfung: die letzte Zeile des Dokuments ist im Fenster.
	view := l.View()
	last := strings.Split(stripANSI(view), "\n")
	if !strings.Contains(strings.Join(last, "\n"), "antwort 59") {
		t.Fatalf("letzte Antwortzeile nicht im Fenster:\n%s", view)
	}
}

// TestSetSizeAtTopStaysAtTop: Leseposition Dokumentanfang (0 %) — der
// Baum darf den Anfang nicht wegschieben.
func TestSetSizeAtTopStaysAtTop(t *testing.T) {
	l := longChat(80, 12)
	l.offset, l.follow = 0, false
	l = l.SetSize(80, 5)
	if l.offset != 0 {
		t.Fatalf("offset = %d, want 0 (Anfang bleibt Anfang)", l.offset)
	}
}

// TestSetSizeSameHeightKeepsOffset: ohne Höhenänderung (Spinner-Tick ruft
// relayout → SetSize mit gleichem Maß) darf sich gar nichts tun.
func TestSetSizeSameHeightKeepsOffset(t *testing.T) {
	l := longChat(80, 12)
	maxOff := l.totalLines() - l.height
	l.offset, l.follow = maxOff/3, false
	l = l.SetSize(80, 12)
	if l.offset != maxOff/3 {
		t.Fatalf("offset = %d, want unverändert %d", l.offset, maxOff/3)
	}
}

// TestTreeGrowthKeepsChatScrollPercent: der echte Ablauf im Model — der
// Agent denkt (thoughtMsg treibt den Working-on-Baum), der Chat-Viewport
// gibt Zeile für Zeile ab, die Leseposition bleibt in Prozent.
func TestTreeGrowthKeepsChatScrollPercent(t *testing.T) {
	r := newRig(t)
	r.seed(strings.Repeat("antwort\n\n", 40))
	h0 := r.m.chat.height
	maxOff := r.m.chat.totalLines() - h0
	if maxOff <= 4 {
		t.Fatalf("Test braucht scrollbaren Chat, maxOff=%d", maxOff)
	}
	r.m.chat.offset = maxOff / 2
	r.m.chat.follow = false
	want := scrollPct(r.m.chat)

	r.m.startTurn() // laufender Turn + Idle-Baum (Working on)
	for i := 0; i < 8; i++ {
		r.do(thoughtMsg{text: "überlege ausführlich " + strconv.Itoa(i) + " …"})
	}
	if r.m.chat.height >= h0 {
		t.Fatalf("Baum hat dem Chat keinen Platz genommen: %d → %d", h0, r.m.chat.height)
	}
	got := scrollPct(r.m.chat)
	if absF(got-want) > 0.03 {
		t.Fatalf("Leseposition gesprungen: vor dem Denken %.3f, danach %.3f (offset %d, height %d)",
			want, got, r.m.chat.offset, r.m.chat.height)
	}
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
