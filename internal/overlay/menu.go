package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// Choice is one entry of a Menu: an action, and the label shown beside its key.
type Choice struct {
	Action keymap.Action
	Label  string
}

// Menu is a keyed list of choices, such as the filter picker that offers "by
// user" and "by repository". Keys resolve through the menu's scope, so the
// entries on screen and the hints in the footer come from the same table.
//
// A menu does not close itself on a choice. What a choice means — opening
// another overlay above this one, or applying a filter and closing every
// overlay — belongs to the caller, which returns the Cmd that says so. Esc
// closes the menu.
type Menu struct {
	// Scope supplies the keys and the footer for the open menu.
	Scope keymap.Scope
	// Heading is the box title.
	Heading string
	// Intro is explanatory text above the choices.
	Intro []string
	// Choices are the entries, in display order.
	Choices []Choice
	// OnPick runs when a choice's key is pressed.
	OnPick func(action keymap.Action) tea.Cmd
	// OnCancel runs when the menu is dismissed. Nil is fine: the menu still
	// closes.
	OnCancel tea.Cmd
}

// Update maps a key press to an action through the menu's scope.
func (m Menu) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	action, ok := keymap.Lookup(m.Scope, keymap.KeyString(k))
	if !ok {
		return m, nil
	}
	if action == keymap.ActionBack {
		return nil, m.OnCancel
	}
	for _, choice := range m.Choices {
		if choice.Action == action && m.OnPick != nil {
			return m, m.OnPick(action)
		}
	}
	return m, nil
}

// View renders the intro text and one line per choice, its key first.
func (m Menu) View(width int) string {
	lines := make([]string, 0, len(m.Intro)+len(m.Choices)+1)
	for _, para := range m.Intro {
		lines = append(lines, wrap(para, width))
	}
	if len(m.Intro) > 0 && len(m.Choices) > 0 {
		lines = append(lines, "")
	}
	// The keys are aligned so the labels read as a column.
	keys := make([]string, 0, len(m.Choices))
	widths := 0
	for _, choice := range m.Choices {
		k := m.keysFor(choice.Action)
		keys = append(keys, k)
		if n := len(k); n > widths {
			widths = n
		}
	}
	for i, choice := range m.Choices {
		lines = append(lines, padRight(keys[i], widths)+"   "+choice.Label)
	}
	return strings.Join(lines, "\n")
}

// Title returns the box heading.
func (m Menu) Title() string { return m.Heading }

// Bindings returns the menu's keys, from its scope.
func (m Menu) Bindings() []keymap.Binding { return keymap.For(m.Scope) }

// keysFor renders the keys bound to an action in this menu's scope.
func (m Menu) keysFor(action keymap.Action) string {
	for _, b := range keymap.For(m.Scope) {
		if b.Action == action {
			return keymap.KeysFor(b)
		}
	}
	return ""
}

// padRight pads s with spaces to width columns when it is shorter.
func padRight(s string, width int) string {
	if n := layout.Width(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
