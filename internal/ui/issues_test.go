package ui

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ghspector/internal/keymap"
	"ghspector/internal/store"
)

// The issue tests run against the same fixture server as the pull request ones,
// because the two lists are aggregated the same way and the issues endpoint
// returns both kinds of entry. What differs is what the screen does with them.

// issuesModel is the state every issue test starts from: the model is ready and
// the Issues tab is active with its first page landed. The tab is reached by
// cycling, which is also the lazy fetch under test.
func issuesModel(t *testing.T, srv *httptest.Server, w, h int, tweak ...func(*Options)) Model {
	t.Helper()
	m := prsModel(t, srv, w, h, tweak...)
	m = pressAndDrive(t, m, "tab", "tab")
	if got := tab(t, m); got != TabIssues {
		t.Fatalf("tab = %v, want Issues", got)
	}
	return m
}

// uncachedModel is an issue test model whose store keeps nothing, so a refresh
// is observable as a request.
func uncachedModel(t *testing.T, srv *httptest.Server, w, h int) Model {
	t.Helper()
	return issuesModel(t, srv, w, h, func(o *Options) {
		o.Store = testStore(srv, store.WithCacheTTL(0))
	})
}

// issuesTabOf reads the main route's issue state.
func issuesTabOf(t *testing.T, m Model) issuesTab {
	t.Helper()
	main, ok := m.routes.Top().(Main)
	if !ok {
		t.Fatalf("top route is %T, want ui.Main", m.routes.Top())
	}
	return main.issues
}

// issueDetailsRoute returns the top route as the issue details route.
func issueDetailsRoute(t *testing.T, m Model) *IssueDetails {
	t.Helper()
	details, ok := m.routes.Top().(*IssueDetails)
	if !ok {
		t.Fatalf("top route is %T, want *ui.IssueDetails", m.routes.Top())
	}
	return details
}

// issueCommentsRoute returns the top route as the issue comments route.
func issueCommentsRoute(t *testing.T, m Model) *IssueComments {
	t.Helper()
	comments, ok := m.routes.Top().(*IssueComments)
	if !ok {
		t.Fatalf("top route is %T, want *ui.IssueComments", m.routes.Top())
	}
	return comments
}

// openIssueDetails opens the first issue of the list and runs the fetch the
// route starts with.
func openIssueDetails(t *testing.T, m Model) Model {
	t.Helper()
	m = pressAndDrive(t, m, "enter")
	if _, ok := m.routes.Top().(*IssueDetails); !ok {
		t.Fatalf("enter did not open the issue details; top = %T", m.routes.Top())
	}
	return m
}

func TestIssueListLoadsAndSortsAcrossRepositories(t *testing.T) {
	srv := prServer(t)
	m := issuesModel(t, srv, 120, 40)

	// The tab opens on the open state, so the closed fixture is not in it yet.
	tab := issuesTabOf(t, m)
	if len(tab.issues) != 1 || tab.issues[0].Number != issueFixtureNumber {
		t.Fatalf("the open list holds %d issues, want just #%d", len(tab.issues), issueFixtureNumber)
	}
	if tab.issues[0].Repository.Name == "" {
		t.Error("the row carries no repository")
	}

	// The closed and the all states bring the second fixture in, sorted by the
	// time it was last touched. The fixture also serves one entry that is
	// really a pull request, so a list of two is the exclusion working end to
	// end: open → closed → all.
	m = pressAndDrive(t, m, "s", "s")
	tab = issuesTabOf(t, m)
	if len(tab.issues) != 2 {
		t.Fatalf("the all list holds %d issues, want 2 out of %d entries served", len(tab.issues), len(fixtureIssues()))
	}
	for i, want := range []int{issueFixtureNumber, 4} {
		if got := tab.issues[i].Number; got != want {
			t.Fatalf("row %d is #%d, want #%d", i, got, want)
		}
		if tab.issues[i].Repository.Name == "" {
			t.Errorf("row %d carries no repository", i)
		}
	}

	out := screen(t, m, 120, 40)
	for _, want := range []string{"TITLE", "#3", "Add the issues view", "Ship the old thing"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "A pull request in the issue list") {
		t.Errorf("a pull request was drawn as an issue:\n%s", out)
	}
}

// TestIssueListExcludesPullRequestsFromTheStore is the same exclusion one layer
// down, so the list test above cannot pass for the wrong reason: the store
// returns one item for a page the server filled with two issues and a pull
// request.
func TestIssueListExcludesPullRequestsFromTheStore(t *testing.T) {
	srv := prServer(t)
	list, err := testStore(srv).Issues(t.Context(),
		store.RepoRef{Owner: "octocat", Repo: fixtureRepo},
		store.IssueQuery{Page: store.Page{Number: 1, Size: issuesPageSize}, State: "open"})
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Number != issueFixtureNumber {
		t.Fatalf("the store returned %d items, want just #%d", len(list.Items), issueFixtureNumber)
	}
	if list.HasMore {
		t.Error("HasMore = true for a page that is not full")
	}
}

func TestIssueStateCycleRefetches(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := uncachedModel(t, srv, 120, 40)

	list := "/repos/octocat/" + fixtureRepo + "/issues"
	if got := log.query(list); !strings.Contains(got, "state=open") {
		t.Errorf("the first fetch asked for %q, want the open state", got)
	}

	for _, want := range []string{"closed", "all", "open"} {
		m = pressAndDrive(t, m, "s")
		if got := issuesTabOf(t, m).state; got != want {
			t.Fatalf("state after s = %q, want %q", got, want)
		}
		if got := log.query(list); !strings.Contains(got, "state="+want) {
			t.Errorf("after s the fetch asked for %q, want the %s state", got, want)
		}
	}
}

func TestIssueQuickFiltersToggleIndependently(t *testing.T) {
	srv := prServer(t)
	m := issuesModel(t, srv, 120, 40)

	m = pressAndDrive(t, m, "a")
	if got := issuesTabOf(t, m).author; got != "octocat" {
		t.Fatalf("author filter = %q, want octocat", got)
	}
	// The assignee key replaces the author filter rather than adding to it: the
	// two keys are two views of the same question.
	m = pressAndDrive(t, m, "i")
	tab := issuesTabOf(t, m)
	if tab.assignee != "octocat" || tab.author != "" {
		t.Fatalf("after i: assignee = %q, author = %q, want assignee only", tab.assignee, tab.author)
	}
	// Pressing the same key again clears it.
	m = pressAndDrive(t, m, "i")
	if got := issuesTabOf(t, m).assignee; got != "" {
		t.Fatalf("assignee filter = %q, want it cleared", got)
	}
}

func TestIssueFiltersNarrowTheListClientSide(t *testing.T) {
	srv := prServer(t)
	m := issuesModel(t, srv, 120, 40)

	m = pressAndDrive(t, m, "a")
	tab := issuesTabOf(t, m)
	if len(tab.issues) != 1 || tab.issues[0].Number != issueFixtureNumber {
		t.Fatalf("the author filter kept %d rows, want just #%d", len(tab.issues), issueFixtureNumber)
	}

	// An assignee filter that matches nobody leaves the screen empty rather
	// than unchanged, and the line above the table says what is in force.
	m = pressAndDrive(t, m, "x", "f", "u")
	m = pressAndDrive(t, m, "n", "o", "b", "o", "d", "y", "enter", "i")
	tab = issuesTabOf(t, m)
	if len(tab.issues) != 0 {
		t.Fatalf("the assignee filter kept %d rows, want none", len(tab.issues))
	}
	if out := screen(t, m, 120, 40); !strings.Contains(out, "assignee @nobody") {
		t.Errorf("the active filter is not on screen:\n%s", out)
	}
}

// manyIssues is n issues in the first repository, ordered so the newest is
// first. It is what makes the Load More row appear: the page size is the issue
// page size, so n of them fill a page.
func manyIssues(n int) []issueSpec {
	specs := make([]issueSpec, 0, n)
	for i := 0; i < n; i++ {
		specs = append(specs, issueSpec{
			number:  100 + i,
			repo:    fixtureRepo,
			title:   fmt.Sprintf("Issue %d", i),
			state:   "open",
			author:  "octocat",
			created: workflowNow.Add(-time.Duration(i) * time.Minute),
			updated: workflowNow.Add(-time.Duration(i) * time.Minute),
		})
	}
	return specs
}

func TestIssueLoadMoreFetchesTheNextPage(t *testing.T) {
	full := manyIssues(issuesPageSize)
	srv := prServer(t, withIssues(full...))
	m := issuesModel(t, srv, 120, 40)

	if !issuesTabOf(t, m).hasMore {
		t.Fatalf("HasMore = false for a full page of %d", len(full))
	}

	// The Load More row is the last selectable row, so the cursor walks to it.
	keys := make([]string, 0, len(full))
	for range full {
		keys = append(keys, "down")
	}
	m = pressAndDrive(t, m, keys...)
	if !issuesTabOf(t, m).onLoadMore() {
		t.Fatalf("cursor = %d, want the Load More row of %d rows",
			issuesTabOf(t, m).cursor, len(issuesTabOf(t, m).issues))
	}
	m = pressAndDrive(t, m, "enter")
	if got := issuesTabOf(t, m).page; got != 2 {
		t.Fatalf("page = %d, want 2", got)
	}
	// The next page is folded in by ID, so a page fetched twice does not double
	// the list.
	if got := len(issuesTabOf(t, m).issues); got != len(full) {
		t.Errorf("the list holds %d issues after loading the next page, want %d", got, len(full))
	}
}

// TestIssueFilterOverlayAppliesATypedLogin walks the free-text path of
// §4.6-4.7 on the Issues tab: the filter menu, the login prompt, then the
// author/assignee picker, which offers two choices rather than three.
func TestIssueFilterOverlayAppliesATypedLogin(t *testing.T) {
	srv := prServer(t)
	m := issuesModel(t, srv, 120, 40)

	m = pressAndDrive(t, m, "f")
	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "Filter by user") || !strings.Contains(out, "Filter by repository") {
		t.Fatalf("the filter menu does not offer the two paths:\n%s", out)
	}

	m = pressAndDrive(t, m, "u")
	out = screen(t, m, 120, 40)
	if !strings.Contains(out, "Filter issues by user") {
		t.Fatalf("u did not open the login prompt:\n%s", out)
	}

	m = typeKeys(t, m, "a", "l", "i", "c", "e")
	m = pressAndDrive(t, m, "enter")
	out = screen(t, m, 120, 40)
	if !strings.Contains(out, "where alice is") {
		t.Fatalf("submitting the login did not advance to the role picker:\n%s", out)
	}
	if strings.Contains(out, "reviewer") {
		t.Errorf("the issue picker offers a reviewer filter:\n%s", out)
	}

	m = pressAndDrive(t, m, "i")
	tab := issuesTabOf(t, m)
	if tab.assignee != "alice" || tab.author != "" {
		t.Fatalf("filters = assignee %q, author %q; want assignee only", tab.assignee, tab.author)
	}
	if !m.overlays.Empty() {
		t.Errorf("overlays remain open after applying the filter: %d", m.overlays.Len())
	}
}

// TestIssueFilterOverlayRepoPickNarrowsTheFanOut follows the repository path,
// which has to reach the store's fan-out rather than only the client-side
// filter: one repository means one list request.
func TestIssueFilterOverlayRepoPickNarrowsTheFanOut(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := issuesModel(t, srv, 120, 40)

	m = pressAndDrive(t, m, "f", "r")
	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "Filter by repository") || !strings.Contains(out, fixtureRepo) {
		t.Fatalf("the repository picker did not open with the fixture repositories:\n%s", out)
	}

	webIssues := "/repos/octocat/" + secondRepo + "/issues"
	beforeWeb := log.count(webIssues)
	m = pressAndDrive(t, m, "enter")
	if got := issuesTabOf(t, m).repo; got != fixtureRepo {
		t.Fatalf("repo filter = %q, want %q", got, fixtureRepo)
	}
	if got := log.count(webIssues); got != beforeWeb {
		t.Errorf("the narrowed fetch still asked %s for issues (%d requests, was %d)", secondRepo, got, beforeWeb)
	}
	if got := log.query("/repos/octocat/" + fixtureRepo + "/issues"); !strings.Contains(got, "state=open") {
		t.Errorf("the narrowed fetch asked %q", got)
	}
}

func TestIssueListReportsAFailure(t *testing.T) {
	srv := prServer(t, failIssueLists())
	m := issuesModel(t, srv, 120, 40)

	tab := issuesTabOf(t, m)
	if tab.err == nil {
		t.Fatal("the failed fetch left no error on the tab")
	}
	if tab.loading {
		t.Error("the tab is still loading after a failure")
	}
	if out := screen(t, m, 120, 40); !strings.Contains(out, "Could not load issues") {
		t.Errorf("the failure is not on screen:\n%s", out)
	}
}

func TestIssueBrowserKeyNeedsASelection(t *testing.T) {
	srv := prServer(t, withIssues())
	m := issuesModel(t, srv, 120, 40)

	if got := issuesTabOf(t, m).selectable(); got != 0 {
		t.Fatalf("selectable = %d, want none", got)
	}
	next, cmd := pressKey(t, m, "w")
	if cmd != nil {
		t.Error("w produced a command with nothing selected")
	}
	if next.stage != stageReady {
		t.Fatalf("the model left the ready stage: %v", next.stage)
	}
}

// TestIssueDetailsShowsTheDescriptionAndTheSidebar checks the screen the Enter
// key opens: the heading, the description, and the metadata §4.15 lists.
func TestIssueDetailsShowsTheDescriptionAndTheSidebar(t *testing.T) {
	srv := prServer(t)
	m := openIssueDetails(t, issuesModel(t, srv, 120, 40))

	if !issueDetailsRoute(t, m).descFocused {
		t.Error("the description does not start focused")
	}
	out := screen(t, m, 120, 40)
	for _, want := range []string{
		"OPEN", "Issue #3: Add the issues view", "Repo: octocat/api",
		"DESCRIPTION", "DETAILS",
		"Issues are the third list",
		"State:", "Author:", "@octocat", "Assignees:", "@hubot",
		"Milestone:", "0.7.0", "Labels:", "enhancement",
		"Created:", "Updated:", "Closed:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the details screen is missing %q:\n%s", want, out)
		}
	}
}

// TestIssueDetailsScrollAndFocus covers the two scroll steps of §4.15 and the
// focus toggle that gates them.
func TestIssueDetailsScrollAndFocus(t *testing.T) {
	srv := prServer(t)
	m := openIssueDetails(t, issuesModel(t, srv, 120, 40))
	details := issueDetailsRoute(t, m)

	wrap := len(wrapLines(details.description(), m.deps.Size.Width))
	last := wrap - 1
	if last < 1+descStep {
		t.Fatalf("the fixture description wraps to %d lines, too few to scroll by %d", wrap, descStep)
	}

	m = pressAndDrive(t, m, "j")
	if got := issueDetailsRoute(t, m).descScroll; got != 1 {
		t.Fatalf("j moved the description to %d, want 1", got)
	}
	m = pressAndDrive(t, m, "d")
	if got := issueDetailsRoute(t, m).descScroll; got != 1+descStep {
		t.Fatalf("d moved the description to %d, want %d", got, 1+descStep)
	}
	// The sidebar holds no selection, so a scroll key while it has the focus
	// leaves the description where it was.
	m = pressAndDrive(t, m, "tab")
	if issueDetailsRoute(t, m).descFocused {
		t.Fatal("tab did not move the focus to the sidebar")
	}
	m = pressAndDrive(t, m, "u")
	if got := issueDetailsRoute(t, m).descScroll; got != 1+descStep {
		t.Fatalf("u scrolled the description to %d while the sidebar had the focus, want %d", got, 1+descStep)
	}
	// Tab returns the focus, and u then walks back up by six, to the line it
	// started this leg on.
	m = pressAndDrive(t, m, "tab", "u")
	if got := issueDetailsRoute(t, m).descScroll; got != 1 {
		t.Fatalf("u moved the description to %d, want one step back from %d", got, 1+descStep)
	}
	// The clamp holds at the bottom, so the last line stays reachable.
	for i := 0; i < wrap; i++ {
		m = pressAndDrive(t, m, "j")
	}
	if got := issueDetailsRoute(t, m).descScroll; got != last {
		t.Fatalf("the description stopped at %d, want the last line %d", got, last)
	}
}

// TestIssueDetailsRefreshRefetches checks the r key reaches the API again.
func TestIssueDetailsRefreshRefetches(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := openIssueDetails(t, uncachedModel(t, srv, 120, 40))

	path := "/repos/octocat/" + fixtureRepo + "/issues/" + fmt.Sprint(issueFixtureNumber)
	before := log.count(path)
	if before == 0 {
		t.Fatalf("the details never fetched %s", path)
	}
	pressAndDrive(t, m, "r")
	if got := log.count(path); got <= before {
		t.Errorf("r did not refetch the issue: %d requests, was %d", got, before)
	}
}

// TestIssueCommentsOpenRefreshAndReturn covers §4.16: c opens the conversation,
// r refetches it, and Esc returns to the details.
func TestIssueCommentsOpenRefreshAndReturn(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := openIssueDetails(t, uncachedModel(t, srv, 120, 40))

	m = pressAndDrive(t, m, "c")
	if !issueCommentsRoute(t, m).loaded {
		t.Fatal("the conversation did not load")
	}
	out := screen(t, m, 120, 40)
	for _, want := range []string{"Comments on issue #3", "Comment 1 looks good to me.", "alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("the conversation is missing %q:\n%s", want, out)
		}
	}

	path := "/repos/octocat/" + fixtureRepo + "/issues/" + fmt.Sprint(issueFixtureNumber) + "/comments"
	before := log.count(path)
	m = pressAndDrive(t, m, "r")
	if got := log.count(path); got <= before {
		t.Errorf("r did not refetch the comments: %d requests, was %d", got, before)
	}

	m = pressAndDrive(t, m, "esc")
	if _, ok := m.routes.Top().(*IssueDetails); !ok {
		t.Fatalf("Esc left the stack at %T, want the issue details", m.routes.Top())
	}
}

// TestIssueCommentsScroll checks a thread longer than the screen scrolls, which
// is what makes it readable. The fixture comments carry distinct timestamps, so
// the frame itself says which end of the thread is on screen.
func TestIssueCommentsScroll(t *testing.T) {
	srv := prServer(t, withComments(12))
	m := openIssueDetails(t, issuesModel(t, srv, 100, 30))
	m = pressAndDrive(t, m, "c")

	comments := issueCommentsRoute(t, m)
	content := len(comments.lines(m.deps))
	if content <= m.deps.Size.Height {
		t.Fatalf("the fixture conversation is %d lines and the screen is %d rows", content, m.deps.Size.Height)
	}
	first, last := comments.comments[0], comments.comments[len(comments.comments)-1]
	out := screen(t, m, 100, 30)
	if !strings.Contains(out, first.Body) {
		t.Errorf("the first comment is not on screen:\n%s", out)
	}
	if strings.Contains(out, last.Body) {
		t.Errorf("the whole thread fits on screen, so scrolling proves nothing:\n%s", out)
	}

	m = pressAndDrive(t, m, "d")
	if got := issueCommentsRoute(t, m).scroll; got != diffStep {
		t.Fatalf("d scrolled to %d, want %d", got, diffStep)
	}

	// Scrolling on to the end reaches the newest comment, leaves the oldest
	// behind, and stops on the last line rather than past it.
	for i := 0; i < content/diffStep+2; i++ {
		m = pressAndDrive(t, m, "d")
	}
	if got := issueCommentsRoute(t, m).scroll; got != content-1 {
		t.Errorf("the conversation stopped at %d, want the last line %d", got, content-1)
	}
	out = screen(t, m, 100, 30)
	if !strings.Contains(out, last.Body) {
		t.Errorf("the end of the thread does not show the newest comment (%q):\n%s", last.Body, out)
	}
	if strings.Contains(out, first.Body) {
		t.Errorf("the top of the thread is still on screen after scrolling to the end:\n%s", out)
	}

	m = pressAndDrive(t, m, "u")
	if got, want := issueCommentsRoute(t, m).scroll, content-1-diffStep; got != want {
		t.Errorf("u scrolled to %d, want one step back from the end (%d)", got, want)
	}
}

// TestIssueCommentsReportAFailure covers the failed state of the conversation.
func TestIssueCommentsReportAFailure(t *testing.T) {
	srv := prServer(t, failCommentLists())
	m := openIssueDetails(t, issuesModel(t, srv, 120, 40))
	m = pressAndDrive(t, m, "c")

	comments := issueCommentsRoute(t, m)
	if comments.err == nil {
		t.Fatal("the failed fetch left no error on the conversation")
	}
	if comments.loaded || comments.loading {
		t.Errorf("loaded = %v, loading = %v, want a finished failure", comments.loaded, comments.loading)
	}
	if out := screen(t, m, 120, 40); !strings.Contains(out, "Could not load the comments") {
		t.Errorf("the failure is not on screen:\n%s", out)
	}
}

// TestIssueViewsScopesAndFooter checks the keymap scope travels with each route,
// so the footer describes the keys the screen actually has.
func TestIssueViewsScopesAndFooter(t *testing.T) {
	srv := prServer(t)
	m := openIssueDetails(t, issuesModel(t, srv, 120, 40))

	if got := m.routes.Top().Scope(); got != keymap.IssueDetails {
		t.Errorf("scope = %q, want %q", got, keymap.IssueDetails)
	}
	if !footerHasAction(m, keymap.ActionComments) {
		t.Error("the footer does not offer the comments key")
	}

	m = pressAndDrive(t, m, "c")
	if got := m.routes.Top().Scope(); got != keymap.IssueComments {
		t.Errorf("comments scope = %q, want %q", got, keymap.IssueComments)
	}
	// The conversation has no way back into the pull request sub-tabs.
	if footerHasAction(m, keymap.ActionTabChecks) {
		t.Error("the comments footer offers a pull request sub-tab")
	}
}

// TestIssueViewsSurviveEveryWidth is the layout invariant for the two new
// screens: every frame is exactly the size it was given, at every width the
// panes might be squeezed to.
func TestIssueViewsSurviveEveryWidth(t *testing.T) {
	srv := prServer(t)
	for _, width := range []int{50, 60, 70, 80, 100, 120, 200} {
		m := openIssueDetails(t, issuesModel(t, srv, width, 24))
		assertGeometry(t, fmt.Sprintf("issue details at %d", width), m.View().Content, width, 24)
		comments := pressAndDrive(t, m, "c")
		assertGeometry(t, fmt.Sprintf("issue comments at %d", width), comments.View().Content, width, 24)
	}
}

// TestIssueStateIndicatorNamesEveryState pins the marker colours against the
// palette, so a closed issue is not drawn as an open one.
func TestIssueStateIndicatorNamesEveryState(t *testing.T) {
	theme := PlainTheme()
	open := issueStateIndicator(theme, store.Issue{State: "open"})
	closed := issueStateIndicator(theme, store.Issue{State: "closed"})
	if !strings.Contains(open, style(theme.StatusOK, normal).Render("■")) {
		t.Errorf("an open issue is not drawn in the ok colour: %q", open)
	}
	if !strings.Contains(closed, style(theme.StatusFail, normal).Render("■")) {
		t.Errorf("a closed issue is not drawn in the fail colour: %q", closed)
	}
}
