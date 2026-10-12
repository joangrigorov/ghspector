// Package overlay implements the modal layer of the TUI as a stack of
// composable overlays.
//
// The previous TUI drew modals as hand-assembled box-drawing strings with
// hardcoded widths, then composited them by stripping ANSI escapes and slicing
// the underlying frame by rune index. That approach cannot be correct: rune
// index is not display width, so any frame containing a wide rune, a combining
// mark, or an OSC-8 hyperlink shifted the modal by an unpredictable amount.
//
// Here an overlay renders its body as plain text and the stack owns every
// geometric decision: the border, the padding, the width, and the centering.
// Composition happens in a cell-aware lipgloss canvas, so display width — not
// rune count — determines where the box lands.
package overlay

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// Overlay is one modal surface. Implementations render content only; the stack
// supplies the box and the position.
type Overlay interface {
	// Update handles a message. Returning a nil Overlay asks the stack to pop
	// this overlay. Returning a different Overlay replaces it in place.
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	// View returns the body, wrapped to fit width. The stack measures the
	// result; the body must not draw its own border.
	View(width int) string
	// Title is the heading drawn above the body. Empty means no heading.
	Title() string
	// Bindings are the keys the overlay handles, for the footer.
	Bindings() []keymap.Binding
}

// CloseMsg asks the stack to pop its topmost overlay. An overlay returns it
// when it wants to close but has other work in the same Cmd batch.
type CloseMsg struct{}

// PushMsg asks the stack to open another overlay above the current one.
type PushMsg struct{ Overlay Overlay }

// ReplaceMsg asks the stack to swap the topmost overlay for another.
type ReplaceMsg struct{ Overlay Overlay }

// Chrome is the visual configuration shared by every modal in the stack. The
// zero value renders a plain rounded box, which keeps golden tests independent
// of terminal colour support.
type Chrome struct {
	Border      lipgloss.Border
	BorderColor color.Color
	// TitleColor and BodyColor are applied when non-nil.
	TitleColor color.Color
	BodyColor  color.Color
	Background color.Color
	// PaddingX and PaddingY are the inner insets. Negative values are treated
	// as zero.
	PaddingX int
	PaddingY int
	// MaxInner caps the text width so a modal does not stretch across a wide
	// terminal. MinInner is the floor used when the terminal is narrow.
	MaxInner int
	MinInner int
}

// DefaultChrome returns the standard modal chrome: a rounded border, two
// columns of horizontal padding, and a body capped at 68 columns so the box
// stays a modal rather than a full-width banner.
func DefaultChrome() Chrome {
	return Chrome{
		Border:   lipgloss.RoundedBorder(),
		PaddingX: 2,
		PaddingY: 1,
		MaxInner: 68,
		MinInner: 12,
	}
}

// borderCols is the space the left and right border sides occupy.
const borderCols = 2

func (c Chrome) padX() int {
	if c.PaddingX < 0 {
		return 0
	}
	return c.PaddingX
}

func (c Chrome) padY() int {
	if c.PaddingY < 0 {
		return 0
	}
	return c.PaddingY
}

// InnerWidth is the text width available inside a box placed in a region of
// avail columns. An overlay calls this to wrap its own body; the stack calls it
// when it builds the style, so the two always agree.
func (c Chrome) InnerWidth(avail int) int {
	inner := avail - borderCols - 2*c.padX()
	if c.MaxInner > 0 && inner > c.MaxInner {
		inner = c.MaxInner
	}
	if c.MinInner > 0 && inner < c.MinInner {
		inner = c.MinInner
	}
	if inner < 1 {
		inner = 1
	}
	return inner
}

// BoxAt wraps body in the modal frame with the text column pinned to exactly
// inner columns.
func (c Chrome) BoxAt(body string, inner int) string {
	if inner < 1 {
		inner = 1
	}
	// lipgloss Width sets the total block width, so add the frame back in.
	s := lipgloss.NewStyle().
		Border(c.Border).
		Padding(c.padY(), c.padX()).
		Width(inner + borderCols + 2*c.padX())

	if c.BorderColor != nil {
		s = s.BorderForeground(c.BorderColor)
	}
	if c.BodyColor != nil {
		s = s.Foreground(c.BodyColor)
	}
	if c.Background != nil {
		s = s.Background(c.Background)
	}
	return s.Render(body)
}

// titleStyle styles the heading when a title colour is configured.
func (c Chrome) titleStyle() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if c.TitleColor != nil {
		s = s.Foreground(c.TitleColor)
	}
	return s
}

// Stack is an ordered set of overlays. The last element is the topmost and
// captures input first.
type Stack struct {
	overlays []Overlay
	chrome   Chrome
}

// NewStack returns an empty stack that draws with the given chrome.
func NewStack(chrome Chrome) Stack { return Stack{chrome: chrome} }

// Len is the number of open overlays.
func (s Stack) Len() int { return len(s.overlays) }

// Empty reports whether no overlay is open.
func (s Stack) Empty() bool { return len(s.overlays) == 0 }

// Top returns the topmost overlay, or nil when the stack is empty.
func (s Stack) Top() Overlay {
	if len(s.overlays) == 0 {
		return nil
	}
	return s.overlays[len(s.overlays)-1]
}

// Push returns a stack with o on top. The receiver is not modified.
func (s Stack) Push(o Overlay) Stack {
	if o == nil {
		return s
	}
	next := make([]Overlay, len(s.overlays), len(s.overlays)+1)
	copy(next, s.overlays)
	return Stack{overlays: append(next, o), chrome: s.chrome}
}

// Pop returns a stack without its topmost overlay.
func (s Stack) Pop() Stack {
	if len(s.overlays) == 0 {
		return s
	}
	next := make([]Overlay, len(s.overlays)-1)
	copy(next, s.overlays)
	return Stack{overlays: next, chrome: s.chrome}
}

// Replace swaps the topmost overlay. An empty stack pushes instead.
func (s Stack) Replace(o Overlay) Stack {
	if s.Empty() {
		return s.Push(o)
	}
	if o == nil {
		return s.Pop()
	}
	next := make([]Overlay, len(s.overlays))
	copy(next, s.overlays)
	next[len(next)-1] = o
	return Stack{overlays: next, chrome: s.chrome}
}

// Bindings returns the topmost overlay's bindings, for the footer. An empty
// stack returns nothing, and the caller keeps the route's bindings.
func (s Stack) Bindings() []keymap.Binding {
	top := s.Top()
	if top == nil {
		return nil
	}
	return top.Bindings()
}

// Update routes a message to the topmost overlay, handling the stack-control
// messages first. An overlay that returns nil is popped.
func (s Stack) Update(msg tea.Msg) (Stack, tea.Cmd) {
	switch m := msg.(type) {
	case CloseMsg:
		return s.Pop(), nil
	case PushMsg:
		return s.Push(m.Overlay), nil
	case ReplaceMsg:
		return s.Replace(m.Overlay), nil
	}

	top := s.Top()
	if top == nil {
		return s, nil
	}
	next, cmd := top.Update(msg)
	// Returning nil pops; anything else replaces. The result is never compared
	// for equality, because an overlay may hold a slice or a function and so not
	// be comparable at all.
	if next == nil {
		return s.Pop(), cmd
	}
	return s.Replace(next), cmd
}

// View composites the open overlays over background, which must already fill a
// width×height region. Each overlay is drawn as a centred box in the order it
// was pushed, so the topmost overlay is painted last.
func (s Stack) View(background string, width, height int) string {
	if s.Empty() || width <= 0 || height <= 0 {
		return background
	}

	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewLayer(background))

	// Deeper overlays are drawn first so a chain reads bottom-up.
	for _, o := range s.overlays {
		box := s.renderBox(o, width)
		x := (width - lipgloss.Width(box)) / 2
		y := (height - lipgloss.Height(box)) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		canvas.Compose(lipgloss.NewLayer(box).X(x).Y(y))
	}
	// The canvas is a drawing buffer: it emits nothing for cells beyond the
	// rightmost drawn one, so rows outside the modal come back ragged. Fit
	// restores the exact width×height contract the frame system relies on.
	return layout.Fit(canvas.Render(), width, height)
}

// renderBox builds the framed content for one overlay at the given avail width.
func (s Stack) renderBox(o Overlay, avail int) string {
	inner := s.chrome.InnerWidth(avail)
	body := o.View(inner)

	if title := o.Title(); title != "" {
		body = s.chrome.titleStyle().Render(title) + "\n\n" + body
	}

	// Normalise to an exact inner×lines rectangle so the interior is clean even
	// when the overlay returned ragged or over-long lines.
	lines := strings.Count(body, "\n") + 1
	return s.chrome.BoxAt(layout.Fit(body, inner, lines), inner)
}

// wrap soft-wraps text to width columns at word boundaries.
func wrap(s string, width int) string {
	if width < 1 {
		return s
	}
	return lipgloss.Wrap(s, width, "")
}
