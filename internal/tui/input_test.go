package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TestInputBoxIsFramedOnAllSides: the prompt area must render a closed box —
// top border, one row per text line, and a bottom border carrying the hint.
func TestInputBoxIsFramedOnAllSides(t *testing.T) {
	r := newRig(t)
	view := r.m.inputView()
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("prompt box has %d rows, want >=3 (top/body/bottom): %q", len(lines), view)
	}
	if !strings.HasPrefix(stripANSI(lines[0]), "┌") {
		t.Fatalf("top border missing: %q", lines[0])
	}
	bottom := stripANSI(lines[len(lines)-1])
	if !strings.HasPrefix(bottom, "└") || !(strings.HasSuffix(bottom, "┘") || strings.HasSuffix(bottom, "╯")) {
		t.Fatalf("bottom border missing: %q", bottom)
	}
	// The model/effort label lives in the footer bar below the box now
	// (crush-style) — the border itself stays clean.
	if !strings.Contains(stripANSI(r.m.View()), "goose · medium") {
		t.Fatalf("model/effort missing in footer: %q", r.m.View())
	}
}

// TestPromptBoxShowsWorkspaceTop: the top border names the workspace, the
// app name stays in the title row.
func TestPromptBoxShowsWorkspaceTop(t *testing.T) {
	r := newRig(t)
	r.m.cwd = "~/projekte/heron"
	r.contains("~/projekte/heron")
}

// TestPromptBoxStableWhileRunning: the bottom shows model · effort in every
// state — no elapsed seconds, no abort hint. Ctrl+C still aborts (FR-7),
// it just does not announce itself.
func TestPromptBoxStableWhileRunning(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.contains("goose · medium")
	if v := r.m.View(); strings.Contains(v, "antwortet") || strings.Contains(v, "bricht ab") {
		t.Fatalf("running state leaked into the box: %q", v)
	}
}

// TestInfoMsgUpdatesLabels: the backend's model/effort announcement (ACP
// configOptions) replaces the flag defaults in the bottom border.
func TestInfoMsgUpdatesLabels(t *testing.T) {
	r := newRig(t)
	r.do(infoMsg{model: "glm-5.3", effort: "max"})
	r.contains("glm-5.3 · max")
}

// TestInfoMsgKeepsFlagOverrides: empty parts of the announcement keep the
// current labels, so a pinned -model/-effort survives.
func TestInfoMsgKeepsFlagOverrides(t *testing.T) {
	r := newRig(t)
	r.m.modelLabel = "pinned-model"
	r.do(infoMsg{effort: "high"})
	r.contains("pinned-model · high")
}

// TestPlaceholderCarriesTheHints: the static key hints live in the
// placeholder so the borders stay free for live information.
func TestPlaceholderCarriesTheHints(t *testing.T) {
	r := newRig(t)
	r.contains("enter sends")
	r.contains("ctrl+j")
}

// TestPromptBoxGrowsWithText: a multi-line prompt widens the box and the chat
// viewport gives the rows back, so the screen still fits.
func TestPromptBoxGrowsWithText(t *testing.T) {
	r := newRig(t)
	before := len(strings.Split(r.m.inputView(), "\n"))

	r.typeLine("eine")
	r.do(tea.KeyMsg{Type: tea.KeyCtrlJ})
	r.typeLine("zweite")

	after := strings.Split(r.m.inputView(), "\n")
	if len(after) <= before {
		t.Fatalf("prompt box did not grow: %d rows before, %d after", before, len(after))
	}
	if !strings.HasPrefix(stripANSI(after[len(after)-1]), "└") {
		t.Fatalf("bottom border lost while growing: %q", after[len(after)-1])
	}
}

// TestPromptBoxGrowsWithWrappedLine: the input soft-wraps a long single
// line and the box grows with every filled row — no explicit ctrl+j needed.
func TestPromptBoxGrowsWithWrappedLine(t *testing.T) {
	r := newRig(t)
	before := len(strings.Split(r.m.inputView(), "\n"))

	// 200 cells at a 96-cell inner width: must wrap onto 3 rows.
	r.typeLine(strings.Repeat("word ", 40))

	after := strings.Split(r.m.inputView(), "\n")
	if len(after) < before+2 {
		t.Fatalf("prompt box did not grow with wrapped text: %d rows before, %d after", before, len(after))
	}
	if !strings.HasPrefix(stripANSI(after[len(after)-1]), "└") {
		t.Fatalf("bottom border lost while growing: %q", after[len(after)-1])
	}
}

// TestScreenFitsWithPromptBox: the composed frame never exceeds the terminal
// height — the chat viewport has to give back the rows the box occupies.
func TestScreenFitsWithPromptBox(t *testing.T) {
	const rows = 20
	r := newRig(t).size(80, rows).seed("kurze antwort")
	if got := len(r.lines()); got > rows {
		t.Fatalf("frame is %d rows, terminal has %d — box would be clipped", got, rows)
	}
}

// TestWorkingLineCentered: die "antwort läuft …"-Zeile sitzt MITTIG über
// der Eingabebox — gleicher Rand links und rechts, nie breiter als das
// Terminal.
func TestWorkingLineCentered(t *testing.T) {
	r := newRig(t)
	r.m.width = 41
	r.m.startTurn()
	wl := r.m.workingLine()
	if wl == "" {
		t.Fatal("workingLine leer im laufenden Turn")
	}
	inner := strings.TrimSpace(wl)
	pad := len(wl) - len(strings.TrimLeft(wl, " "))
	if inner == "" || pad != (41-lipgloss.Width(inner))/2 {
		t.Fatalf("nicht mittig: pad=%d inner=%q (Zeile %q)", pad, inner, wl)
	}
	if strings.Contains(wl, "\n") {
		t.Fatalf("workingLine muss EINE Zeile sein: %q", wl)
	}
}

// TestInputBorderNeonWhileRunning: im laufenden Turn tragen NUR die Rahmen-
// zeichen den Neon-Verlauf Lila -> Magenta (TrueColor); Glyphen-Layout und
// Inhalt bleiben unangetastet, im Leerlauf gibt es keinen Gradienten.
func TestInputBorderNeonWhileRunning(t *testing.T) {
	withTrueColor(t)
	r := newRig(t)
	r.m.cwd = "/ws/projekt"
	r.m.inputFocus = false // Blur: bs ist Lavendel — so ist der Label-Check scharf
	idle := r.m.inputView()
	r.m.startTurn()
	r.m.neonLevel = 1 // volles Band (der Fade-In startet bei 0)
	hot := r.m.inputView()
	if stripANSI(idle) != stripANSI(hot) {
		t.Fatalf("Glyphen duerfen sich durch den Gradienten nicht aendern:\nidle=%q\nhot=%q", stripANSI(idle), stripANSI(hot))
	}
	// Band: Cornflower-Start #6C9EFF = (108,158,255), Magenta-Ende
	// #FF00CC = (255,0,204). Beide nur im laufenden Turn (im Leerlauf ist
	// der Rahmen Lavendel/Fokus — Magenta kommt dort nie vor).
	for _, seq := range []string{"38;2;108;158;255", "38;2;255;0;204"} {
		if !strings.Contains(hot, seq) {
			t.Fatalf("Gradientenfarbe %q fehlt im laufenden Turn", seq)
		}
	}
	for _, seq := range []string{"38;2;255;0;204"} {
		if strings.Contains(idle, seq) {
			t.Fatalf("Gradientenfarbe %q darf im Leerlauf nicht auftreten", seq)
		}
	}
	// Das Workspace-Label (Icon + Pfad) bleibt beim Lavendel-bs — der
	// gerundete TrueColor-Wert von #B4BEFE (179;190;254) liegt ausserhalb
	// der Band: Ihr Gruen-Kanal 190 uebersteigt die Bandbreite (0..158).
	if !strings.Contains(hot, "38;2;179;190;254") {
		t.Fatalf("Label hat nicht den ruhigen Rahmenstil behalten: %q", hot)
	}
}

// TestInputBorderNeonFlows: das Neon-Band WANDERT — verschiedene Phasen
// muessen verschiedene Farbbelegungen liefern (Glyphen identisch), und
// jede Zelle bleibt in der Lila-Magenta-Bandbreite des Ping-Pong-Verlaufs.
func TestInputBorderNeonFlows(t *testing.T) {
	withTrueColor(t)
	r := newRig(t)
	r.m.inputFocus = false
	r.m.startTurn()
	r.m.neonLevel = 1 // volles Band (der Fade-In startet bei 0)
	a := r.m.inputView()
	r.m.neonPhase = 7
	b := r.m.inputView()
	if stripANSI(a) != stripANSI(b) {
		t.Fatal("Glyphen duerfen sich durch die Phase nicht aendern")
	}
	if a == b {
		t.Fatal("Phasenversatz muss das Farbband bewegen")
	}
	// ╯ liegt bei Phase 0 am Band-Mittelpunkt: reines Magenta.
	if !strings.Contains(a, "38;2;255;0;204m╯") {
		t.Fatalf("╯ sollte bei Phase 0 im reinen Magenta stehen: %q", a)
	}
}

// TestNeonFadeInOut: Das Band blendet beim Turnstart EIN (Level steigt pro
// Takt), nach endTurn WIEDER AUS (Level faellt bis 0), der Ticker verstopft
// erst danach — und bei Level 0 ist der Rahmen byte-identisch zum Leerlauf.
func TestNeonFadeInOut(t *testing.T) {
	withTrueColor(t)
	r := newRig(t)
	r.m.inputFocus = false
	idle := r.m.inputView()
	r.m.startTurn()
	if r.m.neonLevel != 0 {
		t.Fatalf("Fade-In startet bei 0, ist %v", r.m.neonLevel)
	}
	r.do(spinnerTickMsg{})
	if r.m.neonLevel <= 0 || r.m.neonLevel >= 1 {
		t.Fatalf("nach einem Takt muss das Level zwischen 0 und 1 liegen: %v", r.m.neonLevel)
	}
	for i := 0; i < 20 && r.m.neonLevel < 1; i++ {
		r.do(spinnerTickMsg{})
	}
	if r.m.neonLevel != 1 {
		t.Fatalf("Level sollte 1 erreichen: %v", r.m.neonLevel)
	}
	// Phase läuft mit neonPhaseStep pro Takt.
	before := r.m.neonPhase
	r.do(spinnerTickMsg{})
	if r.m.neonPhase-before != neonPhaseStep {
		t.Fatalf("Phase sollte pro Takt %d Zellen wandern, delta=%d", neonPhaseStep, r.m.neonPhase-before)
	}
	// Fade-out: Ticks laufen nach endTurn weiter, Level sinkt bis 0.
	r.m.endTurn()
	for i := 0; i < 20 && r.m.neonLevel > 0; i++ {
		r.do(spinnerTickMsg{})
	}
	if r.m.neonLevel != 0 {
		t.Fatalf("Level sollte auf 0 ausfaden: %v", r.m.neonLevel)
	}
	if got := r.m.inputView(); got != idle {
		t.Fatalf("bei Level 0 muss der Rahmen dem Leerlauf entsprechen:\n%q\n%q", got, idle)
	}
	// Zwischenstand: Level 0.5 ist ein Blend — weder Leerlauf noch volles Band.
	r.m.neonLevel = 0.5
	half := r.m.inputView()
	if half == idle {
		t.Fatal("Halbes Level muss sich sichtbar vom Leerlauf unterscheiden")
	}
	r.m.neonLevel = 1
	if r.m.inputView() == half {
		t.Fatal("Volles Band muss sich vom halben Blend unterscheiden")
	}
}
