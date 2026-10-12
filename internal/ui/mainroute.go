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
}{
	TabPRs:       {Name: "Pull Requests", Scope: keymap.MainPRs},
	TabWorkflows: {Name: "Workflows", Scope: keymap.MainWorkflows},
	TabIssues:    {Name: "Issues", Scope: keymap.MainIssues},
}

// Main is the primary screen: a tab strip over the active list.
//
// It owns the tab strip, the key routing that is common to the tabs, and each
// tab's state. A tab's data and behaviour live in a value of its own — prsTab,
// runsTab and issuesTab — so this route stays a host rather than growing a
// field per screen, which is what the previous version's single model did.
type Main struct {
	tab    Tab
	runs   runsTab
	prs    prsTab
	issues issuesTab
}

// NewMain returns the main route on its default tab.
func NewMain() Main {
	return Main{tab: TabPRs, runs: newRunsTab(), prs: newPRsTab(), issues: newIssuesTab()}
}

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
		m.prs = newPRsTab()
		m.issues = newIssuesTab()
		switch m.tab {
		case TabWorkflows:
			return m.startRuns(deps, true)
		case TabPRs:
			return m.startPRs(deps)
		case TabIssues:
			return m.startIssues(deps)
		}
		return m, nil
	case pollMsg:
		var cmd tea.Cmd
		switch m.tab {
		case TabWorkflows:
			m.runs, cmd = m.runs.poll(deps)
		case TabPRs:
			m.prs, cmd = m.prs.poll(deps)
		case TabIssues:
			m.issues, cmd = m.issues.poll(deps)
		}
		return m, cmd
	}

	switch m.tab {
	case TabWorkflows:
		var cmd tea.Cmd
		m.runs, cmd = m.runs.update(deps, msg)
		return m, cmd
	case TabPRs:
		var cmd tea.Cmd
		m.prs, cmd = m.prs.update(deps, msg)
		return m, cmd
	case TabIssues:
		var cmd tea.Cmd
		m.issues, cmd = m.issues.update(deps, msg)
		return m, cmd
	}
	return m, nil
}

// Act handles the tab strip and delegates everything else to the active tab.
func (m Main) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionNextTab:
		return m.switchTab(deps, (m.tab+1)%tabCount)
	case keymap.ActionPrevTab:
		return m.switchTab(deps, (m.tab+tabCount-1)%tabCount)
	}

	switch m.tab {
	case TabWorkflows:
		var cmd tea.Cmd
		var handled bool
		m.runs, cmd, handled = m.runs.act(deps, action)
		return m, cmd, handled
	case TabPRs:
		var cmd tea.Cmd
		var handled bool
		m.prs, cmd, handled = m.prs.act(deps, action)
		return m, cmd, handled
	case TabIssues:
		var cmd tea.Cmd
		var handled bool
		m.issues, cmd, handled = m.issues.act(deps, action)
		return m, cmd, handled
	}

	return m, nil, false
}

// switchTab moves the strip to another tab. Each tab keeps its own selection
// and its own filters, so nothing carries across, and a tab that has not been
// opened yet fetches when it first appears.
func (m Main) switchTab(deps Deps, tab Tab) (Route, tea.Cmd, bool) {
	if tab == m.tab {
		return m, nil, true
	}
	m.tab = tab
	switch tab {
	case TabWorkflows:
		if m.runs.loaded || m.runs.loading {
			return m, nil, true
		}
		next, cmd := m.startRuns(deps, false)
		return next, cmd, true
	case TabPRs:
		if m.prs.loaded || m.prs.loading {
			return m, nil, true
		}
		next, cmd := m.startPRs(deps)
		return next, cmd, true
	case TabIssues:
		if m.issues.loaded || m.issues.loading {
			return m, nil, true
		}
		next, cmd := m.startIssues(deps)
		return next, cmd, true
	}
	return m, nil, true
}

// startRuns begins the Workflows tab's fetch, clearing what it replaces. The
// cached approval answers are dropped with dropApprovals, which is what a fetch
// for a different set of runs needs.
func (m Main) startRuns(deps Deps, dropApprovals bool) (Main, tea.Cmd) {
	next, cmd := m.runs.start(deps, dropApprovals)
	m.runs = next
	return m, cmd
}

// startPRs begins the Pull Requests tab's fetch, clearing what it replaces.
func (m Main) startPRs(deps Deps) (Main, tea.Cmd) {
	next, cmd := m.prs.start(deps)
	m.prs = next
	return m, cmd
}

// startIssues begins the Issues tab's fetch, clearing what it replaces.
func (m Main) startIssues(deps Deps) (Main, tea.Cmd) {
	next, cmd := m.issues.start(deps)
	m.issues = next
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

// content renders the body of the active tab.
func (m Main) content(deps Deps, body layout.Rect) string {
	switch m.tab {
	case TabWorkflows:
		return m.runs.view(deps, body)
	case TabPRs:
		return m.prs.view(deps, body)
	case TabIssues:
		return m.issues.view(deps, body)
	}
	return ""
}

// tabStrip renders the tab labels with the active one highlighted.
func (m Main) tabStrip(deps Deps) string {
	theme := deps.Theme
	parts := make([]string, 0, tabCount)
	for i := 0; i < tabCount; i++ {
		meta := tabMeta[Tab(i)]
		if Tab(i) == m.tab {
			parts = append(parts, style(theme.Accent, bold).Render(" "+meta.Name+" "))
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
