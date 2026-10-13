package gql

import (
	"context"

	"github.com/shurcooL/githubv4"
)

// A pull request's inline threads, and whether each one has been resolved.
//
// This is the one thing about a conversation that REST cannot answer. The
// comments and the reviews REST returns carry no resolution state at all — a
// thread is resolved or not in GraphQL, and only a mutation on that side changes
// it — so the conversation reads the state here and matches it back by the ids
// it already has.
//
// The match is by DatabaseId: it is the same number the REST payload reports as
// a comment's id, which is what makes the two lists joinable without a second
// guess at which thread is which.

// ReviewThread is one inline thread as GraphQL reports it.
type ReviewThread struct {
	// IsResolved reports that the thread has been resolved, which is what the
	// conversation collapses it under.
	IsResolved bool
	// CommentIDs are the REST comment ids of the thread's remarks, oldest
	// first. A thread is identified on the other side by the id of the comment
	// it started from.
	CommentIDs []int64
}

// The connection's limits, which are the schema's rather than this app's:
// reviewThreads takes at most 100 per page, and a thread's comments at most 100.
// One page is fetched, and a thread's remarks are those a page holds — a pull
// request with more than a hundred threads, or a thread with more than a hundred
// remarks, is left partly unknown rather than paid for with a walk of unbounded
// length. A thread the answer does not name is drawn in full, which is what the
// conversation did before any of this existed.
const (
	reviewThreadsPerPage    = 100
	reviewCommentsPerThread = 100
)

// ReviewThreads returns the inline threads of one pull request with their
// resolution state.
func (c *Client) ReviewThreads(ctx context.Context, owner, repo string, number int) ([]ReviewThread, error) {
	var q reviewThreadsQuery
	variables := map[string]any{
		"owner":   githubv4.String(owner),
		"repo":    githubv4.String(repo),
		"number":  githubv4.Int(number),
		"first":   githubv4.Int(reviewThreadsPerPage),
		"remarks": githubv4.Int(reviewCommentsPerThread),
	}
	if err := c.do(ctx, &q, variables, func() Budget { return q.RateLimit.budget() }); err != nil {
		return nil, err
	}

	threads := q.Repository.PullRequest.ReviewThreads.Nodes
	out := make([]ReviewThread, 0, len(threads))
	for _, node := range threads {
		out = append(out, node.thread())
	}
	return out, nil
}

// reviewThreadsQuery is the resolution half of a pull request's conversation.
//
// The thread's comments are asked for but not read: only their ids are wanted,
// and the ids are what tie a thread to the remarks REST returned.
type reviewThreadsQuery struct {
	RateLimit  budgetField
	Repository struct {
		PullRequest struct {
			ReviewThreads struct {
				Nodes    []reviewThreadNode
				PageInfo pageInfo
			} `graphql:"reviewThreads(first: $first)"`
		} `graphql:"pullRequest(number: $number)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
}

// reviewThreadNode is one thread: its state and the ids of its remarks.
type reviewThreadNode struct {
	IsResolved bool
	Comments   struct {
		Nodes []struct {
			DatabaseID int
		}
	} `graphql:"comments(first: $remarks)"`
}

// thread reduces a node to the answer the store carries.
func (n reviewThreadNode) thread() ReviewThread {
	thread := ReviewThread{IsResolved: n.IsResolved}
	if len(n.Comments.Nodes) > 0 {
		thread.CommentIDs = make([]int64, 0, len(n.Comments.Nodes))
	}
	for _, comment := range n.Comments.Nodes {
		thread.CommentIDs = append(thread.CommentIDs, int64(comment.DatabaseID))
	}
	return thread
}
