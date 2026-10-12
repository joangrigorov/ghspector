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

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
