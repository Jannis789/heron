# Anforderungskatalog — heron

> **Stand:** 2026-10-10 · **Status:** Planung (vor Implementation) · **Typ:** verbindlich für M0–M5
> Backend: **goose** (ACP) · Frontend: **heron** (Go/Bubble Tea) · Priorisierung: **MoSCoW**

## 1. Zweck & Ziele

heron ist ein selbst gebautes Terminal-UI, das **goose** über das Agent Client Protocol (ACP)
alle Frontend ersetzt. Das Kernversprechen: **übersteuerbares/erweiterbares Markdown-Rendering** —
jeder Codeblock bekommt eine **Copy-Box**, Codeblöcke mit Sprach-Tag können zusätzlich **interpretiert**
werden (z. B. PlantUML → ASCII), alle Render-Entscheidungen liegen im Client.

Leitziele:
1. volle Kontrolle über die Renderebene (kein App-Fork, kein goose-Fork),
2. schlanke, wartbare, testbare Architektur mit sauberen Seams (acp / render / tui),
3. reproduzierbarer Build über **flake.nix + Overlay**,
4. gute UX im Terminal: keyboard-first, streaming, Follow-Modus, SSH-taugliche Copy.

## 2. Abgrenzung / Non-Goals (v0)

- **Kein** Fork von goose oder eines bestehenden ACP-Clients.
- **Keine** Agent-Tools im Client: Tool-Ausführung liegt bei goose (heron zeigt nur an).
- **Kein** Multi-Client-/Team-Kollaborationsfeature in v0.
- **Kein** Windows/macOS-Support als Ziel (NixOS/Linux primär; nicht-blockierend, aber nicht getestet).
- **Kein** Remote-Transport (`goose serve` HTTP/WS) als Pflicht — später (M5, W).

## 3. Kern-User-Journey

1. Nutzer startet `heron` (dev: `nix develop` + `go run .`), goose wird als `goose acp`-Subprozess gestartet.
2. ACP-Handshake läuft automatisch; Statuszeile zeigt „verbunden“.
3. Nutzer tippt einen Prompt, Enter sendet; Antwort strömt live in den Chat.
4. Codeblöcke erscheinen als **CodeBlock mit Kopfzeile + Copy-Aktion**. PlantUML-Blöcke werden als ASCII-Diagramm gerendert.
5. Copy-Aktion (Maus) oder `ctrl+<i>` (Keyboard) legt den Block-Inhalt in die Zwischenablage.
6. Tool-Aktivität erscheint als Statuszeile; `Ctrl+C` bricht den laufenden Turn ab.

---

## 4. Anforderungen — Gesamttabelle

Jede Anforderung hat eine feste **Nummer**, eine **Kategorie (Typ)**, den **UX-Zugang** (wo/wie der
Nutzer sie erreicht), **Priorität** (MoSCoW), **Meilenstein**, **Status** sowie **Beschreibung** und
**Abnahmekriterium**.

**Legende**

- **Typ:** `F` = funktional (was das System tut) · `T` = technisch (Robustheit/Build/Performance/Sicherheit) · `UX` = Nutzererlebnis & Accessibility (Darstellung, Interaktion, wo/wie für den Nutzer zugänglich)
- **Status:** ✅ erledigt (Abnahme erfüllt/verifiziert) · 🟡 teilweise (begonnen, Abnahme offen) · ⬜ offen (nicht begonnen)
- **Prio:** M Muss · S Should · C Could · W Won't

| Nr | Typ | UX-Zugang (wo/wie für Nutzer) | Prio | Ms | Status | Beschreibung | Abnahmekriterium |
|---|---|---|---|---|---|---|---|
| FR-1 | F | CLI-Start + Statuszeile | M | M0 | ✅ | `goose acp` als Subprozess (stdio, JSON-RPC 2.0, ACP v2): initialize, session/new, session/prompt, session/cancel, session/close; Agent-Anfragen (permission, fs) beantworten | Handshake + session/new + session/prompt gegen echtes goose 1.53.0 verifiziert (acp-probe) |
| FR-2 | F | Chat-Stream + Statuszeile | M | M1 | ✅ | `agent_message_chunk` live inkrementell (Vorschau-Blöcke: Prosa roh, Tabellen wachsen zeilenweise mit, finale Blöcke ersetzen sie); `agent_thought_chunk` als 💭-Zeile in der Statuszeile; `tool_call(_update)` als Statuszeile **plus kollabierbare Tool-Karte im Chat** (Icon nach ACP-`kind`: $ execute · ↯ fetch · ✎ edit · ✚ create · ◦ think · 🔧 sonst; Titel · … läuft/✓ fertig/**✗ Fehler** via `isError`, In-place-Update über `toolCallId`, Teil der Fokus-/Shift-Auswahl-Kette) | interaktiver Stream ohne UI-Blockierung, Autoscroll mit einrastendem Follow |
| FR-3 | F | Chat-Blöcke | M | M1 | ✅ | Markdown in Blöcke zerlegen; Fenced-Code als CodeBlock (Kopfzeile + Copy-Aktion); Text/Tabellen über den eigenen Renderer; stabile Block-IDs | Codeblock mit Kopfzeile und COPY; neue ID pro Block |
| FR-4 | F | Konfig / Registrierung | M | M1 | ⬜ | erweiterbare Map `[lang] → Renderer` (copy/render-ascii/image/raw); eigene Text-Visualisierungen per Konfig ohne Kerncode-Eingriff | zusätzlicher Renderer wirkt nur über Registrierung, kein Kerncode nötig |
| FR-5 | F | Copy je Block (Maus + `alt+<i>`) | M | M1 | 🟡 | OSC52 primär, Fallback wl-copy → xclip → pbcopy (nicht blockierend); Aktion: `c copy` in der zentralen Hint-Leiste unten (Taste `c`; kopiert Auswahl, sonst fokussierten Block) und `alt+<i>`/`ctrl+<i>`; Feedback „✓ kopiert“ in derselben Zeile — Block-Footer sind info-only | Copy lokal wie über SSH geprüft per Einfügen; kein Secret-Leak |
| FR-6 | F | Chat-Block (PlantUML) | C | M1 | ✅ | Sprach-Tag `plantuml` via `plantuml -tutxt` → Unicode-ASCII (┌─┐ statt +-+); bei fehlendem plantuml/Fehler Roh-Text-Fallback | ASCII-Ausgabe bei vorhandenem plantuml; sauberer Fallback ohne |
| FR-7 | F | Eingabefeld, `Ctrl+C` | M | M1 | ✅ | gerahmtes Eingabefeld (Label oben, Hinweis unten), das mit der Eingabe mitwächst — harte Zeilenumbrüche (Ctrl+J) und weiche Umbrüche lange Zeilen zählen beide, nichts wird abgeschnitten; Enter sendet, Ctrl+J Zeilenumbruch; `Ctrl+C` bricht den Turn ab, zweites beendet die App | Senden, Umbruch, Abbruch und Beenden funktionieren; stop asynchron |
| FR-8 | F | Chat-Fläche, Kopf-/Fusszeile | M | M1 | ✅ | Nachrichtenliste mit Autoscroll + einrastendem Follow (Scroll/Selection pausiert), Scrollback, stabile Block-IDs, Kopf-/Fusszeile | langer Turn wird nicht abgeschnitten; Scroll bezieht sich auf stabile Blöcke |
| FR-9 | F | Policy-Konfig (später Dialog) | S | M2 | ⬜ | Policy `allow/deny/ask` beantwortet `session/request_permission`; Tool-Aktivität live, nicht blockierend | jede Policy über Konfig wählbar; Antworten korrekt über OnRequest |
| FR-10 | F | Flags / Env / TOML | S | M2 | 🟡 | v0 Flags+Env (`--goose`, `--theme`, `--cwd`, `--permission`), v1 TOML (goose_bin, cwd, permission_policy, show_thinking, plantuml_bin, renderer-Routing) | Konfig wirkt ohne Codeänderung; unbekannte Schlüssel ignoriert/gemeldet |
| FR-11 | T | Fehlermeldungen im Chat | S | M2 | ⬜ | JSONL-Journal (Zustand nach Neustart), goose-Neustart bei Crash, `session/close` + SIGINT/SIGTERM-Handler, Request-/Stream-Timeouts | heron meldet und restartet bei goose-Crash; kein Hängen nach SIGINT |
| FR-12 | T | kein UI (Env/CI) | S | M2 | ⬜ | `GOOSE_DISABLE_KEYRING=1` + Provider-Credentials via ENV für unbeaufsichtigte/CI-Umgebungen | startet ohne Keyring-Abfrage bei gesetzten ENV-Credentials |
| FR-13 | F | Tabs / Session-Umschaltung | S | M3 | ⬜ | Zuordnung chat → sessionId, nur Delta senden; bei History-Mismatch stateless-Replay; Capability-Check `loadSession` | Fortsetzen ohne token-überdimensionierten Replay; >10 % Sonderfälle → stateless |
| FR-14 | T | kein UI (Build) | M | M0 | ✅ | flake.nix (Overlay `pkgs.heron`, packages, devShells), nix/heron.nix buildGoModule, Makefile run/build/test/lint/tidy | `go test ./...` + `nix develop -c go build` grün; `nix build .#default` nach vendorHash |
| FR-15 | T | kein UI (Tests/Doku) | M | M0 | ✅ | Unit-Tests je Paket, Re-Exec-Fake-Goose, Bubble-Tea-Model-Tests; Doku tech-stack/reference-book/Katalog/README | Suite grün in devShell ohne goose/Netzwerk (verifiziert) |
| FR-16 | UX | Chat-Fläche (Fließtext) | M | M1 | ✅ | lesbarer Absatz mit Umbruch an Terminal-Breite; Standard = System-/Terminal-Font, truecolor, Unicode korrekt (Umlaute, Box-Drawing, Emoji) | Antwort erscheint als lesbarer Absatz, keine Überläufe, System-Font ohne Konfig |
| FR-17 | UX | Theme / Konfig | C | später | ⬜ | Schriftart/-größe über Thema/Theme anpassbar; Default bleibt System-Font (FR-16) | Konfig wirkt auf die Textdarstellung; Default unverändert |
| FR-18 | UX | Chat-Fläche (leer) | C | M1 | ⬜ | ASCII-Art-Grüßung, solange nichts im Chat, statt des textuellen Platzhalters; passt in die Fläche, verschwindet mit erster Nachricht | ASCII-Art bei leerem Chat erscheint, danach normaler Chat; kein Layout-Bruch |
| FR-19 | F | Eingabefeld: `@`-Dropdown | S | M2 | ⬜ | `@` öffnet ein Datei-Popup des Arbeitsverzeichnisses; weiteres Tippen filtert, Enter fügt den Pfad ein, Esc schließt | Dropdown öffnet/filtert/fügt ein; nichts wird versehentlich gesendet |
| FR-20 | F | Eingabefeld: `/`-Popup | S | M2 | ✅ | `/` zeigt den Autovervollständigungsvorschlag inline in der Eingabezeile (abgedimmt); `↑↓` wechselt bei mehreren Treffern, `→`/`tab` übernimmt, `enter` führt aus, `esc` blendet aus; `/model` öffnet die Modellliste mit Suchzeile (Tippen filtert, alle von goose gemeldeten Modelle inkl. glm-5.3-flash/flashx; das Fenster fasst die Liste komplett, der Cursor startet auf dem aktiven Modell — zurückschalten ist ein enter, ✓ markiert das live laufende Modell auch beim Scrollen), `/effort` die Stufenliste ohne Suche (kurze Liste); `/new` startet eine frische Session (Chat und Token-Zähler zurück, laufender Turn blockiert), `/quit` beendet die App; Auswahl wirkt live via `session/set_config_option`, gegen echtes goose verifiziert; unbekannte Befehle melden einen Fehler statt zu senden | Auswahl wirkt sofort auf die laufende Session; unbekannter Befehl meldet Fehler |
| FR-21 | F | Chat-Block (Shell-Ausgabe) | S | M2 | ⬜ | `!` führt das Kommando im Arbeitsverzeichnis der Session aus, Ausgabe als Block mit Copy; respektiert FR-9 | `!pwd` zeigt korrektes Verzeichnis; `ask` fragt, `deny` blockiert |
| FR-22 | UX | `/model`-Liste (Reihenfolge) | C | M2 | ⬜ | häufig/zuletzt genutzte Modelle oben, Rest in Katalog-Reihenfolge | nach Nutzung erscheint das Modell beim nächsten `/model` oben |
| FR-23 | UX | Status-Fusszeile unter dem Eingabefeld | S | M4 | ✅ | Modell und Effort live in der Box (`modell · effort`, aus ACP configOptions bzw. Flags); In-/Out-Tokens als Session-Summen (↑/↓) und Context-Balken im Footer (letzter Turn vs. Fenster, Flag `-context-window`, Default 200k; goose liefert die Werte in `stopReason.usage`, gegen echtes goose verifiziert); Reasoning-Tokens, Preis und Dauer später auf Abruf | Summen + Balken mit Prozent nach dem ersten Turn sichtbar; kein Layout-Bruch |
| FR-24 | F | Tab-Leiste über dem Chat | S | M3 | ⬜ | alle Chats chronologisch als Tabs (älteste links); beim Start ist die Session aktiv, die im jeweiligen Ordner zuerst gestartet wurde | Tabs in Start-Reihenfolge; Klick/Keys laden Chat, Status und Fusszeile |
| FR-25 | F | CLI: `heron` / `heron -c` | S | M3 | 🟡 | `heron` legt beim Start eine neue Session im Ordner an, `heron -c` übernimmt die letzte Session desselben Ordners — Umgesetzt: `-c` wählt aus `session/list` die neueste Session mit passendem cwd und lädt sie via `session/load` (Verlauf replayt als Blöcke, FR-44); Fehlschlag fällt auf eine neue Session zurück (Hinweis im Chat) | zweiter Start ohne `-c` = zweite Session; `-c` stellt Verlauf her |
| FR-26 | UX | Button oben links (Such-Popup) | C | M3 | ⬜ | Session-Suche (Zeit + Volltext) über alle Sessions; Treffer aktivieren die zugehörige Tab | Popup öffnet, Suchbegriff filtert, Auswahl springt in die Session |
| FR-27 | F | Chat (Bild) | C | M4 | 🟡 | PlantUML/mermaid-PNG über Kitty-Graphics mit **Unicode-Platzhaltern**: das Bild ist Zellen im Textfluss — steht am Ort des Codeblocks und scrollt mit; Aspekt über echte Zellpixel (Probe: CSI 16 t + Graphics/Platzhalter-Query beim Start; `HERON_IMAGES=force|off`, `HERON_IMAGE_DEBUG=1` → /tmp/heron-images.log); ohne Platzhalter ASCII-Fallback (FR-6); **`t` am fokussierten Block (Fokus via `tab`) schaltet zwischen Bild (Standard) und rohem Zaun-Quelltext um** (im Quelltext-Modus zusätzlich `i` zurück zum Bild — der Neon-Buchstabe im IMAGE-Kasten) — Quelltext-Ansicht ist ein voller CodeBlock (Label, COPY), `c`/enter kopieren dort den Quelltext, im Bild-Modus die ASCII-Grafik | ImageBlock-/Probe-/Escapes-Tests; Sichtprüfung im echten kitty/ghostty offen |
| FR-28 | F | Chat (mermaid-Block) | C | M5 | 🟡 | mermaid-Fence über die Diagramm-Pipeline: mermaid-cli (`mmdc`, `MERMAID_BIN`) → PNG → Kitty-Protokoll (FR-27); ohne mmdc bleibt der Klartext-CodeBlock | mit installiertem mmdc PNG sichtbar; ohne mmdc Klartext ohne Crash (getestet) |
| FR-29 | UX | Maus-/Tastatur-Selection | S | M1 | ✅ | Space markiert den fokussierten Block, Mausklick auf den Blockkörper toggelt die Markierung (Neon-Rahmen am ganzen Block — kein MARKED-Chip mehr, Copy-UI lebt im Eingabefeld-Chip) — **Einfach-Auswahl**: höchstens EIN Block ist markiert, eine neue Markierung ersetzt die alte (additive Mehrfachauswahl bleibt FR-42/43 vorbehalten); die Markierung überlebt die Diagramm-Konvertierung (Zaun→Bild); `y` kopiert die Auswahl als rohes Markdown ohne ANSI; Esc löscht zuerst die Selektion | eingefügte Auswahl = unverändertes Markdown ohne Stil-Codes; nie mehr als ein MARKED-Chip; FR-5 bleibt |
| FR-30 | UX | Think-Block im Chat | S | M1 | ✅ | Thinking als klappbarer Block im Chat: lange Zeilen brechen an Wortgrenzen um (nichts wird abgeschnitten), zusammengeklappt zeigt der Block die letzten 3 Zeilen (das Ende des Denktexts), aufklappen alles; enter, Klick auf den Blockkopf oder die zentrierte Fuß-Aktion `[ + show more ]` / `[ - show less ]` klappt um — die Aktion erscheint nur, wenn Zeilen versteckt sind (Klick löst die Block-Aktion aus, nicht Copy; Kopf-Klick setzt keine Markierung), Fold-State überlebt Stream-Refreshes; der Blockkopf nennt den Block, der Fuß zeigt nur die Aktion; **Interaktionsmodell (gilt für alle Blöcke): der aktivierende Buchstabe im Label ist immer Neon-Violett fett (`+`/`-` beim Fold, `C` bei COPY, `T`/`I` beim Bild-Switch — die Taste ist der Buchstabe), Hover highlightet exakt die getroffene Zelle (Kopf ODER Fuß-Aktion, ein Kasten im Footer — nie mehrere gleichzeitig: weiß + unterstrichen, Neon-Buchstabe bleibt), Tastatur-Fokus zeigt sich am Neon-Rahmen des ganzen Blocks (kein Invertiert-Label mehr; **Fokus-Kette (Zwei-Modus-Modell)**: STANDARD ist der Eingabe-Modus — das Eingabefeld ist selektiert (Neon-Rahmen, blinkender Cursor), Getippe landet dort; `↑`/`↓` tun im Eingabe-Modus NICHTS (für die spätere Eingabe-History reserviert); `tab` toggelt in den Antwort-Modus: direkt das letzte Chat-Segment der KI-Antwort (z. B. Codeblock) selektiert, `↓`/`↑` laufen dort blockweise runter/hoch und **stoppen an beiden Kettenenden** — der Rückweg ins Eingabefeld führt **ausschließlich über `tab`** (`esc` bleibt der Reset auf den Standard-Zustand: Selection weg, Block-Fokus weg, Eingabefeld selektiert); im Antwort-Modus erreicht Getippe das Eingabefeld NICHT (Textarea geblurrt — nicht selektiert = nicht interaktionsfähig); `Bild ↓`/`Bild ↑` scrollen IMMER seitenweise, auch vom Eingabefeld aus)** | langer Think-Text wird umgebrochen + aufklappbar; kein Layout-Bruch |
| FR-31 | F | Status-/Fusszeile + Tool-Karten | S | M4 | ✅ | Anzahl der Tool-Calls des letzten Turns anzeigen; Tool-Karten weiterhin kollabierbar (FR-2) — Footer-Segment „🔧 n tool call(s)“ vor der Token-Anzeige, Zähler = distinct `toolCallId`s je Turn (In-place-Updates zählen einfach), `startTurn` resettet; Karten-Icon nach ACP-`kind` ($ execute · ↯ fetch · ✎ edit · ✚ create · ◦ think · 🔧 sonst) und Fehler-Call als ✗ (`isError`-Content oder `status:"failed"`) | Turn mit n Tool-Calls zeigt „n tool calls“; Zähler pro Turn korrekt (`TestToolCallFooterCounter`, `TestToolCardKindGlyphAndErrorState`) |
| FR-32 | F | Tool-Block (apply/reset) | S | M4 | ⬜ | Diff-Statistik `+n`/`−n` Zeilen je Turn; apply wendet den Patch an, reset nimmt zurück; danach Zähler auf 0 | Statistik korrekt; apply/reset nullt die Anzeige; `deny` verhindert apply |
| FR-33 | F | Fehler-Block + Retry im Chat | S | M2 | ✅ | Fehler (Abbruch/Timeout/Crash) als markierter Block mit Icon und Retry-Aktion (letzten Prompt erneut) statt Fließtext — **ErrorBlock** (⚠ API-Fehler-Karte): API-/RPC-Fehler des Turns erscheinen gerahnt mit `r retry`-Klappe (Footer) UND Taste `r` am fokussierten Block; Retry entfernt die Karte und sendet denselben Prompt erneut (replyMsg trägt den Prompt mit); Karte ist volles Ketten-Segment (Fokus/Shift-Auswahl/Zeichen-Modus), ohne Retry-Prompt entfällt die Aktion | fehlgeschlagener Turn zeigt Block + Retry; Retry ersetzt den Block; ohne Fehler kein Block (`TestErrorBlockRetry`) |
| FR-34 | UX | überall (Status/Aktionen) | S | M4 | ⬜ | konsistente Icons (🔧 💭 ⚠ ✓ 📋 🔄 📊 🖼 ⌨ …), nie nur Farbe (NFR-9) | jede Statusgruppe hat ein Icon; in farblosem Terminal erkennbar |
| FR-35 | UX | Kopfzeile: LED-Kreis | S | M1 | ⬜ | Arbeitsindikator: LED-Kreis hell/animiert während eines Turns, dunkel im Ruhezustand; Symbolwechsel für farblose Terminals | LED folgt dem Turn-Zustand; ohne Farbe erkennbar; Event-Loop unblockiert |
| FR-36 | UX | überall (Übergänge) | C | M4 | ⬜ | dezente Animationen (Block-Einblenden, LED-Puls, Tab-Wechsel, „COPIED“-Feedback), per `--no-anim` abschaltbar | sichtbare, abschaltbare Übergänge; UI flüssig (NFR-1/NFR-2) |
| FR-37 | UX | Chat-Blöcke (Code) | M | M1 | ✅ | Syntax-Highlighting in Codeblöcken über chroma (Lexer pro Sprach-Tag, Catppuccin-Mocha passend zum Theme); unbekannte Tags fallen auf Klartext zurück | Code in Go/Python/Shell/JSON farbig; falscher Tag rendert ohne Fehler |
| FR-38 | UX | Chat (Bild) — Änderung an FR-27 | S | M1 | ✅ | Der Diagrammblock toggelt mit **einer** Taste `t` (Neon-Marker `T`) zwischen Bild (Standard) und rohem Zaun-Quelltext; die bisherige Zusatztaste `i` zum Zurückwechseln entfällt — `t` wechselt in beide Richtungen (`t`→`t` = vorheriger Zustand), konsistent mit `+`/`-`/`C` als Ein-Tasten-Aktionen | nur `t` schaltet Bild↔Text am fokussierten Block; `i` löst keine Bild-Aktion mehr aus; Neon-Marker nur `T`; kein doppelter Frame-Aufbau |
| FR-39 | UX | überall (Fokus-Kette) | S | M1 | ⬜ | Zu jedem Zeitpunkt hat höchstens EIN Element den Neon-Fokus-Rahmen (Eingabefeld im Eingabe-Modus, genau ein Chat-Segment im Antwort-Modus). Mehrere Rahmen gleichzeitig sind verboten; die Fokus-Kette setzt Neon ausschließlich auf das aktuell selektierte Segment und entfernt ihn vom vorherigen atomar | Invariante „≤1 Neon-Rahmen“ über der Fokus-Kette per Test; beim `↑`/`↓`/`tab`-Wechsel wandert der Rahmen atomar, ohne Zwischenzustand mit zwei Rahmen |
| FR-40 | UX | Chat (Antwort-Modus) | S | M2 | ⬜ | Der Antwort-Modus (`tab`) bewegt den Block-Cursor (`↑`/`↓`) über ALLE Chat-Segmente — nicht nur Code/Bild, sondern auch reine Textabsätze. Überschriften (`#`) können zusätzlich als einzelne Cursor-Ankerpunkte dienen (Springen von Überschrift zu Überschrift innerhalb/zwischen Antworten). Textblöcke sind damit fokussierbar (Neon-Rahmen) und außerhalb des Fokus weiterhin nicht textinteraktiv | jeder Textabsatz einzeln anwähl- und fokussierbar; Überschriften optional als Sprungziele; Getippe erreicht das Eingabefeld nur im Eingabe-Modus |
| FR-41 | UX | Chat (Enter) | C | M3 | ⬜ | `enter` auf dem fokussierten Segment öffnet dessen Interaktionsmodus: in Code- und Textblöcken ist der Cursor **frei beweglich** (Zeichen-/Zeilen-Ebene), in Tabellen wird genau **eine Spalte** aktiv markiert — links steht die aktive Zellreferenz (`A1`) samt Spaltenüberschrift. `esc` beendet den Modus zurück zum Block-Cursor | Code/Text: freie Cursor-Navigation blockintern; Tabelle: genau eine Spalte markiert + Zellreferenz/Überschrift links; esc verlässt sauber |
| FR-42 | UX | Chat (Selektion) — erweitert FR-29 | S | M3 | 🟡 | Gedrückte `shift` startet die Auswahl: der Cursor markiert einen Bereich beliebig granular — blockweise ÜBER mehrere Blöcke hinweg, in Textblöcken bis auf **Satz-/Zeichenebene** (z. B. nur ein Satzmittelstück). Mehrere getrennte Abschnitte sind **additiv** markierbar. Die Auswahl wird visuell hervorgehoben; `ctrl+c` kopiert den gesamten markierten Bereich als **rohes Markdown**. **Umgesetzt:** `shift+enter` IST ein Texteditor-Caret (nur lesend) über dem Chat-Text — Pfeile bewegen es in **alle Richtungen** (Buchstabe/Zeile, über Sektionsgrenzen hinweg, Rahmen-Bloecke werden uebersprungen), `shift+Pfeile` waehlen vom Anker (Richtungsschlag wie im Editor), `c`/`enter` kopieren (blockuebergreifend, Leerzeile zwischen Sektionen), `esc` kollabiert erst die Auswahl und verlaesst dann; die Block-Auswahl startet per `shift+↑/↓` an der Leseposition (fokussiertes Segment bzw. Antwort-Modus) — aus der Eingabe bewusst NICHT mehr (User-Report: unfreiwilliger Modus-Wechsel, „im Cursor-Mode gefangen"); der ERSTE Schritt greift sofort Anker-Einheit PLUS den nächsten Abschnitt (kein „verschenkter" Erstdruck, User-Report: PlantUML brauchte 2 statt 1 Drücke); `shift+enter` uebernimmt eine laufende Block-Auswahl als Zeichen-Auswahl ueber demselben Bereich (sie bleibt sichtbar + erweiterbar) und schaltet ein Diagramm an der Leseposition automatisch (Auto-`t`) auf den Zaun-Quelltext — das Caret spawnt darin statt den Block zu ueberspringen (als gerendertes Bild ohne Quelle bleibt er unadressierbar — keine Zellen); Erreicht die Caret-Bewegung ein Diagramm-BILD mit Quelle, schaltet heron es AUTOMATISCH auf den Zaun-Quelltext um (Auto-`t` beim Überqueren — der Block wird buchstabenfähig, auch als RAND-Block auswählbar, User-Report „im cursor mode kann ich kein mermaid diagramm auswählen"); der Blockrand-Sprung endet im ERSTEN Diagramm auf dem Pfad (Auto-`t` entlang des GESAMTEN Überspring-Pfads — kein Mehrfach-Sektionen-Sprung mehr, User-Report „springt eine unbekannte Zahl an Abschnitten"); der Toggle-AUSTRITT (`shift+enter`) konvertiert NICHTS — ob der Modus offen ist, ist fuer die Auswahl bedeutungslos: Sie bleibt ANKER..CARET in ihrer feinen Granularitaet erhalten (User-Report „der Anker bleibt der selbe, von da aus beginnt unser Select — jetzt wählt er alles aus, das ist falsch, er soll vom Anker starten"; die fruehere Ganzblock-Konversion beim Austritt im Anker-Block ist entfallen); die erhaltene Auswahl bleibt AUCH AUSSERHALB des Modus SICHTBAR (Highlight ohne Caret) und per `c`/`enter` kopierbar, bis `esc` sie löst (User-Report „wenn ich Cursor-Mode schließe wird die Selection entfernt, gehe ich wieder rein ist sie wieder drin — das soll so nicht sein"); nur bei leerer Auswahl gilt der alte Toggle (Modus raus, Position hält); der Toggle-KREISLAUF hält Anker und Caret EXAKT (kein Verschieben auf Blockkanten, User-Report „cursor mode verschiebt den Ankerpunkt") — einziger Anker-Reset ist `esc` (auch im Block-Modus, Rest-Auswahl wird mitgeräumt); shift+↑/↓ starten JEDES Select von der gesetzten CURSOR-POSITION als Anker (User-Report: „ich setze meinen Cursor bei Zeichen 50 — egal ob ich was selektiert habe oder nicht, das ist mein Anker, von da aus beginnt mein Select; er selektiert NICHT den ganzen Schnippsel; das Select expandiert nicht nach unten und nicht nach rechts, weil ich shift+up gedrückt habe; 2× shift+down — der Ankerpunkt ist immer noch der selbe, nur esc kann ihn auf null setzen") — auch außerhalb des Modus, auch ohne laufende Auswahl: Die Auswahl wächst KLASSISCH als Zeichen-Auswahl ab dem Anker, der Caret läuft Blockrand für Blockrand in Druckrichtung (shift+up erfasst zunächst den Ausschnitt ÜBER dem Anker bis zum Blockanfang — nicht den ganzen Block, nichts unterhalb —, weitere Drücke ganze Blöcke darüber; shift+down spiegelig; der Caret darf den Anker passieren — die Auswahl kippt auf die andere Seite, der Anker hält char-scharf; ein Schritt, der auf dem Anker-Rand landet, springt sofort einen weiter — kein verschenkter Erstdruck); kein Neu-Ankern am Viewport-Rand, kein Ganzblock-Neustart, kein Wiedereintritt in den Modus; überquerte (auch unfaehige) Blöcke zählen GANZ, Diagramm-Bilder mit Quelle werden unterwegs per Auto-t adressierbar; erst NACH esc (Anker auf null) startet shift+↑/↓ wieder als frische Block-Auswahl an der Leseposition; nicht adressierbare Segmente OHNE Quelle (User-Schiene, Tabelle, Bild ohne src) nimmt die Auswahl GANZ auf — Rahmen leuchtet live im Neon, die Kopie enthält den gezäunten Quelltext wenn verfügbar. die Auswahl faerbt bg Akzent + fg Hell-Weiss (Kontrast auch ueber Ueberschriften-FG) und ist pro Zelle re-armiert (zeichenweise gestylte Zeilen brechen die Flaeche nicht mehr ab); die Caret-Kante der Auswahl ist zusaetzlich INVERTIERT abgehoben (Richtungs-Marker: unten bei Ausdehnung nach unten, oben nach oben, Anker-Seite nur Akzent-Flaeche); `t` ist im Zeichen-Cursor-Modus stumm (kein Moduswechsel, der Caret/Auswahl den Block entziehen wuerde). **Offen:** additive Mehrfachauswahl, `ctrl+c` als Copy-Taste | Auswahl über ≥1 ganzen Codeblock + teilweisen Satz; Einfügen = Markdown ohne ANSI; visuelle Selektion sichtbar; mehrere Abschnitte additiv |
| FR-43 | UX | Chat (Selektion) | S | M3 | 🟡 | „Pausen“-Selektion: eine begonnene Auswahl bleibt **bis `esc`** bestehen und übersteht anschließende Cursor-Bewegung — weitere Abschnitte können angesprungen und Sektionen übersprungen werden, ohne die Auswahl zu verlieren; erst `esc` beendet sie endgültig. Danach ist die normale Navigation wieder frei. **Umgesetzt:** Auswahl-Verlust NUR per esc — Pfeile kollabieren die Auswahl nicht mehr (Anker haelt, die Auswahl folgt dem Caret; Block-Auswahl ueberlebt die Fokus-Bewegung innerhalb ihres Bereichs). Startet eine NEUE Auswahl (frischer shift-Anfang, neue Block-Markierung — der Zeichen-Cursor-Toggle konvertiert dagegen ZURÜCK in eine Block-Auswahl statt zu demoten) oder verlaesst der Fokus die laufende, wird die alte zur VERGANGENEN Auswahl delegiert: Ihr Rahmen bleibt sichtbar, in der verblichenen Sekundaer-Farbe (Theme.StaleColor — Rahmenzeichen klar von der AKTUELLEN Auswahl im Neon unter-schieden, User-Wunsch); hoechstens eine vergangene Generation. esc loest alle Generationen auf (Block-Modus: eine Stufe; Zeichen-Cursor: eigene Auswahl Stufe 1 kollabiert, Stufe 2 verlaesst — wartet unter ihm eine Block-Auswahl, verlaesst esc den Cursor SOFORT in sie zurueck, ohne Kollaps-Zwischenschritt; erst das weitere esc loest sie, User-Report: „Auswahl weg → wieder da" wirkte wie ein Bug); esc raeumt auch die Caret-Restposition (Wiedereintritt startet frisch an der Leseposition — Positions-Halten bleibt dem Toggle `shift+enter` vorbehalten) und parkt beim Lösen einer Auswahl den Fokus auf der Leseposition (Antwort-Modus bleibt, kein Sprung ins Eingabefeld — User-Report; erst das weitere esc räumt den Fokus und kehrt zur Eingabe). **Offen:** additive Mehrfachauswahl ueber mehrere getrennte Bereiche gleichzeitig | Selektion übersteht Cursor-Bewegung; erst `esc` löst auf; danach normale Navigation |
| FR-44 | F | Eingabefeld: `/resume` | S | M2 | 🟡 | `/resume` listet die gespeicherten goose-Sessions (ACP `session/list` — goose-Erweiterung neben dem Standard, paginiert über opaque `nextCursor`, newest first) in einem Picker mit Suchzeile (Tippen filtert über Titel/Ordner/Modell, ↑↓/enter wie FR-20); `enter` lädt die gewählte Session via `session/load` in einem frischen goose-Prozess — der Verlauf replayt als fertige Blöcke (Nutzerzeile, final gerenderte Antwort inkl. Diagramm-Pipeline, Tool-Karten; Thoughts werden nicht replayt), Modell/Effort-Labels kommen aus der Load-Response, Chat und Token-Zähler starten neu; laufender Turn blockiert (wie `/new`); Fehler in Liste/Load melden sich als Block im Chat | Auswahl lädt den Verlauf sichtbar als Blöcke; Prompts laufen in der geladenen Session weiter (HERON_E2E_GOOSE: load + 111 Replay-Items + Antwort bezog sich auf die damalige Nachricht; `acp-probe -list`/`-load`) |
| FR-45 | F | Eingabefeld: `/provider` | S | M2 | ✅ | Provider-Wechsel im laufenden Chat: `/provider` öffnet den Picker über die ACP-configOptions „provider“ (Value/Name-Paare, Suche filtert über Name UND Wert, aktiver Provider steht zuerst, die Liste zeigt NUR die in goose eingerichteten Provider — Quelle ist goose selbst (`_goose/unstable/config/read`, key „providers": der providers:-Block, nicht das ~90 Einträge katalogisierende Inventar); die Picker-Suche öffnet bei Eingabe den Vollkatalog, nicht Eingerichtetes bleibt also erreichbar); enter sendet `session/set_config_option("provider", value)` — die Antwort trägt die ERFRISCHTEN configOptions (neue Modell-/Effort-Liste und Provider-Label landen via `acp.ParseConfig` in Picker und Footer); Footer zeigt `Provider · Modell · Effort`; während eines Turns blockiert der Wechsel (wie /new) | Auswahl wechselt den Provider sichtbar (Footer-Label + neue Modellliste); Prompts laufen danach gegen den neuen Provider (`TestProviderPickerFlow`; Live-Check Z.AI Coding Plan → Nous Research → Z.AI (GLM)) |
| FR-46 | F | Eingabefeld: Queue während des Turns | S | M2 | ✅ | Prompts, die während eines laufenden Turns abgeschickt werden, landen in einer Warteschlange (ohne User-Block) und gehen nach sauberem Turn-Ende automatisch einzeln raus (User-Block → Turn → Antwort); nach einem Fehler bleibt die Queue stehen (Retry entscheidet); **doppeltes ctrl+c** (< 0,75 s) quittiert/verwirft die Warteschlange mit sichtbarem Feedback — ohne Queue gilt weiter: 1× ctrl+c bricht den Turn ab, 2× beendet heron; Working-/Hint-Leiste zeigt `⏳ n wartend` | Queue sendet automatisch nach dem Turn; 2× ctrl+c verwirft; Versand einzeln und geordnet (`TestQueueWhileRunningAndDoubleCtrlC`, `TestQueueDrainsAfterTurn`) |
| NFR-1 | T | — | M | M1 | ⬜ | Streaming-Start < ~100 ms; kein sichtbares Ruckeln bei langen Turns | Messung im E2E; Follow bleibt flüssig |
| NFR-2 | T | — | M | M1 | ✅ | Event-Loop nie durch I/O blockiert; goose-Reader als Goroutine, Ergebnisse als tea.Msg | UI reagiert während des Turns (send läuft als tea.Cmd) |
| NFR-3 | T | — | S | M2 | ⬜ | Terminal-Tiers erkennen (truecolor/256, OSC52 ja/nein, Kitty/Sixel) | korrektes Verhalten in ghostty, kitty, xterm |
| NFR-4 | T | — | M | M1 | 🟡 | saubere Seams (acp/render/tui), Renderer-Plugins | neue Renderer/Seams ohne Kernänderung |
| NFR-5 | T | Doku | S | — | 🟡 | schlanke Pakete, klare Verantwortungen, Entscheidungs-Log im Stack-Doc | Review-freundlich; Einstieg < 30 min |
| NFR-6 | T | — | M | M2 | 🟡 | Permissions respektieren; keine Secrets (ENV-Credentials) ausgeben/loggen | kein Credential in Log/Journal; Policy wirkt |
| NFR-7 | T | Fehlermeldungen | S | M2 | ⬜ | Crash-Journal, Prozess-Restart, Timeouts | siehe FR-11 |
| NFR-8 | T | — | M | M0 | ✅ | Fake-Goose, Model-Tests, CI ohne echte goose-Installation | Suite grün ohne goose (verifiziert) |
| NFR-9 | UX | überall (Barrierefreiheit) | C | M4 | ⬜ | Farbe nie als einziges Medium; hoher Kontrast; umschaltbare Themes | Review mit farblosem Terminal möglich |
| NFR-10 | UX | UI-Texte zentral | C | M4 | ⬜ | UI-Texte zentral (deutsch als Ausgangssprache; Übersetzung nachrüstbar) | Texte nicht hart über Code verstreut |

**Abnahme-Übersicht (Stand 2026-10-10):** ✅ FR-1, FR-2, FR-3, FR-6, FR-7, FR-8, FR-14, FR-15, FR-16,
FR-20, FR-23, FR-29, FR-30, FR-37, NFR-2, NFR-8 · 🟡 FR-5, FR-10, FR-25, FR-27, FR-28, FR-33, FR-42, FR-43, FR-44, NFR-4, NFR-5, NFR-6 · ⬜ übrige (17 offen).

**Abnahme-Hinweise (wie verifiziert):** FR-14 `nix build .#default` · FR-15/NFR-8 Suite ohne goose ·
FR-3 Modell-Test (Kopfzeile + COPY + stabile ID, per pty-Screenshot bestätigt) · FR-6 echter
`plantuml`-Lauf **und** Fallback ohne Binary · FR-16 Width-/Unicode-Tests · NFR-2 Cmd läuft als Goroutine.
**FR-5 bleibt 🟡:** OSC52 + Fallback-Kette und Maus/Tastatur sind getestet (inkl. Stub-Tools, ohne das
echte Clipboard anzusprechen) — das tatsächliche Einfügen in die Zwischenablage ist hier nicht automatisierbar.
**B03:** FR-2 gegen **echtes goose** verifiziert (`HERON_E2E_GOOSE=1`: 2 Live-Chunks während der Anfrage),
Follow-Verhalten per Modell-Test · FR-7 Ctrl+C-Abbruch per Test mit Cancel-Stub (kein E2E gegen goose);
Zeilenumbruch ist **Ctrl+J**, weil bubbletea v1 Enter (CR) und Shift+Enter (LF) nicht unterscheidet.
**FR-25/FR-44 (Sessions übernehmen):** gegen echtes goose 1.55.0 verifiziert — `session/list` paginiert
(123 Sessions über 3 Seiten), `session/load` replayt den Verlauf (111 Items, Modell/Effort aus der
Load-Response) und ein Prompt nach dem Load läuft in der geladenen Session weiter (Antwort bezog sich
auf die damalige Nachricht); der TUI-Flow (Picker, Suche, Replay-Blöcke, Blockaden) ist per Modell-Tests
abgedeckt, `acp-probe -list`/`-load` dienen der Sichtprüfung.

---

## 5. Umsetzungsbatches (je 4 Anforderungen)

Die 46 Anforderungen sind in **12 Arbeitspakete zu je 4** gegliedert (Arbeitsreihenfolge:
M0 → M1 → M2 → M3 → M4 → optional; 11 × 4 + Rest 2). Ein Batch wird gemeinsam freigegeben
(Gate §7) und zusammen abgenommen; „Status je Item“ zeigt den Stand der vier Anforderungen.

| Batch | Fokus | Anforderungen | Ms | Status je Item |
|---|---|---|---|---|
| B01 | M0-Basis: Build/Test/ACP verifiziert absichern | FR-14, FR-1, FR-15, NFR-8 | M0 | ✅ ✅ ✅ ✅ |
| B02 | M1 Render-/Copy-Kern: Codeblock, Copy-Box, PlantUML, Fließtext | FR-3, FR-5, FR-6, FR-16 | M1 | ✅ 🟡 ✅ ✅ |
| B03 | M1 Streaming & Turn: inkrementell, Follow, Abbruch | FR-2, FR-7, FR-8, NFR-2 | M1 | ✅ ✅ ✅ ✅ |
| B04 | M1 Kern-UX: Renderer-Routing, Selection-Copy, Think-Kürzung, LED | FR-4, FR-29, FR-30, FR-35 | M1 | ⬜ ⬜ ⬜ ⬜ |
| B05 | M1 Abschluss & Qualität: ASCII-Art, Latenz, Seams, Doku | FR-18, NFR-1, NFR-4, NFR-5 | M1 | ⬜ ⬜ 🟡 🟡 |
| B06 | M2 Robustheit/Konfig: TOML-Config, Retry, Permissions, Journal/Restart | FR-10, FR-33, FR-9, FR-11 | M2 | 🟡 🟡 ⬜ ⬜ |
| B07 | M2 Eingabe-Modi: `@`-Dateien, `/`-Commands, `!`-Shell, Modell-Sortierung | FR-19, FR-20, FR-21, FR-22 | M2 | ⬜ ⬜ ⬜ ⬜ |
| B08 | M2 Technik: Headless, Terminal-Tiers, Secrets, Timeouts | FR-12, NFR-3, NFR-6, NFR-7 | M2 | ⬜ ⬜ 🟡 ⬜ |
| B09 | M3 Sessions & Tabs: Persistenz, Tab-Leiste, Start-Flags, Suche | FR-13, FR-24, FR-25, FR-26 | M3 | ⬜ ⬜ 🟡 ⬜ |
| B10 | M4 Status & Tools: Fusszeile, Tool-Count, Git-Diff, Icons | FR-23, FR-31, FR-32, FR-34 | M4 | ⬜ ⬜ ⬜ ⬜ |
| B11 | M4 Visual & A11y: Bild-Protokoll, Animationen, Kontrast, I18n | FR-27, FR-36, NFR-9, NFR-10 | M4 | ⬜ ⬜ ⬜ ⬜ |
| B12 | Optional/später: Schriftgröße, Mermaid (2 von 4) | FR-17, FR-28 | später/M5 | ⬜ ⬜ |

---

## 6. Offene Fragen

1. Wie granular sollen Sprache-Overrides zur Laufzeit konfigurierbar sein (static Map vs. Plugin-Loader)?
2. PlantUML-Bild (Kitty/Sixel) in v1 oder erst v2? (Beeinflusst FR-27 Abnahme.)
3. Soll heron mehrere goose-Profile/Provider parallel verwalten (v0: nein)?
4. Wo landet das JSONL-Journal (XDG_STATE_HOME/heron)?
5. „Ask“-Policy: eigener Inline-Dialog (M4) oder nur allow/deny bis dahin?
6. Overlay zentral in system flake (configuration.nix) einbinden oder pro-user? — Doku sowieso.
7. Effort-Stufen: welche bietet das goose-Backend real an (high/medium/low?), und wie werden sie gesetzt (FR-20)?
8. Preis-Schätzung (FR-23): Token-Preisliste hardcoded, konfigurierbar oder aus Backend-Metadaten?

## 7. Arbeitsablauf / Gate (verbindlich)

> Konflikte: Diese Tabelle hat Vorrang. Kein Feature wird umgesetzt, das nicht hier
> etikettiert ist.

| Schritt | Werkzeug | Regel |
|---|---|---|
| **1. Wunsch** | Diskussion (hier) | Feature nur beschreiben, **nicht** implementieren |
| **2. Katalog-Aufnahme** | `docs/Anforderungskatalog.md` | Feature bekommt eine **FR-/NFR-ID** in der Gesamttabelle, MoSCoW-Prio, Typ (F/T/UX), Status und Abnahmekriterium |
| **3. Freigabe** | UI-Nutzer | explizites „GO“ (Satz/Stichwort) des Nutzers |
| **4. Umsetzung** | heron | nur FR ab Schritt 3; kleine Schritte, Tests |
| **5. Abnahme** | Nutzer + Tests | gegen das Abnahmekriterium prüfen; Status auf ✅ setzen |

**Grundsatz:** Kein zusätzliches Feature, keine ANTI-Feature-„Kleinigkeiten“ ohne FR.
Nicht freigegebene FRs sind „Could“/„Won't“ oder explizit markiert — sie werden nicht gebaut.
Statuswerte werden nur bei nachgewiesener Abnahme aktualisiert (🟡 → ✅).

## 8. Externe Referenzen

- `docs/tech-stack.md` — Stack & Entscheidungs-Log H1–H8
- `docs/reference-book.md` — Best Practices
- `newelle-goose-acp/PLAN.md` (workspace) — verifiziertes ACP-Protokoll-Mapping / Session-Strategie / Permissions
