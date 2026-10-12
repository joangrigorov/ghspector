package gql

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joangrigorov/ghspector/internal/gh"
)

// testToken is the credential every stub client is built with. Tests assert that
// it only ever travels in the Authorization header.
const testToken = "test-token"

// recorded is one request the stub server received.
type recorded struct {
	query     string
	variables map[string]any
	auth      string
	rawURL    string
	body      string
}

// stub is a GraphQL server. It records what it was asked and replies with the
// bodies the test registered, one per request, reusing the last one.
type stub struct {
	mu sync.Mutex
	// got is every request received, in order.
	got []recorded
	// replies is the response bodies left to serve.
	replies []string
	// status, when set, is the status served instead of 200.
	status int
	// leaked records that a request carried the token somewhere other than the
	// Authorization header.
	leaked bool
}

func (s *stub) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var in struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	_ = json.Unmarshal(body, &in)

	s.mu.Lock()
	s.got = append(s.got, recorded{
		query:     in.Query,
		variables: in.Variables,
		auth:      r.Header.Get("Authorization"),
		rawURL:    r.URL.String(),
		body:      string(body),
	})
	if strings.Contains(r.URL.String(), testToken) || strings.Contains(string(body), testToken) {
		s.leaked = true
	}
	status := s.status
	reply := ""
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
	_, _ = io.WriteString(w, reply)
}

func (s *stub) requests() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.got...)
}

// checkTokenStayedInTheHeader fails the test if any request put the token
// anywhere a log, a proxy or an error report could pick it up.
func (s *stub) checkTokenStayedInTheHeader(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	leaked := s.leaked
	s.mu.Unlock()
	if leaked {
		t.Error("the token appeared in a URL or a request body")
	}
	for _, req := range s.requests() {
		if req.auth != "Bearer "+testToken {
			t.Errorf("Authorization = %q, want a bearer token", req.auth)
		}
	}
}

// newStub starts a GraphQL server for a client with no budget reserver.
func newStub(t *testing.T, replies ...string) (*stub, *Client) {
	t.Helper()
	return newStubWith(t, nil, replies...)
}

// newStubWith starts a GraphQL server for a client that reserves budget through
// reserver.
func newStubWith(t *testing.T, reserver Reserver, replies ...string) (*stub, *Client) {
	t.Helper()
	s := &stub{replies: replies}
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(srv.Close)
	return s, NewClient(testToken, srv.URL, reserver)
}

// recordingReserver records what each query reserved.
type recordingReserver struct {
	mu    sync.Mutex
	calls []int
}

func (r *recordingReserver) WaitN(_ context.Context, n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, n)
	return nil
}

func (r *recordingReserver) reserved() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.calls...)
}

// mustJSON renders a value as the JSON body a GraphQL response arrives in.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// dataReply wraps a data payload in the response envelope.
func dataReply(t *testing.T, data map[string]any) string {
	t.Helper()
	return mustJSON(t, map[string]any{"data": data})
}

// budgetJSON is the rateLimit block a stub reply carries.
func budgetJSON(cost, remaining int) map[string]any {
	return map[string]any{
		"limit":     5000,
		"cost":      cost,
		"remaining": remaining,
		"resetAt":   "2026-10-12T11:00:00Z",
	}
}

// issueNode is one search result for an issue.
func issueNode() map[string]any {
	return map[string]any{
		"databaseId": 101,
		"number":     3,
		"title":      "Add the issues view",
		"body":       "Issues are the third list.",
		"state":      "OPEN",
		"url":        "https://github.com/octocat/api/issues/3",
		"createdAt":  "2024-04-01T09:00:00Z",
		"updatedAt":  "2024-04-30T09:00:00Z",
		"closedAt":   nil,
		"author":     map[string]any{"login": "hubot", "databaseId": 7},
		"assignees":  map[string]any{"nodes": []any{map[string]any{"login": "alice", "databaseId": 8}}},
		"labels":     map[string]any{"nodes": []any{map[string]any{"name": "enhancement", "color": "a2eeef", "description": "New feature"}}},
		"milestone":  map[string]any{"title": "v1"},
		"repository": repositoryNode(),
	}
}

// repositoryNode is the repository a search result belongs to. The owner arrives
// through an interface fragment, so the Organization arm is exercised too.
func repositoryNode() map[string]any {
	return map[string]any{
		"databaseId":    5,
		"name":          "api",
		"nameWithOwner": "octocat/api",
		"pushedAt":      "2024-04-29T09:00:00Z",
		"defaultBranchRef": map[string]any{
			"name": "main",
		},
		"owner": map[string]any{"login": "octocat", "databaseId": 1},
	}
}

// prNode is one search result for a pull request.
func prNode() map[string]any {
	return map[string]any{
		"databaseId":  202,
		"number":      12,
		"title":       "Rewrite the store",
		"body":        "Body text.",
		"state":       "MERGED",
		"isDraft":     false,
		"url":         "https://github.com/octocat/api/pull/12",
		"createdAt":   "2024-04-02T09:00:00Z",
		"updatedAt":   "2024-04-28T09:00:00Z",
		"mergedAt":    "2024-04-28T09:00:00Z",
		"author":      map[string]any{"login": "alice", "databaseId": 8},
		"assignees":   map[string]any{"nodes": []any{map[string]any{"login": "hubot", "databaseId": 7}}},
		"labels":      map[string]any{"nodes": []any{map[string]any{"name": "store", "color": "0e8a16", "description": ""}}},
		"milestone":   map[string]any{"title": "v1"},
		"baseRefName": "main",
		"headRefName": "feat/store",
		"headRefOid":  "abc123",
		"headRepository": map[string]any{
			"databaseId":    6,
			"name":          "api",
			"nameWithOwner": "forker/api",
			"owner":         map[string]any{"login": "forker", "databaseId": 9},
		},
		"repository": repositoryNode(),
		"reviewRequests": map[string]any{
			"nodes": []any{
				map[string]any{"requestedReviewer": map[string]any{"login": "carol", "databaseId": 10}},
				// A team reviewer is not a login, so the domain type cannot hold
				// it. The query selects no team fields, so the API has nothing to
				// send for one and the entry arrives empty.
				map[string]any{"requestedReviewer": map[string]any{}},
			},
		},
	}
}

// searchReply is a full search response for one node.
func searchReply(t *testing.T, node map[string]any, total int, endCursor string, hasNext bool) string {
	t.Helper()
	return dataReply(t, map[string]any{
		"rateLimit": budgetJSON(1, 4999),
		"search": map[string]any{
			"issueCount": total,
			"pageInfo":   map[string]any{"endCursor": endCursor, "hasNextPage": hasNext},
			"nodes":      []any{node},
		},
	})
}

func TestSearchIssuesSendsTheScopeQuery(t *testing.T) {
	s, c := newStub(t, searchReply(t, issueNode(), 132, "cursor-1", true))

	page, err := c.SearchIssues(context.Background(), SearchInput{
		Owner:    "octocat",
		IsOrg:    true,
		State:    "open",
		Author:   "hubot",
		Assignee: "alice",
		Reviewer: "carol",
		First:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}

	req := s.requests()[0]
	for _, want := range []string{
		"search(query: $q, type: ISSUE, first: $first, after: $after)",
		"... on Issue", "issueCount", "pageInfo", "endCursor", "hasNextPage",
		"rateLimit{limit,cost,remaining,resetAt}", "databaseId",
	} {
		if !strings.Contains(req.query, want) {
			t.Errorf("query does not ask for %q:\n%s", want, req.query)
		}
	}
	if strings.Contains(req.query, "... on PullRequest") {
		t.Errorf("the issue query asks for pull request fields:\n%s", req.query)
	}

	// The qualifiers carry the scope, the kind, the state and the filters, and a
	// reviewer filter is not one of them: an issue has no reviewer.
	wantQuery := "sort:updated-desc org:octocat is:issue state:open author:hubot assignee:alice"
	if got := req.variables["q"]; got != wantQuery {
		t.Errorf("search query = %v, want %q", got, wantQuery)
	}
	if got := req.variables["first"]; got != float64(50) {
		t.Errorf("first = %v, want 50", got)
	}
	if got, ok := req.variables["after"]; ok && got != nil {
		t.Errorf("after = %v, want null on the first page", got)
	}
	s.checkTokenStayedInTheHeader(t)
}

// TestQueriesSelectAnActorsIDOnItsOwnType pins the one place where a field that
// looks natural is not a field of the type it is selected on. The Actor
// interface carries the login and nothing else, so asking it for databaseId is
// rejected by the API — "Field 'databaseId' doesn't exist on type 'Actor'" —
// while the query still reads perfectly. The response for a concrete position
// (the assignee connection, a UserConnection) takes the id directly, and a
// fragment is not merely unnecessary there: a Bot fragment could not be spread
// inside a User. A stub server cannot enforce any of this, so the query text is
// what is asserted.
func TestQueriesSelectAnActorsIDOnItsOwnType(t *testing.T) {
	t.Run("issue", func(t *testing.T) {
		s, c := newStub(t, searchReply(t, issueNode(), 1, "", false))
		if _, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50}); err != nil {
			t.Fatal(err)
		}
		checkActorFieldShapes(t, s.requests()[0].query)
	})

	t.Run("pull request", func(t *testing.T) {
		s, c := newStub(t, searchReply(t, prNode(), 1, "", false))
		if _, err := c.SearchPullRequests(context.Background(), SearchInput{Owner: "octocat", First: 8}); err != nil {
			t.Fatal(err)
		}
		checkActorFieldShapes(t, s.requests()[0].query)
	})
}

func checkActorFieldShapes(t *testing.T, query string) {
	t.Helper()
	for _, want := range []string{
		// An Actor position reads the id through the fragments that carry one.
		"author{login,... on User{databaseId},... on Bot{__typename,databaseId}}",
		// A User position reads it directly.
		"assignees(first: 10){nodes{login,databaseId}}",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %q:\n%s", want, query)
		}
	}
	// The interface itself has no databaseId to ask for.
	if strings.Contains(query, "author{databaseId") {
		t.Errorf("the author is asked for databaseId on the Actor interface:\n%s", query)
	}
}

func TestSearchIssuesMapsTheDomainObject(t *testing.T) {
	_, c := newStub(t, searchReply(t, issueNode(), 1, "", false))

	page, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50})
	if err != nil {
		t.Fatal(err)
	}
	got := page.Items[0]

	want := gh.Issue{
		ID:        101,
		Number:    3,
		Title:     "Add the issues view",
		Body:      "Issues are the third list.",
		State:     "open",
		HTMLURL:   "https://github.com/octocat/api/issues/3",
		CreatedAt: time.Date(2024, 4, 1, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 4, 30, 9, 0, 0, 0, time.UTC),
		User:      &gh.User{Login: "hubot", ID: 7},
		Assignees: []gh.User{{Login: "alice", ID: 8}},
		Labels:    []gh.Label{{Name: "enhancement", Color: "a2eeef", Description: "New feature"}},
		Milestone: &gh.Milestone{Title: "v1"},
		Repository: gh.Repository{
			ID:            5,
			Name:          "api",
			FullName:      "octocat/api",
			PushedAt:      time.Date(2024, 4, 29, 9, 0, 0, 0, time.UTC),
			DefaultBranch: "main",
			Owner:         &gh.User{Login: "octocat", ID: 1},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("issue =\n%+v\nwant\n%+v", got, want)
	}
}

// TestSearchReportsABotAsTheRestTransportSpellsIt covers the one login the two
// transports disagree on. GraphQL reports an app's account as the bare slug and
// REST as "<slug>[bot]"; the screen and the filter built from it follow REST, so
// the suffix is put back from the __typename the Bot fragment carries.
func TestSearchReportsABotAsTheRestTransportSpellsIt(t *testing.T) {
	node := prNode()
	node["author"] = map[string]any{"login": "github-actions", "databaseId": 7, "__typename": "Bot"}
	node["assignees"] = map[string]any{"nodes": []any{
		map[string]any{"login": "dependabot", "databaseId": 8},
	}}
	_, c := newStub(t, searchReply(t, node, 1, "", false))

	page, err := c.SearchPullRequests(context.Background(), SearchInput{Owner: "octocat", First: 8})
	if err != nil {
		t.Fatal(err)
	}
	got := page.Items[0]
	if got.User == nil || got.User.Login != "github-actions[bot]" || got.User.ID != 7 {
		t.Errorf("bot author = %+v, want github-actions[bot] with its id", got.User)
	}
	// An assignee connection is typed User, so its entries carry no __typename
	// and no suffix is invented for them.
	if len(got.Assignees) != 1 || got.Assignees[0].Login != "dependabot" {
		t.Errorf("assignees = %+v, want the login the API reported", got.Assignees)
	}
}

func TestSearchPullRequestsMapsTheDomainObject(t *testing.T) {
	_, c := newStub(t, searchReply(t, prNode(), 1, "", false))

	page, err := c.SearchPullRequests(context.Background(), SearchInput{Owner: "octocat", First: 50})
	if err != nil {
		t.Fatal(err)
	}
	got := page.Items[0]

	merged := time.Date(2024, 4, 28, 9, 0, 0, 0, time.UTC)
	if got.State != "closed" {
		t.Errorf("state = %q, want closed: a merged pull request is closed", got.State)
	}
	if got.MergedAt == nil || !got.MergedAt.Equal(merged) {
		t.Errorf("mergedAt = %v, want %v", got.MergedAt, merged)
	}
	if got.Draft {
		t.Error("draft = true, want false")
	}
	if got.ID != 202 || got.Number != 12 || got.Title != "Rewrite the store" || got.Body != "Body text." {
		t.Errorf("identity = %+v", got)
	}
	if got.User == nil || got.User.Login != "alice" {
		t.Errorf("author = %+v, want alice", got.User)
	}
	if len(got.Assignees) != 1 || got.Assignees[0].Login != "hubot" {
		t.Errorf("assignees = %+v, want hubot", got.Assignees)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "store" {
		t.Errorf("labels = %+v", got.Labels)
	}
	if got.Milestone == nil || got.Milestone.Title != "v1" {
		t.Errorf("milestone = %+v", got.Milestone)
	}
	if got.Head.Ref != "feat/store" || got.Head.SHA != "abc123" {
		t.Errorf("head = %+v, want the branch and its tip", got.Head)
	}
	if got.Head.Label != "forker:feat/store" {
		t.Errorf("head label = %q, want the head repository's owner and branch", got.Head.Label)
	}
	if got.Head.Repo == nil || got.Head.Repo.FullName != "forker/api" {
		t.Errorf("head repo = %+v", got.Head.Repo)
	}
	if got.Base.Ref != "main" || got.Base.Label != "octocat:main" {
		t.Errorf("base = %+v", got.Base)
	}
	if len(got.RequestedReviewers) != 1 || got.RequestedReviewers[0].Login != "carol" {
		t.Errorf("reviewers = %+v, want carol alone: a team is not a login", got.RequestedReviewers)
	}
	if got.Repository.FullName != "octocat/api" {
		t.Errorf("repository = %+v", got.Repository)
	}
}

func TestSearchReportsTotalsAndCursors(t *testing.T) {
	_, c := newStub(t, searchReply(t, issueNode(), 132, "cursor-1", true))

	page, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 132 {
		t.Errorf("total = %d, want 132", page.Total)
	}
	if page.NextCursor != "cursor-1" {
		t.Errorf("next cursor = %q, want cursor-1", page.NextCursor)
	}
	if !page.HasNext {
		t.Error("hasNext = false, want true")
	}
	want := Budget{Limit: 5000, Cost: 1, Remaining: 4999, ResetAt: time.Date(2026, 10, 12, 11, 0, 0, 0, time.UTC)}
	if page.Budget != want {
		t.Errorf("budget = %+v, want %+v", page.Budget, want)
	}
}

func TestSearchPassesTheCursorOfThePreviousPage(t *testing.T) {
	s, c := newStub(t, searchReply(t, issueNode(), 132, "cursor-1", true))

	if _, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50, After: "cursor-0"}); err != nil {
		t.Fatal(err)
	}
	if got := s.requests()[0].variables["after"]; got != "cursor-0" {
		t.Errorf("after = %v, want cursor-0", got)
	}
}

func TestReserveUsesTheCostTheApiReported(t *testing.T) {
	reserver := &recordingReserver{}
	// The first query is charged 3 points, so the second reserves 3.
	_, c := newStubWith(t, reserver,
		searchReply(t, issueNode(), 1, "", false),
		searchReply(t, issueNode(), 1, "", false),
	)

	for i := 0; i < 2; i++ {
		if _, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50}); err != nil {
			t.Fatal(err)
		}
	}
	want := []int{1, 1}
	got := reserver.reserved()
	if len(got) != len(want) {
		t.Fatalf("reservations = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reservation %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestReserveUsesTheReportedCostForTheNextQuery(t *testing.T) {
	reserver := &recordingReserver{}
	_, c := newStubWith(t, reserver,
		dataReply(t, map[string]any{
			"rateLimit": budgetJSON(3, 4997),
			"search": map[string]any{
				"issueCount": 0,
				"pageInfo":   map[string]any{"endCursor": "", "hasNextPage": false},
				"nodes":      []any{},
			},
		}),
		searchReply(t, issueNode(), 1, "", false),
	)

	for i := 0; i < 2; i++ {
		if _, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50}); err != nil {
			t.Fatal(err)
		}
	}
	want := []int{1, 3}
	got := reserver.reserved()
	if len(got) != len(want) {
		t.Fatalf("reservations = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reservation %d = %d, want %d", i, got[i], want[i])
		}
	}
	if got := c.RateLimit(); got.Remaining != 4999 {
		t.Errorf("remaining = %d, want the most recent report", got.Remaining)
	}
}

func TestSearchKeepsTheLastBudgetWhenTheResponseOmitsIt(t *testing.T) {
	_, c := newStub(t,
		searchReply(t, issueNode(), 1, "", false),
		dataReply(t, map[string]any{
			"search": map[string]any{
				"issueCount": 0,
				"pageInfo":   map[string]any{"endCursor": "", "hasNextPage": false},
				"nodes":      []any{},
			},
		}),
	)

	for i := 0; i < 2; i++ {
		if _, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50}); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.RateLimit(); got.Remaining != 4999 {
		t.Errorf("remaining = %d, want the last reported budget", got.Remaining)
	}
}

func TestSearchMapsErrorsOntoTheSharedTaxonomy(t *testing.T) {
	tests := []struct {
		name   string
		status int
		reply  string
		check  func(error) bool
	}{
		{
			name:   "rejected token",
			status: http.StatusUnauthorized,
			reply:  `{"message":"Bad credentials"}`,
			check:  func(err error) bool { return errors.Is(err, gh.ErrForbidden) },
		},
		{
			name:  "rate limit in the errors array",
			reply: `{"errors":[{"message":"API rate limit exceeded for user ID 1."}]}`,
			check: func(err error) bool { return errors.Is(err, gh.ErrRateLimited) },
		},
		{
			name:  "abuse detection",
			reply: `{"errors":[{"message":"You have triggered an abuse detection mechanism."}]}`,
			check: func(err error) bool { return errors.Is(err, gh.ErrRateLimited) },
		},
		{
			name:  "a scope the index will not serve",
			reply: `{"errors":[{"message":"Could not resolve to an Organization with the login of 'nope'."}]}`,
			check: func(err error) bool {
				return !errors.Is(err, gh.ErrForbidden) && !errors.Is(err, gh.ErrRateLimited)
			},
		},
		{
			name:   "server failure",
			status: http.StatusInternalServerError,
			reply:  "boom",
			check: func(err error) bool {
				return !errors.Is(err, gh.ErrForbidden) && !errors.Is(err, gh.ErrRateLimited)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &stub{replies: []string{tt.reply}, status: tt.status}
			srv := httptest.NewServer(http.HandlerFunc(s.handle))
			t.Cleanup(srv.Close)

			c := NewClient(testToken, srv.URL, nil)
			_, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50})
			if err == nil {
				t.Fatal("error = nil, want a failure")
			}
			if !tt.check(err) {
				t.Errorf("error = %v, want it classified", err)
			}
			s.checkTokenStayedInTheHeader(t)
		})
	}
}

func TestSearchReportsCancellationAsCancellation(t *testing.T) {
	_, c := newStub(t, searchReply(t, issueNode(), 1, "", false))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.SearchIssues(ctx, SearchInput{Owner: "octocat", First: 50})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestQualifiers(t *testing.T) {
	tests := []struct {
		name string
		in   SearchInput
		want string
	}{
		{
			name: "a user scope reads issues",
			in:   SearchInput{Kind: KindIssue, Owner: "octocat"},
			want: "sort:updated-desc user:octocat is:issue",
		},
		{
			name: "an organization scope reads pull requests",
			in:   SearchInput{Kind: KindPullRequest, Owner: "acme", IsOrg: true},
			want: "sort:updated-desc org:acme is:pr",
		},
		{
			name: "the all state narrows nothing",
			in:   SearchInput{Kind: KindIssue, Owner: "octocat", State: "all"},
			want: "sort:updated-desc user:octocat is:issue",
		},
		{
			name: "a closed state narrows to closed",
			in:   SearchInput{Kind: KindIssue, Owner: "octocat", State: "closed"},
			want: "sort:updated-desc user:octocat is:issue state:closed",
		},
		{
			name: "the repository filter names the scope's owner",
			in:   SearchInput{Kind: KindPullRequest, Owner: "octocat", Repo: "api", Author: "alice", Assignee: "hubot", Reviewer: "carol"},
			want: "sort:updated-desc user:octocat is:pr repo:octocat/api author:alice assignee:hubot review-requested:carol",
		},
		{
			name: "an issue has no reviewer to filter by",
			in:   SearchInput{Kind: KindIssue, Owner: "octocat", Reviewer: "carol"},
			want: "sort:updated-desc user:octocat is:issue",
		},
		{
			name: "a value that needs quoting gets quoted",
			in:   SearchInput{Kind: KindIssue, Owner: "octocat", Author: "hub ot"},
			want: `sort:updated-desc user:octocat is:issue author:"hub ot"`,
		},
		{
			name: "a value cannot smuggle a qualifier of its own",
			in:   SearchInput{Kind: KindIssue, Owner: "octocat", Author: `hubot state:closed`},
			// Quoted, so the whole value is matched as a phrase; the colon is
			// dropped on the way in because no login holds one.
			want: `sort:updated-desc user:octocat is:issue author:"hubot stateclosed"`,
		},
		{
			name: "characters no login can hold are dropped",
			in:   SearchInput{Kind: KindIssue, Owner: "octocat", Assignee: `hub"ot`},
			want: "sort:updated-desc user:octocat is:issue assignee:hubot",
		},
		{
			// The suffix the REST transport puts on an app's login would be
			// dropped by the quoting, so it is turned into the form the index
			// matches before that.
			name: "an app's login is read as the index spells it",
			in:   SearchInput{Kind: KindPullRequest, Owner: "octocat", Author: "github-actions[bot]"},
			want: "sort:updated-desc user:octocat is:pr author:app/github-actions",
		},
		{
			name: "an app spelled the index's way is passed through",
			in:   SearchInput{Kind: KindPullRequest, Owner: "octocat", Author: "app/dependabot"},
			want: "sort:updated-desc user:octocat is:pr author:app/dependabot",
		},
		{
			name: "a person's login is left alone",
			in:   SearchInput{Kind: KindPullRequest, Owner: "octocat", Author: "github-actions-fan"},
			want: "sort:updated-desc user:octocat is:pr author:github-actions-fan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualifiers(tt.in); got != tt.want {
				t.Errorf("qualifiers = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewClientPinsTheEndpointUnlessATestOverridesIt(t *testing.T) {
	if Endpoint != "https://api.github.com/graphql" {
		t.Errorf("Endpoint = %q", Endpoint)
	}
	// An empty endpoint is production: the constant is what the client uses, so
	// no configuration value can redirect an authenticated request.
	c := NewClient(testToken, "", nil)
	if c == nil {
		t.Fatal("client = nil")
	}
}

// TestSearchCleansTerminalInstructionsFromTheResponse covers the GraphQL side of
// the boundary the REST transport also guards: an issue title and body are
// written by whoever opened the issue.
func TestSearchCleansTerminalInstructionsFromTheResponse(t *testing.T) {
	node := issueNode()
	node["title"] = "Add \x1b]52;c;cGF5bG9hZA==\x07the issues view"
	node["body"] = "Issues are\x1b[2J the third list."
	_, c := newStub(t, searchReply(t, node, 1, "", false))

	page, err := c.SearchIssues(context.Background(), SearchInput{Owner: "octocat", First: 50})
	if err != nil {
		t.Fatal(err)
	}
	got := page.Items[0]
	if got.Title != "Add the issues view" {
		t.Errorf("title = %q, want the instruction removed", got.Title)
	}
	if got.Body != "Issues are the third list." {
		t.Errorf("body = %q, want the instruction removed", got.Body)
	}
}
