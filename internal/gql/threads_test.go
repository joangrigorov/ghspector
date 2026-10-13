package gql

import (
	"context"
	"strings"
	"testing"
)

// threadsReply is a reviewThreads response: one thread per entry, each with the
// ids of the remarks it holds.
func threadsReply(t *testing.T, threads ...map[string]any) string {
	t.Helper()
	nodes := make([]any, 0, len(threads))
	for _, thread := range threads {
		nodes = append(nodes, thread)
	}
	return dataReply(t, map[string]any{
		"rateLimit": budgetJSON(1, 4999),
		"repository": map[string]any{
			"pullRequest": map[string]any{
				"reviewThreads": map[string]any{
					"nodes":    nodes,
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				},
			},
		},
	})
}

// aThread is one thread node: its resolution state and the ids of its remarks.
func aThread(resolved bool, ids ...int) map[string]any {
	comments := make([]any, 0, len(ids))
	for _, id := range ids {
		comments = append(comments, map[string]any{"databaseId": id})
	}
	return map[string]any{
		"isResolved": resolved,
		"comments":   map[string]any{"nodes": comments},
	}
}

// TestReviewThreadsAsksForTheResolutionState covers the one question REST cannot
// answer about a conversation: which of a pull request's inline threads have been
// resolved. The state is GraphQL's alone, and the ids are what tie each thread
// back to the remarks the REST payload carries.
func TestReviewThreadsAsksForTheResolutionState(t *testing.T) {
	s, c := newStub(t, threadsReply(t,
		aThread(true, 4001, 4002),
		aThread(false, 4003),
	))

	threads, err := c.ReviewThreads(context.Background(), "octocat", "api", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Fatalf("threads = %d, want 2", len(threads))
	}
	if !threads[0].IsResolved {
		t.Error("the first thread is reported unresolved, want resolved")
	}
	if threads[1].IsResolved {
		t.Error("the second thread is reported resolved, want unresolved")
	}
	if got, want := threads[0].CommentIDs, []int64{4001, 4002}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the thread's remarks are %v, want %v", got, want)
	}

	req := s.requests()[0]
	for _, want := range []string{
		"repository(owner: $owner, name: $repo)",
		"pullRequest(number: $number)",
		"reviewThreads(first: $first)",
		"isResolved",
		"comments(first: $remarks)",
		"databaseId",
		"rateLimit{limit,cost,remaining,resetAt}",
	} {
		if !strings.Contains(req.query, want) {
			t.Errorf("the query does not ask for %q:\n%s", want, req.query)
		}
	}
	for key, want := range map[string]any{
		"owner":   "octocat",
		"repo":    "api",
		"number":  float64(7),
		"first":   float64(reviewThreadsPerPage),
		"remarks": float64(reviewCommentsPerThread),
	} {
		if got := req.variables[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	s.checkTokenStayedInTheHeader(t)
}

// TestReviewThreadsReportsAThreadWithNoRemarks covers the thread whose page of
// remarks is empty: it is still a thread, and it carries no id to be matched by.
func TestReviewThreadsReportsAThreadWithNoRemarks(t *testing.T) {
	_, c := newStub(t, threadsReply(t, aThread(true)))

	threads, err := c.ReviewThreads(context.Background(), "octocat", "api", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || !threads[0].IsResolved {
		t.Fatalf("threads = %+v, want one resolved thread", threads)
	}
	if len(threads[0].CommentIDs) != 0 {
		t.Errorf("the thread carries the ids %v, want none", threads[0].CommentIDs)
	}
}

// TestReviewThreadsReportsAComparisonFailure covers the pull request the token
// cannot see: the answer is a GraphQL error rather than an empty conversation, so
// the caller can tell "no resolution data" from "everything is unresolved".
func TestReviewThreadsReportsAComparisonFailure(t *testing.T) {
	_, c := newStub(t, mustJSON(t, map[string]any{
		"errors": []any{map[string]any{"message": "Could not resolve to a PullRequest with the number of 7."}},
	}))

	if _, err := c.ReviewThreads(context.Background(), "octocat", "api", 7); err == nil {
		t.Fatal("a failed query reported no error")
	}
}
