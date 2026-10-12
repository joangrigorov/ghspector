package ui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/joangrigorov/ghspector/internal/overlay"
)

// promptInput adapts the bubbles text input to overlay.TextInput. The overlay
// package owns the modal behaviour and stays free of the editor; this adapter
// owns the editor and stays free of the modal.
type promptInput struct {
	model textinput.Model
}

// promptInputWidth is the editor's own width in cells.
//
// The input has to be given one: with no width it renders only the first rune of
// its placeholder and no field to type in, so an empty actor prompt drew "l" for
// "login". The value is the modal's inner column at its default cap, which leaves
// the box the width its intro text already asks for.
const promptInputWidth = 40

// newPromptInput returns an input styled for the theme.
//
// A theme with no colours gets an input with no colours and no virtual cursor:
// the plain theme is what golden renders use, and its contract is that not one
// escape sequence reaches the screen. A coloured theme keeps the input's cursor,
// tinted with the theme accent.
func newPromptInput(theme Theme) *promptInput {
	m := textinput.New()
	// The modal already labels the field, so an inline prompt would repeat it.
	m.Prompt = ""
	m.Placeholder = "login"
	m.CharLimit = 64
	m.SetWidth(promptInputWidth)

	if theme.Text == nil {
		m.SetVirtualCursor(false)
		m.SetStyles(textinput.Styles{})
	} else {
		styles := textinput.DefaultDarkStyles()
		styles.Focused.Text = lipgloss.NewStyle().Foreground(theme.Text)
		styles.Blurred.Text = lipgloss.NewStyle().Foreground(theme.Text)
		styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(theme.Muted)
		styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(theme.Muted)
		// The cursor colour is set explicitly: the input paints its cursor from
		// this field, and a nil colour would be handed to the renderer.
		styles.Cursor.Color = theme.Accent
		styles.Cursor.Blink = true
		m.SetStyles(styles)
	}

	// Focus is what makes the input accept keys. Its returned command drives the
	// cursor blink; the screen redraws on every key, so dropping it costs only
	// the idle blink.
	m.Focus()
	return &promptInput{model: m}
}

// SetValue replaces the text, which is how the actor prompt opens pre-filled.
func (p *promptInput) SetValue(value string) {
	p.model.SetValue(value)
	p.model.CursorEnd()
}

// Update forwards a message to the input.
func (p *promptInput) Update(msg tea.Msg) (overlay.TextInput, tea.Cmd) {
	m, cmd := p.model.Update(msg)
	p.model = m
	return p, cmd
}

// View renders the input, its cursor included.
func (p *promptInput) View() string { return p.model.View() }

// Value is the text entered so far.
func (p *promptInput) Value() string { return p.model.Value() }
