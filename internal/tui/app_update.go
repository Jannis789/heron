package tui

// Update-Handler: Der Elm-Dispatcher bleibt flach — jede Msg-Art hat ihre
// eigene Methode (Skalierungsmuster wie pickerKey/applyExtToggled). Key- und
// Mouse-Handler melden mit ok=false, dass die Nachricht NICHT konsumiert
// wurde; sie faellt dann in Update auf den Eingabe-Fallthrough durch.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// Update verteilt eingehende Nachrichten auf die Handler-Methoden in dieser
// Datei. Nicht behandelte Nachrichten laufen auf den Eingabe-Fallthrough am
// Ende durch.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevValue := m.input.Value()
	// Der Copy-Flash des Eingabefeld-Chips verfaellt mit der naechsten
	// Eingabe — Tasten und Maus-PRESS. Ein RELEASE (folgt jedem Klick
	// unmittelbar) loescht ihn nicht: sonst waere der Klick-Flash nie
	// sichtbar (Press setzt, Release loescht, bevor gerendert wird).
	switch mm := msg.(type) {
	case tea.KeyMsg:
		m.copiedFlash = false
	case tea.MouseMsg:
		if mm.Action == tea.MouseActionPress {
			m.copiedFlash = false
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.updateWindowSize(msg)
	case tea.MouseMsg:
		if m, cmd, ok := m.updateMouse(msg); ok {
			return m, cmd
		}
	case tea.KeyMsg:
		if m, cmd, ok := m.updateKey(msg); ok {
			return m, cmd
		}
	case spinnerTickMsg:
		return m.updateSpinnerTick()
	case charBlinkMsg:
		return m.updateCharBlink()
	case chunkMsg:
		return m.updateChunk(msg)
	case thoughtMsg:
		return m.updateThought(msg)
	case toolMsg:
		return m.updateTool(msg)
	case infoMsg:
		return m.updateInfo(msg)
	case usageMsg:
		m.totalIn += msg.in
		m.totalOut += msg.out
		m.lastIn, m.lastOut = msg.in, msg.out
		return m, nil
	case newSessionMsg:
		return m.updateNewSession(msg)
	case resumeListMsg:
		return m.updateResumeList(msg)
	case skillsLoadedMsg:
		return m.updateSkillsLoaded(msg)
	case extLoadedMsg:
		return m.updateExtLoaded(msg)
	case extToggledMsg:
		return m.applyExtToggled(msg)
	case resumeStartMsg:
		return m.updateResumeStart(msg)
	case replayMsg:
		return m.updateReplay(msg)
	case noticeMsg:
		m.chat = m.chat.Append(NewTextBlock(msg.text, "1"))
		return m, m.flushPrinted()
	case providerFavoritesMsg:
		// Eigene Provider, wie GOOSE sie klassifiziert
		// (_goose/unstable/providers/list: "Custom"/"Declarative") —
		// providerRows() sortiert sie hinter den aktiven (FR-45).
		m.providerFavorites = msg.ids
		return m, nil
	case configResultMsg:
		return m.updateConfigResult(msg)
	case diagramsDoneMsg:
		m.chat, _ = m.chat.applyDiagrams(msg.results, m.imagesEnabled(), m.caps)
		// Jetzt sind die Bloecke final (Bild oder Fallback-Text): drucken.
		return m, m.flushPrinted()
	case replyMsg:
		return m.updateReply(msg)
	case retryPromptMsg:
		return m.updateRetry(msg)
	}
	// Eingabe-Modus: nur die SELEKTIERTE Textarea bekommt Tasten — im
	// Antwort-Modus faellt hier nichts mehr durch, Getippe kann das
	// Eingabefeld nicht erreichen (nicht selektiert = nicht interaktiv).
	var cmd tea.Cmd
	if m.inputFocus {
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != prevValue {
			// Typing resets the slash-command state (FR-20).
			m.cmdDismissed = false
			m.cmdCursor = 0
		}
	}
	// The prompt box grows with the text, so the chat gives the rows back.
	m.syncInputSize()
	return m, cmd
}

// updateWindowSize setzt die Viewport-Masse und stoesst im ersten Frame die
// Seed-Diagramm-Konvertierung an.
func (m Model) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	imageLogf("viewport %dx%d Zellen", msg.Width, msg.Height)
	m.input.SetWidth(msg.Width - 4)
	m.syncInputSize()
	// Der Seed haengt Diagramm-Fences an; die Konvertierung braucht das
	// laufende Programm — der erste Frame loest sie aus (FR-6/27/28).
	if m.seedDiagrams {
		m.seedDiagrams = false
		// Der Flush druckt alle non-pending Seed-Bloecke sofort; die
		// Diagramm-Bloecke druckt diagramsDoneMsg nach der Konvertierung.
		return m, tea.Batch(m.pendingDiagrams(), m.flushPrinted())
	}
	return m, m.flushPrinted()
}

// updateMouse behandelt Klicks und Mausrad. ok=false: nichts hat die
// Nachricht konsumiert (Fallthrough auf die Eingabe in Update).
func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd, bool) {
	// Activity-Tree (§ 5): Die Kopfzeilen der Hauptknoten sind Klick-
	// ziele (Zeile 0 Thinking, Zeile 1 Tool Calls — sie stehen immer
	// an denselben Positionen), das Rad scrollt den aufgeklappten
	// Tool-Call-Bereich. Klicks im Activity-Bereich gehen NIE an den
	// Chat darunter — die Koordinaten passen nicht mehr.
	if m.height > 0 {
		if top, n := m.activityRect(); n > 0 && msg.Y >= top && msg.Y < top+n {
			switch msg.Button {
			case tea.MouseButtonLeft:
				if msg.Action != tea.MouseActionPress {
					return m, nil, true
				}
				// Der Klick SELEKTIERT den Baum (Ketten-Glied wie der
				// Chat-Klick) und setzt den Cursor auf den Knoten der
				// Zeile; auf dessen Kopfzeile klappt er ihn direkt um —
				// dieselbe Struktur, die ↑/↓ + enter benutzt.
				m.chat = m.chat.Unfocus()
				m.chat = m.chat.clearMarks()
				m.activity = m.activity.Click(msg.Y-top, m.width, m.activityBudget())
				// Kopfzeilen-Klick klappt Knoten um (Click ruft Enter)
				// — Zeilenzahl des Baums kann sich aendern: Chat-
				// Viewport nachfuehren.
				m.relayout()
				if !m.activity.Focused() {
					// Kein Knoten unter dem Klick (winziges Budget,
					// nur Textzeilen): die Eingabe behaelt den Fokus,
					// statt in einen toten Zustand zu laufen.
					return m, m.setInputFocus(true), true
				}
				return m, m.setInputFocus(false), true
			case tea.MouseButtonWheelUp:
				m.activity = m.activity.Wheel(1)
				return m, nil, true
			case tea.MouseButtonWheelDown:
				m.activity = m.activity.Wheel(-1)
				return m, nil, true
			}
			return m, nil, true // Region schluckt den Rest (kein Chat-Klick)
		}
	}
	// The [ modell · effort ] footer chip toggles the popup; clicks
	// inside the open popup select, outside close it (FR-20-lite).
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if lo, hi := m.modelSegmentRange(); msg.Y == m.height-1 && msg.X >= lo && msg.X < hi {
			m.openPicker("model")
			return m, nil, true
		}
	}
	// Translate the screen row into chat-area coordinates, then let the
	// pager handle hover/click on COPY actions.
	mm := msg
	mm.Y -= chatTop
	var cmd tea.Cmd
	var handled bool
	marksBefore := m.chat.markedCount()
	m.chat, cmd, handled = m.chat.Update(mm)
	if handled {
		// FR-39-Invariante "hoechstens EIN Neon-Rahmen", Maus-Pfad: Die
		// Klick-Markierung IST die Auswahl und haelt DEN Rahmen — das
		// Eingabefeld weicht (blur, Rahmen zur Ruhefarbe), genauso wie
		// die Shift-Auswahl die Eingabe weichen laesst. Loeist der Klick
		// die letzte Markierung und haelt die Kette keinen Chat-Fokus,
		// kehrt der Fokus auf seinen STANDARD zurueck: Eingabe-Modus.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			switch {
			case m.chat.markedCount() > 0 && m.inputFocus:
				cmd = tea.Batch(cmd, m.setInputFocus(false))
			case marksBefore > 0 && m.chat.markedCount() == 0 && !m.chat.Focused():
				cmd = tea.Batch(cmd, m.setInputFocus(true))
			}
		}
		return m, cmd, true
	}
	return m, tea.Cmd(nil), false
}

// updateKey behandelt Tastendruecke: Overlay, Picker, Slash-Navigation und
// die Block-/Fokus-Tasten. ok=false: die Taste faellt auf die Textarea
// durch (normales Tippen).
func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	// Detail-Alertbox (Overlay): schluckt alle Tasten; enter/esc/q
	// schliesst, pgup/pgdown scrollen im Inhalt. Die Baum-Auswahl
	// bleibt dabei unveraendert — Schliessen kehrt exakt zurueck.
	if m.detail != nil {
		switch msg.String() {
		case "enter", "esc", "q":
			m.detail = nil
			m.detailScroll = 0
		case "pgup":
			if m.detailScroll > 5 {
				m.detailScroll -= 5
			} else {
				m.detailScroll = 0
			}
		case "pgdown":
			m.detailScroll += 5
		}
		return m, nil, true
	}
	// The open picker swallows keys first (FR-20).
	if m.pickerOpen {
		nm, cmd := m.pickerKey(msg)
		return nm, cmd, true
	}
	// Slash-command navigation (FR-20): while the prompt starts with
	// "/", up/down move the selection, tab completes, esc dismisses.
	// Nur im Eingabe-Modus: im Antwort-Modus sind ↑/↓ Block-Tasten.
	if m.inputFocus && m.slashActive() && len(m.slashMatches()) > 0 {
		switch msg.String() {
		case "down":
			m.cmdCursor = m.nextCmd(1)
			return m, nil, true
		case "up":
			m.cmdCursor = m.nextCmd(-1)
			return m, nil, true
		case "tab", "right":
			// Accept the ghost into the input (right does what tab does
			// in shells).
			if full := m.slashGhostFull(); full != "" {
				m.input.SetValue(full + " ")
				m.cmdCursor = 0
				m.syncInputSize()
			}
			return m, nil, true
		case "esc":
			m.cmdDismissed = true
			return m, nil, true
		case "enter":
			if matches := m.slashMatches(); m.cmdCursor < len(matches) {
				nm, cmd := m.execCommand("/" + matches[m.cmdCursor].name)
				return nm, cmd, true
			}
		}
	}
	// ctrl+j is the line break (FR-7). Grow the box BEFORE the textarea
	// moves the cursor to the new line: its internal viewport scrolls
	// when the cursor leaves a too-small window and never scrolls back
	// after SetHeight — the first line was cut off above. Growing first
	// keeps every row visible; syncInputSize normalises afterwards.
	// Antwort-Modus: kein Zeilenumbruch — das Eingabefeld ist nicht
	// selektiert und damit nicht interaktionsfaehig.
	if msg.String() == "ctrl+j" && m.inputFocus {
		if h := m.input.Height(); h < inputMaxHeight {
			m.input.SetHeight(h + 1)
		}
	}
	// Numbered copy fallback (FR-5): alt+<n> / ctrl+<n> copy block n.
	if n := blockNumFromKey(msg.String()); n > 0 {
		chat, cmd, ok := m.chat.copyByID(n)
		if !ok {
			return m, nil, true
		}
		m.chat = chat
		return m, cmd, true
	}
	switch msg.String() {
	case "shift+enter", "shift+tab":
		if m, cmd, ok := m.keyCharCursor(msg); ok {
			return m, cmd, true
		}
	case "ctrl+c":
		nm, cmd := m.keyCtrlC()
		return nm, cmd, true
	case "enter":
		nm, cmd := m.keyEnter(msg)
		return nm, cmd, true
	case "up", "down", "pgup", "pgdown", "shift+up", "shift+down":
		nm, cmd := m.keyUpDown(msg)
		return nm, cmd, true
	case "left", "right", "shift+left", "shift+right", "home", "end":
		// Antwort-Modus: links/rechts steuern das pannende Bild (die
		// BlockList leitet an den Block weiter); im Zeichen-Cursor
		// (FR-42) bewegen sie das Caret bzw. waehlen mit shift. Ohne
		// beides sind es KEINE Block-Tasten und werden unten verschluckt.
		// Im Eingabe-Modus fallen sie durch zur Textarea (Cursor bewegt
		// sich im Text) — Slash-Completion fängt right oben ab.
		if !m.inputFocus {
			var cmd tea.Cmd
			m.chat, cmd, _ = m.chat.Update(msg)
			return m, cmd, true
		}
	case "tab":
		nm, cmd := m.keyTab()
		return nm, cmd, true
	case " ", "y":
		if m, cmd, ok := m.keySelection(msg); ok {
			return m, cmd, true
		}
	case "c":
		if m, cmd, ok := m.keyCopyChip(msg); ok {
			return m, cmd, true
		}
	case "esc", "t", "+", "=", "-", "_", "r":
		if m, cmd, ok := m.keyBlockAction(msg); ok {
			return m, cmd, true
		}
	}
	return m, tea.Cmd(nil), false
}

// keyCharCursor: Zeichen-Cursor (FR-42): shift+enter IST das Editor-Caret —
// frei bewegend, shift+Pfeile auswaehlend (nur lesend). handleKey
// entscheidet: Eintritt an der Leseposition (bzw. an laufender
// Block-Auswahl), Austritt per Toggle. Aus beiden Modi (Eingabe
// weicht, Antwort bleibt). shift+enter kommt in ghostty/kitty
// als eigene Sequenz an (Kitty-Protokoll, kbd.go uebersetzt sie
// auf den selben Trigger); alle anderen Terminals senden fuer
// shift+enter schlicht \r — dort ist shift+tab der Ausloeser.
func (m Model) keyCharCursor(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if !m.chat.Empty() {
		var cmd tea.Cmd
		var handled bool
		m.chat, cmd, handled = m.chat.Update(msg)
		if handled {
			focus := m.setInputFocus(false)
			cmds := []tea.Cmd{cmd, focus}
			if m.chat.charActive() && !m.charBlinkArmed {
				// Modus-Eintritt: GENAU EINE Blink-Kette starten
				// (Armed-Marker gegen Doppel-Takte, s. Model).
				m.charBlinkArmed = true
				cmds = append(cmds, m.nextCharBlink())
			}
			return m, tea.Batch(cmds...), true
		}
	}
	return m, tea.Cmd(nil), false
}

// keyCtrlC: FR-7: during a turn Ctrl+C aborts the turn (session/cancel)
// instead of killing the app. FR-46 (neu): die Warteschlange
// wird NICHT mehr verworfen — der erste wartende Prompt geht
// sofort raus (flushQueued armt den Versand fuer den Abbruch-
// Reply, im Ruhestand direkt). Ein Doppel-Druck (< 0,75 s)
// beendet heron — erst DANACH gilt "alles".
func (m Model) keyCtrlC() (tea.Model, tea.Cmd) {
	now := time.Now()
	double := now.Sub(m.lastCtrlC) < 750*time.Millisecond
	m.lastCtrlC = now
	if double {
		return m, tea.Quit
	}
	if m.running {
		if m.cancel != nil && !m.cancelling {
			m.cancelling = true
			if len(m.queued) > 0 {
				m.flushQueued = true
			}
			cancel := m.cancel
			return m, func() tea.Msg { cancel(); return nil }
		}
		// Cancel haengt (oder fehlt): erneutes ctrl+c beendet hart.
		return m, tea.Quit
	}
	// Ruhestand mit wartender Queue (z.B. nach Fehler): erster
	// Prompt geht sofort raus statt zu quitten.
	if len(m.queued) > 0 && m.send != nil {
		return m, m.dispatchNextQueued()
	}
	return m, tea.Quit
}

// keyEnter: enter schliesst die aktuelle Ebene ab — Activity-Knoten auf/
// zu, Zeichen-Auswahl kopieren, Block-Aktion — und sendet sonst den
// Prompt (bzw. stellt ihn in die Queue, FR-46).
func (m Model) keyEnter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Activity-Auswahlmodus: enter klappt den Knoten unter dem
	// Cursor auf — und wieder zu (kein gesondertes Zuklappen).
	if m.activity.Focused() {
		// Ein ausgewaehltes Element (Call oder Thought-Sektion)
		// oeffnet die Detail-Alertbox. Oeffnen mutiert die
		// Auswahl NICHT — Schliessen kehrt genau hierher zurueck.
		if d, ok := m.activity.SelectedDetail(); ok {
			m.detail = &d
			m.detailScroll = 0
			return m, nil
		}
		m.activity = m.activity.Enter()
		// Auf-/Zuklappen aendert die Baumzeilenzahl: Der Chat-
		// Viewport folgt sofort (Relayout-Vertrag wie thought/
		// toolMsg), sonst verschiebt der Zuschnitt in View()
		// den ganzen Screen nach oben.
		m.relayout()
		return m, nil
	}
	// Zeichen-Modus (FR-42): enter kopiert den markierten Buchstaben-
	// Bereich (wie c) — die Cursor-Sektion hat keinen Block-Fokus.
	// Erhaltene Auswahl (22:18): auch von aussen kopierbar.
	if (m.chat.charActive() || m.chat.residualSelShown()) && m.copyFn != nil {
		m.copiedFlash = true
		return m, m.copyFn(m.chat.selMarkdown())
	}
	if m.chat.Focused() {
		var cmd tea.Cmd
		m.chat, cmd, _ = m.chat.Update(msg)
		return m, cmd
	}
	if !m.inputFocus {
		return m, nil // Antwort-Modus ohne Block-Fokus: schlucken
	}
	prompt := strings.TrimSpace(m.input.Value())
	if prompt == "" {
		return m, nil
	}
	if strings.HasPrefix(prompt, "/") {
		// Slash commands (FR-20): /model opens the selection dialog,
		// unknown commands report an error instead of sending.
		name := strings.TrimPrefix(prompt, "/")
		for _, c := range commands {
			if c.name == name {
				return m.execCommand("/" + name)
			}
		}
		m.chat = m.chat.Append(NewTextBlock("Unbekannter Befehl: "+prompt, "1"))
		m.input.SetValue("")
		m.syncInputSize()
		return m, m.flushPrinted()
	}
	m.recordHistory(prompt)
	// Queue (FR-46): Laeuft der Turn noch, wartet der Prompt in der
	// Warteschlange und geht automatisch raus, sobald er endet —
	// ctrl+c drengt ihn sofort nach (Abbruch + Versand), der
	// Doppel-Druck beendet heron.
	if m.running {
		m.queued = append(m.queued, prompt)
		m.input.SetValue("")
		m.syncInputSize()
		return m, m.flushPrinted()
	}
	m.chat = m.chat.Append(NewUserBlock(prompt))
	m.input.SetValue("")
	if m.send == nil {
		return m, nil
	}
	m.startTurn()
	send := m.send
	sendCmd := func() tea.Msg {
		text, err := send(prompt)
		return replyMsg{text: text, err: err, prompt: prompt}
	}
	// User-Block append-only echoen (timg-Modell), Versand parallel;
	// der Status-Spinner laeuft, bis die Antwort eintrifft (FR-2).
	return m, tea.Batch(sendCmd, m.flushPrinted(), nextSpinner())
}

// keyUpDown: Activity-Auswahlmodus: ↑/↓ laufen die Baum-Zeilen ab (am
// Rand des Tool-Bands scrollen sie die Historie weiter),
// Bild-Seiten springen im Tool-Fenster — der Chat bekommt
// diese Tasten im Baum-Fokus nicht.
func (m Model) keyUpDown(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.activity.Focused() {
		switch msg.String() {
		case "up", "shift+up":
			m.activity = m.activity.Move(-1, m.width, m.activityBudget())
		case "down", "shift+down":
			m.activity = m.activity.Move(1, m.width, m.activityBudget())
		case "pgup":
			m.activity = m.activity.Wheel(5)
		case "pgdown":
			m.activity = m.activity.Wheel(-5)
		}
		return m, nil
	}
	// Eingabe-Modus: ↑/↓ tun NICHTS — sie sind fuer die Eingabe-
	// History reserviert (spaeter) und springen keinen Fokus. Nur
	// Bild ↓/↑ scrollen seitenweise (Lesen ohne Fokus-Wechsel).
	// Antwort-Modus: ↑/↓ laufen blockweise und STOPPEN an beiden
	// Kettenenden — der Rueckweg ins Eingabefeld fuehrt hier
	// AUSSCHLIESSLICH tab (esc bleibt der Reset auf den Standard).
	if m.inputFocus {
		s := msg.String()
		if s == "up" || s == "down" {
			if s == "up" {
				m.recallHistory(-1)
			} else {
				m.recallHistory(1)
			}
			return m, nil
		}
		if s == "pgup" || s == "pgdown" {
			// Bild-Seitenwechsel aus der Eingabe: reines Scrollen,
			// KEIN Fokus-Wechsel (die Kette bleibt in der Eingabe).
			var cmd tea.Cmd
			m.chat, cmd, _ = m.chat.Update(msg)
			return m, cmd
		}
		// shift+up/down starten KEINE Auswahl mehr aus der Eingabe
		// (User-Report: unfreiwilliger Modus-Wechsel — "Man ist im
		// Cursor-Mode gefangen", Getippte erreichte das Feld nicht
		// mehr). Der Weg in die Auswahl fuehrt bewusst ueber tab
		// (Antwort-Modus) bzw. shift+enter (Zeichen-Cursor).
		return m, nil // ↑/↓/shift+↑/shift+↓: History-Reserve, no-op
	}
	var cmd tea.Cmd
	m.chat, cmd, _ = m.chat.Update(msg)
	return m, cmd
}

// keyTab: tab laeuft die Kette: Eingabe → Activity-Baum (falls er
// etwas zeigt) → Antwort-Blöcke → Eingabe. Der Baum ist ein
// gleichwertiges Ketten-Glied: ↑/↓ navigieren, enter klappt.
func (m Model) keyTab() (tea.Model, tea.Cmd) {
	if m.activity.Focused() {
		m.activity = m.activity.Blur()
		nl := m.chat.FocusVisible()
		if !nl.Focused() {
			return m, m.setInputFocus(true)
		}
		m.chat = nl
		return m, m.setInputFocus(false)
	}
	if m.chat.Focused() || !m.inputFocus {
		m.chat = m.chat.Unfocus()
		return m, m.setInputFocus(true)
	}
	if m.activityVisible() {
		m.chat = m.chat.Unfocus()
		m.chat = m.chat.clearMarks()
		m.activity = m.activity.Focus(m.width, m.activityBudget())
		return m, m.setInputFocus(false)
	}
	nl := m.chat.FocusVisible()
	if !nl.Focused() {
		// Kein fokussierbares Segment im Chat: im Eingabe-Modus
		// bleiben — sonst hinge die Kette ohne Ziel fest.
		return m, nil
	}
	m.chat = nl
	return m, m.setInputFocus(false)
}

// keySelection: Selection keys (FR-29): space marks the focused block, y
// yanks the marked range as plain markdown. Unhandled they fall through to
// the input as normal typing.
func (m Model) keySelection(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	var cmd tea.Cmd
	var handled bool
	m.chat, cmd, handled = m.chat.Update(msg)
	if handled {
		return m, cmd, true
	}
	return m, tea.Cmd(nil), false
}

// keyCopyChip: Copy-Chip im Eingabefeld-Footer: c kopiert die Auswahl (oder
// den fokussierten Block) — das Copy-UI lebt nicht mehr in den
// Block-Footern. Shift-Auswahl UND Zeichen-Cursor haben Vorrang:
// c kopiert NUR sie.
func (m Model) keyCopyChip(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if (m.chat.selActive() || m.chat.charActive() || m.chat.residualSelShown()) && m.copyFn != nil {
		m.copiedFlash = true
		return m, m.copyFn(m.chat.selMarkdown()), true
	}
	if m.chat.markedCount() > 0 || m.chat.Focused() {
		nm, cmd := m.copySelection()
		return nm, cmd, true
	}
	return m, tea.Cmd(nil), false
}

// keyBlockAction: Activity-Auswahlmodus: esc klappt zunaechst offene Knoten
// zu (Stufe 1) und verlaesst den Baum erst, wenn er geschlossen ist (Stufe 2:
// Blur + zurueck ins Eingabefeld).
// Block-Aktionen am fokussierten Block: t toggelt EIN-TASTIG
// zwischen Bild und Quelltext (FR-38 — i ist entfallen), +/-
// klappen den Think-Block. Im Zeichen-Cursor kollabiert esc
// zunaechst die Auswahl. Ohne Fokus faellt alles durch und
// tippt normal ins Eingabefeld.
func (m Model) keyBlockAction(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.activity.Focused() {
		if msg.String() == "esc" {
			// esc raeumt in STUFEN ab (User-Wunsch 23.11., derselbe
			// Vertrag wie im Chat): ERST offene Knoten zuklappen — der
			// Baum bleibt fokussiert, der Tab-Modus wechselt nicht —,
			// erst wenn der Baum ganz geschlossen ist, verlaesst der
			// naechste Druck ihn und kehrt ins Eingabefeld.
			if nl, changed := m.activity.CollapseAll(); changed {
				m.activity = nl
				// Zuklappen schrumpft die Baumzeilen: Der Chat bekommt
				// den Platz sofort zurueck (Relayout-Vertrag wie enter).
				m.relayout()
				return m, nil, true
			}
			m.activity = m.activity.Blur()
			return m, m.setInputFocus(true), true
		}
	}
	if m.chat.Focused() || m.chat.selActive() || m.chat.charActive() || m.chat.prevSel || m.chat.residualSelShown() {
		var cmd tea.Cmd
		var handled bool
		m.chat, cmd, handled = m.chat.Update(msg)
		if handled {
			// Hat esc den BLOCK-Fokus weggeraeumt, faellt die Kette
			// auf ihren Standard zurueck: das Eingabefeld. esc raeumt
			// aber in STUFEN ab: Laeuft noch der Zeichen-Cursor (esc
			// hat NUR die Auswahl aufs Caret kollabiert) oder wartet
			// unter ihm eine Block-Auswahl (esc kehrt in sie zurueck),
			// bleibt die Kette IM Chat — das Eingabefeld darf den
			// Nutzer nicht mitten im Modus uebernehmen (User-Report:
			// esc warf aus dem Schnipsel-Modus in die Eingabe).
			if msg.String() == "esc" && !m.chat.Focused() && !m.chat.charActive() && !m.chat.selActive() {
				return m, tea.Batch(cmd, m.setInputFocus(true)), true
			}
			return m, cmd, true
		}
	}
	if msg.String() == "esc" {
		// Esc raeumt der Reihe nach weg: Auswahl, Block-Fokus — und
		// kehrt auf den STANDARD zurueck: Eingabefeld selektiert.
		m.setInputFocus(true)
	}
	return m, tea.Cmd(nil), false
}

// updateSpinnerTick: Advance the running animation one frame; re-arm while the turn
// is still in flight (FR-2) ODER der Rahmen-Fade noch auslaeuft:
// Das Neon-Band blendet beim Start ein, nach dem Turn wieder aus —
// der Ticker verstopft erst, wenn beides Ruhe hat.
func (m Model) updateSpinnerTick() (tea.Model, tea.Cmd) {
	m.neonPhase += neonPhaseStep
	if m.running {
		m.spinner = (m.spinner + 1) % len(spinnerFrames)
		// Working-on-Leerlaufzeile folgt demselben Frame — Baum und
		// Statuszeile rotieren synchron (User-Report: Ladeindikator).
		m.activity = m.activity.IdleFrame(m.spinner)
		// Sicherheitsnetz: Der Tick laeuft den ganzen Turn — er holt
		// jeden Baum-Zeilen-Drift nach maximal einem Frame nach, auch
		// aus Pfaden ohne eigenes relayout.
		m.relayout()
		m.neonLevel = min(1, m.neonLevel+neonFadeStep)
		return m, nextSpinner()
	}
	if m.neonLevel > 0 {
		m.neonLevel = max(0, m.neonLevel-neonFadeStep)
		return m, nextSpinner()
	}
	return m, nil
}

// updateCharBlink: Caret-Blink (FR-42): Die Phase toggelt NUR, solange der Zeichen-
// Cursor laeuft — sonst endet die Kette hier (und der Arm-Marker
// wird fuer den naechsten Eintritt frei). Eine laufende Auswahl
// blinkt nicht mit: charDecorate blendet nur das alleinstehende
// Caret ab, der Bereich steht als Farbe.
func (m Model) updateCharBlink() (tea.Model, tea.Cmd) {
	if m.chat.charActive() {
		m.chat = m.chat.toggleCharBlink()
		return m, m.nextCharBlink()
	}
	m.charBlinkArmed = false
	return m, nil
}

// updateChunk: Agent-Text ist Antwort, kein Prozess (FR-2, User-Wunsch): Er
// streamt als EIN wachsender Block in den Chat — Thinking und
// Tool-Calls leben allein im Activity-Baum. Die Vorschau ist ein
// TextBlock mit stabiler Block-ID und wird pro Chunk in-place
// ersetzt; endTurn nimmt ihn heraus, replyMsg haengt die fertig
// gerenderten Widgets an. Im Inline-Modus haelt flushPrinted an
// der noch mutierenden Vorschau an — der Scrollback sieht nur
// das finale Rendering.
func (m Model) updateChunk(msg chunkMsg) (tea.Model, tea.Cmd) {
	m.streaming += msg.text
	preview := NewTextBlock(m.streaming, textColorFor(m.theme))
	if m.streamID > 0 {
		m.chat = m.chat.replaceBlock(m.streamID, preview)
		return m, nil
	}
	m.chat = m.chat.Append(preview)
	m.streamID = m.chat.items[len(m.chat.items)-1].ID()
	return m, nil
}

// updateThought haengt einen Denkschritt in den Activity-Baum.
func (m Model) updateThought(msg thoughtMsg) (tea.Model, tea.Cmd) {
	m.activity = m.activity.Thought(msg.text)
	// Der Baum wächst mit jedem Denkschritt — der Chat-Viewport gibt
	// die Zeilen SOFORT ab (kein Warten auf den naechsten Spinner-
	// Tick), sonst driften Layout und View auseinander.
	m.relayout()
	return m, nil
}

// updateTool: Der Activity-Baum uebernimmt die Karte: neuer Call schliesst die
// laufende Denk-Sektion, Updates derselben callID aktualisieren
// denselben Eintrag in Place (FR-30/31-Geist).
func (m Model) updateTool(msg toolMsg) (tea.Model, tea.Cmd) {
	m.activity = m.activity.Tool(ToolUpdate{
		CallID: msg.callID, Label: msg.name, Kind: msg.kind, Text: msg.text,
		Command: msg.cmd, Stdout: msg.out, Stderr: msg.errOut,
		Exit: msg.exit, HasOutput: msg.hasOut, Live: msg.live,
		Done: msg.done, Failed: msg.failed,
	})
	// Neuer Call = neue Baumzeile(n): Chat-Viewport sofort nachfuehren
	// (derselbe Relayout-Vertrag wie thoughtMsg).
	m.relayout()
	return m, nil
}

// updateInfo: The backend announced the real model/effort/provider (ACP
// configOptions); empty parts keep the current labels (flag
// overrides). Auch ERNEUT nach einem Config-Wechsel — der Provider-
// Wechsel meldet die NEUE Modellliste. Die Options-Listen
// ueberschreibt nur eine GEFUELLTE Liste: eine Antwort ohne
// options (Schema-Drift) darf /model direkt nach /provider nicht
// die Liste leeren (FR-20/45: beide Befehle nacheinander nutzbar).
func (m Model) updateInfo(msg infoMsg) (tea.Model, tea.Cmd) {
	if msg.model != "" {
		m.modelLabel = msg.model
	}
	if msg.effort != "" {
		m.effort = msg.effort
	}
	if msg.provider != "" {
		m.provider = msg.provider
	}
	if len(msg.modelOptions) > 0 {
		m.modelOptions = msg.modelOptions
	}
	if len(msg.effortOptions) > 0 {
		m.effortOptions = msg.effortOptions
	}
	if len(msg.providerOptions) > 0 {
		m.providerOptions = msg.providerOptions
	}
	return m, nil
}

// updateNewSession: Fresh session: fresh chat, fresh counters (FR-20).
func (m Model) updateNewSession(msg newSessionMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.chat = m.chat.Append(NewTextBlock("Neue Session fehlgeschlagen: "+msg.err.Error(), "1"))
		return m, nil
	}
	m.chat = m.chat.Clear()
	m.printedUpTo = 0 // Inline: von vorn drucken, sonst druckt /new nie wieder
	m.totalIn, m.totalOut = 0, 0
	m.lastIn, m.lastOut = 0, 0
	return m, nil
}

// updateResumeList oeffnet den Resume-Picker (FR-44) oder meldet Fehler/
// Leere als Chat-Block.
func (m Model) updateResumeList(msg resumeListMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.chat = m.chat.Append(NewTextBlock("Resume-Liste fehlgeschlagen: "+msg.err.Error(), "1"))
		return m, m.flushPrinted()
	}
	if len(msg.rows) == 0 {
		m.chat = m.chat.Append(NewTextBlock("goose meldet keine früheren Sessions.", "1"))
		return m, m.flushPrinted()
	}
	m.resumeRows = msg.rows
	m.openPicker("resume")
	return m, nil
}

// updateSkillsLoaded oeffnet den Skill-Picker (/skills, Mehrfachauswahl).
func (m Model) updateSkillsLoaded(msg skillsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.chat = m.chat.Append(NewTextBlock("Skill-Liste fehlgeschlagen: "+msg.err.Error(), "1"))
		return m, m.flushPrinted()
	}
	if len(msg.rows) == 0 {
		m.chat = m.chat.Append(NewTextBlock("goose meldet keine Skills.", "1"))
		return m, m.flushPrinted()
	}
	m.skillRows = msg.rows
	m.skillSel = map[string]bool{}
	m.openPicker("skills")
	return m, nil
}

// updateExtLoaded oeffnet den MCP-Picker (/mcp).
func (m Model) updateExtLoaded(msg extLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.chat = m.chat.Append(NewTextBlock("Extension-Liste fehlgeschlagen: "+msg.err.Error(), "1"))
		return m, m.flushPrinted()
	}
	if len(msg.rows) == 0 {
		m.chat = m.chat.Append(NewTextBlock("goose meldet keine konfigurierten MCP-Server.", "1"))
		return m, m.flushPrinted()
	}
	m.extRows = msg.rows
	m.openPicker("mcp")
	return m, nil
}

// updateResumeStart meldet das Ergebnis des session/load; der Replay ist
// schon unterwegs (er lief WÄHREND des Loads).
func (m Model) updateResumeStart(msg resumeStartMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.chat = m.chat.Append(NewTextBlock("Resume fehlgeschlagen: "+msg.err.Error(), "1"))
		return m, m.flushPrinted()
	}
	// Der Replay ist schon da (er lief WÄHREND des Loads) — Inline
	// druckt Reste nach (endet der Verlauf z.B. auf Tool-Karten).
	return m, m.flushPrinted()
}

// updateReplay: session/load-Replay (FR-44): der geladene Verlauf landet als
// fertige Blöcke — Nutzerzeile bzw. final gerenderte Antwort,
// inkl. Diagramm-Konvertierung wie in replyMsg.
func (m Model) updateReplay(msg replayMsg) (tea.Model, tea.Cmd) {
	switch msg.kind {
	case replayUser:
		m.chat = m.chat.Append(NewUserBlock(msg.text))
		return m, m.flushPrinted()
	case replayAgent:
		nodes, err := render.Parse(msg.text)
		if err != nil {
			m.chat = m.chat.Append(NewTextBlock("Parse-Fehler: "+err.Error(), "1"))
			return m, m.flushPrinted()
		}
		m.chat = m.chat.Append(BuildWidgets(nodes, m.theme, m.copyFn)...)
		return m, tea.Batch(m.pendingDiagrams(), m.flushPrinted())
	}
	return m, nil
}

// updateConfigResult: The backend confirmed (or failed) the model/effort change.
func (m Model) updateConfigResult(msg configResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.chat = m.chat.Append(NewTextBlock("Fehler: "+msg.err.Error(), "1"))
		return m, nil
	}
	// Echter Stand nach dem Wechsel: das infoMsg mit den FRISCHEN
	// configOptions ist vor dieser Message durchs Update gelaufen
	// (publishConfigResult sendet, bevor das Cmd configResultMsg
	// zurueckgibt) — die Labels hier sind also das, was goose wirklich
	// uebernommen hat, nicht der Picker-Wunsch.
	var actual string
	switch msg.kind {
	case "effort":
		actual = m.effort
	case "provider":
		actual = m.provider
	default:
		actual = m.modelLabel
	}
	m.pickerKind = msg.kind
	m.applyPicker(actual)
	m.closePicker()
	// Silent no-op abfangen (FR-45-Nachfass, gegen echtes goose 1.55.0
	// verifiziert): goose akzeptiert set_config_option und behaelt
	// trotzdem den alten Stand — beim unkonfigurierten Default-Provider
	// "goose" ohne JEDE Fehlermeldung, dafuer mit still zurueckgesetztem
	// Modell. Der Chat meldet hier klar, was wirklich passiert ist.
	if msg.kind == "provider" || msg.kind == "model" {
		label := "Modell"
		if msg.kind == "provider" {
			label = "Provider"
		}
		switch {
		case actual == msg.value:
			// sauber uebernommen
		case actual == msg.prevProvider && msg.kind == "provider",
			actual == msg.prevModel && msg.kind == "model":
			note := fmt.Sprintf("%s-Wechsel verworfen: goose bleibt bei „%s“ — „%s“ wurde nicht übernommen.",
				label, actual, msg.value)
			if msg.kind == "provider" && m.modelLabel != msg.prevModel {
				// goose setzt beim verworfenen Provider-Wechsel still
				// das Modell auf den Session-Default zurueck.
				note += fmt.Sprintf(" Das Modell wurde dabei auf %s zurückgesetzt.", m.modelLabel)
			}
			m.chat = m.chat.Append(NewTextBlock(note, "1"))
			return m, m.flushPrinted()
		default:
			// goose hat einen eigenen Wert gewaehlt (Normalisierung/
			// Ableitung) — sichtbar machen statt still differieren.
			m.chat = m.chat.Append(NewTextBlock(
				fmt.Sprintf("%s-Wechsel: goose hat „%s“ übernommen statt „%s“.", label, actual, msg.value), "1"))
			return m, m.flushPrinted()
		}
	}
	return m, nil
}

// updateReply: The final text supersedes the live preview (FR-2).
func (m Model) updateReply(msg replyMsg) (tea.Model, tea.Cmd) {
	m.endTurn()
	switch {
	case msg.err != nil:
		// API-/RPC-Fehler als eigener Block mit Retry-Aktion (FR-33):
		// "r" auf dem fokussierten Block sendet den Prompt erneut.
		m.chat = m.chat.Append(NewErrorBlock(msg.err.Error(), msg.prompt, m.theme, m.copyFn))
	case msg.text == "":
	case true:
		nodes, err := render.Parse(msg.text)
		if err != nil {
			m.chat = m.chat.Append(NewTextBlock("Parse-Fehler: "+err.Error(), "1"))
		} else {
			m.chat = m.chat.Append(BuildWidgets(nodes, m.theme, m.copyFn)...)
		}
	}
	// Queue-Drainage (FR-46): Nach einem sauberen Turn-Ende geht der
	// naechste wartende Prompt raus — nach einem Fehler bleibt die
	// Warteschlange stehen (Retry entscheidet). AUSNAHME flushQueued
	// (ctrl+c hat den Turn abgebrochen): der erste wartende Prompt
	// geht sofort raus, nicht erst nach Retry (FR-46 neu).
	flush := m.flushQueued
	m.flushQueued = false
	if (msg.err == nil || flush) && len(m.queued) > 0 && m.send != nil {
		return m, m.dispatchNextQueued()
	}
	// PlantUML blocks in the fresh reply convert in the background
	// (FR-6); die finalen Bloecke druckt der Flush append-only.
	return m, tea.Batch(m.pendingDiagrams(), m.flushPrinted())
}

// updateRetry: Retry (FR-33): Fehler-Block weg, derselbe Prompt erneut — nur im
// Ruhestand; laeuft ein Turn (oder fehlt das Backend), passiert nichts.
func (m Model) updateRetry(msg retryPromptMsg) (tea.Model, tea.Cmd) {
	if m.running || m.send == nil {
		return m, nil
	}
	m.chat = m.chat.removeBlock(msg.blockID)
	m.startTurn()
	send := m.send
	p := msg.prompt
	sendCmd := func() tea.Msg {
		text, err := send(p)
		return replyMsg{text: text, err: err, prompt: p}
	}
	return m, tea.Batch(sendCmd, m.flushPrinted(), nextSpinner())
}
