package store

import (
	"context"
	"testing"
	"time"
)

// threadsReply is a reviewThreads response for the stub.
func threadsReply(t *testing.T, threads ...map[string]any) string {
	t.Helper()
	nodes := make([]any, 0, len(threads))
	for _, thread := range threads {
		nodes = append(nodes, thread)
	}
	return mustJSON(t, map[string]any{"data": map[string]any{
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

// threadNode is one thread of a threadsReply.
func threadNode(resolved bool, ids ...int) map[string]any {
	comments := make([]any, 0, len(ids))
	for _, id := range ids {
		comments = append(comments, map[string]any{"databaseId": id})
	}
	return map[string]any{
		"isResolved": resolved,
		"comments":   map[string]any{"nodes": comments},
	}
}

// TestReviewThreadsReadsTheResolutionState covers what the store adds to the
// GraphQL answer: the threads of one pull request, with the ids that tie them to
// the comments the conversation already holds.
func TestReviewThreadsReadsTheResolutionState(t *testing.T) {
	rec := newRecorder()
	stub := &gqlStub{replies: []string{threadsReply(t, threadNode(true, 4001), threadNode(false, 4002))}}
	c := newSearchStore(t, rec, stub)

	threads, err := c.ReviewThreads(context.Background(), RepoRef{Owner: "octocat", Repo: "api"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Fatalf("threads = %d, want 2", len(threads))
	}
	if !threads[0].IsResolved || threads[1].IsResolved {
		t.Errorf("the states are %v and %v, want resolved then unresolved", threads[0].IsResolved, threads[1].IsResolved)
	}
	if len(threads[0].CommentIDs) != 1 || threads[0].CommentIDs[0] != 4001 {
		t.Errorf("the first thread's remarks are %v, want [4001]", threads[0].CommentIDs)
	}

	// The read is cached like every other one: a poll and a redraw share it.
	if _, err := c.ReviewThreads(context.Background(), RepoRef{Owner: "octocat", Repo: "api"}, 7); err != nil {
		t.Fatal(err)
	}
	if got := stub.calls(); got != 1 {
		t.Errorf("the query ran %d times, want the second read served from the cache", got)
	}
}

// TestReviewThreadsReportsAFailure covers the degraded case: a query the token
// cannot run is an error, not an empty answer, so a caller does not mistake "no
// resolution data" for "everything is unresolved".
func TestReviewThreadsReportsAFailure(t *testing.T) {
	rec := newRecorder()
	stub := &gqlStub{
		status:  500,
		replies: []string{mustJSON(t, map[string]any{"errors": []any{map[string]any{"message": "something went wrong"}}})},
	}
	c := newSearchStore(t, rec, stub, WithCacheTTL(time.Minute))

	if _, err := c.ReviewThreads(context.Background(), RepoRef{Owner: "octocat", Repo: "api"}, 7); err == nil {
		t.Fatal("a failed query reported no error")
	}
	// The failure is not cached, so the next poll tries again.
	if _, err := c.ReviewThreads(context.Background(), RepoRef{Owner: "octocat", Repo: "api"}, 7); err == nil {
		t.Fatal("the failure was cached as an answer")
	}
}
