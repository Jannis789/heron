package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"heron/internal/render"
)

// ErrorBlock ist die Karte eines gescheiterten Turns (FR-33): API-/RPC-
// Fehler des Prompts erscheinen nicht als Fliesstext, sondern als
// gerahmter Block mit ⚠-Kopf und — wenn der Prompt bekannt ist — einer
// Retry-Aktion: "r" auf dem fokussierten Block sendet ihn erneut (der
// Block wird dafuer entfernt, der Nutzer-Block bleibt im Chat). Wie jede
// Karte ist auch diese ein volles Ketten-Segment (Fokus, Shift-Auswahl,
// Zeichen-Modus).
type ErrorBlock struct {
	id      int
	message string
	// prompt ist der gescheiterte Prompt — leer bedeutet: kein Retry
	// moeglich (dann fehlt die Footer-Aktion).
	prompt string
	focused bool
	marked  bool
	// stale: Teil der VERGANGENEN Auswahl (FR-43) — Rahmen in der
	// verblichenen Sekundaer-Farbe.
	stale     bool
	th        *render.Theme
	copyFn    CopyFunc
	hoverFoot bool
}

// NewErrorBlock baut die Fehler-Karte; Append vergibt die Blocknummer.
func NewErrorBlock(message, prompt string, th *render.Theme, copyFn CopyFunc) ErrorBlock {
	return ErrorBlock{id: -1, message: message, prompt: prompt, th: th, copyFn: copyFn}
}

func (b ErrorBlock) bodyLines() []string {
	return strings.Split(strings.TrimRight(b.message, "\n"), "\n")
}

// wrappedBody wrappt die Meldung an der Innenbreite (FR-30-Geist).
func (b ErrorBlock) wrappedBody(inner int) []string {
	if inner <= 0 {
		return b.bodyLines()
	}
	var out []string
	for _, ln := range b.bodyLines() {
		if ln == "" {
			out = append(out, "")
			continue
		}
		out = append(out, render.WrapWords(ln, inner)...)
	}
	return out
}

func (b ErrorBlock) bodyRows(width int) int { return len(b.wrappedBody(codeInnerWidth(width))) }

func (b ErrorBlock) Height(width int) int {
	h := 2 + b.bodyRows(width) // Kopf + Body + Bodenlinie
	if b.retryable() {
		h++ // Footer-Klappe mit der Retry-Aktion
	}
	return h
}

// retryable: nur mit bekanntem Prompt gibt es eine Retry-Aktion.
func (b ErrorBlock) retryable() bool { return b.prompt != "" }

// stateLabel: Kopf-Caption der Karte — ⚠, fester Titel, Falt-Marke.
func (b ErrorBlock) stateLabel() string {
	l := "⚠ API-Fehler"
	if b.marked {
		l += " ✓"
	}
	return l
}

func (b ErrorBlock) actionLabel() string { return "r retry" }

func (b ErrorBlock) action() (string, string) {
	action := b.actionLabel()
	return action, styledAction(b.th, action, "r", b.hoverFoot)
}

func (b ErrorBlock) View(width int) string {
	if width < 16 {
		width = 16
	}
	inner := codeInnerWidth(width)
	// Fokus ODER Auswahl am RAHMEN (Neon-Violett); die VERGANGENE Auswahl
	// (stale, FR-43) traegt die Sekundaer-Farbe — wie Think/Tool/Code.
	bs := borderStyleFor(b.th, b.focused || b.marked, b.stale)
	ls := b.th.LabelStyle()

	label := b.stateLabel()
	labelW := runewidth.StringWidth(label)
	dashes := width - 5 - labelW
	if dashes < 0 {
		dashes = 0
	}
	lines := make([]string, 0, b.Height(width))
	lines = append(lines, bs.Render("┌─ ")+actionStyle(b.th, b.hoverFoot).Render(label)+
		bs.Render(" "+strings.Repeat("─", dashes)+"┐"))
	for _, ln := range b.wrappedBody(inner) {
		txt := ls.Render(ln)
		gap := inner - lipgloss.Width(txt)
		if gap < 0 {
			gap = 0
		}
		lines = append(lines, bs.Render("│")+" "+txt+strings.Repeat(" ", gap)+" "+bs.Render("│"))
	}
	if b.retryable() {
		action, styled := b.action()
		lines = append(lines, render.CenteredActionLine(width, action, styled,
			runewidth.StringWidth(action), bs))
	} else {
		lines = append(lines, bs.Render("└"+strings.Repeat("─", width-2)+"┘"))
	}
	return strings.Join(lines, "\n")
}

func (b ErrorBlock) Update(msg tea.Msg) (Widget, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok || !b.focused {
		return b, nil
	}
	if km.String() == "r" && b.retryable() {
		nb := b
		nb.focused = false
		prompt, id := b.prompt, b.id
		return nb, func() tea.Msg { return retryPromptMsg{prompt: prompt, blockID: id} }
	}
	return b, nil
}

func (b ErrorBlock) Focused() bool   { return b.focused }
func (b ErrorBlock) Focusable() bool { return true }
func (b ErrorBlock) SetFocused(f bool) Widget {
	nb := b
	nb.focused = f
	return nb
}

func (b ErrorBlock) SetHover(on bool) Widget {
	nb := b
	nb.hoverFoot = on && b.retryable()
	return nb
}

// SetHoverAt: nur die Footer-Klappe ist ein Klickziel (Retry), der Kopf
// hat keine Aktion — zielgenau wie am ToolBlock.
func (b ErrorBlock) SetHoverAt(width, x, y int) Widget {
	nb := b
	nb.hoverFoot = b.retryable() && b.HitAction(width, x, y)
	return nb
}

func (b ErrorBlock) SetMarked(on bool) Widget {
	nb := b
	nb.marked = on
	return nb
}

func (b ErrorBlock) Marked() bool { return b.marked }

func (b ErrorBlock) SetStale(on bool) Widget {
	nb := b
	nb.stale = on
	return nb
}

// Markdown: Meldung als Copy-Payload (FR-29 — roh, ohne ANSI).
func (b ErrorBlock) Markdown() string {
	return b.message
}

func (b ErrorBlock) Resize(width int) Widget { return b }

func (b ErrorBlock) HitAction(width, x, y int) bool {
	if !b.retryable() {
		return false
	}
	if y != 1+b.bodyRows(width) {
		return false
	}
	start, end := render.CenteredActionRange(width, runewidth.StringWidth(b.actionLabel()))
	return x >= start && x < end
}

// Action: Klick auf die Klappe ist Retry (wie die Taste r).
func (b ErrorBlock) Action() (Widget, tea.Cmd) {
	if !b.retryable() {
		return b, nil
	}
	nb := b
	nb.focused = false
	prompt, id := b.prompt, b.id
	return nb, func() tea.Msg { return retryPromptMsg{prompt: prompt, blockID: id} }
}

// Copy: die Meldung (der Chip im Eingabefeld-Footer, alt+<n>).
func (b ErrorBlock) Copy() (Widget, tea.Cmd) {
	if b.copyFn == nil {
		return b, nil
	}
	return b, b.copyFn(b.Markdown())
}

func (b ErrorBlock) ID() int { return b.id }
func (b ErrorBlock) SetID(i int) Widget {
	nb := b
	nb.id = i
	return nb
}

// ---- Zeichen-Modus (FR-42): Buchstaben-Cursor in der Meldung --------------

func (b ErrorBlock) CharLines(width int) []string { return b.wrappedBody(codeInnerWidth(width)) }
func (b ErrorBlock) CharPad() (int, int)          { return 2, 1 }
func (b ErrorBlock) CharLeft() string             { return "│ " }
