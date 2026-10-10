# Examples

Static and live renders of heron's block widgets — inspect the box chrome,
footers and copy actions without a running goose session.

## Render an example reply

```sh
go run ./examples/render examples/reply.md
```

- `-width N` — one-shot render at N cells (no live resizing)
- no `-width` + terminal — **live mode**: re-renders on terminal resize
  (SIGWINCH) so the dynamic column layout can be evaluated; Ctrl+C quits
- no `-width` + pipe — one-shot render at the detected width

The renderer parses the markdown with `render.Parse` and prints every block
widget (`tui.BuildWidgets`) exactly like the TUI would lay it out. Table
chrome (borders, row separators) renders dim via the theme border color;
cells stay uncolored.

## Sketch comparison

`sketch.md` is the reference table from the design sketch — render it at the
sketch width to compare:

```sh
go run ./examples/render -width 44 examples/sketch.md
``` (flags work in any position)

`elm.md` is the two-column wrap benchmark from the pi comparison:

```sh
go run ./examples/render examples/elm.md
```

Add more inputs by dropping `.md` files next to `reply.md` and passing their
path.
