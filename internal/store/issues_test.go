package store

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// issueEntry builds one entry of the issues endpoint. The number is also the
// id, so an assertion can name the rows it expects. A pull marks an entry that
// is really a pull request, which the transport has to drop.
func issueEntry(number int, state, author string, assignees []string, pull bool) map[string]any {
	entry := map[string]any{
		"id":         int64(number),
		"number":     number,
		"title":      fmt.Sprintf("issue %d", number),
		"state":      state,
		"updated_at": now.Add(-time.Duration(number) * time.Minute).Format(time.RFC3339),
		"user":       map[string]any{"login": author},
	}
	if len(assignees) > 0 {
		list := make([]map[string]any, 0, len(assignees))
		for _, login := range assignees {
			list = append(list, map[string]any{"login": login})
		}
		entry["assignees"] = list
	}
	if pull {
		entry["pull_request"] = map[string]any{
			"url": fmt.Sprintf("https://api.github.com/repos/acme/api/pulls/%d", number),
		}
	}
	return entry
}

// issuesFixture builds an issues list response.
func issuesFixture(t *testing.T, issues ...map[string]any) string {
	t.Helper()
	return mustJSON(t, issues)
}

// issueIDs is the identity of an issue list, for an order assertion.
func issueIDs(issues []Issue) []int64 {
	out := make([]int64, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.ID)
	}
	return out
}

func TestScopeIssuesFansOutAndSorts(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		serve("/repos/acme/api/issues", issuesFixture(t,
			issueEntry(1, "open", "octocat", nil, false),
			issueEntry(2, "open", "octocat", nil, false),
		)).
		serve("/repos/acme/web/issues", issuesFixture(t,
			issueEntry(3, "open", "octocat", nil, false),
		))
	s := newTestStore(t, rec)

	got, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "acme"}, IssueQuery{})
	if err != nil {
		t.Fatalf("ScopeIssues: %v", err)
	}

	// The fixture's updated_at falls with the number, so the order is the
	// numbers: 1, 2 and 3 by the time each was last touched.
	want := []int64{1, 2, 3}
	if fmt.Sprint(issueIDs(got.Items)) != fmt.Sprint(want) {
		t.Errorf("issue order = %v, want %v", issueIDs(got.Items), want)
	}
	for i, issue := range got.Items {
		if issue.Repository.Name == "" {
			t.Errorf("issue %d has no repository attached", issue.ID)
			continue
		}
		if want := []string{"api", "api", "web"}[i]; issue.Repository.Name != want {
			t.Errorf("issue %d is from %q, want %q", issue.ID, issue.Repository.Name, want)
		}
		if issue.Repository.FullName != "acme/"+issue.Repository.Name {
			t.Errorf("issue %d carries %q, want a full repository", issue.ID, issue.Repository.FullName)
		}
	}
	if got.HasMore {
		t.Error("HasMore = true for short pages, want false")
	}
}

// TestScopeIssuesDropsPullRequests is the rule the spec names for the issues
// endpoint: it returns both kinds of entry, and only the issues belong in the
// list.
func TestScopeIssuesDropsPullRequests(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api")).
		serve("/repos/acme/api/issues", issuesFixture(t,
			issueEntry(1, "open", "octocat", nil, false),
			issueEntry(2, "open", "octocat", nil, true),
		))
	s := newTestStore(t, rec)

	got, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "acme"}, IssueQuery{})
	if err != nil {
		t.Fatalf("ScopeIssues: %v", err)
	}
	if fmt.Sprint(issueIDs(got.Items)) != fmt.Sprint([]int64{1}) {
		t.Errorf("issues = %v, want the one entry that is not a pull request", issueIDs(got.Items))
	}
}

// TestIssuesHasMoreCountsTheRawPage pins spec §5.2: HasMore is reported from the
// page the API returned, before pull requests are dropped, so a full raw page
// keeps the Load More row even when the filtered list is short.
func TestIssuesHasMoreCountsTheRawPage(t *testing.T) {
	entries := make([]map[string]any, 0, defaultIssuePageSize)
	for i := 0; i < defaultIssuePageSize; i++ {
		// Every entry but the first is a pull request, so the visible list is
		// one row long while the raw page is full.
		entries = append(entries, issueEntry(i+1, "open", "octocat", nil, i > 0))
	}
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api")).
		serve("/repos/acme/api/issues", issuesFixture(t, entries...))
	s := newTestStore(t, rec)

	got, err := s.Issues(context.Background(), RepoRef{Owner: "acme", Repo: "api"}, IssueQuery{})
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("Issues returned %d items, want just the one issue", len(got.Items))
	}
	if !got.HasMore {
		t.Error("HasMore = false for a full raw page, want true")
	}
}

// TestIssuesFiltersTheMergedPage checks the login filters the REST list request
// cannot carry. Every case reads the same cached page, so the filters are proved
// to run over the page rather than over a request each.
func TestIssuesFiltersTheMergedPage(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api")).
		serve("/repos/acme/api/issues", issuesFixture(t,
			issueEntry(1, "open", "octocat", []string{"hubot"}, false),
			issueEntry(2, "open", "alice", []string{"alice", "hubot"}, false),
			issueEntry(3, "open", "hubot", nil, false),
		))
	s := newTestStore(t, rec)

	cases := []struct {
		name string
		q    IssueQuery
		want []int64
	}{
		{"no filter", IssueQuery{}, []int64{1, 2, 3}},
		{"author", IssueQuery{Author: "alice"}, []int64{2}},
		{"assignee", IssueQuery{Assignee: "hubot"}, []int64{1, 2}},
		{"author and assignee", IssueQuery{Author: "hubot", Assignee: "alice"}, nil},
		{"a login nobody uses", IssueQuery{Author: "nobody"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "acme"}, tc.q)
			if err != nil {
				t.Fatalf("ScopeIssues: %v", err)
			}
			if fmt.Sprint(issueIDs(got.Items)) != fmt.Sprint(tc.want) {
				t.Errorf("issues = %v, want %v", issueIDs(got.Items), tc.want)
			}
		})
	}
	if got := rec.count("/repos/acme/api/issues"); got != 1 {
		t.Errorf("the filtered reads made %d requests, want the page fetched once", got)
	}
}

func TestScopeIssuesNarrowsToTheRepoFilter(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		serve("/repos/acme/api/issues", issuesFixture(t, issueEntry(1, "open", "octocat", nil, false))).
		serve("/repos/acme/web/issues", issuesFixture(t, issueEntry(2, "open", "octocat", nil, false)))
	s := newTestStore(t, rec)

	got, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "acme"}, IssueQuery{Repo: "web"})
	if err != nil {
		t.Fatalf("ScopeIssues: %v", err)
	}
	if fmt.Sprint(issueIDs(got.Items)) != fmt.Sprint([]int64{2}) {
		t.Errorf("issues = %v, want just the one from web", issueIDs(got.Items))
	}
	if got := rec.count("/repos/acme/api/issues"); got != 0 {
		t.Errorf("the narrowed fan-out still asked api %d times", got)
	}
}

// TestScopeIssuesSkipsARepositoryItCannotList pins the partial-failure rule: one
// repository failing costs its rows, and the view still shows what it has.
func TestScopeIssuesSkipsARepositoryItCannotList(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		serve("/repos/acme/web/issues", issuesFixture(t, issueEntry(2, "open", "octocat", nil, false))).
		fail("/repos/acme/api/issues", http.StatusInternalServerError, "boom")
	s := newTestStore(t, rec)

	got, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "acme"}, IssueQuery{})
	if err != nil {
		t.Fatalf("a single repository failure should not fail the view: %v", err)
	}
	if fmt.Sprint(issueIDs(got.Items)) != fmt.Sprint([]int64{2}) {
		t.Errorf("issues = %v, want the one repository that answered", issueIDs(got.Items))
	}
}

// TestScopeIssuesFailsWhenEveryRepositoryFails is the other half: the user is
// given a reason rather than an empty list that looks like "no issues".
func TestScopeIssuesFailsWhenEveryRepositoryFails(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		fail("/repos/acme/api/issues", http.StatusInternalServerError, "boom").
		fail("/repos/acme/web/issues", http.StatusInternalServerError, "boom")
	s := newTestStore(t, rec)

	if _, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "acme"}, IssueQuery{}); err == nil {
		t.Fatal("ScopeIssues succeeded with every repository failing")
	}
}

// TestScopeIssuesReportsNoRepositoriesAsEmpty covers the context with nothing in
// it, which is not a failure.
func TestScopeIssuesReportsNoRepositoriesAsEmpty(t *testing.T) {
	rec := newRecorder().serve("/users/acme/repos", reposFixture(t))
	s := newTestStore(t, rec)

	got, err := s.ScopeIssues(context.Background(), OwnerRef{Login: "acme"}, IssueQuery{})
	if err != nil {
		t.Fatalf("ScopeIssues: %v", err)
	}
	if len(got.Items) != 0 || got.HasMore {
		t.Errorf("an empty context gave %d issues, HasMore = %v", len(got.Items), got.HasMore)
	}
}

func TestMergeIssuesKeepsOneRowPerIssue(t *testing.T) {
	existing := []Issue{
		{ID: 1, Number: 1, State: "open", UpdatedAt: now.Add(-time.Hour)},
		{ID: 2, Number: 2, State: "open", UpdatedAt: now},
	}
	incoming := []Issue{
		{ID: 2, Number: 2, State: "closed", UpdatedAt: now.Add(time.Minute)},
		{ID: 3, Number: 3, State: "open", UpdatedAt: now.Add(-2 * time.Hour)},
	}

	merged := MergeIssues(existing, incoming)
	if fmt.Sprint(issueIDs(merged)) != fmt.Sprint([]int64{2, 1, 3}) {
		t.Fatalf("merged = %v, want the new issue folded in and the list resorted", issueIDs(merged))
	}
	if merged[0].State != "closed" {
		t.Errorf("issue 2 is %q, want the polled state to replace the row", merged[0].State)
	}
	if got := MergeIssues(merged, nil); fmt.Sprint(issueIDs(got)) != fmt.Sprint(issueIDs(merged)) {
		t.Error("an empty poll changed the list")
	}
}

func TestSortIssuesBreaksTiesByNumber(t *testing.T) {
	same := now
	issues := []Issue{
		{ID: 1, Number: 7, UpdatedAt: same},
		{ID: 2, Number: 9, UpdatedAt: same},
		{ID: 3, Number: 8, UpdatedAt: same.Add(-time.Second)},
	}
	SortIssues(issues)
	if fmt.Sprint(issueIDs(issues)) != fmt.Sprint([]int64{2, 1, 3}) {
		t.Errorf("order = %v, want the higher number first on an equal timestamp", issueIDs(issues))
	}
}
