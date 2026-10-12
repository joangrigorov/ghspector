package overlay

import (
	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
)

// Confirm is the one modal used for every yes/no decision: merge a pull
// request, close one, or approve a workflow run. The previous TUI had a
// separate hand-written overlay for each, which is how the three drifted into
// disagreeing about which key meant "yes".
//
// The scope selects the keymap bindings, so behaviour and the footer text come
// from the same table.
type Confirm struct {
	Scope keymap.Scope
	// Heading is the box title.
	Heading string
	// Body is the explanation, one paragraph per entry, already free of
	// wrapping.
	Body []string
	// OnYes runs the action when the user confirms.
	OnYes tea.Cmd
	// OnNo runs when the user cancels. Nil is fine: the overlay still closes.
	OnNo tea.Cmd
	// OnOpen is the escape hatch offered by scopes that bind a "do it on
	// GitHub instead" key, such as run approval.
	OnOpen tea.Cmd
}

// Update maps a key press to an action through the overlay's scope.
func (c Confirm) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c, nil
	}
	action, ok := keymap.Lookup(c.Scope, keymap.KeyString(k))
	if !ok {
		return c, nil
	}
	switch action {
	case keymap.ActionConfirm:
		return nil, c.OnYes
	case keymap.ActionBack:
		return nil, c.OnNo
	case keymap.ActionOpenBrowser:
		return nil, c.OnOpen
	default:
		return c, nil
	}
}

// View returns the body paragraphs, wrapped to width.
func (c Confirm) View(width int) string {
	if len(c.Body) == 0 {
		return ""
	}
	out := ""
	for i, para := range c.Body {
		if i > 0 {
			out += "\n\n"
		}
		out += wrap(para, width)
	}
	return out
}

// Title returns the box heading.
func (c Confirm) Title() string { return c.Heading }

// Bindings returns the keys the confirm handles, from its scope.
func (c Confirm) Bindings() []keymap.Binding {
	return keymap.For(c.Scope)
}
