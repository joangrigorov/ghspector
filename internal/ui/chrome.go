package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/overlay"
	"github.com/joangrigorov/ghspector/internal/ui/widgets"
)

// rate limit thresholds for the header indicator, matching the previous
// version's colour bands.
const (
	rateWarnBelow = 1000
	rateFailBelow = 200
)

// Style option helpers, so style() reads as a colour plus modifiers.
var (
	normal = func(s lipgloss.Style) lipgloss.Style { return s }
	bold   = func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) }
)

// renderHeader draws the breadcrumb band and its separator rule.
//
// The trail gives way first when the row is tight, because the account and the
// rate limit are what the user needs on a small screen; the account then gives
// way, and the rate limit last. The trail takes the left, and the account takes
// the right-hand edge, where it stays put: the trail grows and the rate limit
// changes width, and neither should move the name the band belongs to. The band
// carries no product name: the splash and the window title already say what is
// running, and the columns are worth more to the trail.
func renderHeader(deps Deps, r layout.Rect, title string, loading bool, tick int) string {
	if r.Empty() {
		return ""
	}
	theme := deps.Theme

	account := style(theme.HeaderContext, normal).Render("Account: " + deps.Account.Owner.String())

	indicator := ""
	if loading {
		indicator += style(theme.StatusWarn, normal).Render(widgets.Dots(tick)) + " "
	}
	indicator += renderRateLimit(deps)

	const gap = 3
	budget := r.W - lipgloss.Width(account) - lipgloss.Width(indicator) - 2*gap
	if budget < 4 {
		// Drop the account before dropping the trail.
		account, budget = "", r.W-lipgloss.Width(indicator)-gap
	}
	if budget < 1 {
		budget = 1
	}
	page := style(theme.HeaderTitle, bold).Render(layout.Truncate(title, budget))

	left := "  " + page
	right := indicator
	if account != "" {
		if right != "" {
			right += strings.Repeat(" ", gap)
		}
		right += account
	}

	titleRow := band(theme, r.W, joinLR(r.W, left, right))
	if r.H < 2 {
		return layout.Fit(titleRow, r.W, r.H)
	}
	rule := style(theme.HeaderRule, normal).Render(strings.Repeat("─", r.W))
	return layout.Fit(titleRow+"\n"+rule, r.W, r.H)
}

// renderRateLimit renders the remaining request budget, coloured by how much is
// left. An unset budget renders nothing rather than a misleading zero.
func renderRateLimit(deps Deps) string {
	rl := deps.Store.RateLimit()
	if rl.Limit <= 0 {
		return ""
	}
	colour := deps.Theme.StatusOK
	switch {
	case rl.Remaining < rateFailBelow:
		colour = deps.Theme.StatusFail
	case rl.Remaining < rateWarnBelow:
		colour = deps.Theme.StatusWarn
	}
	text := "Rate limit: " + strconv.Itoa(rl.Remaining) + "/" + strconv.Itoa(rl.Limit)
	if rl.Remaining == 0 {
		// The countdown is the API's arithmetic: the reset moment is the
		// server's absolute time, and this machine's clock is its own.
		if left, ok := deps.Store.RateLimitResetIn(); ok {
			text += " resets in " + formatDuration(left)
		}
	}
	return style(colour, normal).Render(text)
}

// hintMode says what owns the keyboard, because the footer advertises the keys
// that work where they are written.
type hintMode int

const (
	// hintsRoute is the normal case: a route on screen answering the global
	// keys.
	hintsRoute hintMode = iota
	// hintsModal is an overlay that is not a text box. It eats the keys the
	// route would act on, so only quit is still worth advertising.
	hintsModal
	// hintsText is an overlay with a text box. Every global key is a character
	// somebody is typing, so the footer offers the one way out.
	hintsText
)

// pinnedGlobals returns the global keys the footer writes ahead of the active
// surface's own hints.
//
// A route gets all of them. A modal keeps quit, because the root answers it
// from anywhere; a text box gets Ctrl+C alone, which is the same rule the model
// applies when it gives a typed `q` to the box rather than quitting (spec §2.4).
func pinnedGlobals(mode hintMode) []keymap.Binding {
	globals := keymap.Documented(keymap.Global)
	if mode == hintsRoute {
		return globals
	}
	for _, b := range globals {
		if b.Action != keymap.ActionQuit {
			continue
		}
		if mode == hintsText {
			b.Keys = []string{"ctrl+c"}
		}
		return []keymap.Binding{b}
	}
	return nil
}

// renderFooter draws the separator rule and the key hints, with the transient
// status message right-aligned.
func renderFooter(deps Deps, r layout.Rect, bindings []keymap.Binding, status string, statusErr bool, mode hintMode) string {
	if r.Empty() {
		return ""
	}
	theme := deps.Theme

	if r.H < 2 {
		return layout.Fit(style(theme.HeaderRule, normal).Render(strings.Repeat("─", r.W)), r.W, r.H)
	}
	rule := style(theme.HeaderRule, normal).Render(strings.Repeat("─", r.W))
	hints := renderHints(deps, r, bindings, status, statusErr, mode)
	return layout.Fit(rule+"\n"+hints, r.W, r.H)
}

// renderHints lays the footer out with hints on the left and the status message
// on the right.
func renderHints(deps Deps, r layout.Rect, bindings []keymap.Binding, status string, statusErr bool, mode hintMode) string {
	st := widgets.Style{Key: deps.Theme.KeyHint, Desc: deps.Theme.KeyDesc}

	// The global keys are pinned first, so a narrow footer drops the active
	// surface's hints rather than the keys that work everywhere.
	all := pinnedGlobals(mode)
	for _, b := range bindings {
		if b.Help == "" || hasAction(all, b.Action) {
			continue
		}
		all = append(all, b)
	}
	bindings = all

	statusRendered := ""
	if status != "" {
		colour := deps.Theme.StatusOK
		marker := "ℹ "
		if statusErr {
			colour = deps.Theme.StatusFail
			marker = "✖ "
		}
		statusRendered = style(colour, bold).Render(marker + status)
	}

	hintWidth := r.W - lipgloss.Width(statusRendered) - 2
	if hintWidth < 1 {
		if statusRendered != "" {
			return layout.Fit("  "+statusRendered, r.W, 1)
		}
		hintWidth = 1
	}
	hints := "  " + widgets.Footer(st, bindings, hintWidth)
	return layout.Fit(joinLR(r.W, hints, statusRendered), r.W, 1)
}

// hasAction reports whether bindings already covers action.
func hasAction(bindings []keymap.Binding, action keymap.Action) bool {
	for _, b := range bindings {
		if b.Action == action {
			return true
		}
	}
	return false
}

// selectMarker is the gutter of the highlighted row. It is the same glyph in
// every list, so a highlight means one thing on every screen.
const selectMarker = "› "

// selectedRow renders the highlighted row: the shared marker in the gutter, and
// the row's own text in the accent colour and bold.
//
// There is deliberately no background bar. A bar has to be paired with a
// foreground the palette can guarantee, and the pair this replaced — white on
// purple — rendered as white on white on a light terminal and as a purple block
// on a dark one. The marker carries the meaning, so the colour only has to be
// readable, which every theme's accent is.
//
// A row may bring styles of its own: the state marker in front of a run, a job
// or a check is coloured by how it ended. A nested style ends with a reset, and
// that reset ends the highlight for everything after it, so the accent is
// re-opened after each one. The marker in front of the row keeps its own colour,
// which is the answer it is there to give.
func selectedRow(theme Theme, row string, width int) string {
	row = strings.TrimPrefix(row, tableIndent)
	row = layout.Truncate(row, max(width-layout.Width(selectMarker), 1))

	prefix := highlightPrefix(theme)
	row = strings.ReplaceAll(row, ansi.ResetStyle, ansi.ResetStyle+prefix)
	// A row with a nested style was closed by it; a row of plain cells is still
	// open and has to be closed here.
	if strings.HasSuffix(row, prefix) {
		row = strings.TrimSuffix(row, prefix)
	} else {
		row += ansi.ResetStyle
	}
	return prefix + selectMarker + row
}

// highlightPrefix is the sequence that turns the row highlight on. It is read
// back from lipgloss rather than written out here, so it stays in step with the
// accent colour: rendering the style with nothing in it yields the style's own
// sequence followed by its reset.
func highlightPrefix(theme Theme) string {
	return strings.TrimSuffix(style(theme.Accent, bold).Render(""), ansi.ResetStyle)
}

// overlayChrome maps the theme onto the modal chrome, so a modal is drawn in the
// same palette as the screen behind it and a theme change reaches both.
func overlayChrome(theme Theme) overlay.Chrome {
	c := overlay.DefaultChrome()
	c.BorderColor = theme.OverlayBorder
	c.TitleColor = theme.OverlayTitle
	c.BodyColor = theme.OverlayBody
	return c
}

// breadcrumbs joins the route titles into a single trail. The header truncates
// it, so no screen builds or clips its own trail.
func breadcrumbs(titles []string) string {
	if len(titles) == 0 {
		return ""
	}
	return strings.Join(titles, " › ")
}

// joinLR places left and right on one row of exactly width cells, padding the
// gap. When there is not enough room the right side is trimmed, because the
// left side carries the identity.
func joinLR(width int, left, right string) string {
	leftW, rightW := lipgloss.Width(left), lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap < 1 {
		room := width - leftW - 1
		if room < 1 {
			return layout.Fit(left, width, 1)
		}
		right = layout.Truncate(right, room)
		gap = width - leftW - lipgloss.Width(right)
		if gap < 0 {
			gap = 0
		}
	}
	return layout.Fit(left+strings.Repeat(" ", gap)+right, width, 1)
}

// band paints a full-width row with the header background when one is set.
func band(theme Theme, width int, s string) string {
	if theme.HeaderBackground == nil {
		return s
	}
	return lipgloss.NewStyle().Background(theme.HeaderBackground).Width(width).Render(s)
}

// renderTooSmall is the whole screen when the terminal is below the supported
// minimum. It replaces a malformed layout rather than trying to squeeze one in.
//
// The title goes first so it survives a screen only a few rows tall.
func renderTooSmall(theme Theme, width, height int) string {
	lines := []string{
		style(theme.ErrorTitle, bold).Render("Terminal too small"),
	}
	if height >= 6 {
		lines = append(lines, "")
	}
	lines = append(lines,
		style(theme.Text, normal).Render("ghspector needs at least "+
			strconv.Itoa(layout.MinWidth)+"x"+strconv.Itoa(layout.MinHeight)+" cells."),
		style(theme.Muted, normal).Render("Current size: "+strconv.Itoa(width)+"x"+strconv.Itoa(height)+"."),
		"",
		style(theme.Muted, normal).Render("Resize the window, or press q to quit."),
	)
	return layout.Fit(strings.Join(lines, "\n"), width, height)
}

// renderError is the whole screen when startup cannot continue, for example
// when the token is rejected.
func renderError(theme Theme, title, detail string, width, height int) string {
	lines := []string{
		"",
		style(theme.ErrorTitle, bold).Render(title),
		"",
	}
	for _, line := range strings.Split(detail, "\n") {
		lines = append(lines, style(theme.ErrorBody, normal).Render(line))
	}
	lines = append(lines,
		"",
		style(theme.Muted, normal).Render("Press r to retry, q to quit."),
	)
	return layout.Fit(strings.Join(lines, "\n"), width, height)
}

// statusTTL is how long a transient footer message stays on screen (spec §2.3).
const statusTTL = 3 * time.Second

// statusClearMsg expires a footer status message. The generation counter keeps
// an earlier timer from clearing a newer message.
type statusClearMsg struct{ generation int }

// statusErr returns a command that shows a transient failure in the footer,
// which the chrome renders in the error colour.
func statusErr(text string) tea.Cmd {
	return func() tea.Msg { return StatusMsg{Text: text, IsErr: true} }
}
