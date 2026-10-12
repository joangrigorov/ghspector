package store

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// now is the fixed instant run fixtures are built around.
var now = time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

// reposFixture builds a repository list response, most recently pushed first.
func reposFixture(t *testing.T, names ...string) string {
	t.Helper()
	repos := make([]map[string]any, 0, len(names))
	for i, name := range names {
		repos = append(repos, map[string]any{
			"id":        i + 1,
			"name":      name,
			"full_name": "acme/" + name,
			"owner":     map[string]any{"login": "acme"},
			"pushed_at": now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
		})
	}
	return mustJSON(t, repos)
}

// runsFixture builds a runs list response.
func runsFixture(t *testing.T, runs ...map[string]any) string {
	t.Helper()
	return mustJSON(t, map[string]any{"total_count": len(runs), "workflow_runs": runs})
}

// run builds one run fixture. Status and conclusion drive both sorting and the
// approval rules, so tests set them explicitly.
func run(id int64, repo, status, conclusion string, createdAt time.Time) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        fmt.Sprintf("run %d", id),
		"status":      status,
		"conclusion":  conclusion,
		"created_at":  createdAt.Format(time.RFC3339),
		"run_attempt": 1,
		"repository": map[string]any{
			"name":      repo,
			"full_name": "acme/" + repo,
			"owner":     map[string]any{"login": "acme"},
		},
		"actor": map[string]any{"login": "octocat"},
	}
}

// jobFixture builds one job entry.
func jobFixture(id int64, status string, started time.Time) map[string]any {
	return map[string]any{
		"id":         id,
		"run_id":     7,
		"name":       fmt.Sprintf("job %d", id),
		"status":     status,
		"started_at": started.Format(time.RFC3339),
	}
}

func TestScopeRunsFansOutAndSorts(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		serve("/repos/acme/api/actions/runs", runsFixture(t,
			run(1, "api", "completed", "success", now.Add(-2*time.Hour)),
			run(2, "api", "in_progress", "", now.Add(-time.Hour)),
		)).
		serve("/repos/acme/web/actions/runs", runsFixture(t,
			run(3, "web", "completed", "failure", now.Add(-3*time.Hour)),
		))
	s := newTestStore(t, rec)

	got, err := s.ScopeRuns(context.Background(), OwnerRef{Login: "acme"}, RunQuery{})
	if err != nil {
		t.Fatalf("ScopeRuns: %v", err)
	}

	// The running run sorts first, then the finished ones by recency.
	want := []int64{2, 1, 3}
	if fmt.Sprint(ids(got.Items)) != fmt.Sprint(want) {
		t.Errorf("run order = %v, want %v", ids(got.Items), want)
	}
	for _, r := range got.Items {
		if r.Repository.Name == "" {
			t.Errorf("run %d has no repository attached", r.ID)
		}
	}
	if n := rec.count("/repos/acme/api/actions/runs"); n != 1 {
		t.Errorf("the first repository was queried %d times, want 1", n)
	}
	if got.HasMore {
		t.Error("HasMore = true for short pages, want false")
	}
}

func TestScopeRunsCapsTheFanOut(t *testing.T) {
	names := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10"}
	rec := newRecorder().serve("/users/acme/repos", reposFixture(t, names...))
	for _, n := range names {
		rec.serve("/repos/acme/"+n+"/actions/runs", runsFixture(t))
	}
	s := newTestStore(t, rec)

	if _, err := s.ScopeRuns(context.Background(), OwnerRef{Login: "acme"}, RunQuery{}); err != nil {
		t.Fatalf("ScopeRuns: %v", err)
	}

	queried := 0
	for _, n := range names {
		if rec.count("/repos/acme/"+n+"/actions/runs") > 0 {
			queried++
		}
	}
	if queried != maxAggregateRepos {
		t.Errorf("queried %d repositories, want the cap of %d", queried, maxAggregateRepos)
	}
}

func TestScopeRunsNarrowsToTheFilteredRepository(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		serve("/repos/acme/web/actions/runs", runsFixture(t))
	s := newTestStore(t, rec)

	if _, err := s.ScopeRuns(context.Background(), OwnerRef{Login: "acme"}, RunQuery{Repo: "web"}); err != nil {
		t.Fatalf("ScopeRuns: %v", err)
	}

	if n := rec.count("/repos/acme/api/actions/runs"); n != 0 {
		t.Errorf("the unfiltered repository was queried %d times, want 0", n)
	}
	if n := rec.count("/repos/acme/web/actions/runs"); n != 1 {
		t.Errorf("the filtered repository was queried %d times, want 1", n)
	}
}

func TestScopeRunsSkipsRepositoriesThatFail(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "broken", "api")).
		fail("/repos/acme/broken/actions/runs", http.StatusNotFound, "no actions").
		serve("/repos/acme/api/actions/runs", runsFixture(t,
			run(1, "api", "completed", "success", now),
		))
	s := newTestStore(t, rec)

	got, err := s.ScopeRuns(context.Background(), OwnerRef{Login: "acme"}, RunQuery{})
	if err != nil {
		t.Fatalf("ScopeRuns returned an error for one failing repository: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != 1 {
		t.Errorf("got %d runs, want the one from the repository that answered", len(got.Items))
	}
}

func TestScopeRunsReportsAnErrorWhenEveryRepositoryFails(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "one", "two")).
		fail("/repos/acme/one/actions/runs", http.StatusNotFound, "no actions").
		fail("/repos/acme/two/actions/runs", http.StatusNotFound, "no actions")
	s := newTestStore(t, rec)

	if _, err := s.ScopeRuns(context.Background(), OwnerRef{Login: "acme"}, RunQuery{}); err == nil {
		t.Fatal("ScopeRuns returned no error although every repository failed")
	}
}

func TestScopeRunsReportsMoreWhenAnyRepositoryHasAFullPage(t *testing.T) {
	full := make([]map[string]any, 0, defaultRunPageSize)
	for i := 0; i < defaultRunPageSize; i++ {
		full = append(full, run(int64(i+1), "api", "completed", "success", now))
	}
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api")).
		serve("/repos/acme/api/actions/runs", runsFixture(t, full...))
	s := newTestStore(t, rec)

	got, err := s.ScopeRuns(context.Background(), OwnerRef{Login: "acme"}, RunQuery{})
	if err != nil {
		t.Fatalf("ScopeRuns: %v", err)
	}
	if !got.HasMore {
		t.Error("HasMore = false for a full page, want true")
	}
}

func TestScopeRunsRechecksTheActorClientSide(t *testing.T) {
	other := run(9, "api", "completed", "success", now)
	other["actor"] = map[string]any{"login": "someone-else"}
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api")).
		serve("/repos/acme/api/actions/runs", runsFixture(t,
			run(1, "api", "completed", "success", now), other))
	s := newTestStore(t, rec)

	got, err := s.ScopeRuns(context.Background(), OwnerRef{Login: "acme"}, RunQuery{Actor: "octocat"})
	if err != nil {
		t.Fatalf("ScopeRuns: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != 1 {
		t.Fatalf("got %v, want only the run by the filtered actor", ids(got.Items))
	}
	if q := rec.query("/repos/acme/api/actions/runs").Get("actor"); q != "octocat" {
		t.Errorf("actor query param = %q, want it forwarded to the API too", q)
	}
}

func TestMergeRunsReplacesByIDAndResorts(t *testing.T) {
	existing := []WorkflowRun{
		{ID: 1, Status: "in_progress", CreatedAt: now},
		{ID: 2, Status: "completed", Conclusion: "success", CreatedAt: now.Add(-time.Hour)},
	}
	incoming := []WorkflowRun{
		{ID: 1, Status: "completed", Conclusion: "success", CreatedAt: now},
		{ID: 3, Status: "queued", CreatedAt: now.Add(time.Minute)},
	}

	merged := MergeRuns(existing, incoming)
	if len(merged) != 3 {
		t.Fatalf("merged to %d runs, want 3: %v", len(merged), ids(merged))
	}
	if merged[0].ID != 3 || merged[0].Status != "queued" {
		t.Errorf("first run = %+v, want the queued one", merged[0])
	}
	for _, r := range merged {
		if r.ID == 1 && r.Status != "completed" {
			t.Errorf("run 1 status = %q, want the polled status", r.Status)
		}
	}
}

func TestMergeRunsKeepsTheListWhenNothingArrives(t *testing.T) {
	existing := []WorkflowRun{{ID: 1}}
	if got := MergeRuns(existing, nil); len(got) != 1 {
		t.Errorf("got %d runs, want the existing list untouched", len(got))
	}
}

func TestSortRunsOrdersByStatusThenRecency(t *testing.T) {
	runs := []WorkflowRun{
		{ID: 1, Status: "completed", CreatedAt: now},
		{ID: 2, Status: "queued", CreatedAt: now.Add(-time.Hour)},
		{ID: 3, Status: "in_progress", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: 4, Status: "completed", CreatedAt: now.Add(time.Hour)},
	}
	SortRuns(runs)

	want := []int64{2, 3, 4, 1}
	if fmt.Sprint(ids(runs)) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", ids(runs), want)
	}
}

func TestSortRunsBreaksTiesByID(t *testing.T) {
	runs := []WorkflowRun{
		{ID: 5, Status: "completed", CreatedAt: now},
		{ID: 9, Status: "completed", CreatedAt: now},
	}
	SortRuns(runs)
	if runs[0].ID != 9 {
		t.Errorf("first run = %d, want the higher ID first", runs[0].ID)
	}
}

func TestJobsAreSortedForDisplay(t *testing.T) {
	rec := newRecorder().serve("/repos/acme/api/actions/runs/7/jobs", mustJSON(t, map[string]any{
		"total_count": 2,
		"jobs": []map[string]any{
			jobFixture(1, "completed", now),
			jobFixture(2, "in_progress", now.Add(-time.Hour)),
		},
	}))
	s := newTestStore(t, rec)

	got, err := s.Jobs(context.Background(), RepoRef{Owner: "acme", Repo: "api"}, 7, 0, Page{})
	if err != nil {
		t.Fatalf("Jobs: %v", err)
	}
	if got.Items[0].ID != 2 {
		t.Errorf("first job = %d, want the running one", got.Items[0].ID)
	}
}

func TestMatchActor(t *testing.T) {
	cases := []struct {
		name   string
		actor  string
		filter string
		want   bool
	}{
		{"exact", "octocat", "octocat", true},
		{"case insensitive", "OctoCat", "octocat", true},
		{"bot by bare name", "dependabot[bot]", "dependabot", true},
		{"bot by full login", "dependabot[bot]", "dependabot[bot]", true},
		{"different person", "octocat", "hubot", false},
		{"a prefix of a person is not a match", "octocat", "octo", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchActor(tc.actor, tc.filter); got != tc.want {
				t.Errorf("matchActor(%q, %q) = %v, want %v", tc.actor, tc.filter, got, tc.want)
			}
		})
	}
}

func TestMissingScopes(t *testing.T) {
	cases := []struct {
		name   string
		scopes []string
		want   []string
	}{
		{"unknown scopes deny nothing", nil, nil},
		{"all present", []string{"repo", "workflow", "gist"}, nil},
		{"workflow missing", []string{"repo", "gist"}, []string{"workflow"}},
		{"both missing", []string{"gist"}, []string{"repo", "workflow"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MissingScopes(tc.scopes); fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("MissingScopes(%v) = %v, want %v", tc.scopes, got, tc.want)
			}
		})
	}
}

func TestPendingDeployments(t *testing.T) {
	path := "/repos/acme/api/actions/runs/7/pending_deployments"
	rec := newRecorder().serve(path, mustJSON(t, []map[string]any{
		{"environment": map[string]any{"id": 11, "name": "production"}, "current_user_can_approve": true},
		{"environment": map[string]any{"id": 12, "name": "staging"}, "current_user_can_approve": false},
	}))
	s := newTestStore(t, rec)

	got, err := s.PendingDeployments(context.Background(), RepoRef{Owner: "acme", Repo: "api"}, 7)
	if err != nil {
		t.Fatalf("PendingDeployments: %v", err)
	}
	if len(got) != 2 || got[0].Environment.ID != 11 || !got[0].CurrentUserCanApprove {
		t.Fatalf("deployments = %+v, want both environments with their approval flags", got)
	}
}

func TestApproveDeployments(t *testing.T) {
	path := "/repos/acme/api/actions/runs/7/pending_deployments"
	rec := newRecorder().serve(path, `{}`)
	s := newTestStore(t, rec)

	if err := s.ApproveDeployments(context.Background(), RepoRef{Owner: "acme", Repo: "api"}, 7, []int64{11}, "approved by test"); err != nil {
		t.Fatalf("ApproveDeployments: %v", err)
	}
	if n := rec.count(path); n != 1 {
		t.Errorf("approval posted to %s %d times, want 1", path, n)
	}
}

func TestApproveDeploymentsNeedsAnEnvironment(t *testing.T) {
	s := newTestStore(t, newRecorder())
	err := s.ApproveDeployments(context.Background(), RepoRef{Owner: "acme", Repo: "api"}, 7, nil, "")
	if err == nil {
		t.Fatal("ApproveDeployments accepted an empty environment list")
	}
}

func TestRepoPermission(t *testing.T) {
	rec := newRecorder().serve("/repos/acme/api/collaborators/octocat/permission",
		`{"permission":"write","user":{"login":"octocat"}}`)
	s := newTestStore(t, rec)

	got, err := s.RepoPermission(context.Background(), RepoRef{Owner: "acme", Repo: "api"}, "octocat")
	if err != nil {
		t.Fatalf("RepoPermission: %v", err)
	}
	if got != "write" {
		t.Errorf("permission = %q, want write", got)
	}
}

func TestRepoPermissionNeedsALogin(t *testing.T) {
	s := newTestStore(t, newRecorder())
	if _, err := s.RepoPermission(context.Background(), RepoRef{Owner: "acme", Repo: "api"}, ""); err == nil {
		t.Fatal("RepoPermission accepted an empty login")
	}
}

// ids renders run IDs for failure messages.
func ids(runs []WorkflowRun) []int64 {
	out := make([]int64, 0, len(runs))
	for _, r := range runs {
		out = append(out, r.ID)
	}
	return out
}
