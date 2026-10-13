package store

import (
	"context"
	"fmt"
)

// ReviewThreads answers the one question about a conversation that REST cannot:
// which of a pull request's inline threads have been resolved.
//
// The state exists in the GraphQL schema alone — the REST payload carries no
// resolution field, and only the resolve/unresolve mutations change it — so this
// is the second thing the GraphQL transport is here for. A thread is matched back
// to the remarks the conversation already holds by the id of the comment it
// started from, which both APIs report the same way.
//
// A failure here is not a failure of the conversation. The caller draws every
// thread in full when it has no resolution to work with, which is what the pane
// did before this existed, so an outage on this query costs a quieter screen
// rather than an empty one.
func (c *Client) ReviewThreads(ctx context.Context, repo RepoRef, number int) ([]ReviewThread, error) {
	key := fmt.Sprintf("review-threads|%s|%d", repo, number)
	return fetch(ctx, c.REST, key, func(ctx context.Context) ([]ReviewThread, error) {
		return c.gql.ReviewThreads(ctx, repo.Owner, repo.Repo, number)
	})
}
