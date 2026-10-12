package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// Tab is one of the three top-level lists.
type Tab int

// Tab order is Pull Requests, Workflows, Issues, which is what the code did and
// what the README now documents (spec D2).
const (
	TabPRs Tab = iota
	TabWorkflows
	TabIssues
)

// tabCount is the number of tabs. Kept explicit so the cycling arithmetic reads
// clearly.
const tabCount = 3

// tabMeta ties each tab to its label and keymap scope.
var tabMeta = [tabCount]struct {
	Name  string
	Scope keymap.Scope
	Empty string
}{
	TabPRs:       {Name: "Pull Requests", Scope: keymap.MainPRs, Empty: "No pull requests loaded yet."},
	TabWorkflows: {Name: "Workflows", Scope: keymap.MainWorkflows, Empty: "No workflow runs loaded yet."},
	TabIssues:    {Name: "Issues", Scope: keymap.MainIssues, Empty: "No issues loaded yet."},
}

// Main is the primary screen: a tab strip over the active list.
//
// The list bodies arrive with their data layer in the next phase; what exists
// here is the screen identity, the tab strip, and the key routing, so every
// later route has a working host.
type Main struct {
	tab Tab
}

// NewMain returns the main route on its default tab.
func NewMain() Main { return Main{tab: TabPRs} }

// Title is the breadcrumb label for the active tab.
func (m Main) Title() string { return tabMeta[m.tab].Name }

// Scope is the keymap scope for the active tab.
func (m Main) Scope() keymap.Scope { return tabMeta[m.tab].Scope }

// Update ignores messages: the main route has no data of its own yet.
func (m Main) Update(Deps, tea.Msg) (Route, tea.Cmd) { return m, nil }

// Act handles the tab strip and the refresh key.
func (m Main) Act(_ Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionNextTab:
		return Main{tab: (m.tab + 1) % tabCount}, nil, true
	case keymap.ActionPrevTab:
		return Main{tab: (m.tab + tabCount - 1) % tabCount}, nil, true
	case keymap.ActionRefresh:
		return m, status("Refreshed"), true
	case keymap.ActionClearFilters:
		return m, status("Filters cleared"), true
	}
	return m, nil, false
}

// View renders the tab strip and the empty state for the active tab.
func (m Main) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	muted := func(s string) string { return "  " + style(deps.Theme.Muted, normal).Render(s) }

	meta := tabMeta[m.tab]
	lines := []string{
		m.tabStrip(deps),
		"",
		muted(meta.Empty),
		"",
		// Two short lines rather than one long one, so a narrow terminal does
		// not clip the sentence mid-word.
		muted("The list for this tab arrives with the data layer."),
		muted("The chrome, tabs, help and key handling are live."),
	}
	return layout.Fit(strings.Join(lines, "\n"), r.W, r.H)
}

// tabStrip renders the tab labels with the active one highlighted.
func (m Main) tabStrip(deps Deps) string {
	theme := deps.Theme
	parts := make([]string, 0, tabCount)
	for i := 0; i < tabCount; i++ {
		meta := tabMeta[Tab(i)]
		if Tab(i) == m.tab {
			parts = append(parts, style(theme.Selected, bold).Render(" "+meta.Name+" "))
			continue
		}
		parts = append(parts, style(theme.Muted, normal).Render(" "+meta.Name+" "))
	}
	return " " + strings.Join(parts, style(theme.HeaderRule, normal).Render("│"))
}

// status returns a command that shows a transient footer message.
func status(text string) tea.Cmd {
	return func() tea.Msg { return StatusMsg{Text: text} }
}
