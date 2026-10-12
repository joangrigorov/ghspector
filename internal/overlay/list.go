package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// List is a scrollable list overlay: the repository picker. It owns the cursor
// and the scroll window, and reports the selected row so the caller decides
// what selecting means.
//
// MaxRows is the caller's budget for how many rows fit. The overlay stack hands
// an overlay the width it may use, not the height — the box grows to its
// content — so a list that must not run off the screen is given its limit at
// construction, from the terminal size the route was built with.
type List struct {
	Scope   keymap.Scope
	Heading string
	Intro   []string
	// Rows are the entries, in display order.
	Rows []string
	// Empty is shown instead of the rows when there are none.
	Empty string
	// MaxRows is how many rows may be shown at once. Zero means all of them.
	MaxRows int
	// Cursor is the selected row.
	Cursor int
	// OnSelect reports the chosen row index.
	OnSelect func(index int) tea.Cmd
	// OnCancel runs when the list is dismissed. Nil is fine: it still closes.
	OnCancel tea.Cmd
}

// Update moves the cursor or reports a selection.
func (l List) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return l, nil
	}
	action, ok := keymap.Lookup(l.Scope, keymap.KeyString(k))
	if !ok {
		return l, nil
	}
	switch action {
	case keymap.ActionBack:
		return nil, l.OnCancel
	case keymap.ActionDown:
		if l.Cursor < len(l.Rows)-1 {
			l.Cursor++
		}
		return l, nil
	case keymap.ActionUp:
		if l.Cursor > 0 {
			l.Cursor--
		}
		return l, nil
	case keymap.ActionSelect:
		if len(l.Rows) == 0 || l.OnSelect == nil {
			return l, nil
		}
		return nil, l.OnSelect(l.Cursor)
	default:
		return l, nil
	}
}

// Title returns the box heading.
func (l List) Title() string { return l.Heading }

// Bindings returns the list's keys, from its scope.
func (l List) Bindings() []keymap.Binding { return keymap.For(l.Scope) }

// View renders the intro, then the visible window of rows with the cursor on
// the selected one.
func (l List) View(width int) string {
	lines := make([]string, 0, len(l.Intro)+1+len(l.Rows))
	for _, para := range l.Intro {
		lines = append(lines, wrap(para, width))
	}
	if len(l.Intro) > 0 {
		lines = append(lines, "")
	}

	if len(l.Rows) == 0 {
		empty := l.Empty
		if empty == "" {
			empty = "Nothing to choose from."
		}
		lines = append(lines, wrap(empty, width))
		return strings.Join(lines, "\n")
	}

	visible := l.MaxRows
	if visible <= 0 || visible > len(l.Rows) {
		visible = len(l.Rows)
	}
	start, end := layout.Window(len(l.Rows), l.Cursor, visible)
	for i := start; i < end; i++ {
		marker := "  "
		if i == l.Cursor {
			marker = "> "
		}
		lines = append(lines, marker+layout.Truncate(l.Rows[i], width-2))
	}
	return strings.Join(lines, "\n")
}
