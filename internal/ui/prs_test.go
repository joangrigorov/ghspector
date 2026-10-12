package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
	"ghspector/internal/store"
)

// The pull request fixtures are built from specs rather than from static JSON,
// so a test states the pull requests it cares about and the server renders
// them. The run, job and check endpoints are shared with the workflow fixtures,
// because a pull request check is resolved against the runs of its repository.

// prSpec is one pull request in a fixture.
type prSpec struct {
	number  int
	repo    string
	title   string
	state   string
	draft   bool
	author  string
	merged  bool
	updated time.Time
}

// prFixtureNumber is the pull request the details tests open: the first one in
// the fixture list.
const prFixtureNumber = 7

// prCheckRunID is the fixture run the standard check's URL names: the first run
// in fixtureRuns(8) that has finished, so the job table is what opens.
const prCheckRunID = 898

// fixturePRs is the standard pull request list: one open pull request in the
// first repository, a draft and a merged one in the second. The merged entry is
// what makes the state cycle observable.
func fixturePRs() []prSpec {
	return []prSpec{
		{number: prFixtureNumber, repo: fixtureRepo, title: "Add the pull request view", state: "open", author: "octocat", updated: workflowNow},
		{number: 8, repo: secondRepo, title: "Draft the docs", state: "open", draft: true, author: "hubot", updated: workflowNow.Add(-time.Hour)},
		{number: 9, repo: secondRepo, title: "Ship the old thing", state: "closed", merged: true, author: "octocat", updated: workflowNow.Add(-2 * time.Hour)},
	}
}

// prServerConfig is the fixture server's behaviour.
type prServerConfig struct {
	prs        []prSpec
	runs       []runSpec
	permission string
	// scopes is the X-OAuth-Scopes header. Empty means the API does not report
	// scopes, which the gate must treat as unknown rather than insufficient.
	scopes string
	// failPulls makes every pull request list fail.
	failPulls bool
	// failMerge makes the merge endpoint refuse.
	failMerge bool
	// failPermission makes the collaborators endpoint fail, which is the state
	// the gate has to fail closed in.
	failPermission bool
	// checks is the check list reported for the pull request's head.
	checks []checkSpec
}

// checkSpec is one check run in a fixture.
type checkSpec struct {
	name       string
	status     string
	conclusion string
	app        string
	// runID is the workflow run the check's URL points at. Zero means the URL
	// names no run, which the app cannot open in the app.
	runID int64
	// htmlURL overrides the URL entirely.
	htmlURL string
}

// prServerOption adjusts the fixture server.
type prServerOption func(*prServerConfig)

// withPRPermission sets the repository permission the API reports.
func withPRPermission(permission string) prServerOption {
	return func(c *prServerConfig) { c.permission = permission }
}

// withPRScopes sets the OAuth scopes the API reports.
func withPRScopes(scopes string) prServerOption {
	return func(c *prServerConfig) { c.scopes = scopes }
}

// withPRs replaces the pull request list.
func withPRs(prs ...prSpec) prServerOption {
	return func(c *prServerConfig) { c.prs = prs }
}

// failPullLists makes every pull request list fail.
func failPullLists() prServerOption {
	return func(c *prServerConfig) { c.failPulls = true }
}

// failMergeEndpoint makes the merge endpoint refuse.
func failMergeEndpoint() prServerOption {
	return func(c *prServerConfig) { c.failMerge = true }
}

// failPermissionCheck makes the collaborators endpoint fail.
func failPermissionCheck() prServerOption {
	return func(c *prServerConfig) { c.failPermission = true }
}

// withChecks replaces the check list.
func withChecks(checks ...checkSpec) prServerOption {
	return func(c *prServerConfig) { c.checks = checks }
}

// fixtureChecks is the standard check list: one Actions check whose URL names
// a fixture run, and one check with no run at all.
//
// The Actions check is named after the second fixture job and points at the
// fixture run that has finished, so opening it lands on a job table and has to
// select a row rather than keep the one the cursor already rests on.
func fixtureChecks() []checkSpec {
	return []checkSpec{
		{name: "lint", status: "completed", conclusion: "success", app: "GitHub Actions", runID: prCheckRunID},
		{name: "codecov", status: "completed", conclusion: "success", app: "Codecov", htmlURL: "https://codecov.io/gh/octocat/api/pull/7"},
	}
}

// prServer serves the pull request endpoints for a fixed fixture, plus the run,
// job and check endpoints the details view reads.
//
// Requests are recorded, which is how a test asserts that a merge or a close
// reached the API with the payload it should.
func prServer(t *testing.T, opts ...prServerOption) *httptest.Server {
	t.Helper()
	return prServerWithLog(t, newRequestLog(), opts...)
}

// prServerWithLog serves the standard fixture and records every request into
// log, so a test can assert that a merge or a close reached the API.
func prServerWithLog(t *testing.T, log *requestLog, opts ...prServerOption) *httptest.Server {
	t.Helper()
	config := prServerConfig{
		prs:        fixturePRs(),
		runs:       fixtureRuns(8),
		permission: "write",
		checks:     fixtureChecks(),
	}
	for _, opt := range opts {
		opt(&config)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		log.add(r.Method, path, r.URL.RawQuery)
		if config.scopes != "" {
			w.Header().Set("X-OAuth-Scopes", config.scopes)
		}

		switch {
		case path == "/user":
			writeBody(w, `{"login":"octocat","id":1}`)
		case path == "/user/orgs":
			writeBody(w, `[{"login":"acme","id":2}]`)
		case path == "/users/octocat/repos":
			writeBody(w, reposBody(t, "octocat"))
		case strings.HasSuffix(path, "/pulls") && r.Method == http.MethodGet:
			if config.failPulls {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			repo := repoFromPath(path)
			writeBody(w, prsBody(t, config.prs, repo))
		case strings.HasSuffix(path, "/merge"):
			if config.failMerge {
				w.WriteHeader(http.StatusMethodNotAllowed)
				writeBody(w, `{"message":"Pull Request is not mergeable"}`)
				return
			}
			writeBody(w, `{"merged":true,"message":"Pull Request successfully merged"}`)
		case strings.HasSuffix(path, "/commits") && r.URL.Query().Get("per_page") != "":
			writeBody(w, commitsBody(t))
		case strings.HasSuffix(path, "/files"):
			writeBody(w, filesBody(t))
		case strings.HasSuffix(path, "/comments"):
			writeBody(w, commentsBody(t))
		case strings.HasSuffix(path, "/check-runs"):
			writeBody(w, checksBody(t, config.checks))
		case strings.HasSuffix(path, "/actions/runs") && r.Method == http.MethodGet:
			if r.URL.Query().Get("page") == "2" {
				writeBody(w, runsBody(t, "octocat"))
				return
			}
			writeBody(w, runsBody(t, "octocat", forRepo(config.runs, repoFromRunPath(path))...))
		case strings.HasSuffix(path, "/jobs"):
			writeBody(w, jobsBody(t))
		case strings.Contains(path, "/collaborators/") && strings.HasSuffix(path, "/permission"):
			if config.failPermission {
				w.WriteHeader(http.StatusForbidden)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			writeBody(w, `{"permission":"`+config.permission+`","user":{"login":"octocat"}}`)
		case strings.Contains(path, "/pulls/") && r.Method == http.MethodPatch:
			writeBody(w, `{"number":7,"state":"closed"}`)
		case strings.Contains(path, "/pulls/"):
			writeBody(w, prDetailBody(t, config.prs, "api"))
		default:
			w.WriteHeader(http.StatusNotFound)
			writeBody(w, `{"message":"no fixture"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// requestLog records the requests a fixture server received, so a test can
// assert on a write the UI made. The server answers on its own goroutines, so
// every access is guarded.
type requestLog struct {
	mu      sync.Mutex
	entries []string
	methods map[string]string
	queries map[string]string
}

// newRequestLog returns an empty log.
func newRequestLog() *requestLog {
	return &requestLog{methods: map[string]string{}, queries: map[string]string{}}
}

// add records one request.
func (l *requestLog) add(method, path, query string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, method+" "+path)
	l.methods[path] = method
	l.queries[path] = query
}

// requests is how many requests were recorded in total.
func (l *requestLog) requests() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// count is how many times a path was requested.
func (l *requestLog) count(path string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.entries {
		if strings.HasSuffix(e, " "+path) {
			n++
		}
	}
	return n
}

// method is the method of the last request for a path, which is how a test
// tells a merge (PUT) from a close (PATCH).
func (l *requestLog) method(path string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.methods[path]
}

// query is the query string of the last request for a path, which is how a test
// sees what a filter asked the server for.
func (l *requestLog) query(path string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.queries[path]
}

// saw reports whether a request was made with a method on a path. It is the
// assertion that survives a follow-up read of the same path.
func (l *requestLog) saw(method, path string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e == method+" "+path {
			return true
		}
	}
	return false
}

// prsBody renders the pull request list of one repository.
func prsBody(t *testing.T, prs []prSpec, repo string) string {
	t.Helper()
	items := make([]map[string]any, 0, len(prs))
	for _, spec := range prs {
		if spec.repo != repo {
			continue
		}
		items = append(items, prJSON(t, spec))
	}
	return jsonBody(t, items)
}

// prDetailBody renders one pull request, with a body to describe.
func prDetailBody(t *testing.T, prs []prSpec, repo string) string {
	t.Helper()
	for _, spec := range prs {
		if spec.repo != repo {
			continue
		}
		detail := prJSON(t, spec)
		detail["body"] = "This pull request adds the pull request view.\n\nIt has two paragraphs."
		detail["head"] = map[string]any{"ref": "feat/prs", "sha": "abcdef1234567890", "label": "octocat:feat/prs"}
		detail["base"] = map[string]any{"ref": "main", "sha": "0123456789abcdef", "label": "octocat:main"}
		detail["assignees"] = []map[string]any{{"login": "hubot"}}
		detail["requested_reviewers"] = []map[string]any{{"login": "alice"}}
		detail["labels"] = []map[string]any{{"name": "enhancement", "color": "a2eeef"}}
		detail["milestone"] = map[string]any{"title": "0.4.0"}
		return jsonBody(t, detail)
	}
	t.Fatalf("no fixture pull request in %s", repo)
	return ""
}

// prJSON renders one pull request.
func prJSON(t *testing.T, spec prSpec) map[string]any {
	t.Helper()
	number := spec.number
	if number == 0 {
		number = 1
	}
	author := spec.author
	if author == "" {
		author = "octocat"
	}
	out := map[string]any{
		"id":         int64(1000 + number),
		"number":     number,
		"title":      spec.title,
		"state":      spec.state,
		"draft":      spec.draft,
		"created_at": spec.updated.Add(-24 * time.Hour).Format(time.RFC3339),
		"updated_at": spec.updated.Format(time.RFC3339),
		"html_url":   fmt.Sprintf("https://github.com/octocat/%s/pull/%d", spec.repo, number),
		"user":       map[string]any{"login": author},
		"repository": map[string]any{
			"name":      spec.repo,
			"full_name": "octocat/" + spec.repo,
			"owner":     map[string]any{"login": "octocat"},
		},
	}
	if spec.merged {
		out["merged_at"] = spec.updated.Add(-time.Minute).Format(time.RFC3339)
	}
	return out
}

// commitsBody is the pull request's commit list.
func commitsBody(t *testing.T) string {
	t.Helper()
	return jsonBody(t, []map[string]any{
		{
			"sha":      "abcdef1234567890abcdef1234567890abcdef12",
			"html_url": "https://github.com/octocat/api/commit/abcdef1234567890abcdef1234567890abcdef12",
			"commit": map[string]any{
				"message": "Add the pull request view\n\nWith a body.",
				"author": map[string]any{
					"name":  "Octo Cat",
					"email": "octo@example.com",
					"date":  workflowNow.Format(time.RFC3339),
				},
			},
			"author": map[string]any{"login": "octocat"},
		},
		{
			"sha":      "0123456789abcdef0123456789abcdef01234567",
			"html_url": "https://github.com/octocat/api/commit/0123456789abcdef0123456789abcdef01234567",
			"commit": map[string]any{
				"message": "Fix the tests",
				"author": map[string]any{
					"name":  "Hubot",
					"email": "hubot@example.com",
					"date":  workflowNow.Add(-time.Hour).Format(time.RFC3339),
				},
			},
		},
	})
}

// filesBody is the pull request's changed files, with a patch to render.
func filesBody(t *testing.T) string {
	t.Helper()
	return jsonBody(t, []map[string]any{
		{
			"filename":  "internal/ui/prs.go",
			"status":    "added",
			"additions": 12,
			"deletions": 0,
			"changes":   12,
			"patch":     "@@ -0,0 +1,3 @@\n+package ui\n+\n+// A new file.\n",
		},
		{
			"filename":  "internal/ui/table.go",
			"status":    "modified",
			"additions": 2,
			"deletions": 1,
			"changes":   3,
			"patch":     "@@ -1,3 +1,4 @@\n package ui\n-\n+// A comment.\n",
		},
		{"filename": "assets/logo.png", "status": "modified", "additions": 0, "deletions": 0, "changes": 0},
	})
}

// commentsBody is the pull request's comment thread.
func commentsBody(t *testing.T) string {
	t.Helper()
	return jsonBody(t, []map[string]any{
		{
			"id":         1,
			"body":       "Looks good to me.",
			"created_at": workflowNow.Format(time.RFC3339),
			"html_url":   "https://github.com/octocat/api/pull/7#issuecomment-1",
			"user":       map[string]any{"login": "alice"},
		},
		{
			"id":         2,
			"body":       "Thanks! Merging once the checks pass.",
			"created_at": workflowNow.Add(time.Minute).Format(time.RFC3339),
			"user":       map[string]any{"login": "octocat"},
		},
	})
}

// checksBody renders the check-run list.
func checksBody(t *testing.T, checks []checkSpec) string {
	t.Helper()
	items := make([]map[string]any, 0, len(checks))
	for i, spec := range checks {
		app := spec.app
		if app == "" {
			app = "GitHub Actions"
		}
		url := spec.htmlURL
		if url == "" && spec.runID > 0 {
			url = fmt.Sprintf("https://github.com/octocat/api/actions/runs/%d/job/%d", spec.runID, 7001)
		}
		items = append(items, map[string]any{
			"id":           int64(500 + i),
			"name":         spec.name,
			"status":       spec.status,
			"conclusion":   spec.conclusion,
			"html_url":     url,
			"started_at":   workflowNow.Format(time.RFC3339),
			"completed_at": workflowNow.Add(time.Minute).Format(time.RFC3339),
			"app":          map[string]any{"slug": "github-actions", "name": app},
		})
	}
	return jsonBody(t, map[string]any{"total_count": len(items), "check_runs": items})
}

// repoFromPath reads the repository name out of a /repos/<owner>/<repo>/... path.
func repoFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// prsModel is the state every pull request test starts from: the model is
// ready, the Pull Requests tab is active, and its first page has landed.
func prsModel(t *testing.T, srv *httptest.Server, w, h int, tweak ...func(*Options)) Model {
	t.Helper()
	ready, cmd := startup(t, newModel(t, srv, w, h, tweak...))
	return drive(t, ready, cmd)
}

// prsTabOf reads the main route's pull request state.
func prsTabOf(t *testing.T, m Model) prsTab {
	t.Helper()
	main, ok := m.routes.Top().(Main)
	if !ok {
		t.Fatalf("top route is %T, want ui.Main", m.routes.Top())
	}
	return main.prs
}

// prDetailsRoute returns the top route as the pull request details route.
func prDetailsRoute(t *testing.T, m Model) *PRDetails {
	t.Helper()
	details, ok := m.routes.Top().(*PRDetails)
	if !ok {
		t.Fatalf("top route is %T, want *ui.PRDetails", m.routes.Top())
	}
	return details
}

// openPRDetails opens the first pull request of the list and runs the fetches
// the route starts with.
func openPRDetails(t *testing.T, m Model) Model {
	t.Helper()
	m = pressAndDrive(t, m, "enter")
	if _, ok := m.routes.Top().(*PRDetails); !ok {
		t.Fatalf("enter did not open the details; top = %T", m.routes.Top())
	}
	return m
}

func TestPRListLoadsAndSortsAcrossRepositories(t *testing.T) {
	srv := prServer(t)
	m := prsModel(t, srv, 120, 40)

	tab := prsTabOf(t, m)
	if len(tab.prs) != len(fixturePRs()) {
		t.Fatalf("loaded %d pull requests, want %d", len(tab.prs), len(fixturePRs()))
	}
	// Most recently updated first, across both repositories.
	want := []int{prFixtureNumber, 8, 9}
	for i, pr := range tab.prs {
		if pr.Number != want[i] {
			t.Fatalf("row %d is #%d, want #%d", i, pr.Number, want[i])
		}
		if pr.Repository.Name == "" {
			t.Errorf("row %d carries no repository", i)
		}
	}

	out := screen(t, m, 120, 40)
	for _, want := range []string{"TITLE", "#7", "Add the pull request view", "[Draft] Draft the docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list is missing %q:\n%s", want, out)
		}
	}
}

func TestPRStateCycleRefetches(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	// The state filter defaults to open, so the cycle ends on a query the cache
	// already holds. A store that reuses nothing keeps every step on the wire.
	m := prsModel(t, srv, 120, 40, func(o *Options) {
		o.Store = testStore(srv, store.WithCacheTTL(0))
	})

	list := "/repos/octocat/" + fixtureRepo + "/pulls"
	if got := log.query(list); !strings.Contains(got, "state=open") {
		t.Errorf("the first fetch asked for %q, want the open state", got)
	}

	for _, want := range []string{"closed", "all", "open"} {
		m = pressAndDrive(t, m, "s")
		if got := prsTabOf(t, m).state; got != want {
			t.Fatalf("state after s = %q, want %q", got, want)
		}
		if got := log.query(list); !strings.Contains(got, "state="+want) {
			t.Errorf("after the %s step the fetch asked for %q; %d requests seen", want, got, log.requests())
		}
	}
}

func TestPRQuickFiltersToggleIndependently(t *testing.T) {
	srv := prServer(t)
	m := prsModel(t, srv, 120, 40)

	m = pressAndDrive(t, m, "a")
	if got := prsTabOf(t, m).author; got != "octocat" {
		t.Fatalf("author filter = %q, want octocat", got)
	}
	// A different role replaces the first, which is what the previous version
	// did: one person, one role at a time.
	m = pressAndDrive(t, m, "i")
	tab := prsTabOf(t, m)
	if tab.assignee != "octocat" || tab.author != "" {
		t.Fatalf("after i: assignee = %q, author = %q, want assignee only", tab.assignee, tab.author)
	}
	// Pressing the same key again clears it.
	m = pressAndDrive(t, m, "i")
	if got := prsTabOf(t, m).assignee; got != "" {
		t.Fatalf("assignee filter = %q, want it cleared", got)
	}
}

func TestPRFiltersNarrowTheListClientSide(t *testing.T) {
	srv := prServer(t)
	m := prsModel(t, srv, 120, 40)

	m = pressAndDrive(t, m, "a")
	tab := prsTabOf(t, m)
	if len(tab.prs) != 2 {
		t.Fatalf("author filter kept %d rows, want the two by octocat", len(tab.prs))
	}
	for _, pr := range tab.prs {
		if prAuthor(pr) != "octocat" {
			t.Errorf("row #%d is by %s, want octocat", pr.Number, prAuthor(pr))
		}
	}
}

func TestPRLoadMoreFetchesTheNextPage(t *testing.T) {
	full := make([]prSpec, 0, 8)
	for i := 0; i < 8; i++ {
		full = append(full, prSpec{
			number:  100 + i,
			repo:    fixtureRepo,
			title:   fmt.Sprintf("Pull request %d", i),
			state:   "open",
			author:  "octocat",
			updated: workflowNow.Add(-time.Duration(i) * time.Minute),
		})
	}
	srv := prServer(t, withPRs(full...))
	m := prsModel(t, srv, 120, 40)

	tab := prsTabOf(t, m)
	if !tab.hasMore {
		t.Fatal("HasMore = false for a full page")
	}

	// The Load More row is the last selectable row.
	m = pressAndDrive(t, m, "down", "down", "down", "down", "down", "down", "down", "down")
	if !prsTabOf(t, m).onLoadMore() {
		t.Fatalf("cursor = %d, want the Load More row of %d rows", prsTabOf(t, m).cursor, len(prsTabOf(t, m).prs))
	}
	m = pressAndDrive(t, m, "enter")
	if got := prsTabOf(t, m).page; got != 2 {
		t.Fatalf("page = %d, want 2", got)
	}
}

// TestPRFilterOverlayAppliesATypedLogin walks the free-text path of §4.6-4.7:
// the filter menu, the login prompt, then the author/assignee/reviewer picker.
func TestPRFilterOverlayAppliesATypedLogin(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := prsModel(t, srv, 120, 40)

	// f opens the same two choices as the Workflows tab, with the user path
	// continuing to the picker.
	m = pressAndDrive(t, m, "f")
	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "Filter by user") || !strings.Contains(out, "Filter by repository") {
		t.Fatalf("the filter menu does not offer the two paths:\n%s", out)
	}

	m = pressAndDrive(t, m, "u")
	out = screen(t, m, 120, 40)
	if !strings.Contains(out, "Filter pull requests by user") {
		t.Fatalf("u did not open the login prompt:\n%s", out)
	}

	// The prompt starts empty on this tab, so a login is typed into it.
	m = typeKeys(t, m, "a", "l", "i", "c", "e")
	m = pressAndDrive(t, m, "enter")
	out = screen(t, m, 120, 40)
	if !strings.Contains(out, "where alice is") {
		t.Fatalf("submitting the login did not advance to the role picker:\n%s", out)
	}

	// The role the key names is the one the typed login is filtered by.
	m = pressAndDrive(t, m, "r")
	tab := prsTabOf(t, m)
	if tab.reviewer != "alice" || tab.author != "" || tab.assignee != "" {
		t.Fatalf("filters = reviewer %q, author %q, assignee %q; want reviewer only", tab.reviewer, tab.author, tab.assignee)
	}
	if !m.overlays.Empty() {
		t.Errorf("overlays remain open after applying the filter: %d", m.overlays.Len())
	}
}

// TestPRFilterOverlayRepoPickNarrowsTheFanOut follows the repository path of the
// filter menu: it sets the repo filter and refetches.
func TestPRFilterOverlayRepoPickNarrowsTheFanOut(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := prsModel(t, srv, 120, 40)

	m = pressAndDrive(t, m, "f", "r")
	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "Filter by repository") || !strings.Contains(out, fixtureRepo) {
		t.Fatalf("the repository picker did not open with the fixture repositories:\n%s", out)
	}

	// A narrowed fetch asks one repository, and only the one that was picked.
	webPulls := "/repos/octocat/" + secondRepo + "/pulls"
	beforeWeb := log.count(webPulls)
	m = pressAndDrive(t, m, "enter")
	if got := prsTabOf(t, m).repo; got != fixtureRepo {
		t.Fatalf("repo filter = %q, want %q", got, fixtureRepo)
	}
	if got := log.count(webPulls); got != beforeWeb {
		t.Errorf("the narrowed fetch still asked %s for pull requests (%d requests, was %d)", secondRepo, got, beforeWeb)
	}
	if got := log.query("/repos/octocat/" + fixtureRepo + "/pulls"); !strings.Contains(got, "state=open") {
		t.Errorf("the narrowed fetch asked %q", got)
	}
}

func TestPRListReportsAFailure(t *testing.T) {
	srv := prServer(t, failPullLists())
	m := prsModel(t, srv, 120, 40)

	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "Could not load pull requests") {
		t.Errorf("a failing list did not explain itself:\n%s", out)
	}
}

func TestPRDetailsSubTabsAndScopes(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	want := []struct {
		key   string
		tab   prTab
		scope keymap.Scope
	}{
		{"2", prTabCommits, keymap.PRCommits},
		{"3", prTabFiles, keymap.PRDiff},
		{"4", prTabChecks, keymap.PRDetails},
		{"5", prTabComments, keymap.PRComments},
		{"1", prTabInfo, keymap.PRDetails},
	}
	for _, tc := range want {
		m = pressAndDrive(t, m, tc.key)
		details := prDetailsRoute(t, m)
		if details.tab != tc.tab {
			t.Fatalf("key %s selected tab %v, want %v", tc.key, details.tab, tc.tab)
		}
		if got := details.Scope(); got != tc.scope {
			t.Fatalf("tab %v has scope %q, want %q", tc.tab, got, tc.scope)
		}
		screen(t, m, 120, 40)
	}

	// The letters are aliases for the tabs that have one (spec §4.10).
	m = pressAndDrive(t, m, "v")
	if got := prDetailsRoute(t, m).tab; got != prTabCommits {
		t.Fatalf("v selected tab %v, want Commits", got)
	}
	m = pressAndDrive(t, m, "esc")
	if got := prDetailsRoute(t, m).tab; got != prTabInfo {
		t.Fatalf("esc from a sub-tab landed on %v, want Info", got)
	}
	m = pressAndDrive(t, m, "esc")
	if _, ok := m.routes.Top().(Main); !ok {
		t.Fatalf("esc on Info left the route stack at %T, want the list", m.routes.Top())
	}
}

func TestPRDetailsRendersEverySubTab(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	markers := map[string]string{
		"1": "DESCRIPTION",
		"2": "internal/ui/prs.go",
		"3": "FILES & DIFF",
		"4": "CHECKS",
		"5": "Looks good to me.",
	}
	// The commits tab lists commits, so its marker comes from the commit
	// fixture rather than from a heading.
	markers["2"] = "Add the pull request view"
	for key, marker := range markers {
		m = pressAndDrive(t, m, key)
		out := screen(t, m, 120, 40)
		if !strings.Contains(out, marker) {
			t.Errorf("sub-tab %s is missing %q:\n%s", key, marker, out)
		}
	}
}

// TestDiffStyledColoursPatchLinesByPrefix pins spec §10's diff formatting: a
// patch line is styled by its first character, so an addition does not read
// like a deletion. The plain theme is what the goldens render, so a patch there
// must come through byte for byte.
func TestDiffStyledColoursPatchLinesByPrefix(t *testing.T) {
	lines := []string{
		"@@ -1,2 +1,3 @@",
		"+added",
		"-removed",
		" context",
		"+++ b/file.go",
		"--- a/file.go",
	}
	theme := DarkTheme()
	got := diffStyled(theme, lines)
	if len(got) != len(lines) {
		t.Fatalf("diffStyled returned %d lines, want %d", len(got), len(lines))
	}

	want := []string{
		style(theme.Accent, normal).Render(lines[0]),
		style(theme.StatusOK, normal).Render(lines[1]),
		style(theme.StatusFail, normal).Render(lines[2]),
		lines[3],
		style(theme.Muted, bold).Render(lines[4]),
		style(theme.Muted, bold).Render(lines[5]),
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	if got[1] == got[2] {
		t.Error("an addition and a deletion rendered identically")
	}

	plain := diffStyled(PlainTheme(), lines)
	for i := range lines {
		// The plain theme renders no colour; bold is the one modifier it may
		// emit, so the line has to strip back to the patch text.
		if stripped := ansi.Strip(plain[i]); stripped != lines[i] {
			t.Errorf("plain line %d = %q, want %q", i, stripped, lines[i])
		}
	}
}

func TestPRDetailsInfoShowsMetadataAndChecks(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	out := screen(t, m, 120, 40)
	for _, want := range []string{
		"[OPEN]", "PR #7", "Repo: octocat/api", "Source: feat/prs", "Base: main",
		"DETAILS", "@hubot", "alice", "enhancement", "0.4.0",
		"CHECKS", "lint", "codecov",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the details view is missing %q:\n%s", want, out)
		}
	}
}

func TestPRDetailsReportsAnEmptyCheckList(t *testing.T) {
	srv := prServer(t, withChecks())
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "No checks") {
		t.Errorf("a pull request with no checks has no empty state:\n%s", out)
	}
}

// TestCheckOnEnterOpensTheRun is the check-to-run navigation: the check's URL
// names the run, and the app opens that run's jobs and preselects the job the
// check is for.
func TestCheckOnEnterOpensTheRun(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	// Tab moves the focus to the checks sidebar, where Enter acts on a check.
	m = pressAndDrive(t, m, "tab", "enter")
	jobs, ok := m.routes.Top().(*Jobs)
	if !ok {
		t.Fatalf("enter on a check left the stack at %T, want *ui.Jobs", m.routes.Top())
	}
	if jobs.run.ID != prCheckRunID {
		t.Fatalf("opened run %d, want the run the check names (%d)", jobs.run.ID, prCheckRunID)
	}
	if jobs.target != "lint" {
		t.Errorf("target job = %q, want the check's name", jobs.target)
	}
	// The check is named after the second job, so a selected "lint" proves the
	// target moved the cursor off the first row.
	if got, ok := jobs.selected(); !ok || got.Name != "lint" {
		t.Errorf("selected job = %+v, want the job the check names", got)
	}
	out := screen(t, m, 120, 40)
	if !strings.Contains(out, fmt.Sprintf("Workflow: Build %d", prCheckRunID)) {
		t.Errorf("the jobs heading does not name the run:\n%s", out)
	}
}

// TestCheckWithoutARunStaysInTheApp documents the path where there is nothing
// to open: the route stays put and reports through the browser command instead.
//
// The command itself is not run: it launches the platform opener, which a test
// must not do. What is asserted is the decision — no route was pushed, and a
// command was returned.
func TestCheckWithoutARunStaysInTheApp(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	m = pressAndDrive(t, m, "tab", "j")

	details := prDetailsRoute(t, m)
	if got := details.checkCursor; got != 1 {
		t.Fatalf("check cursor = %d, want the second check", got)
	}
	next, cmd := pressKey(t, m, "enter")
	m = next
	if _, ok := m.routes.Top().(*PRDetails); !ok {
		t.Fatalf("enter on a check with no run pushed %T", m.routes.Top())
	}
	if cmd == nil {
		t.Fatal("enter on a check with a URL returned no command")
	}
}

// TestPRWriteGateFailsClosed is the fix for finding F1: merge and close are
// offered only when repository permission is established, and a permission that
// cannot be read is a refusal rather than a pass.
func TestPRWriteGateFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		opts    []prServerOption
		allowed bool
	}{
		{name: "write", opts: []prServerOption{withPRPermission("write")}, allowed: true},
		{name: "admin", opts: []prServerOption{withPRPermission("admin")}, allowed: true},
		{name: "maintain", opts: []prServerOption{withPRPermission("maintain")}, allowed: true},
		{name: "read", opts: []prServerOption{withPRPermission("read")}, allowed: false},
		{name: "none", opts: []prServerOption{withPRPermission("none")}, allowed: false},
		{
			// The token reports no scopes, which is not a denial, and the
			// permission endpoint still answers.
			name: "no scope header", opts: []prServerOption{withPRPermission("write")}, allowed: true,
		},
		{
			// The permission endpoint cannot be read: the action is withheld.
			name: "unreadable permission", opts: []prServerOption{failPermissionCheck()}, allowed: false,
		},
		{
			// A token that cannot write is refused before the permission read,
			// which is what the scope check is for.
			name:    "missing scopes",
			opts:    []prServerOption{withPRScopes("gist"), withPRPermission("write")},
			allowed: false,
		},
		{
			name:    "reported scopes are enough",
			opts:    []prServerOption{withPRScopes("repo, workflow"), withPRPermission("write")},
			allowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := prServer(t, tc.opts...)
			m := openPRDetails(t, prsModel(t, srv, 120, 40))

			if got := footerHasAction(m, keymap.ActionMerge); got != tc.allowed {
				t.Errorf("merge offered = %v, want %v", got, tc.allowed)
			}
			if got := footerHasAction(m, keymap.ActionClose); got != tc.allowed {
				t.Errorf("close offered = %v, want %v", got, tc.allowed)
			}

			next, cmd := pressKey(t, m, "m")
			if tc.allowed {
				// The overlay is opened by a command, so it is driven before
				// the stack is inspected.
				m = drive(t, next, cmd)
				if m.overlays.Len() != 1 {
					t.Fatalf("merge overlay did not open: %d overlays", m.overlays.Len())
				}
				if _, ok := m.overlays.Top().(interface{ Title() string }); !ok {
					t.Fatalf("the overlay is %T", m.overlays.Top())
				}
				return
			}
			m = next
			if m.overlays.Len() != 0 {
				t.Fatalf("a withheld merge opened %d overlays", m.overlays.Len())
			}
			msgs := messagesOf(t, cmd)
			if len(msgs) == 0 {
				t.Fatal("a withheld merge explained nothing")
			}
			announced, ok := msgs[0].(StatusMsg)
			if !ok {
				t.Fatalf("a withheld merge returned %T, want a status message", msgs[0])
			}
			if !announced.IsErr || !strings.Contains(announced.Text, "write scopes (repo) to merge this PR") {
				t.Errorf("message = %q, want the spec's refusal", announced.Text)
			}
		})
	}
}

func TestMergeFlowChoosesAMethodAndConfirms(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	// m opens the method chooser.
	m = pressAndDrive(t, m, "m")
	if got := m.overlays.Len(); got != 1 {
		t.Fatalf("overlays = %d, want the method chooser", got)
	}
	out := screen(t, m, 120, 40)
	for _, want := range []string{"Squash and merge", "Create a merge commit", "Rebase and merge", "Current default: squash"} {
		if !strings.Contains(out, want) {
			t.Errorf("the method chooser is missing %q:\n%s", want, out)
		}
	}

	// s chooses squash and replaces the chooser with the confirmation.
	m = pressAndDrive(t, m, "s")
	if got := m.overlays.Len(); got != 1 {
		t.Fatalf("overlays = %d, want the confirmation", got)
	}
	out = screen(t, m, 120, 40)
	if !strings.Contains(out, "Confirm merge") || !strings.Contains(out, "a squash commit") {
		t.Errorf("the confirmation does not name the method:\n%s", out)
	}

	// y merges.
	m = pressAndDrive(t, m, "y")
	if m.overlays.Len() != 0 {
		t.Fatalf("the confirmation stayed open: %d overlays", m.overlays.Len())
	}
	if got := log.method("/repos/octocat/api/pulls/7/merge"); got != http.MethodPut {
		t.Fatalf("merge request method = %q, want PUT", got)
	}
	if !log.saw(http.MethodPut, "/repos/octocat/api/pulls/7/merge") {
		t.Error("no PUT reached the merge endpoint")
	}
	if !strings.Contains(m.View().Content, "Pull request merged.") {
		t.Errorf("the merge outcome is not reported:\n%s", m.View().Content)
	}
}

func TestMergeFailureIsReported(t *testing.T) {
	srv := prServer(t, failMergeEndpoint())
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	m = pressAndDrive(t, m, "m", "s", "y")
	if !strings.Contains(m.View().Content, "Could not merge the pull request") {
		t.Errorf("a failed merge was not reported:\n%s", m.View().Content)
	}
}

func TestMergeMethodDefaultCyclesAndPersists(t *testing.T) {
	var saved []string
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40, func(o *Options) {
		o.MergeMethod = "squash"
		o.SaveMergeMethod = func(method string) error {
			saved = append(saved, method)
			return nil
		}
	}))

	m = pressAndDrive(t, m, "m", "d")
	if len(saved) != 1 || saved[0] != "merge" {
		t.Fatalf("saved = %v, want one write of merge", saved)
	}
	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "Current default: merge") {
		t.Errorf("the chooser did not adopt the new default:\n%s", out)
	}
	if !strings.Contains(out, "Default merge method saved: merge.") {
		t.Errorf("the write was not reported:\n%s", out)
	}
	// The route remembers it, so reopening the chooser shows the new default.
	m = pressAndDrive(t, m, "esc", "m")
	if got := prDetailsRoute(t, m).mergeMethod; got != "merge" {
		t.Fatalf("route default = %q, want merge", got)
	}

	// Enter accepts the default.
	m = pressAndDrive(t, m, "enter")
	if got := m.overlays.Len(); got != 1 {
		t.Fatalf("overlays = %d, want the confirmation", got)
	}
	if out := screen(t, m, 120, 40); !strings.Contains(out, "a merge commit") {
		t.Errorf("Enter did not use the configured default:\n%s", out)
	}
}

func TestCloseFlowConfirmsAndCloses(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	m = pressAndDrive(t, m, "C")
	out := screen(t, m, 120, 40)
	if !strings.Contains(out, "Confirm close") {
		t.Fatalf("C did not open the close confirmation:\n%s", out)
	}
	m = pressAndDrive(t, m, "y")
	if !log.saw(http.MethodPatch, "/repos/octocat/api/pulls/7") {
		t.Fatalf("no PATCH reached the pull request; %d requests seen", log.requests())
	}
	if !strings.Contains(m.View().Content, "Pull request closed.") {
		t.Errorf("the close outcome is not reported:\n%s", m.View().Content)
	}
}

func TestCloseConfirmCanBeCancelled(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	m = pressAndDrive(t, m, "C", "n")
	if m.overlays.Len() != 0 {
		t.Fatalf("n did not close the confirmation: %d overlays", m.overlays.Len())
	}
	if log.saw(http.MethodPatch, "/repos/octocat/api/pulls/7") {
		t.Error("cancelling still wrote to the API")
	}
}

func TestPRDetailsRefreshRefetchesTheActiveTab(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	// The cache is the reason an explicit refresh can be invisible on the wire:
	// the default two-second TTL would serve the answer the screen already has.
	// A store that reuses nothing makes the refresh observable.
	m := openPRDetails(t, prsModel(t, srv, 120, 40, func(o *Options) {
		o.Store = testStore(srv, store.WithCacheTTL(0))
	}))

	before := log.count("/repos/octocat/api/pulls/7")
	pressAndDrive(t, m, "r")
	if got := log.count("/repos/octocat/api/pulls/7"); got <= before {
		t.Fatalf("refresh did not refetch the pull request (%d requests)", got)
	}
}

// TestInfoColumnsTileTheBody is the layout property the three-pane Info view
// rests on: whatever the terminal width, the panes and the separators drawn
// between them add up to exactly the body.
func TestInfoColumnsTileTheBody(t *testing.T) {
	for width := 1; width <= 400; width++ {
		desc, meta, checks := infoColumns(width)
		draw := 0
		for _, pane := range []int{desc, meta, checks} {
			if pane > 0 {
				draw++
			}
		}
		separators := 0
		if draw > 1 {
			separators = draw - 1
		}
		if sum := desc + meta + checks + separators*infoSepWidth; sum != width {
			t.Fatalf("width %d: panes and separators tile %d, want %d", width, sum, width)
		}
		if desc < 0 || meta < 0 || checks < 0 {
			t.Fatalf("width %d: negative pane (%d, %d, %d)", width, desc, meta, checks)
		}
		if width >= descMinWidth+metaMinWidth+checksMinWidth+2*infoSepWidth {
			if desc < descMinWidth || meta < metaMinWidth || checks < checksMinWidth {
				t.Fatalf("width %d: a pane is below its minimum (%d, %d, %d)", width, desc, meta, checks)
			}
		}
	}
}

// TestInfoColumnsGivesUpTheMetadataPaneFirst pins the priority: the description
// and the checks are what the screen is for.
func TestInfoColumnsGivesUpTheMetadataPaneFirst(t *testing.T) {
	const width = descMinWidth + checksMinWidth + 2*infoSepWidth + metaMinWidth - 1
	desc, meta, checks := infoColumns(width)
	if meta != 0 {
		t.Fatalf("metadata pane is %d wide at %d columns, want it dropped", meta, width)
	}
	if desc < descMinWidth || checks < checksMinWidth {
		t.Fatalf("panes = (%d, %d), want the description and the checks to survive", desc, checks)
	}
}

// TestInfoViewTilesEveryWidth renders the Info sub-tab across a wide range of
// body widths and checks what comes out: every line is exactly the body width,
// and a pane that is not drawn leaves no separator behind.
func TestInfoViewTilesEveryWidth(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	details := prDetailsRoute(t, m)

	for width := 12; width <= 200; width++ {
		view := details.infoView(m.deps, layout.Rect{W: width, H: 10})
		lines := splitLines(view)
		if len(lines) != 10 {
			t.Fatalf("width %d: %d lines, want 10", width, len(lines))
		}
		for i, line := range lines {
			if got := layout.Width(line); got != width {
				t.Fatalf("width %d: line %d is %d columns: %q", width, i, got, line)
			}
			if strings.Contains(line, "││") {
				t.Fatalf("width %d: line %d draws two separators: %q", width, i, line)
			}
		}
	}
}

func TestRunIDFromURL(t *testing.T) {
	cases := map[string]int64{
		"https://github.com/o/r/actions/runs/12345":                12345,
		"https://github.com/o/r/actions/runs/12345/job/67890":      12345,
		"https://github.com/o/r/actions/runs/12345/job/67890?pr=7": 12345,
		"https://github.com/o/r/actions/runs/":                     0,
		"https://github.com/o/r/actions/runs/not-a-number":         0,
		"https://codecov.io/gh/o/r/pull/7":                         0,
		"":                                                         0,
		"https://github.com/o/r/actions/runs/9/attempts/2":         9,
		"https://github.com/o/r/actions/runs/007":                  7,
	}
	for url, want := range cases {
		if got := runIDFromURL(url); got != want {
			t.Errorf("runIDFromURL(%q) = %d, want %d", url, got, want)
		}
	}
}

func TestMatchRunPrefersTheRunID(t *testing.T) {
	details := &PRDetails{runs: []store.WorkflowRun{
		{ID: 1, Name: "CI"},
		{ID: 2, Name: "CI"},
	}}

	run, ok := details.matchRun(2, "CI")
	if !ok || run.ID != 2 {
		t.Fatalf("matchRun by ID = %+v, %v, want run 2", run, ok)
	}
	// With no run ID the name match is case-insensitive.
	run, ok = details.matchRun(0, "ci")
	if !ok || run.ID != 1 {
		t.Fatalf("matchRun by name = %+v, %v, want the first CI", run, ok)
	}
	if _, ok := details.matchRun(0, "deploy"); ok {
		t.Error("matchRun matched a run that is not in the list")
	}
}

func TestWrapLinesAndClamps(t *testing.T) {
	lines := wrapLines("one two three\n\nfour", 5)
	want := []string{"one", "two", "three", "", "four"}
	if fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Errorf("wrapLines = %v, want %v", lines, want)
	}
	if got := wrapLines("", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("wrapLines(\"\") = %v, want one empty line", got)
	}

	// Trailing blank lines are dropped rather than counted as content.
	if got := wrapLines("text\n\n\n", 10); len(got) != 1 {
		t.Errorf("wrapLines kept trailing blanks: %v", got)
	}

	for _, tc := range []struct{ offset, n, want int }{
		{0, 0, 0}, {-3, 10, 0}, {4, 10, 4}, {20, 10, 9},
	} {
		if got := clampOffset(tc.offset, tc.n); got != tc.want {
			t.Errorf("clampOffset(%d, %d) = %d, want %d", tc.offset, tc.n, got, tc.want)
		}
	}
	for _, tc := range []struct{ index, n, want int }{
		{0, 0, 0}, {-1, 5, 0}, {3, 5, 3}, {9, 5, 4},
	} {
		if got := clampIndex(tc.index, tc.n); got != tc.want {
			t.Errorf("clampIndex(%d, %d) = %d, want %d", tc.index, tc.n, got, tc.want)
		}
	}
}

func TestIsActionsCheck(t *testing.T) {
	cases := []struct {
		name string
		slug string
		app  string
		none bool
		want bool
	}{
		{name: "actions slug", slug: "github-actions", want: true},
		{name: "actions name", slug: "GitHub Actions", want: true},
		{name: "another app", slug: "codecov", app: "Codecov"},
		{name: "no app at all", none: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := store.CheckRun{Name: "build"}
			if !tc.none {
				check.App = &store.CheckApp{Slug: tc.slug, Name: tc.app}
			}
			if got := isActionsCheck(check); got != tc.want {
				t.Errorf("isActionsCheck(%q) = %v, want %v", tc.slug, got, tc.want)
			}
		})
	}
}
