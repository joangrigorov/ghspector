package ui

// renderedLines is content rendered to lines, kept until something that decides
// what it looks like changes: the content it came from, the width it wraps to,
// and the palette it draws in.
//
// A pane that renders its whole content on demand pays for it on every frame and
// every keystroke. On the conversation screens that is the difference between a
// screen that scrolls and one that lags: a review-heavy pull request carries
// dozens of markdown bodies, and glamour wraps and pads each of them, which
// measured 163 ms for a render of the pull request this was reported from
// (BenchmarkConversationRender). The width and the palette are the same questions
// the markdown renderer answers, because a body is what makes the render
// expensive.
//
// The revision says what the content is, in whatever form the caller has it: the
// body text itself, or a counter a route bumps when its data lands.
type renderedLines[R comparable] struct {
	rev   R
	key   markdownKey
	lines []string
	valid bool
}

// linesFrom returns the lines for a revision and a key, rendering them with
// render when they are not the ones already held.
func (c *renderedLines[R]) linesFrom(rev R, key markdownKey, render func() []string) []string {
	if c.valid && c.rev == rev && c.key == key {
		return c.lines
	}
	c.rev, c.key, c.lines, c.valid = rev, key, render(), true
	return c.lines
}
