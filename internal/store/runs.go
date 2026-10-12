package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// maxAggregateRepos caps the run fan-out. Every repository costs one request per
// page, and no transport offers a scope-wide run list: the GraphQL schema's only
// run connection is Workflow.runs, which hangs off a single workflow file, and
// search has no run index. So the runs view covers the repositories pushed to
// most recently rather than every repository in the context.
//
// Eight repositories per five-second poll is 1.6 requests a second against the
// shared five-a-second allowance, which leaves room for the scope lists, whose
// searches cost one query each per ten-second poll. The cap is therefore a
// measured choice rather than a leftover.
const maxAggregateRepos = 8

// ScopeRuns lists runs across the context's repositories, most recently pushed
// first, merged into one list in display order. The repository filter narrows
// the fan-out to a single repository by exact name.
//
// A repository that cannot be listed — no Actions enabled, no access, a
// transient failure — is skipped rather than failing the whole view, because
// one such repository in a context would otherwise hide every other one. If
// every repository fails, the first error is returned so the user sees a
// reason instead of an empty list.
func (r *REST) ScopeRuns(ctx context.Context, owner OwnerRef, q RunQuery) (List[WorkflowRun], error) {
	repos, err := r.repoTargets(ctx, owner, q.Repo)
	if err != nil {
		return List[WorkflowRun]{}, err
	}
	if len(repos) == 0 {
		return List[WorkflowRun]{}, nil
	}

	number, size := pageArgs(q.Page, defaultRunPageSize)

	type result struct {
		runs    []WorkflowRun
		hasMore bool
		err     error
	}
	results := make([]result, len(repos))

	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo Repository) {
			defer wg.Done()
			ref := RepoRef{Owner: loginOf(repo), Repo: repo.Name}
			list, err := r.WorkflowRuns(ctx, ref, RunQuery{
				Page:  Page{Number: number, Size: size},
				Actor: q.Actor,
			})
			if err != nil {
				results[i] = result{err: err}
				return
			}
			runs := list.Items
			// Runs carry their repository so the view can show which one each
			// row belongs to. The list response usually embeds it; fill it in
			// when it does not.
			for j := range runs {
				if runs[j].Repository.Name == "" {
					runs[j].Repository = repo
				}
			}
			results[i] = result{runs: runs, hasMore: list.HasMore}
		}(i, repo)
	}
	wg.Wait()

	merged := List[WorkflowRun]{}
	failures := 0
	var firstErr error
	for _, res := range results {
		if res.err != nil {
			failures++
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		merged.Items = append(merged.Items, res.runs...)
		// A full page from any repository means there may be more to load.
		merged.HasMore = merged.HasMore || res.hasMore
	}
	if failures == len(results) {
		return List[WorkflowRun]{}, firstErr
	}

	SortRuns(merged.Items)
	return merged, nil
}

// repoTargets lists the repositories a scope-wide query covers: the context's
// repositories, most recently pushed first, narrowed by an exact repository
// name and capped at maxAggregateRepos.
func (r *REST) repoTargets(ctx context.Context, owner OwnerRef, filter string) ([]Repository, error) {
	list, err := r.Repositories(ctx, owner, Page{Number: 1, Size: defaultRepoPageSize})
	if err != nil {
		return nil, err
	}
	repos := list.Items
	if filter != "" {
		narrowed := make([]Repository, 0, 1)
		for _, repo := range repos {
			// The filter is an exact name match, which is how the repository
			// picker sets it.
			if repo.Name == filter {
				narrowed = append(narrowed, repo)
				break
			}
		}
		repos = narrowed
	}
	if len(repos) > maxAggregateRepos {
		repos = repos[:maxAggregateRepos]
	}
	return repos, nil
}

// loginOf reports the account a repository belongs to.
func loginOf(repo Repository) string {
	if repo.Owner == nil {
		return ""
	}
	return repo.Owner.Login
}

// MergeRuns folds freshly polled runs into the list on screen. Runs are keyed by
// ID, so a run whose status changed replaces its row instead of appearing
// twice, and the result is in display order.
func MergeRuns(existing, incoming []WorkflowRun) []WorkflowRun {
	if len(incoming) == 0 {
		return existing
	}
	byID := make(map[int64]WorkflowRun, len(existing)+len(incoming))
	for _, run := range existing {
		byID[run.ID] = run
	}
	for _, run := range incoming {
		byID[run.ID] = run
	}
	merged := make([]WorkflowRun, 0, len(byID))
	for _, run := range byID {
		merged = append(merged, run)
	}
	SortRuns(merged)
	return merged
}

// SortRuns orders runs for display: work that is running or waiting first, then
// by recency. The order puts the things a person is watching at the top without
// making them hunt for a finished run.
func SortRuns(runs []WorkflowRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		pi, pj := statusPriority(runs[i].Status), statusPriority(runs[j].Status)
		if pi != pj {
			return pi < pj
		}
		if !runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].CreatedAt.After(runs[j].CreatedAt)
		}
		return runs[i].ID > runs[j].ID
	})
}

// SortJobs orders the jobs of a run the same way, by the instant each started.
func SortJobs(jobs []WorkflowJob) {
	sort.SliceStable(jobs, func(i, j int) bool {
		pi, pj := statusPriority(jobs[i].Status), statusPriority(jobs[j].Status)
		if pi != pj {
			return pi < pj
		}
		if !jobs[i].StartedAt.Equal(jobs[j].StartedAt) {
			return jobs[i].StartedAt.After(jobs[j].StartedAt)
		}
		return jobs[i].ID > jobs[j].ID
	})
}

// statusPriority ranks a run or job for sorting. Queued work sorts before
// running work so a pending job is not buried under the noise of one that is
// already producing output.
func statusPriority(status string) int {
	switch status {
	case "queued":
		return 0
	case "in_progress":
		return 1
	default:
		return 2
	}
}

// matchActor matches a login against an actor filter, case-insensitively. A
// filter that names a bot by its bare name also matches the "[bot]" suffixed
// login the API reports.
func matchActor(actorLogin, filter string) bool {
	actor := strings.ToLower(actorLogin)
	term := strings.ToLower(filter)
	if actor == term {
		return true
	}
	return strings.HasPrefix(actor, term) && strings.HasSuffix(actor, "[bot]")
}

// filterActor applies the client-side half of the actor filter. The API is
// asked to filter as well, because it can see the whole list while this can
// only see the page it was given.
func filterActor(runs []WorkflowRun, actor string) []WorkflowRun {
	if actor == "" {
		return runs
	}
	out := make([]WorkflowRun, 0, len(runs))
	for _, run := range runs {
		if run.Actor != nil && matchActor(run.Actor.Login, actor) {
			out = append(out, run)
		}
	}
	return out
}

// PendingDeployments lists the deployments of a run that are waiting for
// approval.
func (r *REST) PendingDeployments(ctx context.Context, repo RepoRef, runID int64) ([]PendingDeployment, error) {
	if !repo.Valid() {
		return nil, fmt.Errorf("store: pending deployments need owner and repo")
	}
	key := fmt.Sprintf("deployments|%s|%d", repo, runID)
	return fetch(ctx, r, key, func(ctx context.Context) ([]PendingDeployment, error) {
		return r.client.GetPendingDeployments(ctx, repo.Owner, repo.Repo, runID)
	})
}

// ApproveDeployments approves the given environments of a run and drops every
// cached read, so the next list shows the new state.
func (r *REST) ApproveDeployments(ctx context.Context, repo RepoRef, runID int64, envIDs []int64, comment string) error {
	if !repo.Valid() {
		return fmt.Errorf("store: approving deployments needs owner and repo")
	}
	if len(envIDs) == 0 {
		return fmt.Errorf("store: no environments to approve")
	}
	if err := r.wait(ctx); err != nil {
		return err
	}
	if err := r.client.ApprovePendingDeployments(ctx, repo.Owner, repo.Repo, runID, envIDs, comment); err != nil {
		return err
	}
	r.cache.invalidate()
	return nil
}

// RepoPermission reports the viewer's permission level on a repository:
// admin, maintain, write, triage, read, or none.
func (r *REST) RepoPermission(ctx context.Context, repo RepoRef, login string) (string, error) {
	if !repo.Valid() || login == "" {
		return "", fmt.Errorf("store: permission needs owner, repo and login")
	}
	key := fmt.Sprintf("perm|%s|%s", repo, login)
	return fetch(ctx, r, key, func(ctx context.Context) (string, error) {
		return r.client.GetRepoPermission(ctx, repo.Owner, repo.Repo, login)
	})
}

// RequiredScopes are the OAuth scopes the write actions need.
var RequiredScopes = []string{"repo", "workflow"}

// MissingScopes reports which of RequiredScopes a token does not have. An empty
// scope list means the API did not report scopes — common for a fine-grained
// token — which is not the same as a denial, so nothing is reported missing.
func MissingScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	have := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		have[s] = true
	}
	var missing []string
	for _, required := range RequiredScopes {
		if !have[required] {
			missing = append(missing, required)
		}
	}
	return missing
}
