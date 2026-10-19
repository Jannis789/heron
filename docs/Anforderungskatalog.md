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
| FR-2 | F | Chat-Stream + Statuszeile | M | M1 | ✅ | `agent_message_chunk` live inkrementell (Vorschau-Blöcke: Prosa roh, Tabellen wachsen zeilenweise mit, finale Blöcke ersetzen sie); `agent_thought_chunk` als 💭-Zeile in der Statuszeile; `tool_call(_update)` als Statuszeile | interaktiver Stream ohne UI-Blockierung, Autoscroll mit einrastendem Follow |
| FR-3 | F | Chat-Blöcke | M | M1 | ✅ | Markdown in Blöcke zerlegen; Fenced-Code als CodeBlock (Kopfzeile + Copy-Aktion); Text/Tabellen über den eigenen Renderer; stabile Block-IDs | Codeblock mit Kopfzeile und COPY; neue ID pro Block |
| FR-4 | F | Konfig / Registrierung | M | M1 | ⬜ | erweiterbare Map `[lang] → Renderer` (copy/render-ascii/image/raw); eigene Text-Visualisierungen per Konfig ohne Kerncode-Eingriff | zusätzlicher Renderer wirkt nur über Registrierung, kein Kerncode nötig |
| FR-5 | F | Copy je Block (Maus + `alt+<i>`) | M | M1 | 🟡 | OSC52 primär, Fallback wl-copy → xclip → pbcopy (nicht blockierend); Mausklick und Keyboard `alt+<i>`/`ctrl+<i>`; Feedback „COPIED“ | Copy lokal wie über SSH geprüft per Einfügen; kein Secret-Leak |
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
| FR-25 | F | CLI: `heron` / `heron -c` | S | M3 | ⬜ | `heron` legt beim Start eine neue Session im Ordner an, `heron -c` übernimmt die letzte Session desselben Ordners | zweiter Start ohne `-c` = zweite Session; `-c` stellt Verlauf her |
| FR-26 | UX | Button oben links (Such-Popup) | C | M3 | ⬜ | Session-Suche (Zeit + Volltext) über alle Sessions; Treffer aktivieren die zugehörige Tab | Popup öffnet, Suchbegriff filtert, Auswahl springt in die Session |
| FR-27 | F | Chat (Bild) | C | M4 | 🟡 | PlantUML/mermaid-PNG über Kitty-Graphics mit **Unicode-Platzhaltern**: das Bild ist Zellen im Textfluss — steht am Ort des Codeblocks und scrollt mit; Aspekt über echte Zellpixel (Probe: CSI 16 t + Graphics/Platzhalter-Query beim Start; `HERON_IMAGES=force|off`, `HERON_IMAGE_DEBUG=1` → /tmp/heron-images.log); ohne Platzhalter ASCII-Fallback (FR-6); **`t` am fokussierten Block (TAB fokussiert) schaltet zwischen Bild (Standard) und rohem Zaun-Quelltext um** — Quelltext-Ansicht ist ein voller CodeBlock (Label, COPY), `c`/enter kopieren dort den Quelltext, im Bild-Modus die ASCII-Grafik | ImageBlock-/Probe-/Escapes-Tests; Sichtprüfung im echten kitty/ghostty offen |
| FR-28 | F | Chat (mermaid-Block) | C | M5 | 🟡 | mermaid-Fence über die Diagramm-Pipeline: mermaid-cli (`mmdc`, `MERMAID_BIN`) → PNG → Kitty-Protokoll (FR-27); ohne mmdc bleibt der Klartext-CodeBlock | mit installiertem mmdc PNG sichtbar; ohne mmdc Klartext ohne Crash (getestet) |
| FR-29 | UX | Maus-/Tastatur-Selection | S | M1 | ✅ | Space markiert den fokussierten Block, Mausklick auf den Blockkörper toggelt die Markierung (MARKED-Chip, invertierte Prosa); `y` kopiert die Auswahl als rohes Markdown ohne ANSI; Esc löscht zuerst die Selektion | eingefügte Auswahl = unverändertes Markdown ohne Stil-Codes; FR-5 bleibt |
| FR-30 | UX | Think-Block im Chat | S | M1 | ✅ | Thinking als klappbarer Block im Chat: lange Zeilen brechen an Wortgrenzen um (nichts wird abgeschnitten), zusammengeklappt zeigt der Block die letzten 3 Zeilen (das Ende des Denktexts), aufklappen alles; enter, Klick auf den Blockkopf oder die zentrierte Fuß-Aktion `[ + mehr anzeigen ]` / `[ − weniger anzeigen ]` klappt um — die Aktion erscheint nur, wenn Zeilen versteckt sind (Klick löst die Block-Aktion aus, nicht Copy; Kopf-Klick setzt keine Markierung), Fold-State überlebt Stream-Refreshes; der Blockkopf nennt den Block, der Fuß zeigt nur die Aktion — mit demselben Hover-/Fokus-Stil wie COPY (unterstrichen bei Hover, invertiert bei Fokus, gemeinsamer actionStyle) | langer Think-Text wird umgebrochen + aufklappbar; kein Layout-Bruch |
| FR-31 | F | Status-/Fusszeile + Tool-Karten | S | M4 | ⬜ | Anzahl der Tool-Calls des letzten Turns anzeigen; Tool-Karten weiterhin kollabierbar (FR-2) | Turn mit n Tool-Calls zeigt „n tool calls“; Zähler pro Turn korrekt |
| FR-32 | F | Tool-Block (apply/reset) | S | M4 | ⬜ | Diff-Statistik `+n`/`−n` Zeilen je Turn; apply wendet den Patch an, reset nimmt zurück; danach Zähler auf 0 | Statistik korrekt; apply/reset nullt die Anzeige; `deny` verhindert apply |
| FR-33 | F | Fehler-Block + Retry im Chat | S | M2 | 🟡 | Fehler (Abbruch/Timeout/Crash) als markierter Block mit Icon und Retry-Aktion (letzten Prompt erneut) statt Fließtext | fehlgeschlagener Turn zeigt Block + Retry; Retry ersetzt den Block; ohne Fehler kein Block |
| FR-34 | UX | überall (Status/Aktionen) | S | M4 | ⬜ | konsistente Icons (🔧 💭 ⚠ ✓ 📋 🔄 📊 🖼 ⌨ …), nie nur Farbe (NFR-9) | jede Statusgruppe hat ein Icon; in farblosem Terminal erkennbar |
| FR-35 | UX | Kopfzeile: LED-Kreis | S | M1 | ⬜ | Arbeitsindikator: LED-Kreis hell/animiert während eines Turns, dunkel im Ruhezustand; Symbolwechsel für farblose Terminals | LED folgt dem Turn-Zustand; ohne Farbe erkennbar; Event-Loop unblockiert |
| FR-36 | UX | überall (Übergänge) | C | M4 | ⬜ | dezente Animationen (Block-Einblenden, LED-Puls, Tab-Wechsel, „COPIED“-Feedback), per `--no-anim` abschaltbar | sichtbare, abschaltbare Übergänge; UI flüssig (NFR-1/NFR-2) |
| FR-37 | UX | Chat-Blöcke (Code) | M | M1 | ✅ | Syntax-Highlighting in Codeblöcken über chroma (Lexer pro Sprach-Tag, Catppuccin-Mocha passend zum Theme); unbekannte Tags fallen auf Klartext zurück | Code in Go/Python/Shell/JSON farbig; falscher Tag rendert ohne Fehler |
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
FR-20, FR-23, FR-29, FR-30, FR-37, NFR-2, NFR-8 · 🟡 FR-5, FR-10, FR-27, FR-28, FR-33, NFR-4, NFR-5, NFR-6 · ⬜ übrige (21 offen).

**Abnahme-Hinweise (wie verifiziert):** FR-14 `nix build .#default` · FR-15/NFR-8 Suite ohne goose ·
FR-3 Modell-Test (Kopfzeile + COPY + stabile ID, per pty-Screenshot bestätigt) · FR-6 echter
`plantuml`-Lauf **und** Fallback ohne Binary · FR-16 Width-/Unicode-Tests · NFR-2 Cmd läuft als Goroutine.
**FR-5 bleibt 🟡:** OSC52 + Fallback-Kette und Maus/Tastatur sind getestet (inkl. Stub-Tools, ohne das
echte Clipboard anzusprechen) — das tatsächliche Einfügen in die Zwischenablage ist hier nicht automatisierbar.
**B03:** FR-2 gegen **echtes goose** verifiziert (`HERON_E2E_GOOSE=1`: 2 Live-Chunks während der Anfrage),
Follow-Verhalten per Modell-Test · FR-7 Ctrl+C-Abbruch per Test mit Cancel-Stub (kein E2E gegen goose);
Zeilenumbruch ist **Ctrl+J**, weil bubbletea v1 Enter (CR) und Shift+Enter (LF) nicht unterscheidet.

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
| B09 | M3 Sessions & Tabs: Persistenz, Tab-Leiste, Start-Flags, Suche | FR-13, FR-24, FR-25, FR-26 | M3 | ⬜ ⬜ ⬜ ⬜ |
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
