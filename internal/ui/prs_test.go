package ui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/store"
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
// in fixtureRuns(9) that has finished, so the job table is what opens.
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
	issues     []issueSpec
	runs       []runSpec
	permission string
	// scopes is the X-OAuth-Scopes header. Empty means the API does not report
	// scopes, which the gate must treat as unknown rather than insufficient.
	scopes string
	// failPulls makes every pull request list fail.
	failPulls bool
	// failIssues makes every issue list fail.
	failIssues bool
	// failComments makes the conversation-comment list fail, which is the whole
	// conversation on an issue.
	failComments bool
	// failReviews and failInline make the review and inline-comment endpoints
	// fail, which is the state a sub-tab reading three lists has to report
	// rather than draw the lists that did arrive as the whole discussion.
	failReviews bool
	failInline  bool
	// comments is how many comments the conversation holds. Zero means the
	// standard two-comment fixture.
	comments int
	// noComments empties the comment list, which is the shape a review-heavy
	// pull request has.
	noComments bool
	// reviews and inline replace the review and inline-comment fixtures. Nil
	// asks for the standard review and the thread under it.
	reviews []reviewSpec
	inline  []inlineSpec
	// failMerge makes the merge endpoint refuse.
	failMerge bool
	// failPermission makes the collaborators endpoint fail, which is the state
	// the gate has to fail closed in.
	failPermission bool
	// checks is the check list reported for the pull request's head.
	checks []checkSpec
	// resolvedThreads is the answer the GraphQL endpoint serves to the
	// review-thread resolution query. Nil leaves the endpoint refusing, which is
	// the state a screen with no resolution data is in; a non-nil empty list is
	// an answer with nothing resolved.
	resolvedThreads [][]int64
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

// reviewSpec is one review in the conversation fixture. A PENDING review is one
// its author has not submitted, which the fixture renders without a submission
// time.
type reviewSpec struct {
	id     int64
	author string
	state  string
	body   string
	at     time.Time
}

// inlineSpec is one inline review comment in the conversation fixture. replyTo
// names the comment it answers, which is how a thread is built.
type inlineSpec struct {
	id      int64
	review  int64
	author  string
	body    string
	path    string
	line    int
	hunk    string
	at      time.Time
	replyTo int64
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

// withIssues replaces the issue list.
func withIssues(issues ...issueSpec) prServerOption {
	return func(c *prServerConfig) { c.issues = issues }
}

// failPullLists makes every pull request list fail.
func failPullLists() prServerOption {
	return func(c *prServerConfig) { c.failPulls = true }
}

// failIssueLists makes every issue list fail.
func failIssueLists() prServerOption {
	return func(c *prServerConfig) { c.failIssues = true }
}

// failCommentLists makes every conversation fail.
func failCommentLists() prServerOption {
	return func(c *prServerConfig) { c.failComments = true }
}

// withComments replaces the conversation with n comments, so a test can scroll a
// thread longer than the screen.
func withComments(n int) prServerOption {
	return func(c *prServerConfig) { c.comments = n }
}

// withoutConversationComments empties the comment list, leaving the reviews as
// the only things in the discussion.
func withoutConversationComments() prServerOption {
	return func(c *prServerConfig) { c.noComments = true }
}

// failReviewList makes the reviews endpoint fail.
func failReviewList() prServerOption {
	return func(c *prServerConfig) { c.failReviews = true }
}

// withResolvedThreads serves the GraphQL answer the fixture refuses by default:
// one resolved thread per group, holding exactly the remarks the group names,
// which is the shape the schema returns for a thread that has been settled. No
// groups at all is an answer with nothing resolved, which is not the same as an
// endpoint that will not answer.
func withResolvedThreads(threads ...[]int64) prServerOption {
	return func(c *prServerConfig) { c.resolvedThreads = threads }
}

// queryAsks reports whether a GraphQL request holds a field of this name, which
// is how the fixture tells the queries it answers from the ones it refuses.
func queryAsks(r *http.Request, field string) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	return strings.Contains(string(body), field)
}

// threadsBody is the review-thread answer: one resolved thread per group, as the
// GraphQL transport receives it.
func threadsBody(t *testing.T, threads [][]int64) string {
	t.Helper()
	nodes := make([]any, 0, len(threads))
	for _, group := range threads {
		comments := make([]map[string]any, 0, len(group))
		for _, id := range group {
			comments = append(comments, map[string]any{"databaseId": id})
		}
		nodes = append(nodes, map[string]any{
			"isResolved": true,
			"comments":   map[string]any{"nodes": comments},
		})
	}
	return jsonBody(t, map[string]any{"data": map[string]any{
		"rateLimit": map[string]any{
			"limit":     5000,
			"cost":      1,
			"remaining": 4999,
			"resetAt":   "2026-10-12T11:00:00Z",
		},
		"repository": map[string]any{
			"pullRequest": map[string]any{
				"reviewThreads": map[string]any{
					"nodes": nodes,
				},
			},
		},
	}})
}

// failConversation makes every list the conversation is assembled from fail, so
// a test can cover the sub-tab that has nothing to draw and has to say why.
func failConversation() prServerOption {
	return func(c *prServerConfig) {
		c.failComments = true
		c.failReviews = true
		c.failInline = true
	}
}

// withReviews replaces the review fixture. A review that is not the standard one
// leaves the standard inline comments without a review to belong to, so a test
// that replaces the reviews usually replaces the thread with it.
func withReviews(reviews ...reviewSpec) prServerOption {
	return func(c *prServerConfig) { c.reviews = reviews }
}

// withInlineComments replaces the inline review comments. No arguments is a
// conversation review without inline remarks.
func withInlineComments(comments ...inlineSpec) prServerOption {
	return func(c *prServerConfig) { c.inline = comments }
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

// reviewFixtureID is the standard review's id, which the standard thread names.
const reviewFixtureID = 3001

// replyReviewFixtureID is the second standard review, the one that posted the
// reply. GitHub posts a reply as part of a new review, so the remarks of one
// thread name different reviews; the fixture carries that shape rather than the
// same-review shape the API never returns.
const replyReviewFixtureID = 3002

// fixtureHunk is the diff context the standard inline comment was written
// against: one removed line, one added line, and the line between them.
const fixtureHunk = "@@ -10,4 +10,5 @@\n func width() int {\n-    return 40\n+    return tableWidth\n }\n"

// fixtureReviews is the standard pair: an approval with a summary, submitted
// after both conversation comments, and the review that carried the reply to its
// thread, which has no summary of its own.
func fixtureReviews() []reviewSpec {
	return []reviewSpec{
		{
			id: reviewFixtureID, author: "carol", state: "APPROVED",
			body: "Ship it once the checks pass.", at: workflowNow.Add(3 * time.Minute),
		},
		{
			id: replyReviewFixtureID, author: "octocat", state: "COMMENTED",
			at: workflowNow.Add(4 * time.Minute),
		},
	}
}

// fixtureInline is the standard inline thread: two remarks on one file, the
// second answering the first from the review that came after.
func fixtureInline() []inlineSpec {
	return []inlineSpec{
		{
			id: 4001, review: reviewFixtureID, author: "carol", path: "internal/ui/table.go",
			line: 12, hunk: fixtureHunk, at: workflowNow.Add(3 * time.Minute),
			body: "This width needs a name.",
		},
		{
			id: 4002, review: replyReviewFixtureID, author: "octocat", path: "internal/ui/table.go",
			line: 12, hunk: fixtureHunk, at: workflowNow.Add(4 * time.Minute), replyTo: 4001,
			body: "Named it tableWidth.",
		},
	}
}

// manyReviews is n reviews with distinct ids and times, which is what a page
// that comes back full looks like.
func manyReviews(n int) []reviewSpec {
	reviews := make([]reviewSpec, 0, n)
	for i := 0; i < n; i++ {
		reviews = append(reviews, reviewSpec{
			id:     reviewFixtureID + int64(i),
			author: "carol",
			state:  "COMMENTED",
			body:   fmt.Sprintf("Review %d.", i+1),
			at:     workflowNow.Add(time.Duration(i) * time.Minute),
		})
	}
	return reviews
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
		issues:     fixtureIssues(),
		runs:       fixtureRuns(9),
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
		case path == graphQLPath:
			// The fixture answers one GraphQL query — the resolution state a
			// test asked for — and refuses the rest: it models no search index,
			// which is what graphql_test.go covers.
			if config.resolvedThreads != nil && queryAsks(r, "reviewThreads") {
				writeBody(w, threadsBody(t, config.resolvedThreads))
				return
			}
			w.WriteHeader(http.StatusNotImplemented)
			writeBody(w, `{"message":"the fixture serves REST only"}`)
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
		case strings.Contains(path, "/pulls/") && strings.HasSuffix(path, "/reviews"):
			if config.failReviews {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			writeBody(w, reviewsBody(t, config.reviews))
		// A review's inline comments live on the pull request path and an
		// issue's comments on the issues path, so the pull request case comes
		// first: the two share the /comments suffix and would otherwise shadow
		// each other.
		case strings.Contains(path, "/pulls/") && strings.HasSuffix(path, "/comments"):
			if config.failInline {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			writeBody(w, inlineCommentsBody(t, config.inline))
		case strings.HasSuffix(path, "/comments"):
			if config.failComments {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			if config.noComments {
				writeBody(w, `[]`)
				return
			}
			writeBody(w, commentsBody(t, config.comments))
		case strings.HasSuffix(path, "/check-runs"):
			writeBody(w, checksBody(t, config.checks))
		case strings.HasSuffix(path, "/actions/runs") && r.Method == http.MethodGet:
			writeBody(w, runsPageBody(t, "octocat", config.runs, repoFromRunPath(path), r.URL.Query()))
		case strings.HasSuffix(path, "/jobs"):
			writeBody(w, jobsBody(t))
		case strings.Contains(path, "/collaborators/") && strings.HasSuffix(path, "/permission"):
			if config.failPermission {
				w.WriteHeader(http.StatusForbidden)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			writeBody(w, `{"permission":"`+config.permission+`","user":{"login":"octocat"}}`)
		// The issues paths are matched after the pull request ones, so the two
		// fixtures never shadow each other: the issues endpoint also returns
		// pull requests, and that overlap lives in the fixture body rather than
		// in the routing.
		case strings.Contains(path, "/issues/"):
			writeBody(w, issueDetailBody(t, config.issues, repoFromPath(path)))
		case strings.HasSuffix(path, "/issues"):
			if config.failIssues {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			writeBody(w, issuesBody(t, config.issues, repoFromPath(path), r.URL.Query().Get("state")))
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
		// The detail response names the repository only under head and base, so
		// the screen has to draw and link it from the reference it was opened
		// with. Keeping the top-level field here would hide a heading that links
		// nothing on a real pull request.
		delete(detail, "repository")
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

// issueSpec is one entry of the issues endpoint in a fixture. The endpoint
// returns pull requests alongside issues, and the transport drops them, so a
// spec can say that it is one.
type issueSpec struct {
	number    int
	repo      string
	title     string
	state     string
	author    string
	assignees []string
	labels    []string
	milestone string
	created   time.Time
	updated   time.Time
	// closed is when the issue was closed. Zero means it is still open.
	closed time.Time
	// isPull marks an entry that is really a pull request, which the transport
	// has to drop before the list ever sees it.
	isPull bool
}

// issueFixtureNumber is the issue the details tests open: the first issue in
// the fixture list.
const issueFixtureNumber = 3

// fixtureIssues is the standard issue list: an open issue carrying an assignee,
// a label and a milestone in the first repository, a closed one in the second,
// and one pull request the endpoint returns alongside them. The last entry is
// what makes the exclusion observable from the screen.
func fixtureIssues() []issueSpec {
	return []issueSpec{
		{
			number: issueFixtureNumber, repo: fixtureRepo, title: "Add the issues view",
			state: "open", author: "octocat", assignees: []string{"hubot"},
			labels: []string{"enhancement"}, milestone: "0.7.0",
			created: workflowNow.Add(-48 * time.Hour), updated: workflowNow,
		},
		{
			number: 4, repo: secondRepo, title: "Ship the old thing",
			state: "closed", author: "hubot",
			created: workflowNow.Add(-72 * time.Hour), updated: workflowNow.Add(-2 * time.Hour),
			closed: workflowNow.Add(-2 * time.Hour),
		},
		{
			number: 5, repo: fixtureRepo, title: "A pull request in the issue list",
			state: "open", author: "octocat",
			created: workflowNow.Add(-time.Hour), updated: workflowNow.Add(-time.Hour),
			isPull: true,
		},
	}
}

// issuesBody renders the issues endpoint's response for one repository and the
// requested state, the way the API does: an open query leaves the closed issues
// out. Two entries is the fixture's page, so HasMore is false.
func issuesBody(t *testing.T, issues []issueSpec, repo, state string) string {
	t.Helper()
	if state == "" {
		state = "open"
	}
	items := make([]map[string]any, 0, len(issues))
	for _, spec := range issues {
		if spec.repo != repo || (state != "all" && spec.state != state) {
			continue
		}
		items = append(items, issueJSON(t, spec))
	}
	return jsonBody(t, items)
}

// issueDetailBody renders one issue, with a body to describe. Like the pull
// request fixture it answers with the first issue of the repository, which is
// the row every details test opens.
func issueDetailBody(t *testing.T, issues []issueSpec, repo string) string {
	t.Helper()
	for _, spec := range issues {
		if spec.repo != repo || spec.isPull {
			continue
		}
		detail := issueJSON(t, spec)
		detail["body"] = strings.Join([]string{
			"Issues are the third list of the app.",
			"",
			"They have no reviewers and no diff, so the sidebar carries what the list cannot: the assignees, the labels and the milestone.",
			"",
			"The description scrolls by one line with j and k, and by six with u and d, which is the pair the pull request description uses.",
			"",
			"Press c for the conversation.",
		}, "\n")
		// As with a pull request, the issue detail response carries no
		// repository object: the screen names the reference it was opened with.
		return jsonBody(t, detail)
	}
	t.Fatalf("no fixture issue in %s", repo)
	return ""
}

// issueJSON renders one entry of the issues endpoint. It carries no repository
// object, which is what the real endpoint returns: the store fills that in from
// the repository it asked about.
func issueJSON(t *testing.T, spec issueSpec) map[string]any {
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
		"id":         int64(2000 + number),
		"number":     number,
		"title":      spec.title,
		"state":      spec.state,
		"created_at": spec.created.Format(time.RFC3339),
		"updated_at": spec.updated.Format(time.RFC3339),
		"html_url":   fmt.Sprintf("https://github.com/octocat/%s/issues/%d", spec.repo, number),
		"user":       map[string]any{"login": author},
	}
	if !spec.closed.IsZero() {
		out["closed_at"] = spec.closed.Format(time.RFC3339)
	}
	if len(spec.assignees) > 0 {
		assignees := make([]map[string]any, 0, len(spec.assignees))
		for _, login := range spec.assignees {
			assignees = append(assignees, map[string]any{"login": login})
		}
		out["assignees"] = assignees
	}
	if len(spec.labels) > 0 {
		labels := make([]map[string]any, 0, len(spec.labels))
		for _, name := range spec.labels {
			labels = append(labels, map[string]any{"name": name, "color": "a2eeef"})
		}
		out["labels"] = labels
	}
	if spec.milestone != "" {
		out["milestone"] = map[string]any{"title": spec.milestone}
	}
	if spec.isPull {
		out["pull_request"] = map[string]any{
			"url": fmt.Sprintf("https://api.github.com/repos/octocat/%s/pulls/%d", spec.repo, number),
		}
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
// commentsBody renders the conversation. Zero asks for the standard two
// comments; a larger count repeats them with distinct ids, which is what a test
// that scrolls a long thread needs.
func commentsBody(t *testing.T, count int) string {
	t.Helper()
	if count <= 0 {
		count = 2
	}
	items := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		body := fmt.Sprintf("Comment %d looks good to me.", i+1)
		user := "alice"
		if i%2 == 1 {
			body = fmt.Sprintf("Comment %d: thanks! Merging once the checks pass.", i+1)
			user = "octocat"
		}
		items = append(items, map[string]any{
			"id":         int64(i + 1),
			"body":       body,
			"created_at": workflowNow.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			"html_url":   fmt.Sprintf("https://github.com/octocat/api/pull/7#issuecomment-%d", i+1),
			"user":       map[string]any{"login": user},
		})
	}
	return jsonBody(t, items)
}

// reviewsBody renders a review list. Nil asks for the standard review.
func reviewsBody(t *testing.T, reviews []reviewSpec) string {
	t.Helper()
	if reviews == nil {
		reviews = fixtureReviews()
	}
	items := make([]map[string]any, 0, len(reviews))
	for _, spec := range reviews {
		item := map[string]any{
			"id":       spec.id,
			"state":    spec.state,
			"body":     spec.body,
			"user":     map[string]any{"login": spec.author},
			"html_url": fmt.Sprintf("https://github.com/octocat/api/pull/7#pullrequestreview-%d", spec.id),
		}
		// A review still being written has no submission time, which is what
		// makes it a draft.
		if spec.state != "PENDING" {
			item["submitted_at"] = spec.at.Format(time.RFC3339)
		}
		items = append(items, item)
	}
	return jsonBody(t, items)
}

// inlineCommentsBody renders an inline review comment list. Nil asks for the
// standard thread.
func inlineCommentsBody(t *testing.T, comments []inlineSpec) string {
	t.Helper()
	if comments == nil {
		comments = fixtureInline()
	}
	items := make([]map[string]any, 0, len(comments))
	for _, spec := range comments {
		item := map[string]any{
			"id":                     spec.id,
			"pull_request_review_id": spec.review,
			"body":                   spec.body,
			"path":                   spec.path,
			"line":                   spec.line,
			"original_line":          spec.line,
			"diff_hunk":              spec.hunk,
			"created_at":             spec.at.Format(time.RFC3339),
			"user":                   map[string]any{"login": spec.author},
			"html_url":               fmt.Sprintf("https://github.com/octocat/api/pull/7#discussion_r%d", spec.id),
		}
		if spec.replyTo != 0 {
			item["in_reply_to_id"] = spec.replyTo
		}
		items = append(items, item)
	}
	return jsonBody(t, items)
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
		"5": "Comment 1 looks good to me.",
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

// TestPRCommentsShowsTheThreeListsInOneConversation covers the feature the
// Comments sub-tab exists for: the API keeps comments, reviews and inline review
// comments apart, and a review-heavy pull request has nothing in the first of
// the three.
func TestPRCommentsShowsTheThreeListsInOneConversation(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	// Taller than the goldens' 120x40: the cards give every contribution air and
	// a rule, so the whole conversation needs the room to be read at once.
	const w, h = 120, 56
	m := openPRDetails(t, prsModel(t, srv, w, h))

	m = pressAndDrive(t, m, "5")
	out := ansi.Strip(screen(t, m, w, h))
	for _, want := range []string{
		// The conversation comment.
		"Comment 1 looks good to me.",
		// The review, with its conclusion and its summary.
		"carol", "[approved]", "Ship it once the checks pass.",
		// The inline thread: where it is, and the exchange under it.
		"internal/ui/table.go:12", "This width needs a name.", "Named it tableWidth.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the conversation is missing %q:\n%s", want, out)
		}
	}
	// The conversation comments come before the review, which was submitted
	// after them, and the review carries the thread.
	if first, review := strings.Index(out, "Comment 1 looks good to me."), strings.Index(out, "Ship it once the checks pass."); first > review {
		t.Errorf("the conversation is not in the order it happened:\n%s", out)
	}

	for _, path := range []string{
		"/repos/octocat/api/issues/7/comments",
		"/repos/octocat/api/pulls/7/reviews",
		"/repos/octocat/api/pulls/7/comments",
	} {
		if log.count(path) == 0 {
			t.Errorf("the Comments sub-tab did not read %s", path)
		}
	}
}

// TestPRCommentsReportsAFailedList covers a partial failure: the lists that
// arrived are drawn, and the one that did not is remembered, rather than the
// discussion being presented as complete.
func TestPRCommentsReportsAFailedList(t *testing.T) {
	srv := prServer(t, failReviewList())
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	m = pressAndDrive(t, m, "5")

	if prDetailsRoute(t, m).failed[prSectionReviews] == nil {
		t.Error("a failed review fetch left no record on the route")
	}
	if out := ansi.Strip(screen(t, m, 120, 40)); !strings.Contains(out, "Comment 1 looks good to me.") {
		t.Errorf("the lists that arrived are not on screen:\n%s", out)
	}
}

// TestPRCommentsReportWhenNothingArrived covers the whole conversation failing:
// the pane says what went wrong rather than drawing an empty discussion.
func TestPRCommentsReportWhenNothingArrived(t *testing.T) {
	srv := prServer(t, failConversation())
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	m = pressAndDrive(t, m, "5")

	out := ansi.Strip(screen(t, m, 120, 40))
	if !strings.Contains(out, "Could not load:") {
		t.Errorf("the failure is not on screen:\n%s", out)
	}
	if strings.Contains(out, "No comments or reviews") {
		t.Errorf("a failed conversation reads as an empty one:\n%s", out)
	}
}

// TestPRCommentsSkipsAReviewDraft covers the review its author has not
// submitted: the API reports it with no submission time, so it is not part of
// the discussion and neither are the inline remarks it is still holding.
func TestPRCommentsSkipsAReviewDraft(t *testing.T) {
	srv := prServer(t,
		withReviews(reviewSpec{id: 3002, author: "dave", state: "PENDING"}),
		withInlineComments(),
	)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	m = pressAndDrive(t, m, "5")

	out := ansi.Strip(screen(t, m, 120, 40))
	if strings.Contains(out, "dave") {
		t.Errorf("an unsubmitted review is on screen:\n%s", out)
	}
	if !strings.Contains(out, "Comment 1 looks good to me.") {
		t.Errorf("the conversation comments are missing:\n%s", out)
	}
}

// TestPRCommentsSayWhenAPageWasCut covers the full page: a review page that
// comes back at the API's maximum is part of a longer discussion, and the pane
// says so at the end of it rather than showing a fraction as the whole.
func TestPRCommentsSayWhenAPageWasCut(t *testing.T) {
	srv := prServer(t, withReviews(manyReviews(conversationPageSize)...), withInlineComments())
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	m = pressAndDrive(t, m, "5")

	details := prDetailsRoute(t, m)
	if !details.conversationHasMore {
		t.Fatal("a full review page did not mark the conversation truncated")
	}

	content := details.commentLines(m.deps)
	for i := 0; i <= len(content)/diffStep+1; i++ {
		m = pressAndDrive(t, m, "d")
	}
	if out := ansi.Strip(screen(t, m, 120, 40)); !strings.Contains(out, truncatedNote) {
		t.Errorf("the end of a cut conversation does not say so:\n%s", out)
	}
}

// TestPRCommentsKeepAThreadTogetherAcrossReviews covers the screen the defect
// was reported on: GitHub posts a reply as part of a new review, and the
// exchange still reads as one thread — one location, one hunk excerpt, the reply
// under the comment it answers — while the review that only carried the reply
// draws its own heading and nothing under it.
func TestPRCommentsKeepAThreadTogetherAcrossReviews(t *testing.T) {
	srv := prServer(t)
	// The pane is taller than the 120x40 the goldens use: a framed thread costs a
	// row or two more than the flat list did, and this test counts a review that
	// sits at the end of the conversation, so all of it has to fit at once.
	const w, h = 120, 56
	m := openPRDetails(t, prsModel(t, srv, w, h))
	m = pressAndDrive(t, m, "5")

	out := ansi.Strip(screen(t, m, w, h))
	if got := strings.Count(out, "internal/ui/table.go:12"); got != 1 {
		t.Errorf("the thread's location is drawn %d times, want one thread:\n%s", got, out)
	}
	root, reply := strings.Index(out, "This width needs a name."), strings.Index(out, "Named it tableWidth.")
	if root < 0 || reply < 0 {
		t.Fatalf("the thread is not on screen:\n%s", out)
	}
	if reply < root {
		t.Errorf("the reply reads above the comment it answers:\n%s", out)
	}
	if got := strings.Count(out, "octocat  [reviewed]"); got != 1 {
		t.Errorf("the review that carried the reply is drawn %d times, want once:\n%s", got, out)
	}
}

// TestPRCommentsFoldAResolvedThreadAndOpenItAgain covers the resolution state on
// the screen: a thread GitHub reports as resolved reaches the Comments sub-tab as
// one row with a button, and the button opens and folds it again.
func TestPRCommentsFoldAResolvedThreadAndOpenItAgain(t *testing.T) {
	srv := prServerWithLog(t, newRequestLog(), withResolvedThreads([]int64{4001, 4002}))
	const w, h = 120, 56
	m := openPRDetails(t, prsModel(t, srv, w, h))
	m = pressAndDrive(t, m, "5")

	folded := ansi.Strip(screen(t, m, w, h))
	for _, want := range []string{
		threadShowLabel, "internal/ui/table.go:12", "carol", "2 remarks", "resolved",
	} {
		if !strings.Contains(folded, want) {
			t.Errorf("the folded conversation is missing %q:\n%s", want, folded)
		}
	}
	if strings.Contains(folded, "This width needs a name.") {
		t.Errorf("a folded thread shows its remarks:\n%s", folded)
	}
	// The control carries the focus as soon as it exists, so the reader can see
	// where Enter acts.
	if !strings.Contains(folded, selectMarker) {
		t.Errorf("no control carries the focus:\n%s", folded)
	}

	// Enter opens it: the remarks are back and the button says hide.
	m = pressAndDrive(t, m, "enter")
	opened := ansi.Strip(screen(t, m, w, h))
	for _, want := range []string{threadHideLabel, "This width needs a name.", "Named it tableWidth."} {
		if !strings.Contains(opened, want) {
			t.Errorf("the opened thread is missing %q:\n%s", want, opened)
		}
	}
	if strings.Contains(opened, threadShowLabel) {
		t.Errorf("an opened thread still offers %q:\n%s", threadShowLabel, opened)
	}

	// And folds it again, which is what makes the button a control rather than a
	// one-way door.
	m = pressAndDrive(t, m, "enter")
	refolded := ansi.Strip(screen(t, m, w, h))
	if !strings.Contains(refolded, threadShowLabel) || strings.Contains(refolded, "This width needs a name.") {
		t.Errorf("the thread did not fold again:\n%s", refolded)
	}
}

// TestPRCommentsWalkTheThreadControlsWithTheArrows covers what the arrows do on a
// conversation whose threads have been resolved: they move between the buttons,
// one thread at a time, and leave the pane's own scrolling to the scroll keys.
func TestPRCommentsWalkTheThreadControlsWithTheArrows(t *testing.T) {
	second := inlineSpec{
		id: 4003, review: reviewFixtureID, author: "carol", path: "internal/ui/prpanes.go",
		line: 4, hunk: fixtureHunk, at: workflowNow.Add(3 * time.Minute),
		body: "A free function would do here.",
	}
	srv := prServerWithLog(t, newRequestLog(),
		withInlineComments(append(fixtureInline(), second)...),
		withResolvedThreads([]int64{4001, 4002}, []int64{4003}))
	const w, h = 120, 56
	m := openPRDetails(t, prsModel(t, srv, w, h))
	m = pressAndDrive(t, m, "5")

	route := prDetailsRoute(t, m)
	if len(route.threadControls) != 2 {
		t.Fatalf("the conversation carries %d controls, want the two folded threads", len(route.threadControls))
	}
	if route.threadFocus != 0 {
		t.Errorf("the focus starts on control %d, want the first", route.threadFocus)
	}

	// The first arrow moves to the second control, and does not scroll the pane.
	moved := pressAndDrive(t, m, "down")
	route = prDetailsRoute(t, moved)
	if route.threadFocus != 1 {
		t.Errorf("down moved the focus to control %d, want the second", route.threadFocus)
	}
	if route.commentScroll != 0 {
		t.Errorf("the arrows scrolled the pane to line %d, want the scroll left alone", route.commentScroll)
	}
	// Exactly one row carries the marker, and it is the second thread's.
	rows := strings.Split(ansi.Strip(screen(t, moved, w, h)), "\n")
	marked := 0
	for _, row := range rows {
		if !strings.HasPrefix(strings.TrimLeft(row, " "), selectMarker) {
			continue
		}
		marked++
		if !strings.Contains(row, "prpanes.go:4") {
			t.Errorf("the focus is on %q, want the second thread's row", row)
		}
	}
	if marked != 1 {
		t.Errorf("%d rows carry the focus marker, want one", marked)
	}

	// The scroll keys still scroll, and leave the focus where it was.
	scrolled := pressAndDrive(t, moved, "d")
	route = prDetailsRoute(t, scrolled)
	if route.commentScroll == 0 {
		t.Error("the scroll key did not scroll the conversation")
	}
	if route.threadFocus != 1 {
		t.Errorf("scrolling moved the focus to control %d, want it left on the second", route.threadFocus)
	}
}

// TestTheFocusMarkerIsNotBakedIntoTheCache covers the render that draws the
// focus. The marked row is one of the conversation's cached lines, so a render
// that wrote the marker into that slice would leave it there and every later
// render would add another one. The coloured palette is the one that matters: the
// folded row is drawn beside a washed body, and the two must not mix.
func TestTheFocusMarkerIsNotBakedIntoTheCache(t *testing.T) {
	second := inlineSpec{
		id: 4003, review: reviewFixtureID, author: "carol", path: "internal/ui/prpanes.go",
		line: 4, hunk: fixtureHunk, at: workflowNow.Add(3 * time.Minute),
		body: "A free function would do here.",
	}
	srv := prServerWithLog(t, newRequestLog(),
		withInlineComments(append(fixtureInline(), second)...),
		withResolvedThreads([]int64{4001, 4002}, []int64{4003}))
	const w, h = 120, 56
	painted := prsModel(t, srv, w, h, func(o *Options) { o.Theme = DarkTheme() })
	m := pressAndDrive(t, openPRDetails(t, painted), "5")

	first := screen(t, m, w, h)
	var marked, unmarked string
	for _, raw := range strings.Split(first, "\n") {
		row := ansi.Strip(raw)
		if n := strings.Count(row, selectMarker); n > 1 {
			t.Errorf("a row carries the focus marker %d times: %q", n, row)
		}
		if !strings.Contains(row, threadShowLabel) {
			continue
		}
		if strings.Contains(row, selectMarker) {
			marked = row
			continue
		}
		unmarked = row
	}
	if marked == "" || unmarked == "" {
		t.Fatalf("the screen holds %q marked and %q unmarked, want one of each", marked, unmarked)
	}
	// The marker takes the cells of the indent, so the control does not move
	// when the focus lands on it.
	column := func(row string) int {
		return ansi.StringWidth(row[:strings.Index(row, threadShowLabel)])
	}
	if got, want := column(marked), column(unmarked); got != want {
		t.Errorf("the focused control is at column %d and the other at %d: %q", got, want, marked)
	}
	if again := screen(t, m, w, h); again != first {
		t.Errorf("the same state drew two screens; the second one is:\n%s", ansi.Strip(again))
	}
}

// TestPRCommentsWithoutResolutionKeepTheArrowsOnTheScroll covers the page whose
// resolution query refused: no thread carries a button, so the arrows scroll the
// conversation exactly as they did before any of this existed.
func TestPRCommentsWithoutResolutionKeepTheArrowsOnTheScroll(t *testing.T) {
	srv := prServerWithLog(t, newRequestLog(), withComments(20))
	const w, h = 120, 40
	m := openPRDetails(t, prsModel(t, srv, w, h))
	m = pressAndDrive(t, m, "5")

	if controls := prDetailsRoute(t, m).threadControls; len(controls) != 0 {
		t.Fatalf("the conversation carries %d controls, want none", len(controls))
	}
	route := prDetailsRoute(t, pressAndDrive(t, m, "down", "down"))
	if route.commentScroll != 2 {
		t.Errorf("two downs scrolled to line %d, want 2", route.commentScroll)
	}
	if route.threadFocus >= 0 {
		t.Errorf("the focus is on control %d with no controls in the pane", route.threadFocus)
	}
}

// TestTheWheelScrollsTheRouteOnScreen covers the wheel: it resolves into the
// scroll the keys ask for, so every pane that scrolls is scrolled by it, and it
// is not a cursor move on a list.
func TestTheWheelScrollsTheRouteOnScreen(t *testing.T) {
	srv := prServerWithLog(t, newRequestLog(), withComments(20))
	const w, h = 120, 40
	m := openPRDetails(t, prsModel(t, srv, w, h))
	m = pressAndDrive(t, m, "5")

	down, _ := send(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	route := prDetailsRoute(t, down)
	if route.commentScroll != diffStep {
		t.Errorf("a wheel down scrolled to line %d, want the scroll key's %d lines", route.commentScroll, diffStep)
	}
	up, _ := send(t, down, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if got := prDetailsRoute(t, up).commentScroll; got != 0 {
		t.Errorf("a wheel up scrolled to line %d, want back at the top", got)
	}

	list := prsModel(t, srv, w, h)
	listed, _ := send(t, list, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := prsTabOf(t, listed).cursor; got != 0 {
		t.Errorf("the wheel moved the list cursor to %d, want it left on the first row", got)
	}
}

// TestPRCommentsScrollWithTheArrowKeys covers the keys the conversation pane
// answers, which is what made it read as a screen that does not scroll: the
// arrows and j/k move it a line at a time, and u/d and the page keys keep
// moving it a screenful.
func TestPRCommentsScrollWithTheArrowKeys(t *testing.T) {
	srv := prServer(t, withReviews(manyReviews(8)...), withInlineComments())
	m := openPRDetails(t, prsModel(t, srv, 100, 24))
	m = pressAndDrive(t, m, "5")

	details := prDetailsRoute(t, m)
	content := len(details.commentLines(m.deps))
	if content <= m.deps.Size.Height {
		t.Fatalf("the fixture conversation is %d lines and the screen is %d rows", content, m.deps.Size.Height)
	}

	// One line down, one line up, with both spellings of each.
	for i, key := range []string{"down", "j"} {
		m = pressAndDrive(t, m, key)
		if got := prDetailsRoute(t, m).commentScroll; got != i+1 {
			t.Fatalf("%s scrolled to %d, want %d", key, got, i+1)
		}
	}
	for i, key := range []string{"up", "k"} {
		m = pressAndDrive(t, m, key)
		if got := prDetailsRoute(t, m).commentScroll; got != 1-i {
			t.Fatalf("%s scrolled to %d, want %d", key, got, 1-i)
		}
	}
	if got := prDetailsRoute(t, m).commentScroll; got != 0 {
		t.Fatalf("the conversation did not return to the top: %d", got)
	}

	// The frame follows the offset, and the conversation is longer than the
	// pane, so the offsets above are scrolls rather than a clamp against a
	// screenful that fits.
	m = pressAndDrive(t, m, "down")
	lines := details.commentLines(m.deps)
	out := ansi.Strip(screen(t, m, 100, 24))
	if last := strings.TrimSpace(lines[content-1]); last != "" && strings.Contains(out, last) {
		t.Fatalf("the last conversation line is already on screen:\n%s", out)
	}
	if first := strings.TrimSpace(lines[0]); first != "" && strings.Contains(out, first) {
		t.Errorf("the first conversation line is still on screen after scrolling past it:\n%s", out)
	}

	m = pressAndDrive(t, m, "d")
	if got := prDetailsRoute(t, m).commentScroll; got != diffStep+1 {
		t.Errorf("d scrolled to %d, want a screenful past the line the arrow moved", got)
	}
	m = pressAndDrive(t, m, "u")
	if got := prDetailsRoute(t, m).commentScroll; got != 1 {
		t.Errorf("u scrolled to %d, want back to the line the arrow left", got)
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

// TestInfoColumnsTileTheBody is the layout property the two-column Info view
// rests on: whatever the terminal width, the columns and the separator drawn
// between them add up to exactly the body.
func TestInfoColumnsTileTheBody(t *testing.T) {
	for width := 1; width <= 400; width++ {
		desc, side := detailColumns(width)
		separators := detailSepWidth
		if side == 0 {
			separators = 0
		}
		if sum := desc + side + separators; sum != width {
			t.Fatalf("width %d: columns and separators tile %d, want %d", width, sum, width)
		}
		if desc < 0 || side < 0 {
			t.Fatalf("width %d: negative column (%d, %d)", width, desc, side)
		}
		if width >= descMinWidth+sideMinWidth+detailSepWidth {
			if desc < descMinWidth || side < sideMinWidth {
				t.Fatalf("width %d: a column is below its minimum (%d, %d)", width, desc, side)
			}
		}
	}
}

// TestInfoColumnsDropsTheSidebarWhenItCannotAnswer pins the priority: a sidebar
// too narrow to hold a label and a value is dropped rather than squeezed, and the
// description takes the width back.
func TestInfoColumnsDropsTheSidebarWhenItCannotAnswer(t *testing.T) {
	const width = descMinWidth + sideMinWidth + detailSepWidth - 1
	desc, side := detailColumns(width)
	if side != 0 {
		t.Fatalf("sidebar is %d wide at %d columns, want it dropped", side, width)
	}
	if desc != width {
		t.Fatalf("description is %d wide at %d columns, want the whole body", desc, width)
	}
}

// TestSideHeightsGivesUpTheDetailsFirst pins the vertical priority: the checks
// are what the route's keys act on, so the details block gives way when the body
// is short and the checks keep the column. The blank row that separates the two
// blocks is part of what the column has to hold.
func TestSideHeightsGivesUpTheDetailsFirst(t *testing.T) {
	full := detailsRows + sideGapRows + checksMinRows
	if details, checks := sideHeights(full); details != detailsRows || checks != checksMinRows {
		t.Errorf("sideHeights(%d) = (%d, %d), want (%d, %d)",
			full, details, checks, detailsRows, checksMinRows)
	}
	if details, checks := sideHeights(full - 1); details != 0 || checks != full-1 {
		t.Errorf("a short body kept the details block: (%d, %d)", details, checks)
	}
	for height := 0; height <= 60; height++ {
		details, checks := sideHeights(height)
		used := details + checks
		if details > 0 {
			used += sideGapRows
		}
		if used != height {
			t.Fatalf("height %d: (%d, %d) and a %d-row gap do not fill the column", height, details, checks, sideGapRows)
		}
	}
}

// TestInfoViewTilesEveryWidth renders the Info sub-tab across a wide range of
// body widths and heights and checks what comes out: every line is exactly the
// body width, and a column that is not drawn leaves no separator behind.
func TestInfoViewTilesEveryWidth(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	details := prDetailsRoute(t, m)

	for height := 6; height <= 20; height++ {
		for width := 12; width <= 200; width++ {
			view := details.infoView(m.deps, layout.Rect{W: width, H: height})
			lines := splitLines(view)
			if len(lines) != height {
				t.Fatalf("%dx%d: %d lines, want %d", width, height, len(lines), height)
			}
			for i, line := range lines {
				if got := layout.Width(line); got != width {
					t.Fatalf("%dx%d: line %d is %d columns: %q", width, height, i, got, line)
				}
				if strings.Contains(line, "││") {
					t.Fatalf("%dx%d: line %d draws two separators: %q", width, height, i, line)
				}
			}
		}
	}
}

// TestInfoViewStacksTheDetailsOverTheChecks pins the arrangement the screenshot
// review asked for: the details block sits above the checks in the sidebar, and
// both are in the same column as the description's neighbour.
func TestInfoViewStacksTheDetailsOverTheChecks(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	details := prDetailsRoute(t, m)

	view := details.infoView(m.deps, layout.Rect{W: 120, H: 40})
	rows := splitLines(view)
	detailsRow, checksRow := -1, -1
	for i, row := range rows {
		if at := strings.Index(row, "DETAILS"); at >= 0 && detailsRow < 0 {
			detailsRow = i
			if _, side := detailColumns(120); at <= side {
				t.Errorf("DETAILS is at column %d, want it in the sidebar", at)
			}
		}
		if strings.Contains(row, "CHECKS") {
			checksRow = i
		}
	}
	if detailsRow < 0 || checksRow < 0 {
		t.Fatalf("the sidebar is missing a block: details=%d checks=%d", detailsRow, checksRow)
	}
	if detailsRow >= checksRow {
		t.Errorf("DETAILS is on row %d and CHECKS on row %d, want the details above the checks", detailsRow, checksRow)
	}
}

// TestInfoViewKeepsTheDetailsOnTheCanonicalScreen pins the size the screenshot
// review was read at. At 80x24 the Info body is fifteen rows, and the details
// block has to be drawn in them: it is the pane that answers "whose pull request
// is this", and dropping it to give the checks one more row leaves the commonest
// terminal size without it.
func TestInfoViewKeepsTheDetailsOnTheCanonicalScreen(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 80, 24))
	details := prDetailsRoute(t, m)

	// 24 rows less the two header rows, the two footer rows and the five rows of
	// heading and sub-tab strip the route draws above its body.
	const infoBody = 24 - 2 - 2 - 5
	view := details.infoView(m.deps, layout.Rect{W: 80, H: infoBody})

	for _, want := range []string{"DETAILS", "State:", "CHECKS"} {
		if !strings.Contains(view, want) {
			t.Errorf("the sidebar does not show %q at 80x24:\n%s", want, view)
		}
	}

	// The blank row between the blocks, so the checks heading is not glued to the
	// last value above it.
	lines := splitLines(ansi.Strip(view))
	checksAt := -1
	for i, line := range lines {
		if strings.Contains(line, "CHECKS") {
			checksAt = i
		}
	}
	if checksAt < 1 {
		t.Fatalf("the sidebar has no CHECKS heading:\n%s", view)
	}
	_, side := detailColumns(80)
	above := []rune(lines[checksAt-1])
	if got := strings.TrimSpace(string(above[len(above)-side:])); got != "" {
		t.Errorf("the row above CHECKS is not blank: %q", got)
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
