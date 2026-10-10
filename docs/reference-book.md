# Referenzbuch: Best Practices für Coding-Agent-TUIs

> Sammelstelle von Architekturmustern bekannter Coding-Agents (Zed, Claude Code,
> OpenCode, Aider, Codex CLI, goose, …), destilliert für den Eigenbau **heron**.

## 0. Leitidee: Agent und Renderer strikt trennen

- Die sauberste Anordnung ist **"Pager und Agent als separate Prozesse über ACP/JSON-RPC"**. [Zed/ACP] [goose]
- Folge für heron: goose liefert nur Text-Events; **das Rendering gehört komplett dem Client** → Override (Copy-Box, PlantUML) wird möglich, ohne goose zu forken.

## 1. Streaming & inkrementelles Rendering

- **Blockweise rendern, nicht zeichengranular.** Einen Codefence nicht mitten im Block final rendern; davor den letzten Block als "live" zeigen, beim Blockabschluss final. [OpenCode] [Claude Code]
- **Follow-Modus:** Nach unten scrollen wenn der Nutzer nichts tut; bei Scroll/Selection deaktivieren, bis er zurück zum Boden springt. [Claude Code] [Zed]
- **Stabile Block-IDs**, damit Aktualisierungen in-place passieren. [dex]

## 2. Rendering-Semantik übersteuern (Kernfeature)

- Renderer muss **Language-Tag-agnostisch** erweiterbar sein: Kopfzeile + Aktionsset pro Tag (copy / render-ascii / image / raw). [goose/ACP]
- **Copy-Box:** OSC 52 > wl-copy/xclip/pbcopy; nur OSC 52 ist über SSH verlässlich. Nummerierte Blöcke (keyboard-first) als Access-Fallback. [allgemein]

## 3. Kontext-/Session-Management

- **Persistente Sessions mit Delta** sind effizient, aber Fragility > ~10% macht stateless Replay billiger. Capability-Detect (`loadSession`) vorab. [goose]
- **Compaction** (ACP-UNSTABLE) nicht als v1-Feature bauen; Kontext-Transparenz (`/status`) ist Pflicht. [Claude Code]

## 4. Permission-Modell

- Ebenen-Modell von **Claude Code**: normal / `acceptEdits` / Plan. Skalierbare Kontrolle ohne Reibung.
- heron: **policy-basiert** (`allow`/`deny`/`ask`), automatisch via `OnRequest`; später per-Call-UI. Tool-Aktivität live zeigen. [Claude Code]

## 5. Tool-Ausführung & Status

- `agent_message_chunk` vs `tool_call(_update)` getrennt behandeln: Text-Chunks kontinuierlich, Tool-Calls als kollabierbare Karten. [OpenCode]
- Nichts blockieren: Tool-Ausführung liegt bei goose; der Client zeigt nur an.

## 6. Nicht-blockierende UI / Nebenläufigkeit

- Event-Loop (Bubble Tea) nie mit I/O blockieren; `goose acp`-Reader als Goroutine, Ergebnisse als tea.Msg re-injizieren. [bubbletea]
- `stop()` asynchron: Flag + `session/cancel` senden, auf Turn-Ende warten, statt hart killen. [goose]

## 7. Robustheit & Recovery

- Crash-feste Journalierung (JSONL) der Session, damit Neustart Zustand rekonstruiert. [dex]
- **Prozess-Restart bei Crash:** goose-Subprozess überwachen und sauber neu spawnen. [PLAN]
- Sauberer Shutdown: `session/close` + Terminierung; signal-Notifier auf SIGTERM/SIGINT. [go]

## 8. Terminal-Integration (technisch)

- **OSC 52** für Clipboard (SSH-sicher). [H4]
- **Kitty-Graphics/Sixel** für Bild-Output (PlantUML-PNG). ghostty: Kitty.
- truecolor-Erkennung mit 256-Fallback; Unicode-Box-Drawing nur bei passendem TERM.
- Kitty-Maus-/Keyboard-Protokoll für präzises Klick-Targeting (Copy-Box).

## 9. Testen von TUIs & Agenten

- Bubble-Tea-Tests auf Model-Ebene (`View()`-Snapshots, Msg-Injektion) — kein Terminal nötig. [bubbletea]
- **Re-Exec-Fake-Goose** (Helper-Process spielt JSON-RPC-Sequenzen) → Client-Tests ohne goose. [PLAN]

## 10. Accessibility & Farbe

- Farbe nie als einziges Medium (Status nur-Farbe vs. nur-Symbol); Kontrast + umschaltbare Themes. [Claude Code]

---

**Nutzung:** konkrete Entscheidungen stehen in `tech-stack.md` (Log, Sektion 7).
