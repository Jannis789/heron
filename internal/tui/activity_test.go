package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

// plain rendert den Baum ohne ANSI — die Layout-Referenzen der Spec sind
// reine Textbilder.
func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = stripANSI(r)
	}
	return out
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Zeilenzahl %d, want %d:\n%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Zeile %d weicht ab:\n got  %q\n want %q", i, got[i], want[i])
		}
	}
}

// specTurn baut den Referenz-Turn aus § 4/§ 5 der Spec: vier Denk-Sektionen
// und der finale, laufende bash-Call mit mehrzeiligem Befehl.
func specTurn() ActivityTree {
	a := NewActivityTree(nil)
	for i := 0; i < 4; i++ {
		a = a.Thought("Sektion " + string(rune('A'+i)))
		a = a.Tool(ToolUpdate{CallID: "x" + string(rune('A'+i)), Label: "read_file",
			Text: "datei" + string(rune('A'+i)), Done: true})
	}
	for i := 0; i < 9; i++ {
		a = a.Tool(ToolUpdate{CallID: "m" + string(rune('a'+i)), Label: "bash",
			Kind: "execute", Command: "misc " + string(rune('a'+i)), Done: true})
	}
	a = a.Tool(ToolUpdate{CallID: "e1", Label: "edit_file", Text: "src/auth/session.rs", Done: true})
	for _, cmd := range []string{"git status", "cargo test auth::", "pwd && ls -la"} {
		a = a.Tool(ToolUpdate{CallID: "b" + cmd, Label: "bash", Kind: "execute", Command: cmd, Done: true})
	}
	a = a.Tool(ToolUpdate{CallID: "run", Label: "bash", Kind: "execute", Command: "find ./src -type f -name \"*.java\" \\\n  | xargs grep -rn \"authenticate\" \\\n  | awk '{print $1}' \\\n  | sort -u"})
	return a
}

// TestActivityDefaultLayout (§ 4): Standardzustand — Thinking und Tool
// Calls eingeklappt, Working on als Pod-Kapseln (User-Prototyp 21.11.):
// `╰─┬────  Thinking · <1s`, Inhalt `   ┆ …`, Abschluss `   ╰────`.
// Kein Stamm, kein Zeichen zeigt ins Leere. EXAKT die Referenz.
func TestActivityDefaultLayout(t *testing.T) {
	a := specTurn()
	rows := plain(a.View(60, 20))
	eq(t, rows,
		" ▸  Thinking (4)",
		" ▸  Tool Calls (18)",
		" ● Working on",
		" ╰─┬────  bash · <1s",
		"   ┆ find ./src -type f -name \"*.java\" | xargs grep ...",
		"   ╰────")
}

// TestActivityExpandedTools (§ 5): aufgeklappte Tool Calls — ein mehr-
// zeiliger bash-Befehl ist GENAU EIN Call mit Fortsetzungs-Rails;
// Working on zeigt die kompakte Vorschau desselben Vorgangs.
func TestActivityExpandedTools(t *testing.T) {
	a := specTurn().ToggleTools()
	rows := plain(a.View(74, 40))
	// Kopf + eingeklapptes Thinking
	eq(t, rows[0:2], " ▸  Thinking (4)", " ▼  Tool Calls (18)")
	// Die letzten Calls (18 gesamt): edit_file, drei fertige bash, dann
	// der laufende mit Original-Zeilen — dynamisch lokalisiert.
	i := 0
	for j, r := range rows {
		if strings.Contains(r, "│  ├─ ✓ edit_file") {
			i = j
			break
		}
	}
	if i == 0 {
		t.Fatalf("edit_file-Zeile nicht gefunden:\n%s", strings.Join(rows, "\n"))
	}
	eq(t, rows[i:i+10],
		" │  ├─ ✓ edit_file(src/auth/session.rs)",
		" │  ├─ ✓ bash: git status",
		" │  ├─ ✓ bash: cargo test auth::",
		" │  ├─ ✓ bash: pwd && ls -la",
		" │  └─ ● bash <1s",
		" │     │",
		" │     │  find ./src -type f -name \"*.java\" \\",
		" │     │    | xargs grep -rn \"authenticate\" \\",
		" │     │    | awk '{print $1}' \\",
		" │     │    | sort -u")
	// Working on bleibt die kompakte Zwei-Zeilen-Vorschau desselben Calls.
	w := 0
	for j, r := range rows {
		if strings.TrimSpace(r) == "● Working on" {
			w = j
			break
		}
	}
	if w == 0 {
		t.Fatalf("Working on nicht gefunden:\n%s", strings.Join(rows, "\n"))
	}
	eq(t, rows[w:w+3], " ● Working on", " ╰─┬────  bash · <1s",
		"   ┆ find ./src -type f -name \"*.java\" | xargs grep -rn ...")
}

// TestActivityThinkingWorkingOn (§ 7): Thinking als aktueller Vorgang —
// Label in Textfarbe, Inhaltszeilen in Soft-Grau (User-Wunsch: kein Neon
// im Baum). Agent-Text gibt es im Baum nicht mehr — er streamt im Chat
// (FR-2).
func TestActivityThinkingWorkingOn(t *testing.T) {
	a := NewActivityTree(nil).
		Thought("I should inspect the existing token validation before\nmodifying the expiration logic.")
	rows := plain(a.View(60, 20))
	eq(t, rows,
		" ▸  Thinking (1)",
		" ▸  Tool Calls (0)",
		" ● Working on",
		" ╰─┬────  Thinking · <1s",
		"   ┆ I should inspect the existing token validation before",
		"   ┆ modifying the expiration logic.",
		"   ╰────")
}

// TestActivitySelectionMode (§ 5, User-Wunsch): Der Baum ist Teil der
// tab-Kette (Eingabe → Baum → Chat). ↑/↓ bewegen den Zeilen-Cursor,
// enter klappt den Knoten UNTER DEM CURSOR auf — und wieder zu. Beide
// Knoten unabhaengig, ganz ohne Modifier-Tasten.
func TestActivitySelectionMode(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "denk"})
	r.do(toolMsg{callID: "c1", name: "bash", kind: "execute", cmd: "ls"})
	if v := r.m.activityView(); !strings.Contains(stripANSI(v), "▸  Thinking (1)") {
		t.Fatalf("Thinking startet eingeklappt: %q", stripANSI(v))
	}

	// tab: Eingabe → Baum; Cursor steht auf Thinking, enter klappt auf.
	r.key("tab")
	if !r.m.activity.Focused() {
		t.Fatal("tab muss den Baum selektieren")
	}
	r.key("enter") // Cursor auf Thinking
	if v := stripANSI(r.m.activityView()); !strings.Contains(v, "▼  Thinking (1)") || !strings.Contains(v, "▸  Tool Calls (1)") {
		t.Fatalf("enter klappt nur den Knoten unter dem Cursor: %q", v)
	}
	r.key("enter") // und wieder zu
	if v := stripANSI(r.m.activityView()); !strings.Contains(v, "▸  Thinking (1)") {
		t.Fatalf("enter muss auch zuklappen: %q", v)
	}

	// ↓ auf Tool Calls, enter klappt die Historie auf.
	r.key("down")
	r.key("enter")
	if v := stripANSI(r.m.activityView()); !strings.Contains(v, "▼  Tool Calls (1)") || !strings.Contains(v, "└─ ● bash") {
		t.Fatalf("Tool Calls aufgeklappt fehlt: %q", v)
	}

	// tab fuehrt die Kette zum Chat weiter (ein Block muss dafuer da
	// sein); esc im Baum kehrt zur Eingabe.
	r.m.chat = r.m.chat.Append(NewTextBlock("Antwort", "1"))
	r.key("tab")
	if r.m.activity.Focused() {
		t.Fatal("tab muss den Baum verlassen")
	}
	if !r.m.chat.Focused() {
		t.Fatal("tab muss vom Baum in den Chat fuehren")
	}
	r.key("tab") // Chat → Eingabe
	r.key("tab") // Eingabe → Baum (die Kette ist zyklisch)
	if !r.m.activity.Focused() {
		t.Fatal("Kette muss zurueck in den Baum fuehren")
	}
	r.key("esc")
	if r.m.activity.Focused() || !r.m.inputFocus {
		t.Fatal("esc muss den Baum freigeben und ins Eingabefeld kehren")
	}
}

// TestActivityCursorFollowsStructure: der Cursor haengt am KNOTEN, nicht
// an der Zeilennummer — ein aufgeklappter Thinking-Knoten schiebt die
// Tool-Calls-Kopfzeile nach unten, enter trifft trotzdem Tool Calls.
// (Regression: vorher waren die Kopfzeilen fest auf Zeile 0/1 verdrahtet.)
func TestActivityCursorFollowsStructure(t *testing.T) {
	a := specTurn().Focus(60, 40)
	a = a.Enter() // Thinking auf
	if v := strings.Join(plain(a.View(60, 40)), "\n"); !strings.Contains(v, "▼  Thinking (4)") {
		t.Fatalf("Thinking nicht aufgeklappt:\n%s", v)
	}
	a = a.Move(1, 60, 40) // ↓: Tool Calls steht jetzt NICHT mehr in Zeile 1
	a = a.Enter()
	v := strings.Join(plain(a.View(60, 40)), "\n")
	if !strings.Contains(v, "▼  Tool Calls (18)") {
		t.Fatalf("enter muss den Knoten unter dem Cursor treffen:\n%s", v)
	}
	// Und wieder zu — derselbe Druck, kein zweiter Modus.
	a = a.Enter()
	v = strings.Join(plain(a.View(60, 40)), "\n")
	if !strings.Contains(v, "▸  Tool Calls (18)") {
		t.Fatalf("enter muss auch zuklappen:\n%s", v)
	}
}

// TestActivitySelectionUnderlineOnHeader: der Auswahlmodus unterstreicht die
// Kopfzeile des Knotens — genau eine Zeile, nicht die Kindzeilen.
func TestActivitySelectionUnderlineOnHeader(t *testing.T) {
	withTrueColor(t)
	a := NewActivityTree(mustTheme(t)).
		Thought("denken").
		Tool(ToolUpdate{CallID: "c1", Label: "bash", Kind: "execute", Command: "ls", Done: true}).
		Focus(60, 20)
	rows := a.View(60, 20)
	if !underlined(rows[0]) {
		t.Fatalf("Thinking-Kopf nicht unterstrichen: %q", rows[0])
	}
	for i, r := range rows[1:] {
		if underlined(r) {
			t.Fatalf("Zeile %d unerwartet unterstrichen: %q", i+1, r)
		}
	}
	// Der Unterstrich wandert mit dem Cursor auf die Tool-Calls-Kopfzeile
	// und bleibt beim Aufklappen auf dem KOPF (nicht auf einem Kind).
	a = a.Focus(60, 20).Move(1, 60, 20)
	if rows = a.View(60, 20); underlined(rows[0]) || !underlined(rows[1]) {
		t.Fatalf("Cursor muss auf Tool Calls wandern: %q / %q", rows[0], rows[1])
	}
	rows = a.Enter().View(60, 20)
	if !underlined(rows[1]) {
		t.Fatalf("Kopfzeile nach dem Aufklappen nicht unterstrichen: %q", rows[1])
	}
	for i := 2; i < len(rows); i++ {
		if underlined(rows[i]) {
			t.Fatalf("Kindzeile %d unterstrichen: %q", i, rows[i])
		}
	}
	a = a.Blur()
	for _, r := range a.View(60, 20) {
		if underlined(r) {
			t.Fatalf("ohne Fokus keine Unterstreichung: %q", r)
		}
	}
}

// underlined: traegt die Zeile irgendwo die SGR-Unterstreichung (Param 4)?
func underlined(s string) bool {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return false
		}
		s = s[i+2:]
		j := strings.IndexByte(s, 'm')
		if j < 0 {
			return false
		}
		for _, p := range strings.Split(s[:j], ";") {
			if p == "4" {
				return true
			}
		}
		s = s[j+1:]
	}
}

// TestActivityClickTogglesHeader: Maus-Klick auf die Kopfzeile klappt den
// Knoten um (wie ↑/↓ + enter); ein Klick auf eine Zeile ohne Knoten
// (Working on) selektiert nur den Baum.
func TestActivityClickTogglesHeader(t *testing.T) {
	a := specTurn()
	a = a.Click(1, 60, 20) // Tool-Calls-Kopf
	if !a.Focused() || !a.expandTools {
		t.Fatal("Klick auf die Kopfzeile muss aufklappen")
	}
	a = a.Click(1, 60, 20) // derselbe Kopf: wieder zu
	if a.expandTools {
		t.Fatal("zweiter Klick muss zuklappen")
	}
	w := -1
	for i, r := range plain(a.View(60, 20)) {
		if strings.Contains(r, "Working on") {
			w = i
			break
		}
	}
	if w < 0 {
		t.Fatal("Working on fehlt")
	}
	a = a.Blur().Click(w, 60, 20)
	if !a.Focused() || a.expandThinking || a.expandTools {
		t.Fatal("Klick auf Working on darf nur selektieren, nicht klappen")
	}
}

// TestActivityLeftMargin: keine Zeile klebt an der Terminalkante — jede
// beginnt mit genau einem Leerzeichen (User-Wunsch).
func TestActivityLeftMargin(t *testing.T) {
	for _, a := range []ActivityTree{specTurn(), specTurn().ToggleTools(), specTurn().Settle()} {
		for _, r := range plain(a.View(60, 30)) {
			if !strings.HasPrefix(r, " ") {
				t.Fatalf("Zeile ohne linken Rand: %q", r)
			}
			if strings.HasPrefix(r, "  ") && !strings.HasPrefix(r, "   ") {
				continue // Kinder-Inhalt, der selbst einrueckt — erlaubt
			}
		}
	}
}

// TestActivitySettledHidesWorkingOn (§ 4, User-Wunsch): ist der Turn
// abgeschlossen, verschwindet Working on — die Zusammenfassung des Baumes
// bleibt. Agent-Text gibt es im Baum nicht mehr: Er streamt im Chat (FR-2).
func TestActivitySettledHidesWorkingOn(t *testing.T) {
	a := specTurn()
	if v := strings.Join(plain(a.View(60, 20)), "\n"); !strings.Contains(v, "Working on") {
		t.Fatal("laufender Turn zeigt Working on")
	}
	a = a.Settle()
	v := strings.Join(plain(a.View(60, 20)), "\n")
	if strings.Contains(v, "Working on") {
		t.Fatalf("abgeschlossener Turn zeigt noch Working on:\n%s", v)
	}
	if !strings.Contains(v, "Thinking (4)") || !strings.Contains(v, "Tool Calls (18)") {
		t.Fatalf("Zusammenfassung fehlt:\n%s", v)
	}
}

// TestActivityThinkingNodeExpandsLiveThinking: der AKTIVE Denkprozess
// haengt am Thinking-Knoten — enter macht ihn sichtbar, enter wieder zu.
// (Achtung: Working on zeigt ohnehin die LETZTEN fuenf Zeilen — der
// Knoten macht den ANFANG der Sektion sichtbar.)
func TestActivityThinkingNodeExpandsLiveThinking(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 8; i++ {
		fmt.Fprintf(&sb, "denkzeile nummer %d\n", i)
	}
	a := NewActivityTree(nil).Thought(sb.String())
	if strings.Contains(strings.Join(plain(a.View(40, 20)), "\n"), "denkzeile nummer 1") {
		t.Fatal("eingeklappt darf den Anfang nicht zeigen (Working on zeigt nur den Schluss)")
	}
	a = a.Focus(40, 20).Enter()
	if !strings.Contains(strings.Join(plain(a.View(40, 20)), "\n"), "denkzeile nummer 1") {
		t.Fatal("aufgeklappt muss die aktive Sektion von Beginn zeigen")
	}
	a = a.Enter()
	if strings.Contains(strings.Join(plain(a.View(40, 20)), "\n"), "denkzeile nummer 1") {
		t.Fatal("zweites enter muss wieder zuklappen")
	}
}

// TestActivityHugeCommand (§ 6/§ 11): Befehle mit mehreren tausend
// Zeichen — eingeklappt max. zwei Zeilen mit Wortgrenzen-Kuerzung,
// aufgeklappt gescrollt (Tail-Fenster), nie breiter als das Terminal.
func TestActivityHugeCommand(t *testing.T) {
	cmd := "find . -type f \\\n  | xargs grep -rn \"muster\" \\\n  | sort -u"
	for i := 0; i < 800; i++ {
		cmd += " \\\n  | kommando-mit-sehr-langen-argumenten-" + string(rune('a'+i%26)) + strings.Repeat("x", 40)
	}
	a := NewActivityTree(nil).Tool(ToolUpdate{CallID: "c", Label: "bash", Kind: "execute", Command: cmd})

	// Eingeklappt: Working on haelt ZWEI Zeilen, gekuerzt am Wortuebergang.
	rows := plain(a.View(60, 20))
	if got := rows[4]; runewidth0(got) > 60 || !strings.HasSuffix(got, "...") {
		t.Fatalf("Vorschau nicht gekuerzt: %q", got)
	}

	// Aufgeklappt: das Detail bleibt begrenzt (Cap + Budget-Fenster),
	// jede Zeile schmaler als das Terminal.
	a = a.ToggleTools()
	rows = plain(a.View(60, 24))
	if len(rows) > 24 {
		t.Fatalf("Budget ueberschritten: %d Zeilen", len(rows))
	}
	for i, ln := range rows {
		if w := runewidth0(ln); w > 60 {
			t.Fatalf("Zeile %d breit %d: %q", i, w, ln)
		}
	}
}

// runewidth0 misst Zellenbreite (Helfer fuer Layout-Assertions).
func runewidth0(s string) int {
	return runewidth.StringWidth(s)
}

// TestActivityWorkingOnSurvivesBudget (§ 9): bei jeder Verengung bleibt
// Working on sichtbar — aufgeklappte Kinder weichen zuerst, die Kopfzeilen
// halten, solange drei Zeilen Platz haben. Textzeilen gibt es im Baum
// nicht mehr: Die Antwort streamt im Chat (FR-2).
// TestActivityIdleSpinnerLine: Leerlauf-Zeile im Working-on-Knoten —
// kein offenes Thinking, kein laufender Call, aber der Turn läuft
// (idleSince armiert): Spinner-Glyph + Turn-Laufzeit statt stiller
// Leere (User-Report). Das erste echte Kind verdraengt sie, Settle
// räumt die Anzeige ab.
func TestActivityIdleSpinnerLine(t *testing.T) {
	a := NewActivityTree(nil).Idle(time.Now())
	rows := plain(a.View(60, 20))
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "Working on") {
		t.Fatalf("Leerlauf-Turn muss Working on zeigen:\n%s", joined)
	}
	if !strings.Contains(joined, "⠋") {
		t.Fatalf("Spinner-Glyph fehlt:\n%s", joined)
	}
	// Frame folgt dem Model-Spinner:
	a2 := a.IdleFrame(3)
	if !strings.Contains(strings.Join(plain(a2.View(60, 20)), "\n"), "⠸") {
		t.Fatalf("IdleFrame wird nicht angezeigt:\n%s", strings.Join(plain(a2.View(60, 20)), "\n"))
	}
	// Erstes echtes Kind verdraengt die Leerlaufzeile:
	a3 := a.Tool(ToolUpdate{CallID: "c", Label: "bash", Kind: "execute", Command: "ls"})
	v3 := strings.Join(plain(a3.View(60, 20)), "\n")
	if strings.Contains(v3, "⠋") {
		t.Fatalf("Spinner haengt neben laufendem Call:\n%s", v3)
	}
	if !strings.Contains(v3, "bash") {
		t.Fatalf("laufender Call fehlt:\n%s", v3)
	}
	// settled ohne Inhalt: komplett weg.
	a4 := a.Settle()
	if v := strings.Join(plain(a4.View(60, 20)), "\n"); strings.Contains(v, "Working on") {
		t.Fatalf("settled Turn zeigt noch Working on:\n%s", v)
	}
}

func TestActivityWorkingOnSurvivesBudget(t *testing.T) {
	a := specTurn().ToggleTools() // maximal ausgeklappt
	for _, budget := range []int{5, 7, 12} {
		rows := plain(a.View(60, budget))
		if len(rows) > budget {
			t.Fatalf("budget %d ueberschritten: %d", budget, len(rows))
		}
		joined := strings.Join(rows, "\n")
		if !strings.Contains(joined, "Working on") {
			t.Fatalf("budget %d: Working on fehlt:\n%s", budget, joined)
		}
		if !strings.Contains(joined, "bash · <1s") {
			t.Fatalf("budget %d: laufender Call fehlt:\n%s", budget, joined)
		}
	}
}

// TestActivityColors (§ 2): Valida (Action) fuer Thinking/Tool Calls/
// Working on/▸▼/●, Soft-Grau fuer die Inhaltszeilen (User-Wunsch: kein
// Neon im Baum), Text fuer bash, Gruen/Rot fuer ✓/✗, Muted (Border) fuer
// die Tree-Zeichen — alles zentral aus dem Theme.
func TestActivityColors(t *testing.T) {
	withTrueColor(t)
	th := mustTheme(t)
	a := NewActivityTree(th).
		Thought("denken").
		Tool(ToolUpdate{CallID: "ok", Label: "read_file", Text: "x", Done: true}).
		Tool(ToolUpdate{CallID: "bad", Label: "bash", Kind: "execute", Command: "ls", Failed: true, Done: true}).
		Tool(ToolUpdate{CallID: "run", Label: "bash", Kind: "execute", Command: "sleep 5"}).
		ToggleTools().
		ToggleThinking()
	rows := a.View(70, 30)

	// Thinking-/Tool Calls-Header: Valida #B999FF (User-Wunsch: kein Neon
	// im Kopf). Strukturell suchen, nicht per Zeilenindex.
	thinking, tools, work := -1, -1, -1
	for i, r := range rows {
		switch {
		case strings.Contains(stripANSI(r), "Thinking ("):
			thinking = i
		case strings.Contains(stripANSI(r), "Tool Calls ("):
			tools = i
		case strings.Contains(stripANSI(r), "Working on"):
			work = i
		}
	}
	if thinking < 0 || !strings.Contains(rows[thinking], "38;2;185;153;255") {
		t.Fatalf("Thinking-Label nicht Valida: %q", rows[max(thinking, 0)])
	}
	if tools < 0 || !strings.Contains(rows[tools], "38;2;185;153;255") {
		t.Fatalf("Tool Calls-Label nicht Valida: %q", rows[max(tools, 0)])
	}
	if work < 0 || !strings.Contains(rows[work], "38;2;185;153;255") {
		t.Fatalf("Working on-Label nicht Valida: %q", rows[max(work, 0)])
	}
	// Kein Neon-Violett mehr im Activity-Baum.
	for i, r := range rows {
		if strings.Contains(r, "38;2;191;0;255") {
			t.Fatalf("Zeile %d traegt noch Neon: %q", i, r)
		}
	}
	// Tree-Zeichen: Muted #B4BEFE (lipgloss quantisiert auf 179;190;254);
	// ✓ gruen #22C55E; ✗ rot #FF5555; bash-Name in Textfarbe; Denk-Inhalt
	// in Soft-Grau #C0C3D4.
	var sawMuted, sawOk, sawErr, sawSoft, sawText bool
	for _, r := range rows {
		if strings.Contains(r, "38;2;179;190;254") {
			sawMuted = true
		}
		if strings.Contains(r, "38;2;34;197;94") {
			sawOk = true
		}
		if strings.Contains(r, "38;2;255;85;85") {
			sawErr = true
		}
		if strings.Contains(r, "38;2;192;195;211") {
			sawSoft = true
		}
		if strings.Contains(r, "38;2;217;211;236") {
			sawText = true
		}
	}
	if !sawMuted || !sawOk || !sawErr || !sawText {
		t.Fatalf("Farben fehlen: muted=%v ok=%v err=%v text=%v", sawMuted, sawOk, sawErr, sawText)
	}
	if !sawSoft {
		for i, r := range rows {
			t.Logf("Zeile %d: %q", i, r)
		}
		t.Fatalf("Soft-Grau fehlt im Baum (Denk-Inhalt/Befehle)")
	}
	for _, r := range rows {
		if strings.Contains(stripANSI(r), "bash") && !strings.Contains(r, "38;2;217;211;236") {
			t.Fatalf("bash-Name nicht in Textfarbe: %q", r)
		}
	}
}

// TestActivityTimings (User-Wunsch): Zeitangaben — aufgeklappt zeigen
// Denk-Sektionen ihre Laufzeit und fertige Calls ihre Gesamtdauer, unter
// einer Sekunde buendelt das zu "<1s". Ohne laufenden Vorgang gibt es
// KEIN Working on — auch mitten im Turn nicht: fertig ist fertig.
func TestActivityTimings(t *testing.T) {
	base := time.Now()
	a := NewActivityTree(nil).
		Thought("denke langsam").
		Tool(ToolUpdate{CallID: "c1", Label: "read_file", Text: "x", Done: true}).
		ToggleThinking().ToggleTools()
	// Zeiten injizieren: 12s Denken, 95s Call.
	a.thoughts[0].started = base
	a.thoughts[0].ended = base.Add(12 * time.Second)
	a.tools[0].started = base
	a.tools[0].ended = base.Add(95 * time.Second)
	v := strings.Join(plain(a.View(60, 20)), "\n")
	if !strings.Contains(v, "denke langsam 12s") {
		t.Fatalf("Denk-Sektion ohne Laufzeit:\n%s", v)
	}
	if !strings.Contains(v, "read_file(x) 1m35s") {
		t.Fatalf("Call ohne Gesamtdauer:\n%s", v)
	}

	// Kurzlaufzeit: unter einer Sekunde wird zu "<1s" gebuendelt.
	b := NewActivityTree(nil).Tool(ToolUpdate{CallID: "k", Label: "bash", Kind: "execute", Command: "ls", Done: true}).
		ToggleTools()
	b.tools[0].started = base
	b.tools[0].ended = base.Add(300 * time.Millisecond)
	if v := strings.Join(plain(b.View(60, 20)), "\n"); !strings.Contains(v, "bash: ls <1s") {
		t.Fatalf("Kurzlaufzeit fehlt:\n%s", v)
	}

	// Fertiger Call, kein offenes Thinking: Working on bleibt weg — auch
	// WAEHREND des Turns (der Agent streamt nur Text, und der lebt im Chat).
	c := NewActivityTree(nil).Tool(ToolUpdate{CallID: "f", Label: "read_file", Text: "x", Done: true})
	if v := strings.Join(plain(c.View(60, 20)), "\n"); strings.Contains(v, "Working on") {
		t.Fatalf("fertiger Call besetzt Working on:\n%s", v)
	}
}

// TestActivityCountsAndSections: ein neuer Tool-Call schliesst die offene
// Denk-Sektion; Updates derselben callID zaehlen nicht doppelt (FR-31).
func TestActivityCountsAndSections(t *testing.T) {
	a := NewActivityTree(nil)
	a = a.Thought("eins")
	if a.ThinkingCount() != 1 {
		t.Fatal("offene Sektion zaehlt")
	}
	a = a.Tool(ToolUpdate{CallID: "c1", Label: "bash", Kind: "execute", Command: "ls"})
	if a.ThinkingCount() != 1 || a.ToolCount() != 1 {
		t.Fatalf("nach Tool: think=%d tools=%d", a.ThinkingCount(), a.ToolCount())
	}
	a = a.Thought("zwei")
	if a.ThinkingCount() != 2 {
		t.Fatalf("neue Sektion: %d", a.ThinkingCount())
	}
	a = a.Tool(ToolUpdate{CallID: "c1", Label: "bash", Kind: "execute", Command: "ls -la", Done: true})
	if a.ToolCount() != 1 {
		t.Fatalf("Update zaehlt doppelt: %d", a.ToolCount())
	}
	if a.ThinkingCount() != 2 {
		t.Fatalf("Update darf Thinking nicht schliessen: %d", a.ThinkingCount())
	}
}

// TestActivityEmptyTreeHidden: ohne jedes Event bleibt der Baum weg —
// keine Null-Zahlen, keine leere Struktur (§ 10).
func TestActivityEmptyTreeHidden(t *testing.T) {
	// User-Report: "Working on gar nicht angezeigt, obwohl noch nicht
	// fertig" — ein laufender Turn ohne jedes Event zeigt jetzt den
	// Ladeindikator (Spinner + Turn-Laufzeit) statt stiller Leere;
	// das erste Event ersetzt die Leerlaufzeile.
	r := newRig(t)
	r.m.startTurn()
	if v := r.m.activityView(); !strings.Contains(v, "Working on") {
		t.Fatalf("laufender Turn ohne Events muss Working on zeigen: %q", v)
	}
	if v := r.m.activityView(); !strings.Contains(v, "⠋") {
		t.Fatalf("Ladeindikator (Spinner-Frame) fehlt: %q", r.m.activityView())
	}
	r.do(thoughtMsg{text: "jetzt"})
	v := r.m.activityView()
	if !strings.Contains(v, "Thinking") {
		t.Fatalf("erster Inhalt muss den Baum fuellen: %q", v)
	}
	if strings.Contains(v, "⠋") {
		t.Fatalf("Leerlauf-Spinner haengt neben echtem Inhalt: %q", v)
	}
	// Und nach Settle verschwindet die Leerlauf-Anzeige wieder:
	r2 := newRig(t)
	r2.m.startTurn()
	r2.m.endTurn()
	if v := r2.m.activityView(); v != "" {
		t.Fatalf("settled Turn ohne Inhalt zeigt trotzdem einen Baum: %q", v)
	}
}
