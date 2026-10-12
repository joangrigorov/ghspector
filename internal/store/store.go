// Package store is the single seam between the UI and GitHub.
//
// Before the rewrite every route held a *gh.Client and called the transport
// directly: 60-odd call sites, each building its own request, doing its own
// fan-out, and racing its own cached slices. Nothing could be tested without a
// live token, and nothing could be swapped for GraphQL later without touching
// every screen.
//
// Here the UI depends on the Store interface only. Transport, caching, rate
// limiting, pagination and fan-out live behind it. The UI never imports a
// transport package.
package store

import (
	"context"
	"errors"

	"ghspector/internal/gh"
)

// Domain types are aliases, so a route can name store.WorkflowRun and the
// concrete transport stays an implementation detail. When the GraphQL transport
// lands these aliases move with the types and no route changes.
type (
	User              = gh.User
	Org               = gh.Org
	Repository        = gh.Repository
	WorkflowRun       = gh.WorkflowRun
	WorkflowJob       = gh.WorkflowJob
	JobStep           = gh.JobStep
	PendingDeployment = gh.PendingDeployment
	PullRequest       = gh.PullRequest
	Label             = gh.Label
	Milestone         = gh.Milestone
	CommitFile        = gh.CommitFile
	RepositoryCommit  = gh.RepositoryCommit
	CheckRun          = gh.CheckRun
	CheckApp          = gh.CheckApp
	Issue             = gh.Issue
	IssueComment      = gh.IssueComment
	RateLimitInfo     = gh.RateLimitInfo
)

// Transport errors are re-exported so callers classify a failure without
// importing the transport.
var (
	ErrRateLimited = gh.ErrRateLimited
	ErrForbidden   = gh.ErrForbidden
)

// ErrNotFound reports that a resource does not exist or is not visible to the
// authenticated token.
var ErrNotFound = errors.New("store: not found")

// RepoRef identifies one repository. Routes carry it instead of reading owner
// and repo back out of shared model fields, which is how the previous version
// ended up mixing the selected pull request's repository into unrelated calls.
type RepoRef struct {
	Owner string
	Repo  string
}

// Valid reports whether both halves are set.
func (r RepoRef) Valid() bool { return r.Owner != "" && r.Repo != "" }

// String renders the reference as owner/repo.
func (r RepoRef) String() string {
	if r.Repo == "" {
		return r.Owner
	}
	return r.Owner + "/" + r.Repo
}

// OwnerRef identifies an account whose repositories can be listed.
type OwnerRef struct {
	// Login is the user or organization name.
	Login string
	// IsOrg selects the organization endpoint rather than the user endpoint.
	IsOrg bool
}

// String renders the reference as a display label.
func (o OwnerRef) String() string {
	if o.Login == "" {
		return "none"
	}
	if o.IsOrg {
		return o.Login + " (org)"
	}
	return o.Login
}

// Page selects a page of a paginated list.
type Page struct {
	// Number is 1-based. Zero is treated as 1.
	Number int
	// Size is the page size. Zero means the store's default.
	Size int
}

// List is one page of results. HasMore drives the "Load More" row, so it is
// reported by the store rather than guessed by a route from the item count: on
// the issues endpoint the raw page includes pull requests, and the filtered
// count can be short while another page still exists.
type List[T any] struct {
	Items   []T
	HasMore bool
}

// RunQuery filters a workflow run list.
type RunQuery struct {
	Page Page
	// Actor narrows the list to runs started by one login. Empty means all.
	Actor string
	// Repo narrows the list to one repository by exact name. Empty means every
	// repository in the context.
	Repo string
}

// PRQuery filters a pull request list. The filter fields are applied by the
// store, because the REST transport cannot express them in one request.
type PRQuery struct {
	Page  Page
	State string
	// Repo narrows the scope-wide list to one repository by exact name. Empty
	// means every repository in the context. The single-repository PullRequests
	// call ignores it.
	Repo string
	// Author, Assignee and Reviewer narrow by login. Empty means no filter.
	Author   string
	Assignee string
	Reviewer string
}

// IssueQuery filters an issue list.
type IssueQuery struct {
	Page   Page
	State  string
	Author string
}

// Store is the read and write surface the UI is allowed to use.
//
// Every method takes a context and returns the domain types above. Reads may be
// served from cache; writes invalidate it.
type Store interface {
	// Viewer returns the authenticated account.
	Viewer(ctx context.Context) (User, error)
	// Organizations returns the organizations the viewer belongs to.
	Organizations(ctx context.Context) ([]Org, error)
	// Repositories lists repositories owned by owner, most recently pushed
	// first.
	Repositories(ctx context.Context, owner OwnerRef, page Page) (List[Repository], error)

	// WorkflowRuns lists runs in one repository.
	WorkflowRuns(ctx context.Context, repo RepoRef, q RunQuery) (List[WorkflowRun], error)
	// ScopeRuns lists runs across the context's repositories, merged into one
	// list in display order. It is what the runs view asks for; WorkflowRuns
	// exists for the single-repository case and for tests.
	ScopeRuns(ctx context.Context, owner OwnerRef, q RunQuery) (List[WorkflowRun], error)
	// WorkflowRun fetches one run.
	WorkflowRun(ctx context.Context, repo RepoRef, runID int64) (WorkflowRun, error)
	// Jobs lists the jobs of a run attempt. A zero attempt means the latest.
	Jobs(ctx context.Context, repo RepoRef, runID int64, attempt int, page Page) (List[WorkflowJob], error)
	// JobLogs fetches the plain-text log of one job.
	JobLogs(ctx context.Context, repo RepoRef, jobID int64) (string, error)

	// PullRequests lists pull requests in one repository.
	PullRequests(ctx context.Context, repo RepoRef, q PRQuery) (List[PullRequest], error)
	// ScopePRs lists pull requests across the context's repositories, merged
	// into one list in display order. It is what the pull request view asks
	// for; PullRequests exists for the single-repository case and for tests.
	ScopePRs(ctx context.Context, owner OwnerRef, q PRQuery) (List[PullRequest], error)
	// PullRequest fetches one pull request.
	PullRequest(ctx context.Context, repo RepoRef, number int) (PullRequest, error)
	// PullRequestCommits lists the commits of a pull request.
	PullRequestCommits(ctx context.Context, repo RepoRef, number int, page Page) (List[RepositoryCommit], error)
	// PullRequestFiles lists the changed files of a pull request.
	PullRequestFiles(ctx context.Context, repo RepoRef, number int, page Page) (List[CommitFile], error)
	// Commit fetches one commit with the files it changed.
	Commit(ctx context.Context, repo RepoRef, sha string) (RepositoryCommit, []CommitFile, error)
	// CheckRuns lists the latest check run per check name for a ref. The
	// transport already reduces the response to one entry per name, so the
	// result is not paginated.
	CheckRuns(ctx context.Context, repo RepoRef, ref string) ([]CheckRun, error)

	// Issues lists issues in one repository. Pull requests are excluded, as
	// the GitHub issues endpoint returns both.
	Issues(ctx context.Context, repo RepoRef, q IssueQuery) (List[Issue], error)
	// Issue fetches one issue.
	Issue(ctx context.Context, repo RepoRef, number int) (Issue, error)
	// Comments lists the comments on a pull request or issue.
	Comments(ctx context.Context, repo RepoRef, number int, page Page) (List[IssueComment], error)

	// Merge merges a pull request. Method is one of merge, squash or rebase.
	Merge(ctx context.Context, repo RepoRef, number int, method, title, message string) error
	// ClosePR closes a pull request without merging.
	ClosePR(ctx context.Context, repo RepoRef, number int) error

	// PendingDeployments lists the deployments of a run waiting for approval.
	PendingDeployments(ctx context.Context, repo RepoRef, runID int64) ([]PendingDeployment, error)
	// ApproveDeployments approves the given environments of a run.
	ApproveDeployments(ctx context.Context, repo RepoRef, runID int64, envIDs []int64, comment string) error
	// ApproveRun approves a workflow run from a fork.
	ApproveRun(ctx context.Context, repo RepoRef, runID int64) error
	// RepoPermission reports the viewer's permission level on a repository:
	// admin, maintain, write, triage, read, or none.
	RepoPermission(ctx context.Context, repo RepoRef, login string) (string, error)

	// RateLimit reports the most recent rate limit headers.
	RateLimit() RateLimitInfo
	// Scopes reports the OAuth scopes of the active token, when the API
	// supplies them. An empty result means unknown, which is not the same as
	// insufficient.
	Scopes() []string
	// Invalidate drops every cached read, so the next call refetches.
	Invalidate()
}
