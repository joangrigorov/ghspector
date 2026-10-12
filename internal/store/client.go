package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/joangrigorov/ghspector/internal/gql"
)

// Client is the store the app uses: the REST store for single resources, writes
// and the fan-out fallback, plus the GraphQL transport for the scope-wide lists.
// The Store interface is what hides the difference, so no route knows which
// transport answered it.
//
// The split follows what each API can do rather than what is convenient to port:
// GraphQL lists a whole scope in one query, and REST reaches the resources
// GraphQL has no connection for — job logs, file patches, check runs — and every
// write, which is already implemented and tested there.
type Client struct {
	*REST
	gql *gql.Client

	mu sync.Mutex
	// gqlDownUntil is when a search may be attempted again after one failed.
	gqlDownUntil time.Time
}

// New returns the store. restBaseURL and gqlEndpoint are empty in production and
// set only by tests, which is what keeps both request paths pinned to GitHub: no
// configuration value can redirect an authenticated request.
func New(token, restBaseURL, gqlEndpoint string, opts ...Option) *Client {
	rest := NewREST(token, restBaseURL, opts...)
	return &Client{
		REST: rest,
		// The limiter is shared, so both transports draw on one budget: REST
		// spends a request, GraphQL spends the points its query costs.
		gql: gql.NewClient(token, gqlEndpoint, rest.limiter),
	}
}

// ScopeRuns lists runs across the context's repositories with the REST fan-out.
//
// It is restated here, rather than left to the embedded store, so the reason is
// in one place: no transport can list runs across a scope, so this is the only
// way to do it. See maxAggregateRepos for the cost of the fan-out.
func (c *Client) ScopeRuns(ctx context.Context, owner OwnerRef, q RunQuery) (List[WorkflowRun], error) {
	return c.REST.ScopeRuns(ctx, owner, q)
}

// RateLimit reports the budget the header renders.
//
// Two transports now draw on the token's allowance — REST spends requests and
// GraphQL spends the points a query costs — so the number worth showing is the
// one closer to running out, which is the one that will cut the app off first.
// Before a search has ever reported a budget, the REST figure stands alone.
func (c *Client) RateLimit() RateLimitInfo {
	rest := c.REST.RateLimit()
	budget := c.gql.RateLimit()
	if budget.ResetAt.IsZero() || rest.Limit == 0 {
		return rest
	}
	if budget.Remaining < rest.Remaining {
		return RateLimitInfo{Limit: budget.Limit, Remaining: budget.Remaining, Reset: budget.ResetAt}
	}
	return rest
}

// RateLimitResetIn reports how long the window RateLimit reports has left.
//
// The GraphQL budget carries no response headers, so its reset is read as the
// absolute time the API wrote into the response, and only the REST budget can be
// corrected against the server's clock. The GraphQL window is reported when it is
// the tighter of the two, which is the same choice RateLimit makes.
func (c *Client) RateLimitResetIn() (time.Duration, bool) {
	rest, restKnown := c.REST.RateLimitResetIn()
	budget := c.gql.RateLimit()
	if budget.ResetAt.IsZero() || c.REST.RateLimit().Limit == 0 {
		return rest, restKnown
	}
	if budget.Remaining < c.REST.RateLimit().Remaining {
		left := time.Until(budget.ResetAt)
		if left < 0 {
			left = 0
		}
		return left, true
	}
	return rest, restKnown
}

// ScopePRs lists pull requests across the context's repositories, most recently
// updated first.
//
// The search index answers it in one query, with the state narrowing done by the
// API and the login filters applied to the whole scope rather than to the page
// in hand. A scope the index will not serve — an organization the token cannot
// see, an index outage, a token without the scope search needs — falls back to
// the REST fan-out rather than leaving the screen empty.
func (c *Client) ScopePRs(ctx context.Context, owner OwnerRef, q PRQuery) (List[PullRequest], error) {
	list, err := c.searchPRs(ctx, owner, q)
	if err == nil {
		return list, nil
	}
	if isCancellation(err) {
		return List[PullRequest]{}, err
	}
	list, fallbackErr := c.REST.ScopePRs(ctx, owner, q)
	if fallbackErr != nil {
		// The fan-out's failure is the one worth reporting: it is the path that
		// covers the whole scope, and it is the one that would work.
		return List[PullRequest]{}, fallbackErr
	}
	return list, nil
}

// ScopeIssues lists issues across the context's repositories, most recently
// updated first, on the same terms as ScopePRs. Pull requests are excluded by
// the search query itself rather than by a filter over the page.
func (c *Client) ScopeIssues(ctx context.Context, owner OwnerRef, q IssueQuery) (List[Issue], error) {
	list, err := c.searchIssues(ctx, owner, q)
	if err == nil {
		return list, nil
	}
	if isCancellation(err) {
		return List[Issue]{}, err
	}
	list, fallbackErr := c.REST.ScopeIssues(ctx, owner, q)
	if fallbackErr != nil {
		return List[Issue]{}, fallbackErr
	}
	return list, nil
}

// errSearchSkipped reports that a search was not attempted because a recent one
// failed.
var errSearchSkipped = errors.New("store: search skipped after a recent failure")

// scopeSearchRetryAfter is how long a failed search is left alone. Without it,
// every poll of a scope the index will not serve would pay for a doomed query
// before falling back. Half a minute still notices the index recovering.
const scopeSearchRetryAfter = 30 * time.Second

// searchUsable reports whether a search is worth attempting.
func (c *Client) searchUsable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.cache.now().Before(c.gqlDownUntil)
}

// searchFailed remembers a failed search. A cancelled search is the caller going
// away rather than a failure of the transport, so it is not remembered.
func (c *Client) searchFailed(err error) {
	if isCancellation(err) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gqlDownUntil = c.cache.now().Add(scopeSearchRetryAfter)
}

// isCancellation reports whether an error is the caller going away. Another
// request on another path would not help, so the fallback is skipped.
func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
