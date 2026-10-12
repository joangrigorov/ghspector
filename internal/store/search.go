package store

import (
	"context"
	"fmt"

	"github.com/joangrigorov/ghspector/internal/gql"
)

// The scope-wide pull request and issue lists are search queries: one request
// for the whole scope instead of one per repository, with the API applying the
// state and login filters across the scope and reporting how many matches exist.
//
// Two things differ from the REST fan-out, deliberately:
//
//   - The login filters are the API's rather than the store's. The search
//     qualifiers are exact, so a page does not need filtering again afterwards;
//     filtering it again could only drop rows the API counted, which would make
//     the count and the page disagree. The REST path keeps its own filter,
//     because the REST list endpoints cannot express one.
//   - HasMore comes from the count the index reports, so the Load More row is
//     offered exactly while matches remain instead of whenever a page came back
//     full.
//
// The index is a different service, and its limits are its own: it lags a
// write by seconds, it holds at most 1000 matches for one query — which is also
// where it stops serving pages — and it draws on a separate search allowance.
// Those are why a query the index will not serve falls back to the fan-out.

// searchPager loads one page of a search. The cursor that starts the page is
// passed in, because a cursor API names pages by where they begin.
type searchPager[T any] func(ctx context.Context, after string) (gql.SearchPage[T], error)

// searchKey identifies a search so its pages share cache entries. The cursor is
// not part of it: a page's cursor comes from the page before it, and the page
// number is what names the page.
func searchKey(in gql.SearchInput) string {
	scope := "user"
	if in.IsOrg {
		scope = "org"
	}
	return fmt.Sprintf("search|%s|%s|%s|%s|%s|%s|%s|%s|%d",
		in.Kind, scope, in.Owner, in.State, in.Repo, in.Author, in.Assignee, in.Reviewer, in.First)
}

// searchPage loads one page of a search through the store's cache, so a poll and
// a page walk share one request.
func searchPage[T any](ctx context.Context, c *Client, key string, page int, after string, call searchPager[T]) (gql.SearchPage[T], error) {
	return fetch(ctx, c.REST, fmt.Sprintf("%s|%d", key, page), func(ctx context.Context) (gql.SearchPage[T], error) {
		return call(ctx, after)
	})
}

// searchScope returns one page of a scope-wide search, walking the pages before
// it to collect the cursors that start them.
//
// A cursor API cannot jump to page N, so the pages in between are fetched. Each
// one goes through the cache, so the walk costs a request only for a page that is
// not already held, and the page before the one being loaded is the page on
// screen.
func searchScope[T any](ctx context.Context, c *Client, key string, size, wanted int, call searchPager[T]) (List[T], error) {
	var (
		after string
		page  int
		items []T
		total int
	)
	for page = 1; ; page++ {
		result, err := searchPage(ctx, c, key, page, after, call)
		if err != nil {
			return List[T]{}, err
		}
		items, total = result.Items, result.Total
		if page >= wanted || !result.HasNext {
			// Either the page that was asked for, or the last one the index has.
			break
		}
		after = result.NextCursor
	}
	return listOfTotal(items, page, size, total), nil
}

// searchPRs returns one page of the scope-wide pull request search.
func (c *Client) searchPRs(ctx context.Context, owner OwnerRef, q PRQuery) (List[PullRequest], error) {
	number, size := pageArgs(q.Page, defaultPRPageSize)
	in := gql.SearchInput{
		Kind:     gql.KindPullRequest,
		Owner:    owner.Login,
		IsOrg:    owner.IsOrg,
		Repo:     q.Repo,
		State:    q.State,
		Author:   q.Author,
		Assignee: q.Assignee,
		Reviewer: q.Reviewer,
		First:    size,
	}
	if !c.searchUsable() {
		return List[PullRequest]{}, errSearchSkipped
	}
	list, err := searchScope(ctx, c, searchKey(in), size, number, func(ctx context.Context, after string) (gql.SearchPage[PullRequest], error) {
		page := in
		page.After = after
		return c.gql.SearchPullRequests(ctx, page)
	})
	if err != nil {
		c.searchFailed(err)
		return List[PullRequest]{}, err
	}
	// The display order is the store's to decide, not the index's.
	SortPRs(list.Items)
	return list, nil
}

// searchIssues returns one page of the scope-wide issue search.
func (c *Client) searchIssues(ctx context.Context, owner OwnerRef, q IssueQuery) (List[Issue], error) {
	number, size := pageArgs(q.Page, defaultIssuePageSize)
	in := gql.SearchInput{
		Kind:     gql.KindIssue,
		Owner:    owner.Login,
		IsOrg:    owner.IsOrg,
		Repo:     q.Repo,
		State:    q.State,
		Author:   q.Author,
		Assignee: q.Assignee,
		First:    size,
	}
	if !c.searchUsable() {
		return List[Issue]{}, errSearchSkipped
	}
	list, err := searchScope(ctx, c, searchKey(in), size, number, func(ctx context.Context, after string) (gql.SearchPage[Issue], error) {
		page := in
		page.After = after
		return c.gql.SearchIssues(ctx, page)
	})
	if err != nil {
		c.searchFailed(err)
		return List[Issue]{}, err
	}
	SortIssues(list.Items)
	return list, nil
}
