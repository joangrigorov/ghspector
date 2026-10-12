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
	// Empty is the placeholder a tab without a data layer shows. The Workflows
	// tab has a data layer and renders its own loading, failure and empty
	// states, so it has no placeholder.
	Empty string
}{
	TabPRs:       {Name: "Pull Requests", Scope: keymap.MainPRs, Empty: "No pull requests loaded yet."},
	TabWorkflows: {Name: "Workflows", Scope: keymap.MainWorkflows},
	TabIssues:    {Name: "Issues", Scope: keymap.MainIssues, Empty: "No issues loaded yet."},
}

// Main is the primary screen: a tab strip over the active list.
//
// It owns the tab strip, the key routing that is common to the tabs, and the
// Workflows tab's state. A tab's data and behaviour live in a value of its own
// — runsTab today — so this route stays a host rather than growing a field per
// screen, which is what the previous version's single model did.
type Main struct {
	tab  Tab
	runs runsTab
}

// NewMain returns the main route on its default tab.
func NewMain() Main { return Main{tab: TabPRs, runs: newRunsTab()} }

// Title is the breadcrumb label for the active tab.
func (m Main) Title() string { return tabMeta[m.tab].Name }

// Scope is the keymap scope for the active tab.
func (m Main) Scope() keymap.Scope { return tabMeta[m.tab].Scope }

// Available narrows the footer to the keys that do something on this tab.
func (m Main) Available(action keymap.Action) bool {
	if action != keymap.ActionApprove || m.tab != TabWorkflows {
		return true
	}
	run, ok := m.runs.selected()
	return ok && m.runs.canApprove(run)
}

// Update handles a context change, a poll tick, and the data messages of the
// active tab.
func (m Main) Update(deps Deps, msg tea.Msg) (Route, tea.Cmd) {
	switch msg.(type) {
	case AccountChangedMsg:
		// Everything on screen belonged to the previous context, so it is
		// dropped. A tab that is not on screen fetches when it is next opened.
		m.runs = newRunsTab()
		if m.tab != TabWorkflows {
			return m, nil
		}
		return m.startRuns(deps, true)
	case pollMsg:
		if m.tab != TabWorkflows {
			return m, nil
		}
		var cmd tea.Cmd
		m.runs, cmd = m.runs.poll(deps)
		return m, cmd
	}

	if m.tab != TabWorkflows {
		return m, nil
	}
	var cmd tea.Cmd
	m.runs, cmd = m.runs.update(deps, msg)
	return m, cmd
}

// Act handles the tab strip and delegates everything else to the active tab.
func (m Main) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionNextTab:
		return m.switchTab(deps, (m.tab+1)%tabCount)
	case keymap.ActionPrevTab:
		return m.switchTab(deps, (m.tab+tabCount-1)%tabCount)
	}

	if m.tab != TabWorkflows {
		// The pull request and issue tabs arrive with the data layer. Until
		// then their keys are accepted and do nothing beyond a status line.
		switch action {
		case keymap.ActionRefresh:
			return m, status("Refreshed"), true
		case keymap.ActionClearFilters:
			return m, status("Filters cleared"), true
		}
		return m, nil, false
	}

	var cmd tea.Cmd
	var handled bool
	m.runs, cmd, handled = m.runs.act(deps, action)
	return m, cmd, handled
}

// switchTab moves the strip to another tab. Each tab keeps its own selection,
// so nothing carries across, and a tab that has not been opened yet fetches
// when it first appears.
func (m Main) switchTab(deps Deps, tab Tab) (Route, tea.Cmd, bool) {
	if tab == m.tab {
		return m, nil, true
	}
	m.tab = tab
	if tab != TabWorkflows || m.runs.loaded || m.runs.loading {
		return m, nil, true
	}
	next, cmd := m.startRuns(deps, false)
	return next, cmd, true
}

// startRuns begins the Workflows tab's fetch, clearing what it replaces. The
// cached approval answers are dropped with dropApprovals, which is what a fetch
// for a different set of runs needs.
func (m Main) startRuns(deps Deps, dropApprovals bool) (Main, tea.Cmd) {
	next, cmd := m.runs.start(deps, dropApprovals)
	m.runs = next
	return m, cmd
}

// View renders the tab strip and the active tab.
func (m Main) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	strip, body := r.SplitRows(1)
	lines := []string{layout.Fit(m.tabStrip(deps), strip.W, strip.H)}
	if !body.Empty() {
		lines = append(lines, m.content(deps, body))
	}
	return layout.Fit(strings.Join(lines, "\n"), r.W, r.H)
}

// content renders the body of the active tab into body.
func (m Main) content(deps Deps, body layout.Rect) string {
	if m.tab == TabWorkflows {
		return m.runs.view(deps, body)
	}
	muted := func(s string) string { return "  " + style(deps.Theme.Muted, normal).Render(s) }
	lines := []string{
		"",
		muted(tabMeta[m.tab].Empty),
		"",
		// Two short lines rather than one long one, so a narrow terminal does
		// not clip the sentence mid-word.
		muted("The list for this tab arrives with the data layer."),
		muted("The chrome, tabs, help and key handling are live."),
	}
	return layout.Fit(strings.Join(lines, "\n"), body.W, body.H)
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
