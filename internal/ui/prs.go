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

// prsPageSize is how many pull requests one page of the scope-wide list asks
// each repository for (spec §5.2).
const prsPageSize = 8

// PR state cycles, in the order the `s` key steps through them (spec §4.2).
var prStates = []string{"open", "closed", "all"}

// prsTab is the Pull Requests tab: the scope-wide list, its filters, and the
// selection.
//
// It mirrors runsTab, because the two tabs are the same kind of screen: a
// filtered, paginated, polled list of API objects with a Load More sentinel.
// The shared shape is deliberate and the differences are the filters.
type prsTab struct {
	prs     []store.PullRequest
	loading bool
	loaded  bool
	err     error
	page    int
	hasMore bool
	cursor  int

	// state, author, assignee, reviewer and repo are the active filters. An
	// empty login filter matches everything, and an empty state means the
	// default of open.
	state    string
	author   string
	assignee string
	reviewer string
	repo     string
}

// newPRsTab returns an unfiltered tab showing open pull requests, which is the
// state the previous version started in.
func newPRsTab() prsTab {
	return prsTab{page: 1, state: "open"}
}

// prsLoadedMsg carries one fetched page of pull requests. A poll reports page
// 0, which is folded into the list rather than replacing it.
type prsLoadedMsg struct {
	prs     []store.PullRequest
	hasMore bool
	page    int
	err     error
}

// load fetches one page of the scope-wide list for the tab's filters.
func (t prsTab) load(deps Deps, page int) tea.Cmd {
	owner := deps.Account.Owner
	q := store.PRQuery{
		Page:     store.Page{Number: page, Size: prsPageSize},
		State:    t.state,
		Repo:     t.repo,
		Author:   t.author,
		Assignee: t.assignee,
		Reviewer: t.reviewer,
	}
	ctx := deps.Ctx
	return func() tea.Msg {
		list, err := deps.Store.ScopePRs(ctx, owner, q)
		if err != nil {
			return prsLoadedMsg{page: page, err: err}
		}
		return prsLoadedMsg{prs: list.Items, hasMore: list.HasMore, page: page}
	}
}

// start clears the list and refetches page 1. It is what opening the tab, a
// refresh, and a filter change all mean.
func (t prsTab) start(deps Deps) (prsTab, tea.Cmd) {
	t.prs = nil
	t.hasMore = false
	t.cursor = 0
	t.page = 1
	t.loading = true
	t.err = nil
	return t, t.load(deps, 1)
}

// poll refreshes page 1 and folds the result into the list by pull request ID,
// without clearing what is on screen. A poll failure is not reported: it is a
// background hiccup, and the next tick tries again.
func (t prsTab) poll(deps Deps) (prsTab, tea.Cmd) {
	if t.loading || !t.loaded {
		return t, nil
	}
	return t, t.load(deps, 0)
}

// update handles the messages this tab owns.
func (t prsTab) update(deps Deps, msg tea.Msg) (prsTab, tea.Cmd) {
	switch msg := msg.(type) {
	case prsLoadedMsg:
		return t.apply(msg)
	case FilterMsg:
		return t.acceptFilter(deps, msg.Kind, msg.Value)
	}
	return t, nil
}

// apply folds a fetch result into the tab.
func (t prsTab) apply(msg prsLoadedMsg) (prsTab, tea.Cmd) {
	if msg.err != nil {
		t.loading = false
		if msg.page == 0 {
			return t, nil
		}
		t.err = msg.err
		return t, statusErr("Could not load pull requests: " + msg.err.Error())
	}

	t.loading = false
	t.loaded = true
	t.err = nil

	switch msg.page {
	case 0:
		t.prs = store.MergePRs(t.prs, msg.prs)
	case 1:
		t.prs = msg.prs
		t.hasMore = msg.hasMore
		t.cursor = 0
	default:
		t.prs = store.MergePRs(t.prs, msg.prs)
		t.hasMore = msg.hasMore
	}
	t.clampCursor()
	return t, nil
}

// selectable is the number of rows the cursor can rest on: the pull requests,
// plus the Load More row while another page exists.
func (t prsTab) selectable() int {
	n := len(t.prs)
	if t.hasMore {
		n++
	}
	return n
}

// clampCursor keeps the selection inside the selectable rows.
func (t *prsTab) clampCursor() {
	if t.cursor > t.selectable()-1 {
		t.cursor = t.selectable() - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// selected returns the highlighted pull request. The Load More row is not one,
// so it reports false.
func (t prsTab) selected() (store.PullRequest, bool) {
	if t.cursor < 0 || t.cursor >= len(t.prs) {
		return store.PullRequest{}, false
	}
	return t.prs[t.cursor], true
}

// onLoadMore reports whether the Load More row is highlighted.
func (t prsTab) onLoadMore() bool {
	return t.hasMore && t.cursor == len(t.prs)
}

// act handles a keymap action. It reports whether the tab consumed it.
func (t prsTab) act(deps Deps, action keymap.Action) (prsTab, tea.Cmd, bool) {
	switch action {
	case keymap.ActionDown:
		return t.move(1)
	case keymap.ActionUp:
		return t.move(-1)
	case keymap.ActionOpen:
		return t.open(deps)
	case keymap.ActionRefresh:
		next, cmd := t.start(deps)
		return next, tea.Batch(status("Refreshing pull requests..."), cmd), true
	case keymap.ActionCycleState:
		t.state = nextPRState(t.state)
		next, cmd := t.start(deps)
		return next, tea.Batch(status("State: "+next.state), cmd), true
	case keymap.ActionFilterAuthor:
		return t.filterBy(deps, FilterAuthor)
	case keymap.ActionFilterAssignee:
		return t.filterBy(deps, FilterAssignee)
	case keymap.ActionFilterReviewer:
		return t.filterBy(deps, FilterReviewer)
	case keymap.ActionFilterType:
		return t, pushOverlay(prFilterMenu(deps, t.actorFilter())), true
	case keymap.ActionClearFilters:
		return t.clearFilters(deps)
	case keymap.ActionOpenBrowser:
		pr, ok := t.selected()
		if !ok {
			return t, nil, true
		}
		return t, browserCmd(prURL(pr), "Opened the pull request in the browser."), true
	}
	return t, nil, false
}

// move shifts the selection by delta rows.
func (t prsTab) move(delta int) (prsTab, tea.Cmd, bool) {
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

// open loads the next page from the Load More row, and opens the PR Details
// route from a pull request.
func (t prsTab) open(deps Deps) (prsTab, tea.Cmd, bool) {
	if t.onLoadMore() {
		if t.loading {
			return t, nil, true
		}
		t.page++
		t.loading = true
		return t, t.load(deps, t.page), true
	}
	pr, ok := t.selected()
	if !ok {
		return t, nil, true
	}
	details, cmd := newPRDetails(deps, pr)
	return t, pushRouteThen(details, cmd), true
}

// filterBy toggles one of the three login filters between the signed-in login
// and no filter, which is what the keys mean: show only mine, or stop.
//
// The three keys are three views of the same question, so setting one drops the
// other two rather than piling a second condition onto the first. That is what
// the previous version did, and it is what makes the keys readable as a choice.
func (t prsTab) filterBy(deps Deps, kind FilterKind) (prsTab, tea.Cmd, bool) {
	login := deps.Account.Login
	var current string
	switch kind {
	case FilterAuthor:
		current = t.author
	case FilterAssignee:
		current = t.assignee
	case FilterReviewer:
		current = t.reviewer
	default:
		return t, nil, false
	}

	value := login
	if current == login {
		value = ""
	}
	t.author, t.assignee, t.reviewer = "", "", ""
	switch kind {
	case FilterAuthor:
		t.author = value
	case FilterAssignee:
		t.assignee = value
	case FilterReviewer:
		t.reviewer = value
	}

	next, cmd := t.start(deps)
	return next, tea.Batch(status(next.filterLineText()), cmd), true
}

// clearFilters drops every filter and returns the state to open, then
// refetches.
func (t prsTab) clearFilters(deps Deps) (prsTab, tea.Cmd, bool) {
	t.author, t.assignee, t.reviewer, t.repo = "", "", "", ""
	t.state = "open"
	next, cmd := t.start(deps)
	return next, tea.Batch(status("Filters cleared."), cmd), true
}

// acceptFilter applies a filter chosen in an overlay and refetches. A login
// filter chosen this way replaces the other two, the same as the quick keys.
func (t prsTab) acceptFilter(deps Deps, kind FilterKind, value string) (prsTab, tea.Cmd) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	switch kind {
	case FilterAuthor, FilterAssignee, FilterReviewer:
		t.author, t.assignee, t.reviewer = "", "", ""
	}
	switch kind {
	case FilterAuthor:
		t.author = value
	case FilterAssignee:
		t.assignee = value
	case FilterReviewer:
		t.reviewer = value
	case FilterRepo:
		t.repo = value
	default:
		return t, nil
	}
	next, cmd := t.start(deps)
	return next, tea.Batch(status(next.filterLineText()), cmd)
}

// actorFilter is the login the filter prompt opens with: whichever of the three
// login filters is in force.
func (t prsTab) actorFilter() string {
	for _, v := range []string{t.author, t.assignee, t.reviewer} {
		if v != "" {
			return v
		}
	}
	return ""
}

// filterLineText states the active filters for a status message.
func (t prsTab) filterLineText() string {
	parts := make([]string, 0, 5)
	parts = append(parts, "state "+t.state)
	if t.author != "" {
		parts = append(parts, "author @"+t.author)
	}
	if t.assignee != "" {
		parts = append(parts, "assignee @"+t.assignee)
	}
	if t.reviewer != "" {
		parts = append(parts, "reviewer @"+t.reviewer)
	}
	if t.repo != "" {
		parts = append(parts, "repo "+t.repo)
	}
	return "Filter: " + strings.Join(parts, ", ")
}

// filterLine renders the active filters, so a narrowed list is never mistaken
// for a context with no pull requests in it.
func (t prsTab) filterLine(deps Deps) string {
	if t.author == "" && t.assignee == "" && t.reviewer == "" && t.repo == "" {
		return ""
	}
	return "  " + style(deps.Theme.StatusWarn, normal).Render(t.filterLineText()+" (x clears)")
}

// message describes the empty table: still loading, failed, or genuinely empty.
func (t prsTab) message() string {
	switch {
	case t.loading:
		return "Loading pull requests..."
	case t.err != nil:
		return "Could not load pull requests: " + t.err.Error()
	case t.state == "closed" || t.state == "all":
		return "No " + t.state + " pull requests found."
	default:
		return "No open pull requests found."
	}
}

// nextPRState returns the state that follows s in the cycle.
func nextPRState(s string) string {
	for i, state := range prStates {
		if state == s {
			return prStates[(i+1)%len(prStates)]
		}
	}
	return prStates[0]
}

// prURL is the pull request's page, falling back to a constructed URL when the
// API did not supply one.
func prURL(pr store.PullRequest) string {
	if pr.HTMLURL != "" {
		return pr.HTMLURL
	}
	if pr.Repository.FullName == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/pull/%d", pr.Repository.FullName, pr.Number)
}

// prRepo is the repository a pull request belongs to. A pull request carries
// it, so no route reads it back out of shared state.
func prRepo(pr store.PullRequest) store.RepoRef {
	owner := ""
	if pr.Repository.Owner != nil {
		owner = pr.Repository.Owner.Login
	}
	return store.RepoRef{Owner: owner, Repo: pr.Repository.Name}
}

// prAuthor is the login that opened the pull request.
func prAuthor(pr store.PullRequest) string {
	if pr.User != nil && pr.User.Login != "" {
		return pr.User.Login
	}
	return "unknown"
}

// prReviewers is the requested-reviewer column.
func prReviewers(pr store.PullRequest) string {
	if len(pr.RequestedReviewers) == 0 {
		return "—"
	}
	names := make([]string, 0, len(pr.RequestedReviewers))
	for _, u := range pr.RequestedReviewers {
		names = append(names, u.Login)
	}
	return strings.Join(names, ", ")
}

// prLabels is the label column.
func prLabels(pr store.PullRequest) string {
	if len(pr.Labels) == 0 {
		return "—"
	}
	names := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		names = append(names, l.Name)
	}
	return strings.Join(names, ", ")
}

// prTitle is the title as shown: a draft is marked, which the state indicator
// alone does not say.
func prTitle(pr store.PullRequest) string {
	if pr.Draft {
		return "[Draft] " + pr.Title
	}
	return pr.Title
}

// prStateIndicator is the one-cell marker in front of a pull request row,
// coloured by what the pull request is: open, draft, merged, or closed.
func prStateIndicator(theme Theme, pr store.PullRequest) string {
	_, colour := prStateLabel(theme, pr)
	return style(colour, normal).Render("■")
}

// prColumns allocates the pull request table's columns for a row of width
// cells. The title absorbs the slack; the reviewer and label columns are the
// first to be given up on a narrow terminal.
func prColumns(width int) []tableColumn {
	return fitTable([]tableColumn{
		{Header: "ST", Column: layout.Column{Width: 3, Min: 3}},
		{Header: "PR", Column: layout.Column{Width: 6, Min: 5}},
		{Header: "TITLE", Column: layout.Column{Width: 34, Min: 16, Flex: true}},
		{Header: "AUTHOR", Column: layout.Column{Width: 16}},
		{Header: "REPOSITORY", Column: layout.Column{Width: 16}},
		{Header: "REVIEWERS", Column: layout.Column{Width: 16}},
		{Header: "LABELS", Column: layout.Column{Width: 14}},
	}, width)
}

// view renders the pull request table inside r.
func (t prsTab) view(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	cols := prColumns(r.W)

	lines := make([]string, 0, r.H)
	if f := t.filterLine(deps); f != "" {
		lines = append(lines, f, "")
	}
	lines = append(lines, style(deps.Theme.Muted, bold).Render(tableHeaderRow(cols)))

	if len(t.prs) == 0 {
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
		if i >= len(t.prs) {
			lines = append(lines, t.loadMoreRow(deps, r.W, i == t.cursor))
			continue
		}
		lines = append(lines, t.prRow(deps, t.prs[i], cols, r.W, i == t.cursor))
	}
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// prRow renders one pull request. The title is an OSC 8 link to the pull
// request, so a click opens it even though the key is `w`.
func (t prsTab) prRow(deps Deps, pr store.PullRequest, cols []tableColumn, width int, selected bool) string {
	title := hyperlink(deps.Theme, prTitle(pr), prURL(pr))
	row := tableRow(cols, []string{
		prStateIndicator(deps.Theme, pr),
		fmt.Sprintf("#%d", pr.Number),
		title,
		prAuthor(pr),
		pr.Repository.Name,
		prReviewers(pr),
		prLabels(pr),
	})
	if selected {
		return selectedRow(deps.Theme, row, width)
	}
	return row
}

// loadMoreRow renders the sentinel that fetches the next page.
func (t prsTab) loadMoreRow(deps Deps, width int, selected bool) string {
	label := "  [ Load More Pull Requests... ]"
	if t.loading {
		label = "  [ Loading More Pull Requests... ]"
	}
	if selected {
		return selectedRow(deps.Theme, label, width)
	}
	return style(deps.Theme.Muted, normal).Render(label)
}

// prFilterMenu is the filter overlay for the Pull Requests tab: the same two
// choices as the Workflows tab, with the user path continuing to the
// author/assignee/reviewer picker (spec §4.2, §4.6, §4.7).
func prFilterMenu(deps Deps, current string) overlay.Overlay {
	return overlay.Menu{
		Scope:   keymap.FilterType,
		Heading: "Filter",
		Intro:   []string{"Narrow the pull requests on this screen."},
		Choices: []overlay.Choice{
			{Action: keymap.ActionFilterUser, Label: "Filter by user"},
			{Action: keymap.ActionFilterRepo, Label: "Filter by repository"},
		},
		OnPick: func(action keymap.Action) tea.Cmd {
			switch action {
			case keymap.ActionFilterUser:
				return func() tea.Msg { return overlay.PushMsg{Overlay: prUserPrompt(deps, current)} }
			case keymap.ActionFilterRepo:
				return tea.Batch(status("Loading repositories..."), loadRepos(deps))
			}
			return nil
		},
	}
}

// prUserPrompt is the free-text login box for the pull request filters. Its
// Enter continues to the author/assignee/reviewer picker, which is what applies
// the filter (spec §4.6).
func prUserPrompt(deps Deps, current string) overlay.Overlay {
	input := newPromptInput(deps.Theme)
	input.SetValue(current)

	return overlay.Prompt{
		Scope:   keymap.UserInput,
		Heading: "Filter pull requests by user",
		Intro: []string{
			"Show only pull requests this GitHub login is involved in.",
			"Current user filter: " + displayFilter(current) + ".",
			"Enter continues to choose author, assignee or reviewer.",
		},
		Input: input,
		OnSubmit: func(value string) tea.Cmd {
			return func() tea.Msg {
				return overlay.ReplaceMsg{Overlay: prFilterTypeMenu(strings.TrimSpace(strings.TrimPrefix(value, "@")))}
			}
		},
		OnCancel: func() tea.Msg { return overlay.CloseAllMsg{} },
	}
}

// prFilterTypeMenu applies the role the typed login is filtered by.
func prFilterTypeMenu(login string) overlay.Overlay {
	who := login
	if who == "" {
		who = "the signed-in user"
	}
	return overlay.Menu{
		Scope:   keymap.PRFilterType,
		Heading: "Filter pull requests by",
		Intro:   []string{"Show pull requests where " + who + " is:"},
		Choices: []overlay.Choice{
			{Action: keymap.ActionFilterAuthor, Label: "Author"},
			{Action: keymap.ActionFilterAssignee, Label: "Assignee"},
			{Action: keymap.ActionFilterReviewer, Label: "Requested reviewer"},
		},
		OnPick: func(action keymap.Action) tea.Cmd {
			switch action {
			case keymap.ActionFilterAuthor:
				return applyFilter(FilterAuthor, login)
			case keymap.ActionFilterAssignee:
				return applyFilter(FilterAssignee, login)
			case keymap.ActionFilterReviewer:
				return applyFilter(FilterReviewer, login)
			}
			return nil
		},
		OnCancel: func() tea.Msg { return overlay.CloseAllMsg{} },
	}
}
