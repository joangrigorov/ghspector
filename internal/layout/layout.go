// Package layout divides the terminal into the regions every view renders into.
//
// A view never sees the terminal size and never does arithmetic on it: it
// receives a Rect and stays inside it. That is what removes the per-view magic
// constants the previous TUI was built from, where visible-row counts were
// hand-tuned per screen and clipping bugs appeared one screen at a time.
//
// Fit is the workhorse. Every region renderer passes its output through Fit, so
// a rendered screen is always exactly width x height cells no matter what the
// content did. Determinism here is what makes golden files and the resize
// invariant possible.
package layout

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Rect is a rectangle of terminal cells.
type Rect struct {
	X, Y, W, H int
}

// Frame is a screen divided into chrome and content.
type Frame struct {
	Header Rect
	Body   Rect
	Footer Rect
}

// Chrome heights, in rows.
const (
	HeaderHeight = 2 // title row plus separator rule
	FooterHeight = 2 // separator rule plus key hints
)

// Supported minimum terminal size. Below this the app renders a message
// instead of a malformed screen.
const (
	MinWidth  = 50
	MinHeight = 12
)

// Gutter is the space every screen keeps clear at the left and right edge.
//
// It is one number for the whole program so the chrome and the content it
// frames share one box: a bar's text, a rule, a table row and a pane all start
// at Gutter and end at width-Gutter-1. The one thing drawn outside the box is a
// list's selection marker, which sits in the gutter itself.
const Gutter = 2

// ContentWidth is the width of the text area of a screen width cells wide: the
// screen less one gutter on each edge. The chrome measures itself with this, and
// a route that has to know a pane's width without being handed its rect — the
// scroll clamps, which run outside a render — measures the same text area.
func ContentWidth(width int) int {
	if inner := width - 2*Gutter; inner > 0 {
		return inner
	}
	return 1
}

// BoxWidth is the width of a block that spans a screen width cells wide and
// carries its own left gutter: the screen less the gutter on the right.
//
// A route's rect is this wide. The left gutter is the surface's own business —
// a table row reserves it for the selection marker, a heading and a pane write
// it out — so it is not taken away twice.
func BoxWidth(width int) int {
	if inner := width - Gutter; inner > 0 {
		return inner
	}
	return 1
}

// TooSmall reports whether the terminal is below the supported minimum.
func TooSmall(width, height int) bool {
	return width < MinWidth || height < MinHeight
}

// Layout divides a screen into header, body and footer.
//
// When the screen is too short to hold both bars, the body takes the whole
// height and the bars collapse to zero rather than producing negative sizes.
func Layout(width, height int) Frame {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	headerH, footerH := HeaderHeight, FooterHeight
	if headerH+footerH >= height {
		headerH, footerH = 0, 0
	}
	bodyH := height - headerH - footerH

	return Frame{
		Header: Rect{X: 0, Y: 0, W: width, H: headerH},
		Body:   Rect{X: 0, Y: headerH, W: width, H: bodyH},
		Footer: Rect{X: 0, Y: headerH + bodyH, W: width, H: footerH},
	}
}

// Empty reports whether the rect has no drawable cells.
func (r Rect) Empty() bool {
	return r.W <= 0 || r.H <= 0
}

// Inner insets a rect on all sides, clamping so the result never goes negative.
func (r Rect) Inner(dx, dy int) Rect {
	if dx < 0 {
		dx = 0
	}
	if dy < 0 {
		dy = 0
	}
	w, h := r.W-2*dx, r.H-2*dy
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return Rect{X: r.X + dx, Y: r.Y + dy, W: w, H: h}
}

// SplitCols splits off a left column of the given width. The width is clamped to
// the rect, so the two results always tile the original exactly.
func (r Rect) SplitCols(leftWidth int) (left, right Rect) {
	leftWidth = clamp(leftWidth, 0, r.W)
	return Rect{X: r.X, Y: r.Y, W: leftWidth, H: r.H},
		Rect{X: r.X + leftWidth, Y: r.Y, W: r.W - leftWidth, H: r.H}
}

// SplitRows splits off a top band of the given height, clamped like SplitCols.
func (r Rect) SplitRows(topHeight int) (top, bottom Rect) {
	topHeight = clamp(topHeight, 0, r.H)
	return Rect{X: r.X, Y: r.Y, W: r.W, H: topHeight},
		Rect{X: r.X, Y: r.Y + topHeight, W: r.W, H: r.H - topHeight}
}

// SplitRowsFair splits off a top band of at most want rows, and never more than
// a third of the rect.
//
// It exists for a block whose height is not known before it is rendered — a
// commit message, say, which is as long as its author made it. The cap is what
// keeps such a block from taking the region the block below it needs, and it is
// here rather than in a view because it is the same division at every size.
func (r Rect) SplitRowsFair(want int) (top, bottom Rect) {
	return r.SplitRows(min(want, max(r.H/3, 1)))
}

// Fit forces s into exactly w columns and h rows.
//
// Lines are truncated or space-padded to w, and the block is padded or clipped
// to h. Only the first h lines are kept. This is the guarantee that a rendered
// frame always matches the terminal geometry.
func Fit(s string, w, h int) string {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	if h == 0 || w == 0 {
		return ""
	}

	// Tabs and carriage returns make terminal geometry unpredictable, so they
	// are normalised away before anything is measured.
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "    ")

	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}

	var b strings.Builder
	b.Grow((w + 1) * h)
	blank := strings.Repeat(" ", w)

	for i := 0; i < h; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(fitLine(line, w, blank))
	}
	return b.String()
}

// fitLine truncates or pads a single line to exactly w cells.
//
// Padding happens after truncation because a wide rune straddling the boundary
// cannot be split: truncating a line of CJK text to an odd width yields one cell
// fewer, and the line still has to fill the region.
func fitLine(line string, w int, blank string) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(line) > w {
		line = ansi.Truncate(line, w, "")
	}
	width := ansi.StringWidth(line)
	if width >= w {
		return line
	}
	return line + blank[:w-width]
}

// Truncate shortens s to at most w cells, appending an ellipsis when it cuts.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w == 1 {
		return ansi.Truncate(s, 1, "")
	}
	return ansi.Truncate(s, w, "…")
}

// Width reports the display width of s in cells, ignoring ANSI sequences.
func Width(s string) int {
	return ansi.StringWidth(s)
}

// Wrap soft-wraps text to at most width cells at word boundaries, returning the
// wrapped block with newlines. It is here rather than in a route so no route
// reaches for a text library, and so the wrapping rule is the same everywhere a
// paragraph is wrapped.
func Wrap(text string, width int) string {
	if width < 1 {
		return text
	}
	return lipgloss.Wrap(text, width, "")
}

// PadRight pads s with spaces to exactly w cells. A string already at or past w
// is returned unchanged: padding never truncates.
func PadRight(s string, w int) string {
	if n := ansi.StringWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// Column is one column of a table: its preferred width, the narrowest it may
// become, and whether it absorbs spare width.
//
// A column with a zero minimum is dropped when the row does not fit, which is
// how a table degrades on a narrow terminal. Dropping is better than clipping:
// a half-drawn column reads as a rendering fault, while a missing optional
// column reads as a smaller table.
type Column struct {
	Width int
	Min   int
	Flex  bool
}

// FitColumns allocates column widths for a row of width cells, with indent
// cells before the first column and sep cells between each pair.
//
// Spare width goes to the flexible column. When the row does not fit, columns
// that may be dropped are dropped from the right, and then the rest shrink
// towards their minimums, right to left. The result always fits unless every
// column is already at its minimum.
func FitColumns(cols []Column, width, indent, sep int) []Column {
	out := make([]Column, len(cols))
	copy(out, cols)
	for i := range out {
		if out[i].Min < 0 {
			out[i].Min = 0
		}
		if out[i].Min > 0 && out[i].Width < out[i].Min {
			out[i].Width = out[i].Min
		}
	}

	total := func() int {
		n := indent
		first := true
		for _, c := range out {
			if c.Width <= 0 {
				continue
			}
			if !first {
				n += sep
			}
			n += c.Width
			first = false
		}
		return n
	}

	if slack := width - total(); slack > 0 {
		for i := range out {
			if out[i].Flex && out[i].Width > 0 {
				out[i].Width += slack
				break
			}
		}
	}

	for total() > width {
		dropped := false
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Width > 0 && out[i].Min == 0 {
				out[i].Width = 0
				dropped = true
				break
			}
		}
		if !dropped {
			break
		}
	}

	for total() > width {
		shrunk := false
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Width > out[i].Min {
				out[i].Width--
				shrunk = true
				break
			}
		}
		if !shrunk {
			break
		}
	}
	return out
}

// Window returns the slice of length items to show around cursor, so that the
// cursor is visible in a viewport of visible rows. The range returned is
// half-open: [start, end). A list that fits is shown whole, and the window
// never runs past the end of the list, so the last row of a long list sits at
// the bottom of the viewport instead of on a half-empty one.
func Window(length, cursor, visible int) (start, end int) {
	if length <= 0 {
		return 0, 0
	}
	if visible < 1 {
		visible = 1
	}
	if length <= visible {
		return 0, length
	}
	cursor = clamp(cursor, 0, length-1)
	start = clamp(cursor-visible/2, 0, length-visible)
	return start, start + visible
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
