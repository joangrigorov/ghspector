package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// TextInput is the single-line editor a Prompt drives. It is an interface so
// this package owns the modal behaviour without owning the editor: the caller
// supplies the concrete input, already styled for the active theme, and tests
// can supply a stub.
type TextInput interface {
	// Update handles a message the prompt did not consume.
	Update(msg tea.Msg) (TextInput, tea.Cmd)
	// View renders the input, cursor included.
	View() string
	// Value is the text entered so far.
	Value() string
}

// Prompt is a single-line text input modal, such as the actor/user filter box.
//
// Every key that the scope does not bind is forwarded to the input, so editing
// keys (arrows, backspace, paste) behave the way the input implements them
// rather than the way this package guesses.
type Prompt struct {
	Scope   keymap.Scope
	Heading string
	Intro   []string
	Input   TextInput
	// OnSubmit reports the entered text.
	OnSubmit func(value string) tea.Cmd
	// OnCancel runs when the prompt is dismissed. Nil is fine: it still closes.
	OnCancel tea.Cmd
}

// Update gives bound keys to the prompt's own behaviour and forwards the rest to
// the input.
func (p Prompt) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if action, found := keymap.Lookup(p.Scope, keymap.KeyString(k)); found {
			switch action {
			case keymap.ActionBack:
				return nil, p.OnCancel
			case keymap.ActionSubmit:
				value := ""
				if p.Input != nil {
					value = p.Input.Value()
				}
				if p.OnSubmit == nil {
					return nil, nil
				}
				return nil, p.OnSubmit(value)
			}
		}
	}
	if p.Input == nil {
		return p, nil
	}
	input, cmd := p.Input.Update(msg)
	p.Input = input
	return p, cmd
}

// View renders the intro text, then the input, then a hint that an empty value
// clears the filter.
func (p Prompt) View(width int) string {
	lines := make([]string, 0, len(p.Intro)+3)
	for _, para := range p.Intro {
		lines = append(lines, wrap(para, width))
	}
	if len(p.Intro) > 0 {
		lines = append(lines, "")
	}
	if p.Input != nil {
		lines = append(lines, fitInput(p.Input.View(), width))
	}
	lines = append(lines, "", wrap("Submit an empty value to clear the filter.", width))
	return strings.Join(lines, "\n")
}

// Title returns the box heading.
func (p Prompt) Title() string { return p.Heading }

// Bindings returns the prompt's keys, from its scope.
func (p Prompt) Bindings() []keymap.Binding { return keymap.For(p.Scope) }

// fitInput keeps the input on one line. The input scrolls its own text, so the
// only job here is to stop a long value from widening the box.
func fitInput(view string, width int) string {
	if idx := strings.IndexByte(view, '\n'); idx != -1 {
		view = view[:idx]
	}
	return layout.Truncate(view, width)
}
