# Footer-Spezifikation (Info im Block-Footer, Aktionen unten)

Status: umgesetzt. Gilt fuer die Abschlusszeile jeder gerahmten Box.

## Grundsatz
- **Der Block-Footer ist reine INFORMATION** (nicht klickbar, keine Aktionen).
- **Alle Aktionen** eines fokussierten Blocks stehen in der zentralen
  Aktions-/Hint-Leiste GANZ UNTEN (app.footerView) — eine Stelle, ganze
  Breite, kein  [ ] -Chrome pro Block.

## Prinzipien
1. Eine Abschlusszeile pro Box, immer vorhanden (kein Hoehen-/Scroll-Drift).
2. Label linksbendig, als `[ info ]` im Chrome einer `└──[ info ]──┘`-Zeile;
   es ist NICHT klickbar (kein ActionAt-Treffer).
3. Progressive Disclosure: ohne Fokus/Hover/Auswahl nur die leere Bahn
   `└──────┘` (Ausnahme ThinkBlock: Fold manchmal zentriert/sichtbar).
4. Schmal-Fallback: zuerst faellt das Info-Label, die Zeile bleibt genau
   `width` Zellen breit (Layout-Exact-Contract, s.u.).
5. Klick = Taste entfaellt fuer den Footer; die Maus bedient die zentrale
   Aktionsleiste (bei Bedarf).

## Zeilenexakte Breite (Kernfix gelegentlich)
- Mit Label:  n = width - 8 - lw - sum - sep  (sum/sep = Aktionsboxen, =0 hier).
- Faellt das Label:  n = width - 4 - sum - sep  — NICHT -8, sonst wuerde die
  Zeile 4 Zellen zu kurz (Exact-Width-Bug, layoutFooter).

## Info je Blockform (Footer)
| Form | Info (links) |
|------|--------------|
| TextBlock | keine Box, pur |
| CodeBlock | `<N> lines` |
| TableBlock | `<N> rows` |
| ThinkBlock | Kopf: `thinking ▸/▾ <N> lines` (Zustand im Kopf); Footer frei |
| ImageBlock, Bild, kein Zoom | `<W>x<H> px` (Pixel-Aufloesung) |
| ImageBlock, Bild, Zoom/Pan | `<W>x<H> px · ⤢ <N>%` |
| ImageBlock, Quelltext (TEXT) | `<N> lines` |
| ImageBlock, ASCII-Fallback | `<N> lines` |

## Aktionen — Copy lebt in der zentralen Hint-Leiste
- `c copy` steht in der Hint-Leiste unten (dort wo auch pan & zoom
  steht), sobald es ein Ziel gibt: die Auswahl (markierter Block) oder
  der fokussierte Block. Taste `c` kopiert als rohes Markdown; danach
  steht kurz `✓ kopiert` in derselben Zeile.
- Die Eingabebox-Rahmenlinien bleiben leer (crush-style).
- Die Block-Footer selbst sind INFO-ONLY: keine klickbaren Kaesten, kein
  COPIED/MARKED-Chip — die Auswahl zeigt der Neon-Rahmen (FR-39).

## Zentrale Hint-Leiste unten (app.footerView)
- Fokussierter Zoom-Block:  `+/- zoom · ←↑↓→ bewegen · enter/esc fertig`
- Fokussierter Diagramm-Block:  `⏎ pan & zoom · t text·bild`
- Jeder fokussierte Block:  `space auswählen · y yank · esc lösen`

## Tasten (Block)
- ImageBlock: `enter` Pan&Zoom ein, `esc`/enter aus; `+/-` zoom;
  `←↑↓→` pan; `t` toggelt EIN-TASTIG text<->bild (FR-38, kein `i`).
- ThinkBlock: `+/-` auf/zu, Kopf/Fuß-Klick klappt.
- Copy (alle markierbaren Blöcke): `c`/Klick auf den Eingabefeld-Chip,
  `y` yank der Auswahl, `alt+<n>` nummerierter Fallback.
