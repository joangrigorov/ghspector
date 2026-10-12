package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The scope-wide lists are served by the GraphQL search index with the REST
// fan-out behind them. These tests cover what the swap has to preserve: the same
// domain objects, the same display order, a page that follows the cursor the
// previous page ended with, and a fallback that keeps the screen working when the
// index will not answer.

// gqlStub is a GraphQL server. It records the searches it was asked and replies
// with the bodies the test registered, one per request, reusing the last one.
type gqlStub struct {
	mu       sync.Mutex
	searches []searchSeen
	replies  []string
	// status, when set, is the status served instead of 200.
	status int
}

// searchSeen is one search request's variables.
type searchSeen struct {
	q     string
	after string
}

// fixedClock is a clock a test moves by hand, so the search retry window is
// observable without sleeping.
type fixedClock struct {
	mu sync.Mutex
	at time.Time
}

func newFixedClock(at time.Time) *fixedClock { return &fixedClock{at: at} }

func (c *fixedClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func (s *gqlStub) handle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Variables struct {
			Q     string  `json:"q"`
			After *string `json:"after"`
		} `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)

	s.mu.Lock()
	after := ""
	if in.Variables.After != nil {
		after = *in.Variables.After
	}
	s.searches = append(s.searches, searchSeen{q: in.Variables.Q, after: after})
	status, reply := s.status, ""
	if len(s.replies) > 0 {
		reply = s.replies[0]
		if len(s.replies) > 1 {
			s.replies = s.replies[1:]
		}
	}
	s.mu.Unlock()

	if status != 0 {
		w.WriteHeader(status)
	}
	_, _ = fmt.Fprint(w, reply)
}

// seen returns the searches the server was asked, in order.
func (s *gqlStub) seen() []searchSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]searchSeen(nil), s.searches...)
}

// calls is how many searches the server was asked.
func (s *gqlStub) calls() int { return len(s.seen()) }

// repositoriesFixture is the context's repository list for the scope these tests
// use. The owner is the one the search results name, so the fallback's fan-out
// looks for the same repository the search returned.
func repositoriesFixture(t *testing.T, names ...string) string {
	t.Helper()
	repos := make([]map[string]any, 0, len(names))
	for i, name := range names {
		repos = append(repos, map[string]any{
			"id":        i + 1,
			"name":      name,
			"full_name": "octocat/" + name,
			"owner":     map[string]any{"login": "octocat"},
			"pushed_at": now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
		})
	}
	return mustJSON(t, repos)
}

// searchReply builds a search response holding one page.
func searchReply(t *testing.T, total int, cursor string, hasNext bool, nodes ...map[string]any) string {
	t.Helper()
	items := make([]any, 0, len(nodes))
	for _, n := range nodes {
		items = append(items, n)
	}
	return mustJSON(t, map[string]any{"data": map[string]any{
		"rateLimit": map[string]any{
			"limit":     5000,
			"cost":      1,
			"remaining": 4999,
			"resetAt":   "2026-10-12T11:00:00Z",
		},
		"search": map[string]any{
			"issueCount": total,
			"pageInfo":   map[string]any{"endCursor": cursor, "hasNextPage": hasNext},
			"nodes":      items,
		},
	}})
}

// newSearchStore returns a store whose GraphQL transport is the stub and whose
// REST transport is the recorder.
func newSearchStore(t *testing.T, rec *recorder, stub *gqlStub, opts ...Option) *Client {
	t.Helper()
	restSrv := httptest.NewServer(rec.handler())
	t.Cleanup(restSrv.Close)
	gqlSrv := httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(gqlSrv.Close)
	opts = append([]Option{WithCacheTTL(time.Minute)}, opts...)
	return New("test-token", restSrv.URL, gqlSrv.URL, opts...)
}

// repositoryNodeFixture is a repository as the search index returns it.
func repositoryNodeFixture() map[string]any {
	return map[string]any{
		"databaseId":       5,
		"name":             "api",
		"nameWithOwner":    "octocat/api",
		"pushedAt":         "2024-04-29T09:00:00Z",
		"defaultBranchRef": map[string]any{"name": "main"},
		"owner":            map[string]any{"login": "octocat", "databaseId": 1},
	}
}

// repositoryRESTFixture is the same repository as the REST list returns it.
func repositoryRESTFixture() map[string]any {
	return map[string]any{
		"id":             5,
		"name":           "api",
		"full_name":      "octocat/api",
		"pushed_at":      "2024-04-29T09:00:00Z",
		"default_branch": "main",
		"owner":          map[string]any{"login": "octocat", "id": 1},
	}
}

// pullNodeFixture is one pull request as the search index returns it.
func pullNodeFixture() map[string]any {
	return map[string]any{
		"databaseId":  1007,
		"number":      7,
		"title":       "Add the pull request view",
		"body":        "Two paragraphs.",
		"state":       "OPEN",
		"isDraft":     false,
		"url":         "https://github.com/octocat/api/pull/7",
		"createdAt":   "2024-04-30T08:00:00Z",
		"updatedAt":   "2024-05-01T10:00:00Z",
		"mergedAt":    nil,
		"author":      map[string]any{"login": "octocat", "databaseId": 1},
		"assignees":   map[string]any{"nodes": []any{map[string]any{"login": "hubot", "databaseId": 7}}},
		"labels":      map[string]any{"nodes": []any{map[string]any{"name": "enhancement", "color": "a2eeef", "description": "New feature"}}},
		"milestone":   map[string]any{"title": "0.4.0"},
		"baseRefName": "main",
		"headRefName": "feat/prs",
		"headRefOid":  "abcdef1234567890",
		"headRepository": map[string]any{
			"databaseId":       5,
			"name":             "api",
			"nameWithOwner":    "octocat/api",
			"pushedAt":         "2024-04-29T09:00:00Z",
			"defaultBranchRef": map[string]any{"name": "main"},
			"owner":            map[string]any{"login": "octocat", "databaseId": 1},
		},
		"repository": repositoryNodeFixture(),
		"reviewRequests": map[string]any{"nodes": []any{
			map[string]any{"requestedReviewer": map[string]any{"login": "alice", "databaseId": 8}},
		}},
	}
}

// pullRESTFixture is the same pull request as the REST list returns it. The
// fields the app does not read are left out of both, because only the fields it
// reads have to agree.
func pullRESTFixture() map[string]any {
	return map[string]any{
		"id":                  1007,
		"number":              7,
		"title":               "Add the pull request view",
		"body":                "Two paragraphs.",
		"state":               "open",
		"draft":               false,
		"html_url":            "https://github.com/octocat/api/pull/7",
		"created_at":          "2024-04-30T08:00:00Z",
		"updated_at":          "2024-05-01T10:00:00Z",
		"user":                map[string]any{"login": "octocat", "id": 1},
		"assignees":           []any{map[string]any{"login": "hubot", "id": 7}},
		"requested_reviewers": []any{map[string]any{"login": "alice", "id": 8}},
		"labels":              []any{map[string]any{"name": "enhancement", "color": "a2eeef", "description": "New feature"}},
		"milestone":           map[string]any{"title": "0.4.0"},
		"head":                map[string]any{"ref": "feat/prs", "sha": "abcdef1234567890", "label": "octocat:feat/prs"},
		"base":                map[string]any{"ref": "main", "label": "octocat:main"},
		"repository":          repositoryRESTFixture(),
	}
}

// issueNodeFixture is one issue as the search index returns it.
func issueNodeFixture() map[string]any {
	return map[string]any{
		"databaseId": 1003,
		"number":     3,
		"title":      "Add the issues view",
		"body":       "Issues are the third list.",
		"state":      "OPEN",
		"url":        "https://github.com/octocat/api/issues/3",
		"createdAt":  "2024-04-29T09:00:00Z",
		"updatedAt":  "2024-05-01T10:00:00Z",
		"closedAt":   nil,
		"author":     map[string]any{"login": "octocat", "databaseId": 1},
		"assignees":  map[string]any{"nodes": []any{map[string]any{"login": "hubot", "databaseId": 7}}},
		"labels":     map[string]any{"nodes": []any{map[string]any{"name": "enhancement", "color": "a2eeef", "description": "New feature"}}},
		"milestone":  map[string]any{"title": "0.7.0"},
		"repository": repositoryNodeFixture(),
	}
}

// issueRESTFixture is the same issue as the REST list returns it.
func issueRESTFixture() map[string]any {
	return map[string]any{
		"id":         1003,
		"number":     3,
		"title":      "Add the issues view",
		"body":       "Issues are the third list.",
		"state":      "open",
		"html_url":   "https://github.com/octocat/api/issues/3",
		"created_at": "2024-04-29T09:00:00Z",
		"updated_at": "2024-05-01T10:00:00Z",
		"user":       map[string]any{"login": "octocat", "id": 1},
		"assignees":  []any{map[string]any{"login": "hubot", "id": 7}},
		"labels":     []any{map[string]any{"name": "enhancement", "color": "a2eeef", "description": "New feature"}},
		"milestone":  map[string]any{"title": "0.7.0"},
		"repository": repositoryRESTFixture(),
	}
}

// prView is what the app reads out of a pull request. The two transports are
// compared on it rather than on the whole struct, because the search index and
// the REST list endpoint do not carry the same set of fields, and only the set a
// screen reads has to agree for the screen to look the same.
type prView struct {
	ID         int64
	Number     int
	Title      string
	Body       string
	State      string
	Draft      bool
	HTMLURL    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Merged     bool
	Author     string
	Assignees  []string
	Reviewers  []string
	Labels     []string
	Milestone  string
	HeadRef    string
	HeadSHA    string
	HeadLabel  string
	BaseRef    string
	Repository string
	Owner      string
}

func prViewOf(pr PullRequest) prView {
	view := prView{
		ID: pr.ID, Number: pr.Number, Title: pr.Title, Body: pr.Body, State: pr.State,
		Draft: pr.Draft, HTMLURL: pr.HTMLURL, CreatedAt: pr.CreatedAt, UpdatedAt: pr.UpdatedAt,
		Merged: pr.MergedAt != nil, HeadRef: pr.Head.Ref, HeadSHA: pr.Head.SHA,
		HeadLabel: pr.Head.Label, BaseRef: pr.Base.Ref,
		Repository: pr.Repository.FullName,
	}
	if pr.User != nil {
		view.Author = pr.User.Login
	}
	view.Assignees = loginsOf(pr.Assignees)
	view.Reviewers = loginsOf(pr.RequestedReviewers)
	for _, l := range pr.Labels {
		view.Labels = append(view.Labels, l.Name)
	}
	if pr.Milestone != nil {
		view.Milestone = pr.Milestone.Title
	}
	if pr.Repository.Owner != nil {
		view.Owner = pr.Repository.Owner.Login
	}
	return view
}

// issueView is what the app reads out of an issue.
type issueView struct {
	ID         int64
	Number     int
	Title      string
	Body       string
	State      string
	HTMLURL    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Closed     bool
	Author     string
	Assignees  []string
	Labels     []string
	Milestone  string
	Repository string
	Owner      string
}

func issueViewOf(issue Issue) issueView {
	view := issueView{
		ID: issue.ID, Number: issue.Number, Title: issue.Title, Body: issue.Body,
		State: issue.State, HTMLURL: issue.HTMLURL, CreatedAt: issue.CreatedAt,
		UpdatedAt: issue.UpdatedAt, Closed: issue.ClosedAt != nil,
		Repository: issue.Repository.FullName,
	}
	if issue.User != nil {
		view.Author = issue.User.Login
	}
	view.Assignees = loginsOf(issue.Assignees)
	for _, l := range issue.Labels {
		view.Labels = append(view.Labels, l.Name)
	}
	if issue.Milestone != nil {
		view.Milestone = issue.Milestone.Title
	}
	if issue.Repository.Owner != nil {
		view.Owner = issue.Repository.Owner.Login
	}
	return view
}

// loginsOf reads the logins out of a user list.
func loginsOf(users []User) []string {
	if len(users) == 0 {
		return nil
	}
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.Login)
	}
	return out
}

func TestScopePullRequestsComesFromOneSearch(t *testing.T) {
	stub := &gqlStub{replies: []string{searchReply(t, 1, "", false, pullNodeFixture())}}
	rec := newRecorder().serve("/users/octocat/repos", repositoriesFixture(t, "api"))
	s := newSearchStore(t, rec, stub)

	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{State: "open"})
	if err != nil {
		t.Fatalf("ScopePRs: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Number != 7 {
		t.Fatalf("items = %+v, want the pull request from the search", got.Items)
	}
	if stub.calls() != 1 {
		t.Errorf("searches = %d, want one query for the whole scope", stub.calls())
	}
	// The fan-out is what the query replaced, so nothing may go out per
	// repository.
	if n := rec.count("/repos/octocat/api/pulls"); n != 0 {
		t.Errorf("the fan-out was still used: %d requests for one repository", n)
	}
	if q := stub.seen()[0].q; !strings.Contains(q, "user:octocat") || !strings.Contains(q, "is:pr") {
		t.Errorf("search query = %q, want the scope and the kind", q)
	}
}

func TestScopeIssuesComesFromOneSearch(t *testing.T) {
	stub := &gqlStub{replies: []string{searchReply(t, 1, "", false, issueNodeFixture())}}
	rec := newRecorder().serve("/users/octocat/repos", repositoriesFixture(t, "api"))
	s := newSearchStore(t, rec, stub)

	got, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "octocat"}, IssueQuery{})
	if err != nil {
		t.Fatalf("ScopeIssues: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Number != 3 {
		t.Fatalf("items = %+v, want the issue from the search", got.Items)
	}
	// Pull requests are excluded by the query, so the index never returns one to
	// filter out.
	if q := stub.seen()[0].q; !strings.Contains(q, "is:issue") {
		t.Errorf("search query = %q, want issues only", q)
	}
	if n := rec.count("/repos/octocat/api/issues"); n != 0 {
		t.Errorf("the fan-out was still used: %d requests for one repository", n)
	}
}

func TestSearchAndFanOutAgreeOnAPullRequest(t *testing.T) {
	fromSearch, fromFanOut := samePullFromBothTransports(t)

	searched, fanOut := prViewOf(fromSearch), prViewOf(fromFanOut)
	if !reflect.DeepEqual(searched, fanOut) {
		t.Errorf("the transports disagree:\nsearch  %+v\nfan-out %+v", searched, fanOut)
	}
	// The comparison above only proves the two agree, so the shape is pinned
	// once, here.
	want := prView{
		ID: 1007, Number: 7, Title: "Add the pull request view", Body: "Two paragraphs.",
		State: "open", HTMLURL: "https://github.com/octocat/api/pull/7",
		CreatedAt: time.Date(2024, 4, 30, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC),
		Author:    "octocat", Assignees: []string{"hubot"}, Reviewers: []string{"alice"},
		Labels: []string{"enhancement"}, Milestone: "0.4.0",
		HeadRef: "feat/prs", HeadSHA: "abcdef1234567890", HeadLabel: "octocat:feat/prs",
		BaseRef: "main", Repository: "octocat/api", Owner: "octocat",
	}
	if !reflect.DeepEqual(searched, want) {
		t.Errorf("search view = %+v\nwant %+v", searched, want)
	}
}

// samePullFromBothTransports fetches one pull request through the search and
// through the fan-out, from the same fixture.
func samePullFromBothTransports(t *testing.T) (fromSearch, fromFanOut PullRequest) {
	t.Helper()
	owner := OwnerRef{Login: "octocat"}
	query := PRQuery{State: "open"}

	stub := &gqlStub{replies: []string{searchReply(t, 1, "", false, pullNodeFixture())}}
	searched, err := newSearchStore(t, newRecorder(), stub).ScopePRs(context.Background(), owner, query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(searched.Items) != 1 {
		t.Fatalf("search returned %d pull requests, want 1", len(searched.Items))
	}

	// A store whose search is refused is a store on the fan-out path.
	refused := &gqlStub{status: http.StatusInternalServerError, replies: []string{"boom"}}
	rec := newRecorder().
		serve("/users/octocat/repos", repositoriesFixture(t, "api")).
		serve("/repos/octocat/api/pulls", mustJSON(t, []any{pullRESTFixture()}))
	fanned, err := newSearchStore(t, rec, refused).ScopePRs(context.Background(), owner, query)
	if err != nil {
		t.Fatalf("fan-out: %v", err)
	}
	if len(fanned.Items) != 1 {
		t.Fatalf("the fan-out returned %d pull requests, want 1", len(fanned.Items))
	}
	return searched.Items[0], fanned.Items[0]
}

func TestSearchAndFanOutAgreeOnAnIssue(t *testing.T) {
	owner := OwnerRef{Login: "octocat"}

	stub := &gqlStub{replies: []string{searchReply(t, 1, "", false, issueNodeFixture())}}
	searched, err := newSearchStore(t, newRecorder(), stub).ScopeIssues(context.Background(), owner, IssueQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	refused := &gqlStub{status: http.StatusInternalServerError, replies: []string{"boom"}}
	rec := newRecorder().
		serve("/users/octocat/repos", repositoriesFixture(t, "api")).
		serve("/repos/octocat/api/issues", mustJSON(t, []any{issueRESTFixture()}))
	fanned, err := newSearchStore(t, rec, refused).ScopeIssues(context.Background(), owner, IssueQuery{})
	if err != nil {
		t.Fatalf("fan-out: %v", err)
	}

	if len(searched.Items) != 1 || len(fanned.Items) != 1 {
		t.Fatalf("search returned %d and the fan-out %d, want one each", len(searched.Items), len(fanned.Items))
	}
	searchView, fanOutView := issueViewOf(searched.Items[0]), issueViewOf(fanned.Items[0])
	if !reflect.DeepEqual(searchView, fanOutView) {
		t.Errorf("the transports disagree:\nsearch  %+v\nfan-out %+v", searchView, fanOutView)
	}
	want := issueView{
		ID: 1003, Number: 3, Title: "Add the issues view", Body: "Issues are the third list.",
		State: "open", HTMLURL: "https://github.com/octocat/api/issues/3",
		CreatedAt:  time.Date(2024, 4, 29, 9, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC),
		Author:     "octocat",
		Assignees:  []string{"hubot"},
		Labels:     []string{"enhancement"},
		Milestone:  "0.7.0",
		Repository: "octocat/api",
		Owner:      "octocat",
	}
	if !reflect.DeepEqual(searchView, want) {
		t.Errorf("search view = %+v\nwant %+v", searchView, want)
	}
}

func TestScopeSearchNarrowsWithTheQualifiers(t *testing.T) {
	stub := &gqlStub{replies: []string{searchReply(t, 0, "", false)}}
	s := newSearchStore(t, newRecorder(), stub)

	owner := OwnerRef{Login: "acme", IsOrg: true}
	if _, err := s.ScopePRs(context.Background(), owner, PRQuery{
		State: "closed", Repo: "api", Author: "alice", Assignee: "hubot", Reviewer: "carol",
	}); err != nil {
		t.Fatal(err)
	}

	q := stub.seen()[0].q
	for _, want := range []string{
		"org:acme", "is:pr", "state:closed", "repo:acme/api",
		"author:alice", "assignee:hubot", "review-requested:carol",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("search query %q does not carry %q", q, want)
		}
	}
}

func TestScopeSearchFollowsTheCursorToTheNextPage(t *testing.T) {
	stub := &gqlStub{replies: []string{
		// Two matches, one per page.
		searchReply(t, 2, "cursor-1", true, pullNodeFixture()),
		searchReply(t, 2, "", false, pullNodeFixture()),
	}}
	s := newSearchStore(t, newRecorder(), stub)
	owner := OwnerRef{Login: "octocat"}

	first, err := s.ScopePRs(context.Background(), owner, PRQuery{Page: Page{Number: 1, Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore {
		t.Error("HasMore = false on the first of two pages")
	}

	second, err := s.ScopePRs(context.Background(), owner, PRQuery{Page: Page{Number: 2, Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore {
		t.Error("HasMore = true on the last page")
	}
	seen := stub.seen()
	if len(seen) != 2 {
		t.Fatalf("searches = %d, want one per page", len(seen))
	}
	if seen[0].after != "" {
		t.Errorf("the first search carried cursor %q, want none", seen[0].after)
	}
	if seen[1].after != "cursor-1" {
		t.Errorf("the second search carried cursor %q, want the one the first ended with", seen[1].after)
	}
}

func TestScopeSearchEndsWhenTheCountHoldsNothingMore(t *testing.T) {
	// A full page whose count is the page: the list has ended even though the
	// page could not hold more.
	stub := &gqlStub{replies: []string{searchReply(t, 1, "cursor-1", true, pullNodeFixture())}}
	s := newSearchStore(t, newRecorder(), stub)

	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{Page: Page{Number: 1, Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if got.HasMore {
		t.Errorf("HasMore = true for a page holding all %d matches", len(got.Items))
	}
}

func TestScopeSearchCachesItsPages(t *testing.T) {
	stub := &gqlStub{replies: []string{searchReply(t, 1, "", false, pullNodeFixture())}}
	s := newSearchStore(t, newRecorder(), stub)
	owner := OwnerRef{Login: "octocat"}

	for i := 0; i < 3; i++ {
		if _, err := s.ScopePRs(context.Background(), owner, PRQuery{}); err != nil {
			t.Fatal(err)
		}
	}
	if stub.calls() != 1 {
		t.Errorf("searches = %d, want the cached page reused", stub.calls())
	}
}

func TestScopeSearchFallsBackWhenTheIndexRefuses(t *testing.T) {
	// The index answers with an error rather than a page, which is what a scope
	// it will not serve looks like: the fan-out is what keeps the screen working.
	stub := &gqlStub{replies: []string{`{"errors":[{"message":"Could not resolve to an Organization with the login of 'octocat'."}]}`}}
	rec := newRecorder().
		serve("/users/octocat/repos", repositoriesFixture(t, "api")).
		serve("/repos/octocat/api/pulls", mustJSON(t, []any{pullRESTFixture()}))
	s := newSearchStore(t, rec, stub)

	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{State: "open"})
	if err != nil {
		t.Fatalf("a refused search left the list empty: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Number != 7 {
		t.Fatalf("items = %+v, want the fan-out's pull request", got.Items)
	}
	if n := rec.count("/repos/octocat/api/pulls"); n != 1 {
		t.Errorf("repository requests = %d, want the fan-out to have covered it once", n)
	}
}

func TestScopeSearchReportsTheFallbackFailure(t *testing.T) {
	// Both paths fail. The fan-out's reason is the one worth reporting: it is the
	// path that covers the whole scope.
	stub := &gqlStub{replies: []string{`{"errors":[{"message":"Could not resolve to an Organization with the login of 'octocat'."}]}`}}
	rec := newRecorder().
		serve("/users/octocat/repos", repositoriesFixture(t, "api")).
		fail("/repos/octocat/api/pulls", http.StatusForbidden, "no access to this repository")
	s := newSearchStore(t, rec, stub)

	_, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{})
	if err == nil {
		t.Fatal("error = nil, want the fan-out's failure")
	}
	if !strings.Contains(err.Error(), "no access to this repository") {
		t.Errorf("error = %v, want the fan-out's reason", err)
	}
}

func TestScopeSearchIsNotRetriedWhileTheIndexIsDown(t *testing.T) {
	clock := newFixedClock(now)
	stub := &gqlStub{status: http.StatusInternalServerError, replies: []string{"boom"}}
	rec := newRecorder().
		serve("/users/octocat/repos", repositoriesFixture(t, "api")).
		serve("/repos/octocat/api/pulls", mustJSON(t, []any{pullRESTFixture()}))
	s := newSearchStore(t, rec, stub, WithClock(clock.now), WithCacheTTL(0))

	owner := OwnerRef{Login: "octocat"}
	for i := 0; i < 3; i++ {
		if _, err := s.ScopePRs(context.Background(), owner, PRQuery{}); err != nil {
			t.Fatal(err)
		}
	}
	if stub.calls() != 1 {
		t.Errorf("searches = %d, want the index left alone after it failed", stub.calls())
	}

	// Once the window passes, the index is worth another try.
	clock.advance(scopeSearchRetryAfter + time.Second)
	if _, err := s.ScopePRs(context.Background(), owner, PRQuery{}); err != nil {
		t.Fatal(err)
	}
	if stub.calls() != 2 {
		t.Errorf("searches = %d, want the index retried after the window", stub.calls())
	}
}

func TestScopeSearchRetriesAfterATransientFailure(t *testing.T) {
	// A search that fails once and then works is used again: the fallback is not
	// allowed to become sticky.
	clock := newFixedClock(now)
	stub := &gqlStub{
		status:  http.StatusInternalServerError,
		replies: []string{"boom", searchReply(t, 1, "", false, pullNodeFixture())},
	}
	// The status applies to every reply, so the second call needs a fresh stub
	// with the failure cleared.
	s := newSearchStore(t, newRecorder(), stub, WithClock(clock.now))

	if _, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{}); err == nil {
		t.Fatal("error = nil, want the failed search reported once the fan-out fails too")
	}
	stub.mu.Lock()
	stub.status = 0
	stub.mu.Unlock()

	clock.advance(scopeSearchRetryAfter + time.Second)
	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{})
	if err != nil {
		t.Fatalf("ScopePRs after the window: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Number != 7 {
		t.Fatalf("items = %+v, want the search's pull request", got.Items)
	}
}

func TestScopeSearchFailsWhenEveryPathFails(t *testing.T) {
	// Neither path can list anything, so the search's failure is what surfaces.
	stub := &gqlStub{status: http.StatusInternalServerError, replies: []string{"boom"}}
	rec := newRecorder().
		serve("/users/octocat/repos", repositoriesFixture(t, "api")).
		fail("/repos/octocat/api/issues", http.StatusNotFound, "no issues")
	s := newSearchStore(t, rec, stub)

	if _, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "octocat"}, IssueQuery{}); err == nil {
		t.Fatal("error = nil, want a failure")
	}
}

func TestRateLimitReportsTheTighterBudget(t *testing.T) {
	stub := &gqlStub{replies: []string{searchReply(t, 0, "", false)}}
	rec := newRecorder().
		serve("/users/octocat/repos", "[]").
		withHeader("/users/octocat/repos", http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4200"},
		})
	s := newSearchStore(t, rec, stub)

	// Before a search, only the REST figure is known.
	if _, err := s.Repositories(context.Background(), OwnerRef{Login: "octocat"}, Page{}); err != nil {
		t.Fatal(err)
	}
	if got := s.RateLimit(); got.Remaining != 4200 {
		t.Fatalf("remaining = %d, want the REST figure alone", got.Remaining)
	}

	// The search reports 4999 points left, which is more than REST has, so the
	// REST figure is still the one that will cut the app off first.
	if _, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{}); err != nil {
		t.Fatal(err)
	}
	if got := s.RateLimit(); got.Remaining != 4200 || got.Limit != 5000 {
		t.Errorf("remaining = %d of %d, want the tighter budget", got.Remaining, got.Limit)
	}
}

func TestRateLimitPrefersTheSearchBudgetWhenItIsTighter(t *testing.T) {
	stub := &gqlStub{replies: []string{searchReply(t, 0, "", false)}}
	rec := newRecorder().
		serve("/users/octocat/repos", "[]").
		withHeader("/users/octocat/repos", http.Header{
			"X-RateLimit-Limit":     []string{"5000"},
			"X-RateLimit-Remaining": []string{"4200"},
		})
	s := newSearchStore(t, rec, stub)

	if _, err := s.Repositories(context.Background(), OwnerRef{Login: "octocat"}, Page{}); err != nil {
		t.Fatal(err)
	}
	// A search that comes back with less left than REST has is the tighter one.
	stub.mu.Lock()
	stub.replies = []string{mustJSON(t, map[string]any{"data": map[string]any{
		"rateLimit": map[string]any{"limit": 5000, "cost": 1, "remaining": 12, "resetAt": "2026-10-12T11:00:00Z"},
		"search": map[string]any{
			"issueCount": 0,
			"pageInfo":   map[string]any{"endCursor": "", "hasNextPage": false},
			"nodes":      []any{},
		},
	}})}
	stub.mu.Unlock()

	if _, err := s.ScopePRs(context.Background(), OwnerRef{Login: "octocat"}, PRQuery{}); err != nil {
		t.Fatal(err)
	}
	if got := s.RateLimit(); got.Remaining != 12 {
		t.Errorf("remaining = %d, want the search's tighter budget", got.Remaining)
	}
}
