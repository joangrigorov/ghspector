package ui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"

	"github.com/joangrigorov/ghspector/internal/sanitize"
)

// Markdown renders the bodies the API returns — a pull request or issue
// description, a comment, a commit message — as a document the terminal can
// draw.
//
// Two properties matter more than typographic fidelity.
//
// The renderer is built once per palette and width and then reused. Building it
// parses a style and stands up a markdown parser, which is not work to repeat
// per frame; the root calls Prepare when the geometry or the palette changes, so
// a frame normally finds the renderer it needs already built. Render is
// serialized, because the renderer underneath is stateful.
//
// The style is the theme's own. It starts from glamour's plain ASCII style and
// adds this palette's colours, so a body cannot introduce a hue the chrome does
// not use — the standard dark style paints its top-level headings on a purple
// bar. A theme with no colours renders with the plain style, which is what keeps
// golden renders free of escape sequences.
type Markdown struct {
	mu    sync.Mutex
	tr    *glamour.TermRenderer
	key   markdownKey
	built bool
	// builds counts renderer constructions, so a test can prove a frame does
	// not build one.
	builds int
}

// markdownKey identifies a renderer: the palette it draws with and the width it
// wraps to. It is made of the three questions a style answers — is there colour,
// which background, how wide — rather than of the theme itself, so comparing two
// keys cannot depend on what a colour implementation happens to be.
type markdownKey struct {
	plain bool
	dark  bool
	width int
}

// NewMarkdown returns the body renderer. The root model owns one and hands it to
// every route through Deps.
func NewMarkdown() *Markdown { return &Markdown{} }

// Prepare builds the renderer for a palette and width, unless that pair is the
// one already built. The root calls it when the terminal is resized or the
// palette changes, which keeps construction out of the frame path.
func (m *Markdown) Prepare(theme Theme, width int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepare(theme, width)
}

// Lines renders text as markdown for a palette and width, and returns its lines.
//
// The text is sanitized first. A body the API handed back has already been
// cleaned at the transport boundary, and this is the second door: markdown is
// the last boundary before a body becomes cells, and the input here can also
// come from a route's own text.
func (m *Markdown) Lines(theme Theme, text string, width int) []string {
	text = sanitize.Text(text)
	if m == nil {
		return wrapLines(text, width)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepare(theme, width)
	if m.tr == nil {
		return wrapLines(text, width)
	}
	out, err := m.tr.Render(text)
	if err != nil {
		// A document this parser refuses is not worth a blank pane: it is
		// drawn the way the views drew it before there was a renderer.
		return wrapLines(text, width)
	}
	return markdownLines(out, theme.Hyperlinks)
}

// prepare builds the renderer when the palette or the width has changed. The
// caller holds the mutex.
func (m *Markdown) prepare(theme Theme, width int) {
	if width < 1 {
		width = 1
	}
	key := markdownKeyOf(theme, width)
	if m.built && m.key == key {
		return
	}
	m.key, m.built = key, true
	m.builds++
	tr, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyles(theme)),
		glamour.WithWordWrap(width),
		// `:robot:` is how GitHub's own release notes are written, so the
		// shortcode a body contains is drawn as the character it names.
		glamour.WithEmoji(),
	)
	if err != nil {
		m.tr = nil
		return
	}
	m.tr = tr
}

// markdownKeyOf reduces a palette and a width to the questions a style answers.
func markdownKeyOf(theme Theme, width int) markdownKey {
	return markdownKey{
		plain: !theme.Colored(),
		dark:  theme.Dark,
		width: width,
	}
}

// markdownStyles is the glamour style for a palette.
//
// Every style starts from the plain ASCII configuration: bullets, rule lines and
// heading markers that need no colour, no width of their own, and no escape
// sequence. A palette with colours then replaces the emphasis markers with the
// real thing and paints headings, links and inline code in the theme's accent,
// which is the same colour the pane titles and key hints use.
func markdownStyles(theme Theme) glamouransi.StyleConfig {
	st := styles.ASCIIStyleConfig
	// The layout owns the margins. A body fills the rect it was given, and the
	// two cells on each side that glamour's defaults reserve are not ours to
	// spend.
	st.Document.Margin = uintPtr(0)
	st.CodeBlock.Margin = uintPtr(0)
	st.CodeBlock.Indent = uintPtr(0)

	switch {
	case !theme.Colored():
		return st
	case theme.Dark:
		return markdownOutfit(st, darkAccent, darkText, darkMuted)
	default:
		return markdownOutfit(st, lightAccent, lightText, lightMuted)
	}
}

// markdownOutfit colours a plain style with a palette's accent, body text and
// muted grey.
func markdownOutfit(st glamouransi.StyleConfig, accent, text, muted string) glamouransi.StyleConfig {
	st.Document.Color = strPtr(text)

	for _, heading := range []*glamouransi.StyleBlock{&st.Heading, &st.H1, &st.H2, &st.H3, &st.H4, &st.H5, &st.H6} {
		heading.Color = strPtr(accent)
		heading.Bold = boolPtr(true)
	}
	st.Strong = glamouransi.StylePrimitive{Bold: boolPtr(true)}
	st.Emph = glamouransi.StylePrimitive{Italic: boolPtr(true)}
	st.Strikethrough = glamouransi.StylePrimitive{CrossedOut: boolPtr(true)}

	// Inline code keeps its accent without the ASCII backticks, which would
	// otherwise be doubled up with a colour.
	st.Code.Color = strPtr(accent)
	st.Code.BlockPrefix = ""
	st.Code.BlockSuffix = ""

	st.HorizontalRule.Color = strPtr(muted)
	st.BlockQuote.IndentToken = strPtr("│ ")
	st.Link = glamouransi.StylePrimitive{Color: strPtr(accent), Underline: boolPtr(true)}
	st.LinkText = glamouransi.StylePrimitive{Color: strPtr(accent), Bold: boolPtr(true)}
	return st
}

// markdownLines splits a rendered document into the lines a pane draws.
//
// A renderer pads every line to the wrap width and wraps a document in blank
// lines. Neither is a row: the pad is a run of styled blanks that the layout
// replaces with its own, the blank lines are space the body did not earn, and
// both would be counted by the scroll bound.
func markdownLines(out string, hyperlinks bool) []string {
	out = strings.Trim(out, "\n")
	if out == "" {
		return []string{""}
	}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !hyperlinks {
			line = stripHyperlinks(line)
		}
		lines[i] = trimTrailingBlanks(line)
	}
	return lines
}

// trimTrailingBlanks drops the pad a renderer writes to the right of every line.
//
// The pad is one styled blank per remaining cell, which at the terminal is
// nothing but bytes: the layout draws the region's padding with its own plain
// spaces. Cutting at the last visible cell takes a line of a few dozen bytes
// down from a few hundred, and a long body down from megabytes.
//
// The line is cut where the last visible cell ends, so an escape sequence that
// was open at the cut is closed with a reset rather than left to bleed into the
// cells that follow.
func trimTrailingBlanks(line string) string {
	end := lastVisibleByte(line)
	if end == len(line) {
		return line
	}
	if end == 0 {
		return ""
	}
	if strings.IndexByte(line, '\x1b') < 0 {
		return line[:end]
	}
	return line[:end] + sgrReset
}

// lastVisibleByte returns the offset just past the last cell of a line that is
// not a blank, skipping the escape sequences on the way.
func lastVisibleByte(line string) int {
	p := ansi.GetParser()
	defer ansi.PutParser(p)
	end := 0
	for i := 0; i < len(line); i++ {
		if p.Advance(line[i]) == parser.PrintAction && line[i] != ' ' {
			end = i + 1
		}
	}
	return end
}

// sgrReset closes whatever style is open.
const sgrReset = "\x1b[0m"

// osc8 introduces a hyperlink token, and the same token with no parameters
// closes one.
const osc8 = "\x1b]8;"

// stripHyperlinks removes the OSC 8 tokens a link is wrapped in. A palette that
// does not advertise hyperlinks draws a link as its text and its URL, which is
// what a terminal without OSC 8 support shows anyway, and the tokens would
// otherwise be an escape sequence in a render that is meant to have none.
func stripHyperlinks(s string) string {
	if !strings.Contains(s, osc8) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if !strings.HasPrefix(s[i:], osc8) {
			b.WriteByte(s[i])
			i++
			continue
		}
		// The token runs to BEL or to ST, which is ESC followed by a
		// backslash, and an unterminated one runs to the end of the line.
		j := i + len(osc8)
		for j < len(s) && !closesHyperlinkToken(s, j) {
			j++
		}
		switch {
		case j >= len(s):
			i = j
		case s[j] == '\x07':
			i = j + 1
		default:
			i = j + 2
		}
	}
	return b.String()
}

// closesHyperlinkToken reports whether position j ends an OSC 8 token: BEL, or
// ST, which is ESC followed by a backslash.
func closesHyperlinkToken(s string, j int) bool {
	if s[j] == '\x07' {
		return true
	}
	return s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\'
}

// Pointers, because glamour's style fields read "unset" as nil.
func uintPtr(v uint) *uint    { return &v }
func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }
