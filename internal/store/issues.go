package store

import (
	"context"
	"sort"
	"sync"
)

// ScopeIssues lists issues across the context's repositories, most recently
// updated first, merged into one list in display order. The repository filter
// narrows the fan-out to a single repository by exact name.
//
// It is the shape ScopePRs already uses, applied to issues: the REST transport
// has no scope-wide list, so the aggregate is built from one request per
// repository. A repository that cannot be listed is skipped rather than failing
// the whole view, and the first error is returned only when every repository
// failed, so the user sees a reason instead of an empty list.
func (r *REST) ScopeIssues(ctx context.Context, owner OwnerRef, q IssueQuery) (List[Issue], error) {
	repos, err := r.repoTargets(ctx, owner, q.Repo)
	if err != nil {
		return List[Issue]{}, err
	}
	if len(repos) == 0 {
		return List[Issue]{}, nil
	}

	type result struct {
		issues  []Issue
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
			// The login filters are applied once, over the merged page, so the
			// per-repository page stays reusable across filter changes.
			list, err := r.Issues(ctx, ref, IssueQuery{Page: q.Page, State: q.State})
			if err != nil {
				results[i] = result{err: err}
				return
			}
			issues := list.Items
			// An issue carries its repository so the view can show which one
			// each row belongs to and open the right detail route.
			for j := range issues {
				if issues[j].Repository.Name == "" {
					issues[j].Repository = repo
				}
			}
			results[i] = result{issues: issues, hasMore: list.HasMore}
		}(i, repo)
	}
	wg.Wait()

	merged := List[Issue]{}
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
		merged.Items = append(merged.Items, res.issues...)
		merged.HasMore = merged.HasMore || res.hasMore
	}
	if failures == len(results) {
		return List[Issue]{}, firstErr
	}

	merged.Items = filterIssues(merged.Items, q)
	SortIssues(merged.Items)
	return merged, nil
}

// MergeIssues folds freshly polled issues into the list on screen. Entries are
// keyed by ID, so an issue whose state changed replaces its row instead of
// appearing twice, and the result is in display order.
func MergeIssues(existing, incoming []Issue) []Issue {
	if len(incoming) == 0 {
		return existing
	}
	byID := make(map[int64]Issue, len(existing)+len(incoming))
	for _, issue := range existing {
		byID[issue.ID] = issue
	}
	for _, issue := range incoming {
		byID[issue.ID] = issue
	}
	merged := make([]Issue, 0, len(byID))
	for _, issue := range byID {
		merged = append(merged, issue)
	}
	SortIssues(merged)
	return merged
}

// SortIssues orders issues for display, most recently updated first, which is
// the order the API returns for a single repository and the order a merged list
// has to be put back into. Two issues touched in the same second fall back to
// the higher number, so the order is stable.
func SortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if !issues[i].UpdatedAt.Equal(issues[j].UpdatedAt) {
			return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
		}
		return issues[i].Number > issues[j].Number
	})
}
