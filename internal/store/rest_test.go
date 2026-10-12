package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a test GitHub server. It counts requests per path and serves the
// canned body registered for that path.
type recorder struct {
	mu       sync.Mutex
	counts   map[string]int
	lastQ    map[string]url.Values
	bodies   map[string]string
	statuses map[string]int
	headers  map[string]http.Header
}

func newRecorder() *recorder {
	return &recorder{
		counts:   make(map[string]int),
		lastQ:    make(map[string]url.Values),
		bodies:   make(map[string]string),
		statuses: make(map[string]int),
		headers:  make(map[string]http.Header),
	}
}

// serve registers a 200 response body for a path.
func (r *recorder) serve(path, body string) *recorder {
	r.bodies[path] = body
	return r
}

// fail registers a non-200 status and message for a path.
func (r *recorder) fail(path string, status int, message string) *recorder {
	r.statuses[path] = status
	r.bodies[path] = fmt.Sprintf(`{"message":%q}`, message)
	return r
}

// withHeader registers response headers for a path.
func (r *recorder) withHeader(path string, h http.Header) *recorder {
	r.headers[path] = h
	return r
}

func (r *recorder) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[path]
}

func (r *recorder) query(path string) url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastQ[path]
}

func (r *recorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path

		r.mu.Lock()
		r.counts[path]++
		r.lastQ[path] = req.URL.Query()
		body, ok := r.bodies[path]
		status, hasStatus := r.statuses[path]
		header := r.headers[path]
		r.mu.Unlock()

		for k, vs := range header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no fixture"}`))
			return
		}
		if hasStatus {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	})
}

// newTestStore starts a recorder-backed store with a long cache TTL so caching
// behaviour is observable without racing the clock.
func newTestStore(t *testing.T, rec *recorder, opts ...Option) *REST {
	t.Helper()
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)
	opts = append([]Option{WithCacheTTL(time.Minute)}, opts...)
	return NewREST("test-token", srv.URL, opts...)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestViewerCachesUntilInvalidated(t *testing.T) {
	rec := newRecorder().serve("/user", `{"login":"octocat","id":1}`)
	s := newTestStore(t, rec)

	for i := 0; i < 3; i++ {
		u, err := s.Viewer(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if u.Login != "octocat" {
			t.Fatalf("login = %q", u.Login)
		}
	}
	if got := rec.count("/user"); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}

	s.Invalidate()
	if _, err := s.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.count("/user"); got != 2 {
		t.Fatalf("after invalidate requests = %d, want 2", got)
	}
}

func TestOrganizationsAndRepositories(t *testing.T) {
	rec := newRecorder().
		serve("/user/orgs", `[{"login":"acme","id":2}]`).
		serve("/orgs/acme/repos", `[{"id":1,"name":"one","full_name":"acme/one","owner":{"login":"acme"}}]`)
	s := newTestStore(t, rec)

	orgs, err := s.Organizations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0].Login != "acme" {
		t.Fatalf("orgs = %+v", orgs)
	}

	repos, err := s.Repositories(context.Background(), OwnerRef{Login: "acme", IsOrg: true}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos.Items) != 1 || repos.Items[0].Name != "one" {
		t.Fatalf("repos = %+v", repos)
	}
	if repos.HasMore {
		t.Error("HasMore true for a short page")
	}

	q := rec.query("/orgs/acme/repos")
	if got := q.Get("per_page"); got != fmt.Sprint(defaultRepoPageSize) {
		t.Errorf("per_page = %q, want %d", got, defaultRepoPageSize)
	}
	if got := q.Get("sort"); got != "pushed" {
		t.Errorf("sort = %q, want pushed", got)
	}
}

func TestRepositoriesUsesUserEndpointForUsers(t *testing.T) {
	rec := newRecorder().serve("/users/octocat/repos", `[]`)
	s := newTestStore(t, rec)

	if _, err := s.Repositories(context.Background(), OwnerRef{Login: "octocat"}, Page{}); err != nil {
		t.Fatal(err)
	}
	if rec.count("/users/octocat/repos") != 1 {
		t.Fatal("user repositories did not hit the user endpoint")
	}
	if rec.count("/orgs/octocat/repos") != 0 {
		t.Fatal("user repositories hit the organization endpoint")
	}
}

// TestWorkflowRunsPaginationAndHasMore pins the page arithmetic and the
// full-page-means-more rule.
func TestWorkflowRunsPaginationAndHasMore(t *testing.T) {
	// The actor is part of the fixture because the store re-checks the filter
	// against the page it was given, and a run that does not report its actor
	// cannot be confirmed to match.
	//
	// The count is the repository's rather than the page's, which is what tells
	// a page that is not the last one apart from the last page.
	runs := `{"total_count":5,"workflow_runs":[{"id":1,"actor":{"login":"octocat"}},{"id":2,"actor":{"login":"octocat"}}]}`
	rec := newRecorder().serve("/repos/acme/one/actions/runs", runs)
	s := newTestStore(t, rec)
	repo := RepoRef{Owner: "acme", Repo: "one"}

	full, err := s.WorkflowRuns(context.Background(), repo, RunQuery{Page: Page{Number: 2, Size: 2}, Actor: "octocat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Items) != 2 || !full.HasMore {
		t.Fatalf("page 2 of 5 = %+v, want 2 items and HasMore", full)
	}
	q := rec.query("/repos/acme/one/actions/runs")
	if q.Get("page") != "2" || q.Get("per_page") != "2" || q.Get("actor") != "octocat" {
		t.Errorf("query = %v", q)
	}

	// Page 3 of a size-3 page starts past the fifth run, so nothing is left.
	short, err := s.WorkflowRuns(context.Background(), repo, RunQuery{Page: Page{Number: 3, Size: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if short.HasMore {
		t.Error("HasMore true for a page past the end of the list")
	}
}

func TestWorkflowRunsEndsOnAFullLastPage(t *testing.T) {
	// Two runs, a page size of two: the page is full and the list has ended, so
	// the count is what says there is nothing more to load.
	runs := `{"total_count":2,"workflow_runs":[{"id":1},{"id":2}]}`
	rec := newRecorder().serve("/repos/acme/one/actions/runs", runs)
	s := newTestStore(t, rec)

	got, err := s.WorkflowRuns(context.Background(), RepoRef{Owner: "acme", Repo: "one"}, RunQuery{Page: Page{Number: 1, Size: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want the whole list", len(got.Items))
	}
	if got.HasMore {
		t.Error("HasMore true for a full page that holds the whole list")
	}
}

func TestWorkflowRunsRequiresRepo(t *testing.T) {
	s := newTestStore(t, newRecorder())
	if _, err := s.WorkflowRuns(context.Background(), RepoRef{Owner: "acme"}, RunQuery{}); err == nil {
		t.Fatal("expected an error for an incomplete repo reference")
	}
}

func TestJobsAttemptRouting(t *testing.T) {
	rec := newRecorder().
		serve("/repos/acme/one/actions/runs/5/jobs", `{"jobs":[{"id":1}]}`).
		serve("/repos/acme/one/actions/runs/5/attempts/2/jobs", `{"jobs":[{"id":2}]}`)
	s := newTestStore(t, rec)
	repo := RepoRef{Owner: "acme", Repo: "one"}

	latest, err := s.Jobs(context.Background(), repo, 5, 0, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(latest.Items) != 1 || latest.Items[0].ID != 1 {
		t.Fatalf("latest attempt jobs = %+v", latest)
	}

	older, err := s.Jobs(context.Background(), repo, 5, 2, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Items) != 1 || older.Items[0].ID != 2 {
		t.Fatalf("attempt 2 jobs = %+v", older)
	}
	if rec.count("/repos/acme/one/actions/runs/5/attempts/2/jobs") != 1 {
		t.Error("attempt 2 did not hit the attempts endpoint")
	}
}

func TestJobLogsRoundTrip(t *testing.T) {
	rec := newRecorder().serve("/repos/acme/one/actions/jobs/9/logs", "line one\nline two\n")
	s := newTestStore(t, rec)

	logs, err := s.JobLogs(context.Background(), RepoRef{Owner: "acme", Repo: "one"}, 9)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, "line two") {
		t.Fatalf("logs = %q", logs)
	}
}

// TestPullRequestsFilterLocallyAndReuseThePage checks that the author,
// assignee and reviewer filters apply client-side, and that changing a filter
// does not refetch the underlying page.
func TestPullRequestsFilterLocallyAndReuseThePage(t *testing.T) {
	prs := []map[string]any{
		{"number": 1, "user": map[string]any{"login": "alice"},
			"assignees":           []map[string]any{{"login": "bob"}},
			"requested_reviewers": []map[string]any{{"login": "carol"}}},
		{"number": 2, "user": map[string]any{"login": "BOB"},
			"assignees":           []map[string]any{},
			"requested_reviewers": []map[string]any{}},
	}
	rec := newRecorder().serve("/repos/acme/one/pulls", mustJSON(t, prs))
	s := newTestStore(t, rec)
	repo := RepoRef{Owner: "acme", Repo: "one"}

	byAuthor, err := s.PullRequests(context.Background(), repo, PRQuery{Author: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAuthor.Items) != 1 || byAuthor.Items[0].Number != 1 {
		t.Fatalf("author filter = %+v", byAuthor.Items)
	}

	byAssignee, err := s.PullRequests(context.Background(), repo, PRQuery{Assignee: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAssignee.Items) != 1 || byAssignee.Items[0].Number != 1 {
		t.Fatalf("assignee filter = %+v", byAssignee.Items)
	}

	byReviewer, err := s.PullRequests(context.Background(), repo, PRQuery{Reviewer: "carol"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byReviewer.Items) != 1 || byReviewer.Items[0].Number != 1 {
		t.Fatalf("reviewer filter = %+v", byReviewer.Items)
	}

	unfiltered, err := s.PullRequests(context.Background(), repo, PRQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(unfiltered.Items) != 2 {
		t.Fatalf("unfiltered = %+v", unfiltered.Items)
	}

	if got := rec.count("/repos/acme/one/pulls"); got != 1 {
		t.Fatalf("underlying page fetched %d times, want 1", got)
	}

	if got := rec.query("/repos/acme/one/pulls").Get("state"); got != "open" {
		t.Errorf("default state = %q, want open", got)
	}
}

func TestPullRequestsStateIsForwarded(t *testing.T) {
	rec := newRecorder().serve("/repos/acme/one/pulls", `[]`)
	s := newTestStore(t, rec)

	for _, state := range []string{"closed", "all"} {
		if _, err := s.PullRequests(context.Background(), RepoRef{Owner: "acme", Repo: "one"}, PRQuery{State: state}); err != nil {
			t.Fatal(err)
		}
		if got := rec.query("/repos/acme/one/pulls").Get("state"); got != state {
			t.Errorf("state = %q, want %q", got, state)
		}
	}
}

// TestIssuesReportsTransportHasMore covers the case that motivates List.HasMore:
// a page of two raw entries where one is a pull request leaves one issue, but
// another page still exists.
func TestIssuesReportsTransportHasMore(t *testing.T) {
	raw := []map[string]any{
		{"number": 10, "title": "a real issue"},
		{"number": 11, "title": "actually a PR", "pull_request": map[string]any{"url": "u"}},
	}
	rec := newRecorder().serve("/repos/acme/one/issues", mustJSON(t, raw))
	s := newTestStore(t, rec)

	got, err := s.Issues(context.Background(), RepoRef{Owner: "acme", Repo: "one"}, IssueQuery{Page: Page{Size: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Number != 10 {
		t.Fatalf("items = %+v", got.Items)
	}
	if !got.HasMore {
		t.Error("HasMore false even though the raw page was full")
	}
}

func TestPullRequestDetailAndPagedChildren(t *testing.T) {
	rec := newRecorder().
		serve("/repos/acme/one/pulls/7", `{"number":7,"title":"t"}`).
		serve("/repos/acme/one/pulls/7/commits", `[{"sha":"abc"}]`).
		serve("/repos/acme/one/pulls/7/files", `[{"filename":"a.go"}]`).
		serve("/repos/acme/one/issues/7/comments", `[{"id":1,"body":"hi"}]`).
		serve("/repos/acme/one/commits/abc", `{"sha":"abc","files":[{"filename":"a.go"}]}`).
		serve("/repos/acme/one/commits/abc/check-runs", `{"check_runs":[{"id":1,"name":"ci"}]}`)
	s := newTestStore(t, rec)
	repo := RepoRef{Owner: "acme", Repo: "one"}

	pr, err := s.PullRequest(context.Background(), repo, 7)
	if err != nil || pr.Number != 7 {
		t.Fatalf("pr = %+v, err = %v", pr, err)
	}

	commits, err := s.PullRequestCommits(context.Background(), repo, 7, Page{Number: 2, Size: 5})
	if err != nil || len(commits.Items) != 1 {
		t.Fatalf("commits = %+v, err = %v", commits, err)
	}
	if q := rec.query("/repos/acme/one/pulls/7/commits"); q.Get("page") != "2" || q.Get("per_page") != "5" {
		t.Errorf("commit query = %v", q)
	}

	files, err := s.PullRequestFiles(context.Background(), repo, 7, Page{})
	if err != nil || len(files.Items) != 1 {
		t.Fatalf("files = %+v, err = %v", files, err)
	}

	comments, err := s.Comments(context.Background(), repo, 7, Page{})
	if err != nil || len(comments.Items) != 1 {
		t.Fatalf("comments = %+v, err = %v", comments, err)
	}

	commit, changed, err := s.Commit(context.Background(), repo, "abc")
	if err != nil || commit.SHA != "abc" || len(changed) != 1 {
		t.Fatalf("commit = %+v, files = %+v, err = %v", commit, changed, err)
	}

	checks, err := s.CheckRuns(context.Background(), repo, "abc")
	if err != nil || len(checks) != 1 || checks[0].Name != "ci" {
		t.Fatalf("checks = %+v, err = %v", checks, err)
	}
}

func TestIssueFetch(t *testing.T) {
	rec := newRecorder().serve("/repos/acme/one/issues/10", `{"number":10,"title":"t"}`)
	s := newTestStore(t, rec)

	issue, err := s.Issue(context.Background(), RepoRef{Owner: "acme", Repo: "one"}, 10)
	if err != nil || issue.Number != 10 {
		t.Fatalf("issue = %+v, err = %v", issue, err)
	}
}

// TestWritesInvalidateTheCache is the consistency contract: after a mutation,
// a subsequent read must not serve a stale list.
func TestWritesInvalidateTheCache(t *testing.T) {
	rec := newRecorder().
		serve("/repos/acme/one/pulls", `[]`).
		serve("/repos/acme/one/pulls/7/merge", `{"merged":true}`).
		serve("/repos/acme/one/pulls/7", `{"number":7,"state":"closed"}`).
		serve("/repos/acme/one/actions/runs/9/approve", ``)
	s := newTestStore(t, rec)
	repo := RepoRef{Owner: "acme", Repo: "one"}
	ctx := context.Background()

	if _, err := s.PullRequests(ctx, repo, PRQuery{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Merge(ctx, repo, 7, "squash", "title", "message"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PullRequests(ctx, repo, PRQuery{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.count("/repos/acme/one/pulls"); got != 2 {
		t.Fatalf("list fetched %d times after merge, want 2", got)
	}

	if err := s.ClosePR(ctx, repo, 7); err != nil {
		t.Fatal(err)
	}
	// ClosePR and the detail fetch share a path, so compare deltas.
	before := rec.count("/repos/acme/one/pulls/7")
	if _, err := s.PullRequest(ctx, repo, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PullRequest(ctx, repo, 7); err != nil {
		t.Fatal(err)
	}
	if got := rec.count("/repos/acme/one/pulls/7") - before; got != 1 {
		t.Fatalf("detail fetched %d times after re-cache, want 1", got)
	}

	if err := s.ApproveRun(ctx, repo, 9); err != nil {
		t.Fatal(err)
	}
	if got := rec.count("/repos/acme/one/actions/runs/9/approve"); got != 1 {
		t.Fatalf("approve calls = %d, want 1", got)
	}
}

func TestErrorsArePropagatedAndNotCached(t *testing.T) {
	rec := newRecorder().fail("/user", http.StatusForbidden, "bad credentials")
	s := newTestStore(t, rec)

	_, err := s.Viewer(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	// A failure must not be cached, so a retry reaches the server again.
	if _, err := s.Viewer(context.Background()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("retry error = %v, want ErrForbidden", err)
	}
	if got := rec.count("/user"); got != 2 {
		t.Fatalf("requests = %d, want 2 (failures are not cached)", got)
	}
}

func TestRateLimitAndScopesAreForwarded(t *testing.T) {
	header := http.Header{}
	header.Set("X-RateLimit-Limit", "5000")
	header.Set("X-RateLimit-Remaining", "4999")
	header.Set("X-RateLimit-Reset", "1800000000")
	header.Set("X-OAuth-Scopes", "repo, workflow")
	rec := newRecorder().serve("/user", `{"login":"octocat"}`).withHeader("/user", header)
	s := newTestStore(t, rec)

	if _, err := s.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}

	rl := s.RateLimit()
	if rl.Limit != 5000 || rl.Remaining != 4999 {
		t.Errorf("rate limit = %+v", rl)
	}
	scopes := s.Scopes()
	if len(scopes) != 2 || scopes[0] != "repo" || scopes[1] != "workflow" {
		t.Errorf("scopes = %v", scopes)
	}
}

// TestRateLimiterThrottlesRequests proves the limiter actually gates outgoing
// calls rather than being decoration.
func TestRateLimiterThrottlesRequests(t *testing.T) {
	rec := newRecorder().serve("/user", `{"login":"octocat"}`)
	// One request per second with a burst of one: the second call must wait.
	s := newTestStore(t, rec, WithRateLimit(1, 1), WithCacheTTL(0))

	start := time.Now()
	for i := 0; i < 2; i++ {
		if _, err := s.Viewer(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Fatalf("two calls took %v, want the limiter to space them out", elapsed)
	}
}

func TestRateLimiterRespectsContextCancellation(t *testing.T) {
	rec := newRecorder().serve("/user", `{"login":"octocat"}`)
	s := newTestStore(t, rec, WithRateLimit(1, 1), WithCacheTTL(0))

	// Consume the burst so the next call must wait.
	if _, err := s.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.Viewer(ctx); err == nil {
		t.Fatal("expected an error when the deadline expires while waiting for the limiter")
	}
	// The throttled call must never reach the network.
	if got := rec.count("/user"); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestCacheClockOverrideIsUsed(t *testing.T) {
	clock := newFakeClock()
	rec := newRecorder().serve("/user", `{"login":"octocat"}`)
	s := newTestStore(t, rec, WithCacheTTL(time.Second), WithClock(clock.Now))

	if _, err := s.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	if _, err := s.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.count("/user"); got != 2 {
		t.Fatalf("requests = %d, want 2 after the injected clock advanced", got)
	}
}
