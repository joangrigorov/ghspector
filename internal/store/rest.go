package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"ghspector/internal/gh"
)

// Default page sizes, matching the previous version's request sizes so the
// "Load More" steps stay the same size.
const (
	defaultRunPageSize     = 8
	defaultPRPageSize      = 8
	defaultIssuePageSize   = 50
	defaultRepoPageSize    = 100
	defaultCommitPageSize  = 30
	defaultCommentPageSize = 30
)

// DefaultCacheTTL is deliberately shorter than the fastest poll interval (5s),
// so a poll still refetches while simultaneous requests for the same key during
// one refresh share a single request.
const DefaultCacheTTL = 2 * time.Second

// Default requests per second and burst. The documented authenticated ceiling is
// 5000 per hour, roughly 1.4 per second; a small allowance above that keeps the
// UI responsive without inviting secondary rate limits.
const (
	defaultRatePerSecond = 5
	defaultRateBurst     = 10
)

// Option configures a REST store.
type Option func(*REST)

// WithCacheTTL sets how long a read result is reused.
func WithCacheTTL(d time.Duration) Option {
	return func(r *REST) { r.cache.ttl = d }
}

// WithRateLimit sets the client-side outgoing request rate.
func WithRateLimit(perSecond float64, burst int) Option {
	return func(r *REST) { r.limiter = rate.NewLimiter(rate.Limit(perSecond), burst) }
}

// WithClock replaces the cache clock, so tests can advance time without
// sleeping.
func WithClock(now func() time.Time) Option {
	return func(r *REST) { r.cache.now = now }
}

// REST implements Store against the GitHub REST API.
type REST struct {
	client  *gh.Client
	cache   *cache
	limiter *rate.Limiter
}

// NewREST returns a store backed by the REST transport. baseURL is empty in
// production and set only by tests, which is what keeps the request path pinned
// to api.github.com.
func NewREST(token, baseURL string, opts ...Option) *REST {
	r := &REST{
		client:  gh.NewClient(token, baseURL),
		cache:   newCache(DefaultCacheTTL),
		limiter: rate.NewLimiter(rate.Limit(defaultRatePerSecond), defaultRateBurst),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Client exposes the underlying REST client for the one caller that needs
// transport-level detail: the startup scope check. Everything else belongs on
// Store.
func (r *REST) Client() *gh.Client { return r.client }

// wait blocks until the rate limiter allows another request.
func (r *REST) wait(ctx context.Context) error {
	if r.limiter == nil {
		return nil
	}
	return r.limiter.Wait(ctx)
}

// fetch runs load behind the rate limiter and the cache. The limiter is inside
// the cache's load function on purpose: a cache hit must not consume budget.
func fetch[T any](ctx context.Context, r *REST, key string, load func(context.Context) (T, error)) (T, error) {
	return getOrLoad(ctx, r.cache, key, func(ctx context.Context) (T, error) {
		if err := r.wait(ctx); err != nil {
			var zero T
			return zero, err
		}
		return load(ctx)
	})
}

// pageArgs resolves a Page into the 1-based number and size the transport
// wants.
func pageArgs(p Page, def int) (number, size int) {
	number = p.Number
	if number < 1 {
		number = 1
	}
	size = p.Size
	if size < 1 {
		size = def
	}
	return number, size
}

// listOf builds a List from a page of items. HasMore follows the transport's
// rule: a full page means there may be another one.
func listOf[T any](items []T, size int) List[T] {
	return List[T]{Items: items, HasMore: len(items) == size}
}

// Viewer returns the authenticated account.
func (r *REST) Viewer(ctx context.Context) (User, error) {
	return fetch(ctx, r, "viewer", func(ctx context.Context) (User, error) {
		u, err := r.client.GetUser(ctx)
		if err != nil {
			return User{}, err
		}
		if u == nil {
			return User{}, ErrNotFound
		}
		return *u, nil
	})
}

// Organizations returns the organizations the viewer belongs to.
func (r *REST) Organizations(ctx context.Context) ([]Org, error) {
	return fetch(ctx, r, "orgs", func(ctx context.Context) ([]Org, error) {
		return r.client.GetOrgs(ctx)
	})
}

// Repositories lists repositories owned by owner.
func (r *REST) Repositories(ctx context.Context, owner OwnerRef, page Page) (List[Repository], error) {
	number, size := pageArgs(page, defaultRepoPageSize)
	ownerType := "user"
	if owner.IsOrg {
		ownerType = "org"
	}
	key := fmt.Sprintf("repos|%s|%s|%d|%d", ownerType, owner.Login, number, size)
	return fetch(ctx, r, key, func(ctx context.Context) (List[Repository], error) {
		repos, err := r.client.GetRepos(ctx, ownerType, owner.Login, number, size)
		if err != nil {
			return List[Repository]{}, err
		}
		return listOf(repos, size), nil
	})
}

// WorkflowRuns lists runs in one repository.
func (r *REST) WorkflowRuns(ctx context.Context, repo RepoRef, q RunQuery) (List[WorkflowRun], error) {
	if !repo.Valid() {
		return List[WorkflowRun]{}, fmt.Errorf("store: workflow runs need owner and repo")
	}
	number, size := pageArgs(q.Page, defaultRunPageSize)
	key := fmt.Sprintf("runs|%s|%d|%d|%s", repo, number, size, q.Actor)
	page, err := fetch(ctx, r, key, func(ctx context.Context) (List[WorkflowRun], error) {
		runs, err := r.client.GetWorkflowRuns(ctx, repo.Owner, repo.Repo, number, size, q.Actor)
		if err != nil {
			return List[WorkflowRun]{}, err
		}
		return listOf(runs, size), nil
	})
	if err != nil {
		return List[WorkflowRun]{}, err
	}
	// The API filter covers the whole list and this re-check covers the page, so
	// a run the API matched loosely cannot appear under a filter it does not
	// match. Filtering after the cache keeps the fetched page reusable.
	return List[WorkflowRun]{Items: filterActor(page.Items, q.Actor), HasMore: page.HasMore}, nil
}

// WorkflowRun fetches one run.
func (r *REST) WorkflowRun(ctx context.Context, repo RepoRef, runID int64) (WorkflowRun, error) {
	key := fmt.Sprintf("run|%s|%d", repo, runID)
	return fetch(ctx, r, key, func(ctx context.Context) (WorkflowRun, error) {
		run, err := r.client.GetWorkflowRun(ctx, repo.Owner, repo.Repo, runID)
		if err != nil {
			return WorkflowRun{}, err
		}
		if run == nil {
			return WorkflowRun{}, ErrNotFound
		}
		return *run, nil
	})
}

// Jobs lists the jobs of a run attempt.
func (r *REST) Jobs(ctx context.Context, repo RepoRef, runID int64, attempt int, page Page) (List[WorkflowJob], error) {
	number, size := pageArgs(page, defaultRunPageSize)
	key := fmt.Sprintf("jobs|%s|%d|%d|%d|%d", repo, runID, attempt, number, size)
	return fetch(ctx, r, key, func(ctx context.Context) (List[WorkflowJob], error) {
		var (
			jobs []WorkflowJob
			err  error
		)
		if attempt > 0 {
			jobs, err = r.client.GetWorkflowRunAttemptJobs(ctx, repo.Owner, repo.Repo, runID, attempt)
		} else {
			jobs, err = r.client.GetWorkflowRunJobs(ctx, repo.Owner, repo.Repo, runID)
		}
		if err != nil {
			return List[WorkflowJob]{}, err
		}
		// Jobs arrive in the order the runner created them; the view shows the
		// ones still working first.
		SortJobs(jobs)
		return listOf(jobs, size), nil
	})
}

// JobLogs fetches the log of one job. Logs are cached briefly so switching
// steps and back does not refetch, but a refresh still sees new output.
func (r *REST) JobLogs(ctx context.Context, repo RepoRef, jobID int64) (string, error) {
	key := fmt.Sprintf("logs|%s|%d", repo, jobID)
	return fetch(ctx, r, key, func(ctx context.Context) (string, error) {
		return r.client.GetJobLogs(ctx, repo.Owner, repo.Repo, jobID)
	})
}

// PullRequests lists pull requests in one repository, applying the author,
// assignee and reviewer filters locally because the REST endpoint cannot
// express them in one request.
func (r *REST) PullRequests(ctx context.Context, repo RepoRef, q PRQuery) (List[PullRequest], error) {
	if !repo.Valid() {
		return List[PullRequest]{}, fmt.Errorf("store: pull requests need owner and repo")
	}
	number, size := pageArgs(q.Page, defaultPRPageSize)
	state := q.State
	if state == "" {
		state = "open"
	}
	key := fmt.Sprintf("prs|%s|%s|%d|%d", repo, state, number, size)
	page, err := fetch(ctx, r, key, func(ctx context.Context) (List[PullRequest], error) {
		prs, err := r.client.GetPullRequestsWithState(ctx, repo.Owner, repo.Repo, state, number, size)
		if err != nil {
			return List[PullRequest]{}, err
		}
		return listOf(prs, size), nil
	})
	if err != nil {
		return List[PullRequest]{}, err
	}
	// Filtering happens after the cache so the unfiltered page is reused across
	// filter changes instead of being refetched per filter.
	return List[PullRequest]{Items: filterPRs(page.Items, q), HasMore: page.HasMore}, nil
}

// filterPRs applies the login filters a REST list request cannot carry.
func filterPRs(prs []PullRequest, q PRQuery) []PullRequest {
	if q.Author == "" && q.Assignee == "" && q.Reviewer == "" {
		return prs
	}
	out := make([]PullRequest, 0, len(prs))
	for _, pr := range prs {
		if q.Author != "" {
			if pr.User == nil || !equalLogin(pr.User.Login, q.Author) {
				continue
			}
		}
		if q.Assignee != "" && !anyLogin(pr.Assignees, q.Assignee) {
			continue
		}
		if q.Reviewer != "" && !anyLogin(pr.RequestedReviewers, q.Reviewer) {
			continue
		}
		out = append(out, pr)
	}
	return out
}

// PullRequest fetches one pull request.
func (r *REST) PullRequest(ctx context.Context, repo RepoRef, number int) (PullRequest, error) {
	key := fmt.Sprintf("pr|%s|%d", repo, number)
	return fetch(ctx, r, key, func(ctx context.Context) (PullRequest, error) {
		pr, err := r.client.GetPullRequest(ctx, repo.Owner, repo.Repo, number)
		if err != nil {
			return PullRequest{}, err
		}
		if pr == nil {
			return PullRequest{}, ErrNotFound
		}
		return *pr, nil
	})
}

// PullRequestCommits lists the commits of a pull request.
func (r *REST) PullRequestCommits(ctx context.Context, repo RepoRef, number int, page Page) (List[RepositoryCommit], error) {
	n, size := pageArgs(page, defaultCommitPageSize)
	key := fmt.Sprintf("prcommits|%s|%d|%d|%d", repo, number, n, size)
	return fetch(ctx, r, key, func(ctx context.Context) (List[RepositoryCommit], error) {
		commits, err := r.client.GetPullRequestCommits(ctx, repo.Owner, repo.Repo, number, n, size)
		if err != nil {
			return List[RepositoryCommit]{}, err
		}
		return listOf(commits, size), nil
	})
}

// PullRequestFiles lists the changed files of a pull request.
func (r *REST) PullRequestFiles(ctx context.Context, repo RepoRef, number int, page Page) (List[CommitFile], error) {
	n, size := pageArgs(page, defaultRepoPageSize)
	key := fmt.Sprintf("prfiles|%s|%d|%d|%d", repo, number, n, size)
	return fetch(ctx, r, key, func(ctx context.Context) (List[CommitFile], error) {
		files, err := r.client.GetPullRequestFiles(ctx, repo.Owner, repo.Repo, number, n, size)
		if err != nil {
			return List[CommitFile]{}, err
		}
		return listOf(files, size), nil
	})
}

// Commit fetches one commit with the files it changed.
func (r *REST) Commit(ctx context.Context, repo RepoRef, sha string) (RepositoryCommit, []CommitFile, error) {
	type result struct {
		commit RepositoryCommit
		files  []CommitFile
	}
	key := fmt.Sprintf("commit|%s|%s", repo, sha)
	res, err := fetch(ctx, r, key, func(ctx context.Context) (result, error) {
		commit, files, err := r.client.GetCommit(ctx, repo.Owner, repo.Repo, sha)
		if err != nil {
			return result{}, err
		}
		if commit == nil {
			return result{}, ErrNotFound
		}
		return result{commit: *commit, files: files}, nil
	})
	if err != nil {
		return RepositoryCommit{}, nil, err
	}
	return res.commit, res.files, nil
}

// CheckRuns lists the latest check run per check name for a ref.
func (r *REST) CheckRuns(ctx context.Context, repo RepoRef, ref string) ([]CheckRun, error) {
	key := fmt.Sprintf("checks|%s|%s", repo, ref)
	return fetch(ctx, r, key, func(ctx context.Context) ([]CheckRun, error) {
		return r.client.GetCheckRuns(ctx, repo.Owner, repo.Repo, ref)
	})
}

// Issues lists issues in one repository.
func (r *REST) Issues(ctx context.Context, repo RepoRef, q IssueQuery) (List[Issue], error) {
	if !repo.Valid() {
		return List[Issue]{}, fmt.Errorf("store: issues need owner and repo")
	}
	number, size := pageArgs(q.Page, defaultIssuePageSize)
	state := q.State
	if state == "" {
		state = "open"
	}
	key := fmt.Sprintf("issues|%s|%s|%d|%d", repo, state, number, size)
	return fetch(ctx, r, key, func(ctx context.Context) (List[Issue], error) {
		issues, hasMore, err := r.client.GetIssuesWithState(ctx, repo.Owner, repo.Repo, state, number, size)
		if err != nil {
			return List[Issue]{}, err
		}
		// The transport reports HasMore from the raw page, before pull requests
		// are filtered out, which is the behaviour the Load More row expects.
		return List[Issue]{Items: issues, HasMore: hasMore}, nil
	})
}

// Issue fetches one issue.
func (r *REST) Issue(ctx context.Context, repo RepoRef, number int) (Issue, error) {
	key := fmt.Sprintf("issue|%s|%d", repo, number)
	return fetch(ctx, r, key, func(ctx context.Context) (Issue, error) {
		issue, err := r.client.GetIssue(ctx, repo.Owner, repo.Repo, number)
		if err != nil {
			return Issue{}, err
		}
		if issue == nil {
			return Issue{}, ErrNotFound
		}
		return *issue, nil
	})
}

// Comments lists the comments on a pull request or issue.
func (r *REST) Comments(ctx context.Context, repo RepoRef, number int, page Page) (List[IssueComment], error) {
	n, size := pageArgs(page, defaultCommentPageSize)
	key := fmt.Sprintf("comments|%s|%d|%d|%d", repo, number, n, size)
	return fetch(ctx, r, key, func(ctx context.Context) (List[IssueComment], error) {
		comments, err := r.client.GetPullRequestComments(ctx, repo.Owner, repo.Repo, number, n, size)
		if err != nil {
			return List[IssueComment]{}, err
		}
		return listOf(comments, size), nil
	})
}

// Merge merges a pull request and drops every cached read, so the next list
// shows the new state.
func (r *REST) Merge(ctx context.Context, repo RepoRef, number int, method, title, message string) error {
	if err := r.wait(ctx); err != nil {
		return err
	}
	if err := r.client.MergePullRequest(ctx, repo.Owner, repo.Repo, number, title, message, method); err != nil {
		return err
	}
	r.cache.invalidate()
	return nil
}

// ClosePR closes a pull request and drops every cached read.
func (r *REST) ClosePR(ctx context.Context, repo RepoRef, number int) error {
	if err := r.wait(ctx); err != nil {
		return err
	}
	if err := r.client.ClosePullRequest(ctx, repo.Owner, repo.Repo, number); err != nil {
		return err
	}
	r.cache.invalidate()
	return nil
}

// ApproveRun approves a workflow run and drops every cached read.
func (r *REST) ApproveRun(ctx context.Context, repo RepoRef, runID int64) error {
	if err := r.wait(ctx); err != nil {
		return err
	}
	if err := r.client.ApproveWorkflowRun(ctx, repo.Owner, repo.Repo, runID); err != nil {
		return err
	}
	r.cache.invalidate()
	return nil
}

// RateLimit reports the most recent rate limit headers.
func (r *REST) RateLimit() RateLimitInfo { return r.client.GetRateLimit() }

// Scopes reports the OAuth scopes of the active token.
func (r *REST) Scopes() []string { return r.client.GetScopes() }

// Invalidate drops every cached read. The UI calls it on an explicit refresh.
func (r *REST) Invalidate() { r.cache.invalidate() }

// equalLogin compares logins the way GitHub does: case-insensitively.
func equalLogin(a, b string) bool {
	return strings.EqualFold(a, b)
}

// anyLogin reports whether logins contains target, case-insensitively.
func anyLogin(logins []User, target string) bool {
	for _, u := range logins {
		if strings.EqualFold(u.Login, target) {
			return true
		}
	}
	return false
}
