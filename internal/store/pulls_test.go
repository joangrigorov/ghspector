package store

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// prFixture builds one pull request entry. The updated_at field drives both the
// sort and the merge test, so it is always set.
func prFixture(id int64, number int, state string, updated time.Time) map[string]any {
	return map[string]any{
		"id":         id,
		"number":     number,
		"title":      fmt.Sprintf("pr %d", number),
		"state":      state,
		"updated_at": updated.Format(time.RFC3339),
		"user":       map[string]any{"login": "octocat"},
	}
}

// prsFixture builds a pull request list response.
func prsFixture(t *testing.T, prs ...map[string]any) string {
	t.Helper()
	return mustJSON(t, prs)
}

func prIDs(prs []PullRequest) []int64 {
	out := make([]int64, 0, len(prs))
	for _, pr := range prs {
		out = append(out, pr.ID)
	}
	return out
}

func TestScopePRsFansOutAndSorts(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		serve("/repos/acme/api/pulls", prsFixture(t,
			prFixture(1, 1, "open", now.Add(-2*time.Hour)),
			prFixture(2, 2, "open", now.Add(-time.Hour)),
		)).
		serve("/repos/acme/web/pulls", prsFixture(t,
			prFixture(3, 3, "open", now.Add(-3*time.Hour)),
		))
	s := newTestStore(t, rec)

	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "acme"}, PRQuery{})
	if err != nil {
		t.Fatalf("ScopePRs: %v", err)
	}

	// Most recently updated first, across both repositories.
	want := []int64{2, 1, 3}
	if fmt.Sprint(prIDs(got.Items)) != fmt.Sprint(want) {
		t.Errorf("pr order = %v, want %v", prIDs(got.Items), want)
	}
	for _, pr := range got.Items {
		if pr.Repository.Name == "" {
			t.Errorf("pr %d has no repository attached", pr.ID)
		}
	}
	if got.HasMore {
		t.Error("HasMore = true for short pages, want false")
	}
}

func TestScopePRsCapsTheFanOut(t *testing.T) {
	names := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10"}
	rec := newRecorder().serve("/users/acme/repos", reposFixture(t, names...))
	for _, n := range names {
		rec.serve("/repos/acme/"+n+"/pulls", prsFixture(t))
	}
	s := newTestStore(t, rec)

	if _, err := s.ScopePRs(context.Background(), OwnerRef{Login: "acme"}, PRQuery{}); err != nil {
		t.Fatalf("ScopePRs: %v", err)
	}

	queried := 0
	for _, n := range names {
		if rec.count("/repos/acme/"+n+"/pulls") > 0 {
			queried++
		}
	}
	if queried != maxAggregateRepos {
		t.Errorf("queried %d repositories, want the cap of %d", queried, maxAggregateRepos)
	}
}

func TestScopePRsNarrowsToTheFilteredRepository(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api", "web")).
		serve("/repos/acme/web/pulls", prsFixture(t))
	s := newTestStore(t, rec)

	if _, err := s.ScopePRs(context.Background(), OwnerRef{Login: "acme"}, PRQuery{Repo: "web"}); err != nil {
		t.Fatalf("ScopePRs: %v", err)
	}

	if n := rec.count("/repos/acme/api/pulls"); n != 0 {
		t.Errorf("the unfiltered repository was queried %d times, want 0", n)
	}
	if n := rec.count("/repos/acme/web/pulls"); n != 1 {
		t.Errorf("the filtered repository was queried %d times, want 1", n)
	}
}

func TestScopePRsSkipsRepositoriesThatFail(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "broken", "api")).
		fail("/repos/acme/broken/pulls", http.StatusNotFound, "no pulls").
		serve("/repos/acme/api/pulls", prsFixture(t,
			prFixture(1, 1, "open", now),
		))
	s := newTestStore(t, rec)

	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "acme"}, PRQuery{})
	if err != nil {
		t.Fatalf("ScopePRs returned an error for one failing repository: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != 1 {
		t.Errorf("got %d pull requests, want the one from the repository that answered", len(got.Items))
	}
}

func TestScopePRsReportsAnErrorWhenEveryRepositoryFails(t *testing.T) {
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "one", "two")).
		fail("/repos/acme/one/pulls", http.StatusNotFound, "no pulls").
		fail("/repos/acme/two/pulls", http.StatusNotFound, "no pulls")
	s := newTestStore(t, rec)

	if _, err := s.ScopePRs(context.Background(), OwnerRef{Login: "acme"}, PRQuery{}); err == nil {
		t.Fatal("ScopePRs returned no error although every repository failed")
	}
}

func TestScopePRsReportsMoreWhenAnyRepositoryHasAFullPage(t *testing.T) {
	full := make([]map[string]any, 0, defaultPRPageSize)
	for i := 0; i < defaultPRPageSize; i++ {
		full = append(full, prFixture(int64(i+1), i+1, "open", now))
	}
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api")).
		serve("/repos/acme/api/pulls", prsFixture(t, full...))
	s := newTestStore(t, rec)

	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "acme"}, PRQuery{})
	if err != nil {
		t.Fatalf("ScopePRs: %v", err)
	}
	if !got.HasMore {
		t.Error("HasMore = false for a full page, want true")
	}
}

func TestScopePRsAppliesLoginFiltersOverTheMergedPage(t *testing.T) {
	other := prFixture(9, 9, "open", now)
	other["user"] = map[string]any{"login": "someone-else"}
	rec := newRecorder().
		serve("/users/acme/repos", reposFixture(t, "api")).
		serve("/repos/acme/api/pulls", prsFixture(t,
			prFixture(1, 1, "open", now), other))
	s := newTestStore(t, rec)

	got, err := s.ScopePRs(context.Background(), OwnerRef{Login: "acme"}, PRQuery{Author: "octocat"})
	if err != nil {
		t.Fatalf("ScopePRs: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != 1 {
		t.Fatalf("got %v, want only the pull request by the filtered author", prIDs(got.Items))
	}
}

func TestMergePRsReplacesByIDAndResorts(t *testing.T) {
	existing := []PullRequest{
		{ID: 1, Number: 1, State: "open", UpdatedAt: now},
		{ID: 2, Number: 2, State: "open", UpdatedAt: now.Add(-time.Hour)},
	}
	incoming := []PullRequest{
		{ID: 1, Number: 1, State: "closed", UpdatedAt: now},
		{ID: 3, Number: 3, State: "open", UpdatedAt: now.Add(time.Minute)},
	}

	merged := MergePRs(existing, incoming)
	if len(merged) != 3 {
		t.Fatalf("merged to %d pull requests, want 3: %v", len(merged), prIDs(merged))
	}
	if merged[0].ID != 3 {
		t.Errorf("first pull request = %d, want the most recently updated", merged[0].ID)
	}
	for _, pr := range merged {
		if pr.ID == 1 && pr.State != "closed" {
			t.Errorf("pr 1 state = %q, want the polled state", pr.State)
		}
	}
}

func TestMergePRsKeepsTheListWhenNothingArrives(t *testing.T) {
	existing := []PullRequest{{ID: 1}}
	if got := MergePRs(existing, nil); len(got) != 1 {
		t.Errorf("got %d pull requests, want the existing list untouched", len(got))
	}
}

func TestSortPRsOrdersByRecencyThenNumber(t *testing.T) {
	prs := []PullRequest{
		{ID: 1, Number: 1, UpdatedAt: now},
		{ID: 2, Number: 2, UpdatedAt: now},
		{ID: 3, Number: 3, UpdatedAt: now.Add(-time.Hour)},
	}
	SortPRs(prs)

	// Equal instants fall back to the higher number.
	want := []int64{2, 1, 3}
	if fmt.Sprint(prIDs(prs)) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", prIDs(prs), want)
	}
}
