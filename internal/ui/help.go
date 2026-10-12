package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
	"ghspector/internal/ui/widgets"
)

// Help is the keybinding reference. Its content comes from the keymap table, so
// it cannot drift from what the routes actually handle.
type Help struct {
	offset int
	// visible and total are recorded during View so a scroll can be clamped
	// exactly. View is the only place that knows the rect.
	visible int
	total   int
}

// NewHelp returns the help route scrolled to the top.
func NewHelp() *Help { return &Help{} }

// Title is the breadcrumb label.
func (h *Help) Title() string { return "Help & Keybindings" }

// Scope is the help keymap scope.
func (h *Help) Scope() keymap.Scope { return keymap.Help }

// Update ignores messages.
func (h *Help) Update(Deps, tea.Msg) (Route, tea.Cmd) { return h, nil }

// Act scrolls the reference.
func (h *Help) Act(_ Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionDown:
		h.scroll(1)
		return h, nil, true
	case keymap.ActionUp:
		h.scroll(-1)
		return h, nil, true
	}
	return h, nil, false
}

// View renders the visible slice of the reference.
func (h *Help) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	st := widgets.Style{
		Key:   deps.Theme.KeyHint,
		Desc:  deps.Theme.KeyDesc,
		Title: deps.Theme.Title,
		Muted: deps.Theme.Muted,
	}
	lines := append(legendLines(deps.Theme, r.W), widgets.HelpLines(st, r.W)...)
	h.total = len(lines)
	h.visible = r.H

	h.scroll(0) // clamp against the new geometry

	end := h.offset + r.H
	if end > len(lines) {
		end = len(lines)
	}
	start := h.offset
	if start > len(lines) {
		start = len(lines)
	}
	return layout.Fit(joinLines(lines[start:end]), r.W, r.H)
}

// scroll moves the offset by delta lines and clamps it to the content.
func (h *Help) scroll(delta int) {
	limit := h.total - h.visible
	if limit < 0 {
		limit = 0
	}
	h.offset += delta
	if h.offset < 0 {
		h.offset = 0
	}
	if h.offset > limit {
		h.offset = limit
	}
}

// statusLegend names the states a row marker can stand for. Each entry is drawn
// by statusIndicator, the same function the run, job, check and log rows use, so
// the legend cannot describe a symbol the tables do not draw.
var statusLegend = []struct {
	label      string
	status     string
	conclusion string
}{
	{label: "waiting", status: "waiting"},
	{label: "queued", status: "queued"},
	{label: "running", status: "in_progress"},
	{label: "success", conclusion: "success"},
	{label: "failed", conclusion: "failure"},
}

// legendLines renders the status legend. It leads the help body so the markers
// in the tables can be read without scrolling to the end of the reference, and
// it ends in a blank line so the first keymap section stays separated.
func legendLines(theme Theme, width int) []string {
	entries := make([]string, 0, len(statusLegend))
	for _, entry := range statusLegend {
		entries = append(entries, statusIndicator(theme, entry.status, entry.conclusion)+" "+entry.label)
	}
	return []string{
		style(theme.Title, bold).Render("Status"),
		layout.Fit("  "+strings.Join(entries, "   "), width, 1),
		"",
	}
}
