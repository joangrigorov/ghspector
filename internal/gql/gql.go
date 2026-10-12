// Package gql is the GraphQL transport.
//
// It exists for the one thing REST cannot do on this app's behalf: list an
// entire scope — every repository a user or organization owns — in a single
// cursor-paginated query. The REST fan-out it replaces cost one request per
// repository, and REST could only guess whether another page existed.
//
// The GitHub schema decides what belongs here, so the boundaries were read off
// the published schema (docs.github.com/public/fpt/schema.docs.graphql) rather
// than assumed:
//
//   - Query.search is the only connection that spans a scope for issues and
//     pull requests. Organization exposes no issue or pull request connection at
//     all, and User.pullRequests/User.issues are the authored-by connections,
//     which answer a different question. Both scope lists are therefore search
//     queries.
//   - Workflow runs have no scope-wide connection: Workflow.runs is the only run
//     connection in the schema, and it hangs off one workflow file. The run list
//     keeps the REST fan-out, and internal/store documents that.
//   - Repository.pullRequests and Repository.issues exist, but using them would
//     rebuild the per-repository fan-out in a different dialect.
//
// Search is not free of limits, and they are the API's, not this package's: the
// search index is eventually consistent, it holds at most 1000 matches for a
// query, and search requests draw on a separate allowance (documented as 30 per
// minute) on top of the point budget. The store keeps the REST fan-out as a
// fallback for a scope the index will not serve.
package gql

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shurcooL/githubv4"

	"github.com/joangrigorov/ghspector/internal/gh"
	"github.com/joangrigorov/ghspector/internal/sanitize"
)

// Endpoint is the production GraphQL endpoint. It is a constant rather than a
// setting so that no configuration value can redirect an authenticated request
// to another host.
const Endpoint = "https://api.github.com/graphql"

// queryTimeout bounds one query, matching the REST transport's timeout.
const queryTimeout = 15 * time.Second

// Reserver blocks until n points of request budget are available. The store's
// shared rate limiter implements it, so both transports draw on one budget.
type Reserver interface {
	WaitN(ctx context.Context, n int) error
}

// Budget is the point budget the API reports for one query.
type Budget struct {
	// Limit is the size of the window in points.
	Limit int
	// Cost is the points this query was charged.
	Cost int
	// Remaining is the points left in the window.
	Remaining int
	// ResetAt is when the window resets.
	ResetAt time.Time
}

// Client is the GraphQL transport.
type Client struct {
	v4       *githubv4.Client
	reserver Reserver

	mu sync.RWMutex
	// cost is what the last query was charged. A cost is only known from the
	// response it arrives in, so the previous query's cost is what the next one
	// reserves.
	cost int
	last Budget
}

// NewClient returns a GraphQL transport. endpoint is empty in production and set
// only by tests, which is what keeps the request path pinned to Endpoint;
// reserver may be nil, which disables budget reservation.
func NewClient(token, endpoint string, reserver Reserver) *Client {
	if endpoint == "" {
		endpoint = Endpoint
	}
	return &Client{
		v4: githubv4.NewEnterpriseClient(endpoint, &http.Client{
			Timeout: queryTimeout,
			// A stock client with a stock transport: no TLS configuration and no
			// transport override, so the platform's own certificate verification
			// is what authenticates the server.
			Transport: authTransport{token: token, base: http.DefaultTransport},
		}),
		reserver: reserver,
		cost:     1,
	}
}

// authTransport adds the bearer token to every request. It exists so the token
// is written in exactly one place and never reaches a URL, a query string, or a
// message.
type authTransport struct {
	token string
	base  http.RoundTripper
}

// RoundTrip implements http.RoundTripper. The request is cloned rather than
// modified, per the RoundTripper contract.
func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(clone)
}

// RateLimit returns the budget the API reported for the most recent query.
func (c *Client) RateLimit() Budget {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.last
}

// do reserves budget, runs one query, and records what the response reported.
// read returns the rateLimit block of the populated query.
func (c *Client) do(ctx context.Context, q any, variables map[string]any, read func() Budget) error {
	if err := c.reserve(ctx); err != nil {
		return err
	}
	if err := c.v4.Query(ctx, q, variables); err != nil {
		return mapError(err)
	}
	// The query struct is this transport's response, so this is where its
	// strings stop being somebody else's data. The REST transport cleans its own
	// the same way.
	sanitize.Texts(q)
	c.record(read())
	return nil
}

// reserve blocks for the budget the last query was charged, or one point before
// any query has run.
func (c *Client) reserve(ctx context.Context) error {
	if c.reserver == nil {
		return nil
	}
	c.mu.RLock()
	n := c.cost
	c.mu.RUnlock()
	if n < 1 {
		n = 1
	}
	return c.reserver.WaitN(ctx, n)
}

// record stores the budget a response reported.
func (c *Client) record(b Budget) {
	if b.ResetAt.IsZero() {
		// The response carried no rateLimit block, so it says nothing about the
		// budget. Keeping the previous report beats replacing it with zeroes.
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if b.Cost > 0 {
		c.cost = b.Cost
	}
	c.last = b
}

// mapError translates the transport's errors into the taxonomy the rest of the
// app classifies on. The underlying client reports a non-200 response as
// "non-200 OK status code: <status> body: <body>" and a GraphQL error as the
// first message of the response's errors array, so the mapping reads the
// message. The message is the API's own text: the token travels in a header and
// is never echoed back into it.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "rate limit"), strings.Contains(lower, "abuse detection"):
		return fmt.Errorf("%w: %s", gh.ErrRateLimited, msg)
	case strings.Contains(lower, "bad credentials"),
		strings.Contains(lower, "resource not accessible"),
		strings.Contains(lower, "401"),
		strings.Contains(lower, "403"),
		strings.Contains(lower, "forbidden"):
		return fmt.Errorf("%w: %s", gh.ErrForbidden, msg)
	}
	return fmt.Errorf("graphql: %w", err)
}
