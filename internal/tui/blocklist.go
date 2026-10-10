package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// BlockList renders a stack of Widgets as one scrollable pager with keyboard
// focus cycling, mouse-wheel scrolling and hover/click on COPY actions. It is
// a value type in the Elm sense: Update returns the next state instead of
// mutating in place — the caller must adopt the returned BlockList.
type BlockList struct {
	// nextID is the last stable block number handed out (FR-3).
	nextID   int
	items    []Widget
	offset   int
	width    int
	height   int
	focus    int
	hoverIdx int
	follow   bool
}

func NewBlockList() BlockList {
	return BlockList{focus: -1, hoverIdx: -1, width: 100, follow: true}
}

func (l BlockList) Empty() bool   { return len(l.items) == 0 }
func (l BlockList) Focused() bool { return l.focus >= 0 && l.focus < len(l.items) }
func (l BlockList) Width() int {
	if l.width <= 0 {
		return 100
	}
	return l.width
}

func (l BlockList) Append(items ...Widget) BlockList {
	for _, it := range items {
		// Copyable blocks receive a stable 1-based number (FR-3), assigned
		// once and never changed, so keyboard copy (alt+<n>) and in-place
		// updates (PlantUML result) can address the block.
		if it.Focusable() && it.ID() < 1 {
			l.nextID++
			it = it.SetID(l.nextID)
		}
		l.items = append(l.items, it)
	}
	if l.follow {
		l = l.ScrollEnd()
	}
	return l
}

// UpdateBlock replaces the widget with the given stable id and keeps its
// focus — the in-place update path (FR-3), e.g. a PlantUML conversion result.
func (l BlockList) UpdateBlock(id int, w Widget) (BlockList, bool) {
	if id < 1 {
		return l, false
	}
	for i, it := range l.items {
		if it.ID() != id {
			continue
		}
		w = w.SetID(id)
		if it.Focused() {
			w = w.SetFocused(true)
		}
		l.items[i] = w
		return l, true
	}
	return l, false
}

// copyByID copies the block carrying the stable number n — the keyboard
// fallback of the copy box (FR-5). Reports whether a copy was triggered.
func (l BlockList) copyByID(n int) (BlockList, tea.Cmd, bool) {
	if n < 1 {
		return l, nil, false
	}
	for i, it := range l.items {
		if it.ID() != n {
			continue
		}
		nw, cmd := it.Copy()
		if cmd == nil {
			return l, nil, false
		}
		l.items[i] = nw
		return l, cmd, true
	}
	return l, nil, false
}

// SetSize stores the viewport size and lets every widget rebuild its layout
// cache for the new width (dynamic re-flow on terminal resize).
func (l BlockList) SetSize(w, h int) BlockList {
	l.width, l.height = w, h
	for i, it := range l.items {
		l.items[i] = it.Resize(w)
	}
	return l.clamp()
}

func (l BlockList) totalLines() int {
	total := 0
	for _, it := range l.items {
		total += it.Height(l.Width())
	}
	return total
}

func (l BlockList) ScrollEnd() BlockList {
	l.offset = l.totalLines()
	return l.clamp()
}

func (l BlockList) clamp() BlockList {
	total := l.totalLines()
	if l.height > 0 && l.offset > total-l.height {
		l.offset = total - l.height
	}
	if l.offset < 0 {
		l.offset = 0
	}
	return l
}

// all stacks every widget into a full-height line list (with blank padding).
func (l BlockList) all() (lines []string, total int) {
	w := l.Width()
	for _, it := range l.items {
		h := it.Height(w)
		view := strings.Split(it.View(w), "\n")
		for len(view) < h {
			view = append(view, "")
		}
		lines = append(lines, view...)
		total += h
	}
	return lines, total
}

func (l BlockList) View() string {
	lines, total := l.all()
	if total == 0 {
		return ""
	}
	if l.height <= 0 {
		return strings.Join(lines, "\n")
	}
	left := l.offset
	if left < 0 {
		left = 0
	}
	right := left + l.height
	if right > total {
		right = total
	}
	if left > right {
		left = right
	}
	return strings.Join(lines[left:right], "\n")
}

// Update routes a message to the pager and returns the next state plus an
// optional command. The bool reports whether the message was consumed.
func (l BlockList) Update(msg tea.Msg) (BlockList, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		return l.handleMouse(msg)
	case tea.KeyMsg:
		return l.handleKey(msg)
	}
	return l, nil, false
}

func (l BlockList) handleKey(k tea.KeyMsg) (BlockList, tea.Cmd, bool) {
	switch k.String() {
	case "up":
		l.offset--
		l.follow = false
		return l.clamp(), nil, true
	case "down":
		l.offset++
		l = l.clamp()
		if l.height > 0 && l.offset >= l.totalLines()-l.height {
			l.follow = true
		}
		return l, nil, true
	case "pgup":
		l.offset -= max(l.height, 1)
		l.follow = false
		return l.clamp(), nil, true
	case "pgdown":
		l.offset += max(l.height, 1)
		l = l.clamp()
		if l.height > 0 && l.offset >= l.totalLines()-l.height {
			l.follow = true
		}
		return l, nil, true
	case "tab":
		return l.focusNext(), nil, true
	case "enter", "c":
		if l.Focused() {
			w, cmd := l.items[l.focus].Update(k)
			l.items[l.focus] = w
			return l, cmd, true
		}
		return l, nil, false
	case "esc":
		if l.Focused() {
			return l.clearFocus(), nil, true
		}
		return l, nil, false
	}
	return l, nil, false
}

// hitAction returns the index of the widget whose COPY action lies at (x, y);
// y is relative to the top of the block list.
func (l BlockList) hitAction(x, y int) (int, bool) {
	w := l.Width()
	y0 := 0
	for i, it := range l.items {
		h := it.Height(w)
		if y >= y0 && y < y0+h {
			if it.HitAction(w, x, y-y0) {
				return i, true
			}
			return 0, false
		}
		y0 += h
	}
	return 0, false
}

// setHoverAt updates the hover highlight for a mouse position.
func (l BlockList) setHoverAt(x, y int) BlockList {
	idx := -1
	if i, ok := l.hitAction(x, y); ok {
		idx = i
	}
	if idx == l.hoverIdx {
		return l
	}
	if l.hoverIdx >= 0 && l.hoverIdx < len(l.items) {
		l.items[l.hoverIdx] = l.items[l.hoverIdx].SetHover(false)
	}
	if idx >= 0 {
		l.items[idx] = l.items[idx].SetHover(true)
	}
	l.hoverIdx = idx
	return l
}

func (l BlockList) handleMouse(msg tea.MouseMsg) (BlockList, tea.Cmd, bool) {
	if msg.Action == tea.MouseActionMotion {
		// The visible row is offset-relative; translate to absolute lines.
		return l.setHoverAt(msg.X, l.offset+msg.Y), nil, false
	}
	if msg.Action != tea.MouseActionPress {
		return l, nil, false
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		// Click on COPY copies the block content (markdown/code).
		if idx, ok := l.hitAction(msg.X, l.offset+msg.Y); ok {
			w, cmd := l.items[idx].Copy()
			l.items[idx] = w
			return l, cmd, true
		}
	case tea.MouseButtonWheelUp:
		l.offset--
		l.follow = false
		return l.clamp(), nil, true
	case tea.MouseButtonWheelDown:
		l.offset++
		l = l.clamp()
		if l.height > 0 && l.offset >= l.totalLines()-l.height {
			l.follow = true
		}
		return l, nil, true
	}
	return l, nil, false
}

func (l BlockList) focusNext() BlockList {
	n := len(l.items)
	if n == 0 {
		return l
	}
	start := l.focus + 1
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		if l.items[idx].Focusable() {
			return l.setFocus(idx)
		}
	}
	return l.clearFocus()
}

func (l BlockList) setFocus(idx int) BlockList {
	for i, it := range l.items {
		l.items[i] = it.SetFocused(i == idx)
	}
	l.focus = idx
	return l.reveal(idx)
}

func (l BlockList) clearFocus() BlockList {
	for i, it := range l.items {
		l.items[i] = it.SetFocused(false)
	}
	l.focus = -1
	return l
}

// reveal scrolls so the focused widget is visible.
func (l BlockList) reveal(idx int) BlockList {
	w := l.Width()
	y := 0
	for i := 0; i < idx; i++ {
		y += l.items[i].Height(w)
	}
	h := l.items[idx].Height(w)
	if y < l.offset {
		l.offset = y
		l.follow = false
	}
	if l.height > 0 && y+h > l.offset+l.height {
		l.offset = y + h - l.height
		l.follow = l.offset >= l.totalLines()-l.height
	}
	return l.clamp()
}
