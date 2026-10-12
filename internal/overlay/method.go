package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
)

// Option is one entry of a Method overlay: the action its key fires, the value
// it stands for, and the label shown beside it.
type Option struct {
	Action keymap.Action
	Value  string
	Label  string
}

// Method is a chooser over a small ordered set of values, where one of them is
// marked as the default: the merge-method picker. It is separate from Menu
// because two of its keys act on the overlay rather than closing it — the key
// that cycles the default, and the key that accepts it — and a Menu closes on
// every key it recognises.
//
// The default cycles through Options, so the order of the options is the order
// of the cycle and this overlay needs no table of its own.
type Method struct {
	Scope   keymap.Scope
	Heading string
	Intro   []string
	// Options are the choices, in cycle order.
	Options []Option
	// Default is the value that is currently the default. It is one of the
	// Options' values.
	Default string
	// OnPick runs when a value is chosen for this merge.
	OnPick func(value string) tea.Cmd
	// OnSubmit runs when the user accepts the default value.
	OnSubmit tea.Cmd
	// OnCycleDefault is called with the new default. It runs while the overlay
	// stays open, so the user sees the label change.
	OnCycleDefault func(value string) tea.Cmd
	// OnCancel runs when the overlay is dismissed. Nil is fine: it still closes.
	OnCancel tea.Cmd
}

// Update maps a key press to an action through the overlay's scope.
func (m Method) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	action, ok := keymap.Lookup(m.Scope, keymap.KeyString(k))
	if !ok {
		return m, nil
	}
	switch action {
	case keymap.ActionBack:
		return nil, m.OnCancel
	case keymap.ActionSubmit:
		return nil, m.OnSubmit
	case keymap.ActionCycleDefault:
		next := m.nextDefault()
		m.Default = next
		if m.OnCycleDefault == nil {
			return m, nil
		}
		return m, m.OnCycleDefault(next)
	}
	for _, opt := range m.Options {
		if opt.Action == action {
			if m.OnPick == nil {
				return nil, nil
			}
			return nil, m.OnPick(opt.Value)
		}
	}
	return m, nil
}

// nextDefault returns the value that follows the current default in Options,
// wrapping around. A default that is not among the options starts at the first.
func (m Method) nextDefault() string {
	for i, opt := range m.Options {
		if opt.Value == m.Default {
			return m.Options[(i+1)%len(m.Options)].Value
		}
	}
	if len(m.Options) == 0 {
		return m.Default
	}
	return m.Options[0].Value
}

// Title returns the box heading.
func (m Method) Title() string { return m.Heading }

// Bindings returns the chooser's keys, from its scope.
func (m Method) Bindings() []keymap.Binding { return keymap.For(m.Scope) }

// View renders the intro, the choices with their keys aligned, and the current
// default.
func (m Method) View(width int) string {
	lines := make([]string, 0, len(m.Intro)+len(m.Options)+3)
	for _, para := range m.Intro {
		lines = append(lines, wrap(para, width))
	}
	if len(m.Intro) > 0 && len(m.Options) > 0 {
		lines = append(lines, "")
	}

	keys := make([]string, len(m.Options))
	keyWidth := 0
	for i, opt := range m.Options {
		keys[i] = m.keysFor(opt.Action)
		if n := len(keys[i]); n > keyWidth {
			keyWidth = n
		}
	}
	for i, opt := range m.Options {
		line := padRight(keys[i], keyWidth) + "   " + opt.Label
		if opt.Value == m.Default {
			line += "  (default)"
		}
		lines = append(lines, line)
	}

	if len(m.Options) > 0 {
		lines = append(lines, "", wrap(m.defaultLine(), width))
	}
	return strings.Join(lines, "\n")
}

// defaultLine states which method a plain Enter uses and how to change it.
func (m Method) defaultLine() string {
	if m.Default == "" {
		return "Enter merges with the configured default method."
	}
	return "Current default: " + m.Default + ". Enter uses it, d changes and saves it."
}

// keysFor renders the keys bound to an action in this overlay's scope.
func (m Method) keysFor(action keymap.Action) string {
	for _, b := range keymap.For(m.Scope) {
		if b.Action == action {
			return keymap.KeysFor(b)
		}
	}
	return ""
}
