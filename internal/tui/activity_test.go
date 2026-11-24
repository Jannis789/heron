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
	for _, cmd := range []string{"git add -A", "cargo test auth::", "pwd && ls -la"} {
		a = a.Tool(ToolUpdate{CallID: "b" + cmd, Label: "bash", Kind: "execute", Command: cmd, Done: true})
	}
	a = a.Tool(ToolUpdate{CallID: "run", Label: "bash", Kind: "execute", Command: "env find ./src -type f -name \"*.java\" \\\n  | xargs grep -rn \"authenticate\" \\\n  | awk '{print $1}' \\\n  | sort -u"})
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
		" ▸ "+iconRead+" Read (4)",
		" ▸ "+iconEdit+" Edit (1)",
		" ▸ "+iconTool+" Execute (13)",
		" ● Working on",
		" ╰─┬────  bash · <1s",
		"   ┆ env find ./src -type f -name \"*.java\" | xargs ...",
		"   ╰────")
}

// TestActivityExpandedTools (§ 5): aufgeklappte Tool Calls — ein mehr-
// zeiliger bash-Befehl ist GENAU EIN Call mit Fortsetzungs-Rails;
// Working on zeigt die kompakte Vorschau desselben Vorgangs.
func TestActivityExpandedTools(t *testing.T) {
	a := specTurn().ToggleTools()
	rows := plain(a.View(74, 40))
	// Kopf + eingeklapptes Thinking
	eq(t, rows[0:2], " ▸ "+iconThinking+" Thinking (4)", " ▼ "+iconRead+" Read (4)")
	// Die letzten Calls (18 gesamt): edit_file, drei fertige bash, dann
	// der laufende mit Original-Zeilen — dynamisch lokalisiert.
	i := 0
	for j, r := range rows {
		if strings.Contains(r, "✓ bash: git add -A") {
			i = j
			break
		}
	}
	if i == 0 {
		t.Fatalf("bash-Zeilen nicht gefunden:\n%s", strings.Join(rows, "\n"))
	}
	eq(t, rows[i:i+7],
		" │  ├─   ✓ bash: git add -A",
		" │  ├─   ✓ bash: cargo test auth::",
		" │  ├─   ✓ bash: pwd && ls -la",
		" │  └─   ● bash <1s",
		" │     │",
		" │     │  env find ./src -type f -name \"*.java\" \\",
		" │     │    | xargs grep -rn \"authenticate\" \\")
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
		"   ┆ env find ./src -type f -name \"*.java\" | xargs grep -rn ...")
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
	r.do(toolMsg{callID: "c1", name: "bash", kind: "execute", cmd: "make"})
	if v := r.m.activityView(); !strings.Contains(stripANSI(v), "▸  Thinking (1)") {
		t.Fatalf("Thinking startet eingeklappt: %q", stripANSI(v))
	}

	// tab: Eingabe → Baum; Cursor steht auf Thinking, enter klappt auf.
	r.key("tab")
	if !r.m.activity.Focused() {
		t.Fatal("tab muss den Baum selektieren")
	}
	r.key("enter") // Cursor auf Thinking
	if v := stripANSI(r.m.activityView()); !strings.Contains(v, "▼  Thinking (1)") || !strings.Contains(v, "▸ "+iconTool+" Execute (1)") {
		t.Fatalf("enter klappt nur den Knoten unter dem Cursor: %q", v)
	}
	r.key("enter") // und wieder zu
	if v := stripANSI(r.m.activityView()); !strings.Contains(v, "▸  Thinking (1)") {
		t.Fatalf("enter muss auch zuklappen: %q", v)
	}

	// ↓ auf Tool Calls, enter klappt die Historie auf.
	r.key("down")
	r.key("enter")
	if v := stripANSI(r.m.activityView()); !strings.Contains(v, "▼ "+iconTool+" Execute (1)") || !strings.Contains(v, "└─   ● bash") {
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
	// esc in STUFEN (User-Wunsch 23.11.): Execute ist noch aufgeklappt —
	// der ERSTE Druck klappt nur zu (Baum bleibt selektiert), erst der
	// zweite gibt den Baum frei und kehrt ins Eingabefeld.
	r.key("esc")
	if !r.m.activity.Focused() || r.m.inputFocus {
		t.Fatal("esc muss zunaechst nur zuklappen, nicht den Baum verlassen")
	}
	if v := stripANSI(r.m.activityView()); !strings.Contains(v, "▸ "+iconTool+" Execute (1)") {
		t.Fatalf("esc muss Execute zuklappen: %q", v)
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
	// ↓ laeuft zuerst durch die Denk-Sektionen (Reinnavigieren in den
	// aufgeklappten Knoten), danach auf den Read-Kopf — der steht jetzt
	// NICHT mehr in Zeile 1.
	for i := 0; i < 5; i++ {
		a = a.Move(1, 60, 40)
	}
	if a.selThought != 0 && len(a.allThoughts()) > 0 {
		t.Fatal("nach 5x ↓ muss der Cursor auf dem Read-Kopf stehen")
	}
	a = a.Enter()
	v := strings.Join(plain(a.View(60, 40)), "\n")
	if !strings.Contains(v, "▼ "+iconRead+" Read (4)") {
		t.Fatalf("enter muss den Knoten unter dem Cursor treffen:\n%s", v)
	}
	// Und wieder zu — derselbe Druck, kein zweiter Modus.
	a = a.Enter()
	v = strings.Join(plain(a.View(60, 40)), "\n")
	if !strings.Contains(v, "▸ "+iconRead+" Read (4)") {
		t.Fatalf("enter muss auch zuklappen:\n%s", v)
	}
}

// TestActivitySelectionUnderlineOnHeader: der Auswahlmodus unterstreicht die
// Kopfzeile des Knotens — genau eine Zeile, nicht die Kindzeilen. Steigt der
// Cursor in einen Tochter-Eintrag ab, uebernimmt der Auswahl-Steg der Kind-
// zeile das Signal und der Kopf verliert den Unterstrich (User-Report
// 24.11.: Kopf-Unterstrich + Kind-Steg gleichzeitig verwirren).
func TestActivitySelectionUnderlineOnHeader(t *testing.T) {
	withTrueColor(t)
	a := NewActivityTree(mustTheme(t)).
		Thought("denken").
		Tool(ToolUpdate{CallID: "c1", Label: "bash", Kind: "execute", Command: "make", Done: true}).
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
	a = a.Enter()
	rows = a.View(60, 20)
	if !underlined(rows[1]) {
		t.Fatalf("Kopfzeile nach dem Aufklappen nicht unterstrichen: %q", rows[1])
	}
	for i := 2; i < len(rows); i++ {
		if underlined(rows[i]) {
			t.Fatalf("Kindzeile %d unterstrichen: %q", i, rows[i])
		}
	}
	// Abstieg in den Tochter-Eintrag: der Kopf verliert den Unterstrich,
	// nur die Kindzeile traegt den Auswahl-Steg.
	a = a.Move(1, 60, 20)
	rows = a.View(60, 20)
	if underlined(rows[1]) {
		t.Fatalf("Kopfzeile muss beim Kind-Cursor ununterstrichen bleiben: %q", rows[1])
	}
	if !strings.Contains(rows[2], "▶") {
		t.Fatalf("Kindzeile muss den Auswahl-Steg tragen: %q", rows[2])
	}
	// Zurueck auf den Kopf: Unterstrich kehrt zurueck, Steg verschwindet.
	a = a.Move(-1, 60, 20)
	rows = a.View(60, 20)
	if !underlined(rows[1]) {
		t.Fatalf("zurueck auf dem Kopf muss der Unterstrich stehen: %q", rows[1])
	}
	if strings.Contains(rows[2], "▶") {
		t.Fatalf("Kindzeile ohne Cursor darf keinen Auswahl-Steg tragen: %q", rows[2])
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
	a = a.Click(1, 60, 20) // Read-Kopf (erster Kind-Knoten)
	if !a.Focused() || !a.expanded("read") {
		t.Fatal("Klick auf die Kopfzeile muss aufklappen")
	}
	a = a.Click(1, 60, 20) // derselbe Kopf: wieder zu
	if a.expanded("read") {
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
	if !a.Focused() || a.expandThinking || a.expanded("read") {
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
	if !strings.Contains(v, "Thinking (4)") || !strings.Contains(v, "Read (4)") || !strings.Contains(v, "Execute (13)") {
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
	preview := ""
	for _, r := range rows {
		if strings.Contains(r, "┆") {
			preview = r
			break
		}
	}
	if preview == "" || runewidth0(preview) > 60 || !strings.HasSuffix(preview, "...") {
		t.Fatalf("Vorschau nicht gekuerzt: %q", preview)
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
	a3 := a.Tool(ToolUpdate{CallID: "c", Label: "bash", Kind: "execute", Command: "make"})
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
		Tool(ToolUpdate{CallID: "bad", Label: "bash", Kind: "execute", Command: "make", Failed: true, Done: true}).
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
		case strings.Contains(stripANSI(r), "Read ("):
			tools = i
		case strings.Contains(stripANSI(r), "Working on"):
			work = i
		}
	}
	if thinking < 0 || !strings.Contains(rows[thinking], "38;2;185;153;255") {
		t.Fatalf("Thinking-Label nicht Valida: %q", rows[max(thinking, 0)])
	}
	if tools < 0 || !strings.Contains(rows[tools], "38;2;185;153;255") {
		t.Fatalf("Read-Label nicht Valida: %q", rows[max(tools, 0)])
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
	b := NewActivityTree(nil).Tool(ToolUpdate{CallID: "k", Label: "bash", Kind: "execute", Command: "make", Done: true}).
		ToggleTools()
	b.tools[0].started = base
	b.tools[0].ended = base.Add(300 * time.Millisecond)
	if v := strings.Join(plain(b.View(60, 20)), "\n"); !strings.Contains(v, "bash: make <1s") {
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
	a = a.Tool(ToolUpdate{CallID: "c1", Label: "bash", Kind: "execute", Command: "make"})
	if a.ThinkingCount() != 1 || a.ToolCount() != 1 {
		t.Fatalf("nach Tool: think=%d tools=%d", a.ThinkingCount(), a.ToolCount())
	}
	a = a.Thought("zwei")
	if a.ThinkingCount() != 2 {
		t.Fatalf("neue Sektion: %d", a.ThinkingCount())
	}
	a = a.Tool(ToolUpdate{CallID: "c1", Label: "bash", Kind: "execute", Command: "make -j4", Done: true})
	if a.ToolCount() != 1 {
		t.Fatalf("Update zaehlt doppelt: %d", a.ToolCount())
	}
	if a.ThinkingCount() != 2 {
		t.Fatalf("Update darf Thinking nicht schliessen: %d", a.ThinkingCount())
	}
}

// TestActivityUpdateKeepsStartMetadata: ein tool_call_update traegt oft nur
// status/content — Name, Kind und Kommando des Start-Frames duerfen nicht
// verloren gehen (sonst rendert die Zeile "tool(insgesamt 14892)").
func TestActivityUpdateKeepsStartMetadata(t *testing.T) {
	a := NewActivityTree(nil).
		Tool(ToolUpdate{CallID: "c1", Label: "shell", Kind: "execute", Command: "make -j4"})
	a = a.Tool(ToolUpdate{CallID: "c1", Text: "insgesamt 14892", Done: true}).ToggleKind("execute")
	v := strings.Join(plain(a.View(80, 20)), "\n")
	if !strings.Contains(v, "shell: make -j4") {
		t.Fatalf("Name/Kommando verloren:\n%s", v)
	}
	if strings.Contains(v, "tool(insgesamt") {
		t.Fatalf("Fallback-Tool mit Output als Argument:\n%s", v)
	}
}

// TestActivityHidesEmptyThinking: ohne Denk-Chunk gibt es keinen
// "Thinking (0)"-Kopf — der Baum zeigt nur, was wirklich passiert ist.
func TestActivityHidesEmptyThinking(t *testing.T) {
	a := NewActivityTree(nil).Tool(ToolUpdate{CallID: "c", Label: "bash", Kind: "execute", Command: "make", Done: true})
	v := strings.Join(plain(a.View(60, 20)), "\n")
	if strings.Contains(v, "Thinking") {
		t.Fatalf("leerer Thinking-Knoten sichtbar:\n%s", v)
	}
	if !strings.Contains(v, "Execute (1)") {
		t.Fatalf("Execute-Knoten fehlt:\n%s", v)
	}
	// Mit Denk-Chunk bleibt der Kopf.
	b := NewActivityTree(nil).Thought("denk")
	if v := strings.Join(plain(b.View(60, 20)), "\n"); !strings.Contains(v, "Thinking (1)") {
		t.Fatalf("Thinking-Knoten fehlt mit Inhalt:\n%s", v)
	}
}

// TestActivityNavigatesIntoExpandedCalls: nach dem Aufklappen laeuft der
// Cursor in die einzelnen Calls; enter auf einem Call klappt NICHT zu.
func TestActivityNavigatesIntoExpandedCalls(t *testing.T) {
	a := NewActivityTree(nil).
		Tool(ToolUpdate{CallID: "c1", Label: "shell", Kind: "execute", Command: "echo one", Done: true}).
		Tool(ToolUpdate{CallID: "c2", Label: "shell", Kind: "execute", Command: "echo two", Done: true}).
		Focus(80, 30)
	a = a.Enter() // Execute aufklappen
	if _, ok := a.SelectedCall(); ok {
		t.Fatal("nach Aufklappen steht der Cursor auf dem Kopf")
	}
	a = a.Move(1, 80, 30)
	if c, ok := a.SelectedCall(); !ok || c.callID != "c1" {
		t.Fatalf("erster Call nicht selektiert: %+v", c)
	}
	a = a.Move(1, 80, 30)
	if c, ok := a.SelectedCall(); !ok || c.callID != "c2" {
		t.Fatalf("zweiter Call nicht selektiert: %+v", c)
	}
	a = a.Enter()
	if !a.expanded("execute") {
		t.Fatal("enter auf einem Call darf die Kategorie nicht zuklappen")
	}
}

// TestActivitySelectionKeepsBranches: der Cursor-Pfeil ▶ spawnt HINTER
// den Rahmenzeichen (fester Auswahl-Steg), er ersetzt sie nicht — sonst
// verliert die Zeile ihre Baumstruktur und `└─` verschwaende (User-Wahl).
func TestActivitySelectionKeepsBranches(t *testing.T) {
	a := NewActivityTree(nil).
		Tool(ToolUpdate{CallID: "c1", Label: "shell", Kind: "execute", Command: "echo one", Done: true}).
		Tool(ToolUpdate{CallID: "c2", Label: "shell", Kind: "execute", Command: "echo two", Done: true}).
		Focus(80, 30).
		Enter()
	a = a.Move(1, 80, 30) // erster Call
	a = a.Move(1, 80, 30) // letzter Call
	a = a.Move(1, 80, 30) // Wendepunkt: bleibt stehen
	rows := plain(a.View(80, 30))
	var got string
	for _, r := range rows {
		if strings.Contains(r, "echo two") {
			got = r
		}
	}
	if !strings.Contains(got, "└─ ▶") {
		t.Fatalf("Auswahl ersetzt das Zweigzeichen statt dahinter zu spawnen: %q", got)
	}
	// Und die Nachbarzeile behaelt ihren Steg (nichts springt).
	for _, r := range rows {
		if strings.Contains(r, "echo one") && !strings.Contains(r, "├─   ") {
			t.Fatalf("nicht selektierte Zeile ohne Steg: %q", r)
		}
	}
}

// TestToolDetailAlertOpens: enter auf einem Call oeffnet die Alertbox mit
// dem vollstaendigen Kommando und der Ausgabe; esc schliesst sie.
func TestToolDetailAlertOpens(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(toolMsg{callID: "c1", name: "shell", kind: "execute", cmd: "echo hi",
		out: "hi\n", hasOut: true, done: true})
	r.key("tab")
	r.key("enter") // Execute aufklappen
	r.key("down")  // in den Call
	r.key("enter") // Alertbox
	if r.m.detail == nil {
		t.Fatal("Alertbox wurde nicht geoeffnet")
	}
	// Fokus-Restore: Oeffnen mutiert die Baum-Auswahl nicht.
	sel, callID, thought := r.m.activity.sel, r.m.activity.selCall, r.m.activity.selThought
	v := stripANSI(r.m.View())
	if !strings.Contains(v, "echo hi") || !strings.Contains(v, "hi") || !strings.Contains(v, "exit: 0") {
		t.Fatalf("Alertbox unvollstaendig:\n%s", v)
	}
	// Die Box ist ein Overlay: die normale TUI bleibt ringsum sichtbar.
	if !strings.Contains(v, "heron · goose acp") {
		t.Fatalf("Overlay verdraengt die Basis-TUI (Header fehlt):\n%s", v)
	}
	r.key("esc")
	if r.m.detail != nil {
		t.Fatal("esc muss die Alertbox schliessen")
	}
	if r.m.activity.sel != sel || r.m.activity.selCall != callID || r.m.activity.selThought != thought {
		t.Fatalf("Schliessen hat die Auswahl veraendert: %v/%q/%d -> %v/%q/%d",
			sel, callID, thought, r.m.activity.sel, r.m.activity.selCall, r.m.activity.selThought)
	}
}

// TestDetailBoxFrameBackground: NUR die Rahmenzeichen der Alertbox tragen
// einen Hintergrund (dunkler Violett-Ton), der Inhalt behaelt den
// Terminal-Grund (User-Wunsch 22.11.) — und das gilt nur fuer die Box,
// nicht fuer den Activity-Baum.
func TestDetailBoxFrameBackground(t *testing.T) {
	withTrueColor(t)
	m := newRig(t).m
	m.width, m.height = 90, 24
	box := m.detailBox(Detail{Title: "shell", Kind: "Execute", Status: "✓ fertig", Body: "$ ls\nfoo"})
	if len(box) < 4 {
		t.Fatalf("Box zu klein: %d Zeilen", len(box))
	}
	for i, ln := range box {
		isFrameRow := i == 0 || i == len(box)-1
		if isFrameRow {
			if strings.Count(ln, "48;2;") != 1 {
				t.Fatalf("Rahmenzeile ohne/mit zu viel Hintergrund: %q", ln)
			}
			continue
		}
		// GENAU zwei eingefärbte Zellen: das linke und das rechte
		// Rahmenzeichen. Padding und Inhalt bleiben Terminal-Grund.
		if n := strings.Count(ln, "48;2;"); n != 2 {
			t.Fatalf("Innenzeile hat %d eingefärbte Zellen, want 2: %q", n, ln)
		}
	}
}

// TestActivityThinkingDetailOverlay: der Thinking-Knoten ist genauso
// selektierbar wie ein Tool-Call — aufgeklappt laufen ↑/↓ in die einzelnen
// Denk-Sektionen, enter oeffnet deren Volltext als Overlay, esc kehrt
// exakt zur selben Sektion zurueck.
func TestActivityThinkingDetailOverlay(t *testing.T) {
	r := newRig(t)
	r.m.startTurn()
	r.do(thoughtMsg{text: "erste Idee"})
	r.do(toolMsg{callID: "c1", name: "shell", kind: "execute", cmd: "echo", done: true})
	r.do(thoughtMsg{text: "zweite Idee\nmit zweiter Zeile"})
	r.key("tab")   // Eingabe -> Activity-Baum (Cursor auf Thinking)
	r.key("enter") // Thinking aufklappen
	r.key("down")  // Thought-Sektion 1
	r.key("down")  // Thought-Sektion 2
	if r.m.activity.sel != nodeThinking || r.m.activity.selThought != 2 {
		t.Fatalf("Thought-Sektion nicht selektiert: sel=%v thought=%d",
			r.m.activity.sel, r.m.activity.selThought)
	}
	d, ok := r.m.activity.SelectedDetail()
	if !ok {
		t.Fatal("Thought-Sektion hat kein Detail")
	}
	if !strings.Contains(d.Body, "zweite Idee") || !strings.Contains(d.Body, "mit zweiter Zeile") {
		t.Fatalf("Detail ohne Volltext: %q", d.Body)
	}
	if d.Kind != "Thinking" || d.Status == "" {
		t.Fatalf("Detail ohne Kategorie/Status: %+v", d)
	}
	sel, callID, thought := r.m.activity.sel, r.m.activity.selCall, r.m.activity.selThought
	r.key("enter")
	if r.m.detail == nil {
		t.Fatal("enter auf einer Thought-Sektion oeffnet keine Alertbox")
	}
	v := stripANSI(r.m.View())
	for _, want := range []string{"zweite Idee", "mit zweiter Zeile", "Thinking", "heron · goose acp"} {
		if !strings.Contains(v, want) {
			t.Fatalf("Overlay unvollstaendig (%q fehlt):\n%s", want, v)
		}
	}
	r.key("esc")
	if r.m.detail != nil {
		t.Fatal("esc muss das Overlay schliessen")
	}
	if r.m.activity.sel != sel || r.m.activity.selCall != callID || r.m.activity.selThought != thought {
		t.Fatalf("Schliessen hat die Thought-Auswahl veraendert: %v/%q/%d -> %v/%q/%d",
			sel, callID, thought, r.m.activity.sel, r.m.activity.selCall, r.m.activity.selThought)
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

// TestKindMergesReadAndSearch: Read und Search fallen in EINE Kategorie
// (User-Entscheidung: beides Lesezugriff) — egal ob als Protokoll-kind,
// als Tool-Name oder als shell-Kommando gemeldet. goose 1.55 liest per
// `shell cat`, listet per `tree` und sucht per `shell grep` (probed);
// schreibende/ bauende Kommandos bleiben Execute.
func TestKindMergesReadAndSearch(t *testing.T) {
	a := NewActivityTree(nil)
	a = a.Tool(ToolUpdate{CallID: "1", Kind: "search", Label: "search", Text: "x", Done: true}) // Protokoll-kind
	a = a.Tool(ToolUpdate{CallID: "2", Kind: "read", Label: "read_file", Text: "x", Done: true})
	a = a.Tool(ToolUpdate{CallID: "3", Label: "tree", Text: "x", Done: true}) // Name-Inferenz (früher: search)
	a = a.Tool(ToolUpdate{CallID: "4", Label: "shell", Command: "cat README.md", Done: true})
	a = a.Tool(ToolUpdate{CallID: "5", Label: "shell", Command: "LC_ALL=C grep -n heron main.go", Done: true})
	a = a.Tool(ToolUpdate{CallID: "6", Label: "shell", Command: "git log --oneline", Done: true})
	a = a.Tool(ToolUpdate{CallID: "7", Label: "shell", Command: "go build ./...", Done: true})
	a = a.Tool(ToolUpdate{CallID: "8", Label: "shell", Command: "git commit -m x", Done: true})

	ks := a.kinds()
	if len(ks) != 2 || ks[0] != "read" || ks[1] != "execute" {
		t.Fatalf("kinds = %v, want [read execute]", ks)
	}
	if n := a.KindCount("read"); n != 6 {
		t.Fatalf("read count = %d, want 6 (search+read+tree+cat+grep+git-log)", n)
	}
	if n := a.KindCount("execute"); n != 2 {
		t.Fatalf("execute count = %d, want 2 (build+commit)", n)
	}
}
