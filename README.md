# heron

Eigenbau-ACP-TUI in **Go / Bubble Tea**: **goose**-Backend, eigene Render-Ebene mit
übersteuerbarem Markdown-Rendering (Copy-Box je Codeblock, PlantUML-Interpretation …).

- [Technologiestack](docs/tech-stack.md)
- [Referenzbuch](docs/reference-book.md)

## Warum (kurz)
ACP trennt Agent (goose) und Frontend strikt. Rendering gehört komplett dem Client →
der TUI-Eigenbau besitzt die Render-Ebene und kann Codeblock-Semantik (copy / PlantUML / …)
frei übersteuern, ohne goose zu forken.

## Setup & Build (Nix / Flake)
```sh
nix develop          # Dev-Shell: go, plantuml, wl-clipboard
go mod tidy          # deps (bubbletea, bubbles, glamour, lipgloss)
go test ./...
go run .

nix build .#default  # paketierter Bau (Overlay: pkgs.heron)
```
## Nutzung
```sh
heron            # neue goose-Session im aktuellen Ordner
heron -c         # letzte Session dieses Ordners fortsetzen (FR-25)
heron -demo      # ohne goose (TUI-Test)
```
Im Prompt: `/model` · `/provider` (Provider-Wechsel inkl. erfrischter Modellliste,
FR-45) · `/effort` · `/resume` (frühere Sessions laden, FR-44) · `/new` ·
`/quit`. Prompts während eines laufenden Turns wandern in eine Queue (2× ctrl+c
verwirft sie, FR-46); API-Fehler erscheinen als ⚠-Karte mit `r retry` (FR-33). `acp-probe -list`/`-load <id>` proben das Protokoll gegen
echtes goose.

## Struktur
```
flake.nix                   # Overlay (pkgs.heron) + devShell + packages
nix/heron.nix               # buildGoModule-Package
main.go                     # CLI-Einstieg
internal/acp/client.go      # ACP JSON-RPC-Client gegen `goose acp` (stdio)
internal/render/            # Render-Schicht: Fences parsen, Copy-Box, plantuml
internal/clipboard/         # OSC52 + Fallbacks
internal/tui/               # Bubble-Tea-Frontend
```

## Nächste Schritte
1. M0-Spike: `goose acp` Handshake/Session/Prompt per `internal/acp` verifizieren.
2. M1: TUI mit Render-Override (Copy-Box + PlantUML) gegen goose verbinden.
