# Technologiestack — heron

> **Stand:** 2026-10-08 · Backend **goose** (1.53.0) · Frontend **Eigenbau** · Ziel: overrideables/erweiterbares Markdown-Rendering

## 0. Vorgaben (verifiziert)

| Punkt | Wert |
|---|---|
| Agent | goose 1.53.0, `goose acp` (stdio, JSON-RPC 2.0, ACP v2) |
| Frontend | **heron** — Eigenbau in Go / Bubble Tea |
| Kernfeature | Copy-Box je Codeblock, PlantUML-Interpretation, beliebige Text-Visual-Erweiterungen |
| Umgebung | NixOS 26.11 · Terminal ghostty (`xterm-ghostty`, truecolor, Kitty-Graphics, OSC52) |
| ACP-Mapping | aus `newelle-goose-acp/PLAN.md` übernommen (Notifications, Session, Permissions) |

## 1. Sprache & Laufzeit
- **Go (>= 1.21)**, kompiliert zu **einer statischen Binary** — ideal für Terminal-Distribution, kein Interpreter-Env nötig.
- Nebenläufigkeit über Goroutinen/Channels — passt zum ACP-Streaming und zum Bubble-Tea-Message-Passing.

## 2. TUI & Rendering

| Schicht | Bibliothek (charmbracelet) | Rolle |
|---|---|---|
| TUI-Loop | **`bubbletea`** | async-Event-Loop, Message-Passing, Alt-Screen, Maus/Keyboard |
| Bausteine | **`bubbles`** | `viewport` (Scroll/Follow), `textarea` für Eingabe |
| Styling | **`lipgloss`** | Stile, Box-Drawing, Farben |
| Markdown | **`glamour`** | Markdown → ANSI (Standard-Render) |
| Override-Ebene | **eigene `internal/render`** | Fences parsen → CodeBlock mit Copy-Box + Language-Routing |

**Override-Mechanik (Kern):**
1. `splitFences` extrahiert ```-Blöcke aus dem Markdown-Stream.
2. Jeder Codeblock → `CodeBlockView(lang, code, i)` mit Header + `[copy i]`-Box.
3. Pro **Language-Tag** Routing: `plantuml` → `plantuml -ttxt` (ASCII); Standard → roh; sonst → glamour.
4. Der Rest (Absätze, Listen…) läuft durch `glamour`.

## 3. Terminal-Integration

| Aspekt | Ansatz |
|---|---|
| Clipboard | **OSC 52** (`ESC ]52;c;base64 BEL`) primär; Fallback wl-copy/xclip/pbcopy folgt |
| Diagramme | v1 `plantuml -ttxt` ASCII; v2 PNG via Kitty-/Sixel-Image-Protokoll (ghostty) |
| Farbe/Unicode | truecolor, Box-Drawing via lipgloss |
| Scroll | `bubbles/viewport` mit einrastendem Follow |

## 4. ACP-Anbindung (goose)

- **Transport:** stdio `goose acp`, JSON-RPC 2.0, ein Message pro Zeile (`internal/acp`).
- **Client:** Raw-JSON-RPC (M0-Spike); Reader-Goroutine + Channel-Dispatch. SDK-Ersatz tauscht nur `internal/acp` aus.
- **Session:** persistent (Map `chat → sessionId`, Delta), stateless-Fallback bei Mismatch; Capability-Check `loadSession`.
- **Permissions:** Policy (allow/deny/ask) → `OnRequest`-Hook beantwortet `session/request_permission`.
- **Notifications:** `agent_message_chunk`, `agent_thought_chunk`, `tool_call(_update)`, `plan`, `usage_update`, `session_update`; `compaction_*` UNSTABLE → v1 ignorieren.
- **Headless:** `GOOSE_DISABLE_KEYRING=1` + ENV-Credentials.

## 5. Konfiguration
- v0: Flags + env (`--goose`, `GOOSE_...`). Später: TOML via eigenem Loader.

## 6. Build & Tooling (Nix)

- **`flake.nix`:** Overlay (`overlays.default` → `pkgs.heron`), `packages.default`, `devShells.default`.
- **`nix/heron.nix`:** `buildGoModule` (statische Go-Binary); `DELETE`-Venv & plantuml als `buildInputs`.
- **devShell (`nix develop`):** go + plantuml + wl-clipboard; `GOTOOLCHAIN=local`.
- Tests: `go test` + **Re-Exec-Fake-Goose** (Helper-Process) ohne echte goose-Installation.

## 7. Entscheidungen (Log)

| ID | Entscheidung | Grund | revidierbar wenn |
|---|---|---|---|
| H1 | **Go / Bubble Tea** | von Nutzer entschieden; statische Binary, kompakte Event-Loop | — |
| H2 | Rendering-Override in eigener Schicht (splitFences) statt glamour-Rohstrom | einzige Weise, Copy-Box/PlantUML pro Block zu steuern | glamour bekommt Block-Hooks |
| H3 | Raw-JSON-RPC für ACP (M0) | minimal, testbar per Fake | Byte-Stream-Details zäh → SDK |
| H4 | OSC52 primär + native Fallbacks | SSH-taugliche Copy-Box | nur-lokal-Workflows |
| H5 | persistent Sessions, stateless-Fallback | Token-Sparsamkeit, identisch PLAN D2 | >10% Alignment-Sonderfälle |
| H6 | plantuml -ttxt ASCII v1, Bild v2 | sofort nutzbar; ghostty kann Bild | bessere Diagrammtools |
| H7 | Test via Re-Exec-Fake-Goose | CI ohne goose-Installation | — |
| H8 | **Flake + Overlay** | deklarative, reproduzierbare Builds; `pkgs.heron` überall via Overlay | — |
