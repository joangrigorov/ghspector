package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/overlay"
	"github.com/joangrigorov/ghspector/internal/store"
)

// issuesPageSize is how many issues one page of the scope-wide list asks each
// repository for (spec §5.2).
const issuesPageSize = 50

// Issue state cycles, in the order the `s` key steps through them (spec §4.3).
var issueStates = []string{"open", "closed", "all"}

// issuesTab is the Issues tab: the scope-wide list, its filters, and the
// selection.
//
// It is the third of the three list tabs and the same shape as the other two: a
// filtered, paginated, polled list of API objects with a Load More sentinel. The
// differences are the filters — issues have no reviewers — and the page size,
// which is the one the previous version asked for.
type issuesTab struct {
	issues  []store.Issue
	loading bool
	loaded  bool
	err     error
	page    int
	hasMore bool
	cursor  int

	// state, author, assignee and repo are the active filters. An empty login
	// filter matches everything, and an empty state means the default of open.
	state    string
	author   string
	assignee string
	repo     string
}

// newIssuesTab returns an unfiltered tab showing open issues, which is the
// state the previous version started in.
func newIssuesTab() issuesTab {
	return issuesTab{page: 1, state: "open"}
}

// issuesLoadedMsg carries one fetched page of issues. A poll reports page 0,
// which is folded into the list rather than replacing it.
type issuesLoadedMsg struct {
	issues  []store.Issue
	hasMore bool
	page    int
	err     error
}

// load fetches one page of the scope-wide list for the tab's filters.
func (t issuesTab) load(deps Deps, page int) tea.Cmd {
	owner := deps.Account.Owner
	q := store.IssueQuery{
		Page:     store.Page{Number: page, Size: issuesPageSize},
		State:    t.state,
		Repo:     t.repo,
		Author:   t.author,
		Assignee: t.assignee,
	}
	ctx := deps.Ctx
	return func() tea.Msg {
		list, err := deps.Store.ScopeIssues(ctx, owner, q)
		if err != nil {
			return issuesLoadedMsg{page: page, err: err}
		}
		return issuesLoadedMsg{issues: list.Items, hasMore: list.HasMore, page: page}
	}
}

// start clears the list and refetches page 1. It is what opening the tab, a
// refresh, and a filter change all mean.
func (t issuesTab) start(deps Deps) (issuesTab, tea.Cmd) {
	t.issues = nil
	t.hasMore = false
	t.cursor = 0
	t.page = 1
	t.loading = true
	t.err = nil
	return t, t.load(deps, 1)
}

// poll refreshes page 1 and folds the result into the list by issue ID, without
// clearing what is on screen. A poll failure is not reported: it is a background
// hiccup, and the next tick tries again.
func (t issuesTab) poll(deps Deps) (issuesTab, tea.Cmd) {
	if t.loading || !t.loaded {
		return t, nil
	}
	return t, t.load(deps, 0)
}

// update handles the messages this tab owns.
func (t issuesTab) update(deps Deps, msg tea.Msg) (issuesTab, tea.Cmd) {
	switch msg := msg.(type) {
	case issuesLoadedMsg:
		return t.apply(msg)
	case FilterMsg:
		return t.acceptFilter(deps, msg.Kind, msg.Value)
	}
	return t, nil
}

// apply folds a fetch result into the tab.
func (t issuesTab) apply(msg issuesLoadedMsg) (issuesTab, tea.Cmd) {
	if msg.err != nil {
		t.loading = false
		if msg.page == 0 {
			return t, nil
		}
		t.err = msg.err
		return t, statusErr("Could not load issues: " + msg.err.Error())
	}

	t.loading = false
	t.loaded = true
	t.err = nil

	switch msg.page {
	case 0:
		t.issues = store.MergeIssues(t.issues, msg.issues)
	case 1:
		t.issues = msg.issues
		t.hasMore = msg.hasMore
		t.cursor = 0
	default:
		t.issues = store.MergeIssues(t.issues, msg.issues)
		t.hasMore = msg.hasMore
	}
	t.clampCursor()
	return t, nil
}

// selectable is the number of rows the cursor can rest on: the issues, plus the
// Load More row while another page exists.
func (t issuesTab) selectable() int {
	n := len(t.issues)
	if t.hasMore {
		n++
	}
	return n
}

// clampCursor keeps the selection inside the selectable rows.
func (t *issuesTab) clampCursor() {
	if t.cursor > t.selectable()-1 {
		t.cursor = t.selectable() - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// selected returns the highlighted issue. The Load More row is not one, so it
// reports false.
func (t issuesTab) selected() (store.Issue, bool) {
	if t.cursor < 0 || t.cursor >= len(t.issues) {
		return store.Issue{}, false
	}
	return t.issues[t.cursor], true
}

// onLoadMore reports whether the Load More row is highlighted.
func (t issuesTab) onLoadMore() bool {
	return t.hasMore && t.cursor == len(t.issues)
}

// act handles a keymap action. It reports whether the tab consumed it.
func (t issuesTab) act(deps Deps, action keymap.Action) (issuesTab, tea.Cmd, bool) {
	switch action {
	case keymap.ActionDown:
		return t.move(1)
	case keymap.ActionUp:
		return t.move(-1)
	case keymap.ActionOpen:
		return t.open(deps)
	case keymap.ActionRefresh:
		next, cmd := t.start(deps)
		return next, tea.Batch(status("Refreshing issues..."), cmd), true
	case keymap.ActionCycleState:
		t.state = nextIssueState(t.state)
		next, cmd := t.start(deps)
		return next, tea.Batch(status("State: "+next.state), cmd), true
	case keymap.ActionFilterAuthor:
		return t.filterBy(deps, FilterAuthor)
	case keymap.ActionFilterAssignee:
		return t.filterBy(deps, FilterAssignee)
	case keymap.ActionFilterType:
		return t, pushOverlay(issueFilterMenu(deps, t.actorFilter())), true
	case keymap.ActionClearFilters:
		return t.clearFilters(deps)
	case keymap.ActionOpenBrowser:
		issue, ok := t.selected()
		if !ok {
			return t, nil, true
		}
		return t, browserCmd(issueURL(issue), "Opened the issue in the browser."), true
	}
	return t, nil, false
}

// move shifts the selection by delta rows.
func (t issuesTab) move(delta int) (issuesTab, tea.Cmd, bool) {
	if t.selectable() == 0 {
		return t, nil, true
	}
	next := t.cursor + delta
	if next < 0 {
		next = 0
	}
	if last := t.selectable() - 1; next > last {
		next = last
	}
	t.cursor = next
	return t, nil, true
}

// open loads the next page from the Load More row, and opens the Issue Details
// route from an issue.
func (t issuesTab) open(deps Deps) (issuesTab, tea.Cmd, bool) {
	if t.onLoadMore() {
		if t.loading {
			return t, nil, true
		}
		t.page++
		t.loading = true
		return t, t.load(deps, t.page), true
	}
	issue, ok := t.selected()
	if !ok {
		return t, nil, true
	}
	details, cmd := newIssueDetails(deps, issue)
	return t, pushRouteThen(details, cmd), true
}

// filterBy toggles one of the two login filters between the signed-in login and
// no filter, which is what the keys mean: show only mine, or stop.
//
// The two keys are two views of the same question, so setting one drops the
// other rather than piling a second condition onto the first. Issues have no
// reviewer filter, so there are two of these rather than three.
func (t issuesTab) filterBy(deps Deps, kind FilterKind) (issuesTab, tea.Cmd, bool) {
	login := deps.Account.Login
	var current string
	switch kind {
	case FilterAuthor:
		current = t.author
	case FilterAssignee:
		current = t.assignee
	default:
		return t, nil, false
	}

	value := login
	if current == login {
		value = ""
	}
	t.author, t.assignee = "", ""
	switch kind {
	case FilterAuthor:
		t.author = value
	case FilterAssignee:
		t.assignee = value
	}

	next, cmd := t.start(deps)
	return next, tea.Batch(status(next.filterLineText()), cmd), true
}

// clearFilters drops every filter and returns the state to open, then refetches.
func (t issuesTab) clearFilters(deps Deps) (issuesTab, tea.Cmd, bool) {
	t.author, t.assignee, t.repo = "", "", ""
	t.state = "open"
	next, cmd := t.start(deps)
	return next, tea.Batch(status("Filters cleared."), cmd), true
}

// acceptFilter applies a filter chosen in an overlay and refetches. A login
// filter chosen this way replaces the other one, the same as the quick keys.
func (t issuesTab) acceptFilter(deps Deps, kind FilterKind, value string) (issuesTab, tea.Cmd) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	switch kind {
	case FilterAuthor, FilterAssignee:
		t.author, t.assignee = "", ""
	}
	switch kind {
	case FilterAuthor:
		t.author = value
	case FilterAssignee:
		t.assignee = value
	case FilterRepo:
		t.repo = value
	default:
		return t, nil
	}
	next, cmd := t.start(deps)
	return next, tea.Batch(status(next.filterLineText()), cmd)
}

// actorFilter is the login the filter prompt opens with: whichever of the two
// login filters is in force.
func (t issuesTab) actorFilter() string {
	for _, v := range []string{t.author, t.assignee} {
		if v != "" {
			return v
		}
	}
	return ""
}

// filterLineText states the active filters for a status message.
func (t issuesTab) filterLineText() string {
	parts := make([]string, 0, 4)
	parts = append(parts, "state "+t.state)
	if t.author != "" {
		parts = append(parts, "author @"+t.author)
	}
	if t.assignee != "" {
		parts = append(parts, "assignee @"+t.assignee)
	}
	if t.repo != "" {
		parts = append(parts, "repo "+t.repo)
	}
	return "Filter: " + strings.Join(parts, ", ")
}

// filterLine renders the active filters, so a narrowed list is never mistaken
// for a context with no issues in it.
func (t issuesTab) filterLine(deps Deps) string {
	if t.author == "" && t.assignee == "" && t.repo == "" {
		return ""
	}
	return "  " + style(deps.Theme.StatusWarn, normal).Render(t.filterLineText()+" (x clears)")
}

// message describes the empty table: still loading, failed, or genuinely empty.
func (t issuesTab) message() string {
	switch {
	case t.loading:
		return "Loading issues..."
	case t.err != nil:
		return "Could not load issues: " + t.err.Error()
	case t.state == "closed" || t.state == "all":
		return "No " + t.state + " issues found."
	default:
		return "No open issues found."
	}
}

// nextIssueState returns the state that follows s in the cycle.
func nextIssueState(s string) string {
	for i, state := range issueStates {
		if state == s {
			return issueStates[(i+1)%len(issueStates)]
		}
	}
	return issueStates[0]
}

// issueURL is the issue's page, falling back to a constructed URL when the API
// did not supply one.
func issueURL(issue store.Issue) string {
	if issue.HTMLURL != "" {
		return issue.HTMLURL
	}
	if issue.Repository.FullName == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", issue.Repository.FullName, issue.Number)
}

// issueRepo is the repository an issue belongs to. An issue carries it, so no
// route reads it back out of shared state.
func issueRepo(issue store.Issue) store.RepoRef {
	owner := ""
	if issue.Repository.Owner != nil {
		owner = issue.Repository.Owner.Login
	}
	return store.RepoRef{Owner: owner, Repo: issue.Repository.Name}
}

// issueAuthor is the login that opened the issue.
func issueAuthor(issue store.Issue) string {
	if issue.User != nil && issue.User.Login != "" {
		return issue.User.Login
	}
	return "unknown"
}

// issueAssignees is the assignee column, and the assignee list in the issue
// heading. Empty means nobody is assigned.
func issueAssignees(issue store.Issue) string {
	if len(issue.Assignees) == 0 {
		return ""
	}
	names := make([]string, 0, len(issue.Assignees))
	for _, u := range issue.Assignees {
		names = append(names, u.Login)
	}
	return strings.Join(names, ", ")
}

// issueTitle is the title as shown: a closed issue keeps its title, and the
// state indicator in front of the row is what says so.
func issueTitle(issue store.Issue) string { return issue.Title }

// issueStateIndicator is the one-cell marker in front of an issue row, coloured
// by whether the issue is open.
func issueStateIndicator(theme Theme, issue store.Issue) string {
	_, colour := issueStateLabel(theme, issue)
	return style(colour, normal).Render("■")
}

// issueColumns allocates the issue table's columns for a row of width cells.
// The title absorbs the slack; the assignee and label columns are the first to
// be given up on a narrow terminal.
func issueColumns(width int) []tableColumn {
	return fitTable([]tableColumn{
		{Header: "ST", Column: layout.Column{Width: 3, Min: 3}},
		{Header: "ISSUE", Column: layout.Column{Width: 7, Min: 6}},
		{Header: "TITLE", Column: layout.Column{Width: 36, Min: 16, Flex: true}},
		{Header: "AUTHOR", Column: layout.Column{Width: 16}},
		{Header: "REPOSITORY", Column: layout.Column{Width: 16}},
		{Header: "ASSIGNEES", Column: layout.Column{Width: 16}},
		{Header: "LABELS", Column: layout.Column{Width: 14}},
	}, width)
}

// view renders the issue table inside r.
func (t issuesTab) view(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	cols := issueColumns(r.W)

	lines := make([]string, 0, r.H)
	if f := t.filterLine(deps); f != "" {
		lines = append(lines, f, "")
	}
	lines = append(lines, style(deps.Theme.Muted, bold).Render(tableHeaderRow(cols)))

	if len(t.issues) == 0 {
		lines = append(lines, "", "  "+style(deps.Theme.Muted, normal).Render(t.message()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	visible := r.H - len(lines)
	if visible < 1 {
		return layout.Fit(joinLines(lines), r.W, r.H)
	}
	total := t.selectable()
	start, end := layout.Window(total, t.cursor, visible)
	for i := start; i < end; i++ {
		if i >= len(t.issues) {
			lines = append(lines, t.loadMoreRow(deps, r.W, i == t.cursor))
			continue
		}
		lines = append(lines, t.issueRow(deps, t.issues[i], cols, r.W, i == t.cursor))
	}
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// issueRow renders one issue. The title is an OSC 8 link to the issue, so a
// click opens it even though the key is `w`.
func (t issuesTab) issueRow(deps Deps, issue store.Issue, cols []tableColumn, width int, selected bool) string {
	title := hyperlink(deps.Theme, issueTitle(issue), issueURL(issue))
	row := tableRow(cols, []string{
		issueStateIndicator(deps.Theme, issue),
		fmt.Sprintf("#%d", issue.Number),
		title,
		issueAuthor(issue),
		issue.Repository.Name,
		orNone(issueAssignees(issue)),
		orNone(labelList(issue.Labels)),
	})
	if selected {
		return selectedRow(deps.Theme, row, width)
	}
	return row
}

// loadMoreRow renders the sentinel that fetches the next page.
func (t issuesTab) loadMoreRow(deps Deps, width int, selected bool) string {
	label := "  [ Load More Issues... ]"
	if t.loading {
		label = "  [ Loading More Issues... ]"
	}
	if selected {
		return selectedRow(deps.Theme, label, width)
	}
	return style(deps.Theme.Muted, normal).Render(label)
}

// issueFilterMenu is the filter overlay for the Issues tab: the same two choices
// as the other tabs, with the user path continuing to the author/assignee picker
// (spec §4.3, §4.6, §4.7).
func issueFilterMenu(deps Deps, current string) overlay.Overlay {
	return overlay.Menu{
		Scope:   keymap.FilterType,
		Heading: "Filter",
		Intro:   []string{"Narrow the issues on this screen."},
		Choices: []overlay.Choice{
			{Action: keymap.ActionFilterUser, Label: "Filter by user"},
			{Action: keymap.ActionFilterRepo, Label: "Filter by repository"},
		},
		OnPick: func(action keymap.Action) tea.Cmd {
			switch action {
			case keymap.ActionFilterUser:
				return func() tea.Msg { return overlay.PushMsg{Overlay: issueUserPrompt(deps, current)} }
			case keymap.ActionFilterRepo:
				return tea.Batch(status("Loading repositories..."), loadRepos(deps))
			}
			return nil
		},
	}
}

// issueUserPrompt is the free-text login box for the issue filters. Its Enter
// continues to the author/assignee picker, which is what applies the filter
// (spec §4.6).
func issueUserPrompt(deps Deps, current string) overlay.Overlay {
	input := newPromptInput(deps.Theme)
	input.SetValue(current)

	return overlay.Prompt{
		Scope:   keymap.UserInput,
		Heading: "Filter issues by user",
		Intro: []string{
			"Show only issues this GitHub login is involved in.",
			"Current user filter: " + displayFilter(current) + ".",
			"Enter continues to choose author or assignee.",
		},
		Input: input,
		OnSubmit: func(value string) tea.Cmd {
			return func() tea.Msg {
				return overlay.ReplaceMsg{Overlay: issueFilterTypeMenu(strings.TrimSpace(strings.TrimPrefix(value, "@")))}
			}
		},
		OnCancel: func() tea.Msg { return overlay.CloseAllMsg{} },
	}
}

// issueFilterTypeMenu applies the role the typed login is filtered by. Issues
// have no reviewers, so this picker has two choices where the pull request one
// has three (spec §4.7).
func issueFilterTypeMenu(login string) overlay.Overlay {
	who := login
	if who == "" {
		who = "the signed-in user"
	}
	return overlay.Menu{
		Scope:   keymap.IssueFilterType,
		Heading: "Filter issues by",
		Intro:   []string{"Show issues where " + who + " is:"},
		Choices: []overlay.Choice{
			{Action: keymap.ActionFilterAuthor, Label: "Author"},
			{Action: keymap.ActionFilterAssignee, Label: "Assignee"},
		},
		OnPick: func(action keymap.Action) tea.Cmd {
			switch action {
			case keymap.ActionFilterAuthor:
				return applyFilter(FilterAuthor, login)
			case keymap.ActionFilterAssignee:
				return applyFilter(FilterAssignee, login)
			}
			return nil
		},
		OnCancel: func() tea.Msg { return overlay.CloseAllMsg{} },
	}
}
