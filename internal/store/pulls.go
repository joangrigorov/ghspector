package store

import (
	"context"
	"sort"
	"sync"
)

// ScopePRs lists pull requests across the context's repositories, most recently
// updated first, merged into one list in display order. The repository filter
// narrows the fan-out to a single repository by exact name.
//
// It is the shape the runs view already uses, applied to pull requests: the
// REST transport has no scope-wide list, so the aggregate is built from one
// request per repository. A repository that cannot be listed is skipped rather
// than failing the whole view, and the first error is returned only when every
// repository failed, so the user sees a reason instead of an empty list.
func (r *REST) ScopePRs(ctx context.Context, owner OwnerRef, q PRQuery) (List[PullRequest], error) {
	repos, err := r.repoTargets(ctx, owner, q.Repo)
	if err != nil {
		return List[PullRequest]{}, err
	}
	if len(repos) == 0 {
		return List[PullRequest]{}, nil
	}

	type result struct {
		prs     []PullRequest
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
			list, err := r.PullRequests(ctx, ref, PRQuery{Page: q.Page, State: q.State})
			if err != nil {
				results[i] = result{err: err}
				return
			}
			prs := list.Items
			// A pull request carries its repository so the view can show which
			// one each row belongs to and open the right detail route.
			for j := range prs {
				if prs[j].Repository.Name == "" {
					prs[j].Repository = repo
				}
			}
			results[i] = result{prs: prs, hasMore: list.HasMore}
		}(i, repo)
	}
	wg.Wait()

	merged := List[PullRequest]{}
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
		merged.Items = append(merged.Items, res.prs...)
		merged.HasMore = merged.HasMore || res.hasMore
	}
	if failures == len(results) {
		return List[PullRequest]{}, firstErr
	}

	merged.Items = filterPRs(merged.Items, q)
	SortPRs(merged.Items)
	return merged, nil
}

// MergePRs folds freshly polled pull requests into the list on screen. Entries
// are keyed by ID, so a pull request whose state changed replaces its row
// instead of appearing twice, and the result is in display order.
func MergePRs(existing, incoming []PullRequest) []PullRequest {
	if len(incoming) == 0 {
		return existing
	}
	byID := make(map[int64]PullRequest, len(existing)+len(incoming))
	for _, pr := range existing {
		byID[pr.ID] = pr
	}
	for _, pr := range incoming {
		byID[pr.ID] = pr
	}
	merged := make([]PullRequest, 0, len(byID))
	for _, pr := range byID {
		merged = append(merged, pr)
	}
	SortPRs(merged)
	return merged
}

// SortPRs orders pull requests for display, most recently updated first, which
// is the order the API returns for a single repository and the order a merged
// list has to be put back into.
func SortPRs(prs []PullRequest) {
	sort.SliceStable(prs, func(i, j int) bool {
		if !prs[i].UpdatedAt.Equal(prs[j].UpdatedAt) {
			return prs[i].UpdatedAt.After(prs[j].UpdatedAt)
		}
		return prs[i].Number > prs[j].Number
	})
}
