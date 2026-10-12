# feat(data): the GraphQL aggregation upgrade

Phase 5 of the v2 rewrite. The scope-wide pull request and issue lists stop being
a bounded guess: each page is one cursor-paginated GraphQL search query, with the
state and login filters applied by the API across the whole scope and the list
ending on the count the index reports. The REST fan-out stays underneath as the
fallback. `store.Store` did not change and `internal/ui` did not change.

Parent plan: `.agents/history/2026-10-12-v2-phase-0-spec-and-prep/implementation_plan.md`,
phase 5. Branch `feat/v2-graphql`, branched from `main` at `deacdfc` (the squash
of #21).

## What changed

| File | What it is |
| --- | --- |
| `internal/gql/gql.go` | New. The transport: `Client` around `shurcooL/githubv4` with a stock `http.Client` (15 s timeout, `http.DefaultTransport`, the bearer token added by a ~20-line `authTransport` `RoundTripper`), a `Reserver` seam onto the store's limiter, the last `rateLimit` report, and an error mapper onto `gh`'s taxonomy (rate-limited, forbidden, cancellation passed through, everything else wrapped). |
| `internal/gql/search.go` | New. `SearchIssues`/`SearchPullRequests` build the qualifier query, page it with `after` cursors, ask for `rateLimit { limit cost remaining resetAt }` on every query, and map responses into `gh.Issue`/`gh.PullRequest` — no parallel domain types. The response structs are the schema, field for field, because the decoder rejects a field the query did not select. |
| `internal/gql/gql_test.go` | New. 14 tests against a stub GraphQL server: query shape, domain mapping for both lists, totals and cursors, cost reservation, the error taxonomy, the qualifier builder, the app-login rule, and the two field shapes only the live schema can referee. |
| `internal/store/client.go` | New. `Client` composes `REST` with `gql`. `ScopePRs`/`ScopeIssues` run the search and fall back to the fan-out; `ScopeRuns` is restated on `REST` with the reason in one place. A 30-second breaker keeps a refused query from being retried on every poll, and `RateLimit()` reports whichever transport has less left. |
| `internal/store/search.go` | New. `searchScope`/`searchPager` walk the cursors before the page asked for through the cache, so a "Load More" costs one query and a poll that lands on the visible page costs none. `HasMore` comes from the index's match count, and the page is not post-filtered: the API's qualifiers are exact, and a second filter would desync the count from the rows. |
| `internal/gh/client.go` | `GetWorkflowRuns` returns the repository's `total_count` with the page, which is what lets the runs list end on a count instead of on a full page. |
| `internal/store/{rest.go,runs.go,runs_test.go,rest_test.go}` | `listOfTotal` turns that count into `HasMore`; `maxAggregateRepos` and `repoTargets` are documented as the runs list's own and the fallback's, with the schema evidence for why runs cannot leave. |
| `internal/store/search_test.go` | New. 15 tests: the same scope run against a GraphQL server and a REST one with equivalent domain objects asserted, the cursor walk, page caching, the fallback (success, failure preference, both failing), the breaker and its recovery, and the two-transport rate limit. |
| `internal/ui/*_test.go` | The shared list fixture server now refuses `/graphql` on purpose (`internal/ui/graphql_test.go` records why), so the UI tests drive the documented fallback and the search path is covered by the two suites above. The runs fixtures became page-aware so a full page no longer stands in for a count. |
| `internal/ui/graphql_test.go` | New. The coverage boundary in one place: which suite tests which path, and why the UI server does not model the search index. |
| `cmd/ghspector/main.go` | `store.New(token, "", "")` — the composed store, with both endpoints empty as production means. |
| `docs/spec.md`, `docs/architecture.md`, `README.md` | The phase's boxes ticked, the transport split rewritten around the schema evidence, §5.3's "fix in v2" recorded as delivered for two of three lists and not deliverable for the third, and the README's aggregation claim narrowed to what the app does. |

`go.mod` gains `github.com/shurcooL/githubv4` (direct, untagged pseudo-version)
and `github.com/shurcooL/graphql` (indirect). `go mod tidy` also records
`golang.org/x/oauth2` as an indirect requirement, because `githubv4`'s own
`example_test.go` imports it; `go list -deps` over this module's packages does
not include it, so no OAuth code is compiled or linked. Writing the
`Authorization` header directly is what keeps it that way — see the architecture
doc's §6.

## How to read it

**The schema decided the transport, not the port.** `Query.search` is the only
connection that spans a scope for issues and pull requests: `Organization`
exposes none, and `User.pullRequests`/`User.issues` answer the authored-by
question. `Workflow.runs` is the schema's only run connection and it hangs off a
single workflow file, and `Query.search` has no run index (`SearchType` is
discussion, issue, repository, user). That is why two lists moved and the third
could not, and it is written down in the code that would have to change, in
`docs/spec.md` §5.3, and in the architecture doc's transport section.

**One seam, so the UI is evidence rather than an excuse.** `deps.Store` is the
same interface, the routes call the same methods, and the whole phase landed
without touching a view. The search path is deliberately not exercised through
the UI test server: the store chooses the transport, so a UI test that served
GraphQL would test the same code twice. The boundary is stated in
`internal/ui/graphql_test.go`, and the live run below is what covers the join.

**A cost can only be read after the query it describes.** The limiter is shared,
so a GraphQL query reserves points where REST spends a request. The cost arrives
in the response, so the *previous* query's cost is what the next one reserves —
and the first implementation read it through a bound method value, which copies
its receiver when the value is created. Every reservation was one point and the
tests could not see it. It is a closure now, with a test asserting that the
reported cost is what the next query reserves.

**A rejected search is a fallback, not an error.** A scope the index will not
serve — an organization the token cannot see, an index outage, a token without
the scope search needs — falls back to the fan-out, which is what keeps the
screen working. A cancellation is not a fallback: another path would not help,
so it is returned. A failure also stops the search for 30 seconds, so a poll
every five seconds does not turn one refusal into twelve; the fallback's error is
the one reported, because it is the path that covers the whole scope.

**Both lists end on a count now.** The Load More row used to appear whenever a
page came back full, which is why a list that ended exactly on a page boundary
offered a row that loaded nothing. Pull requests and issues read the index's
match count, and workflow runs read the repository's `total_count`.

## Verification

The seven checks, on the final commit of the branch:

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race -count=1 ./...` | pass — 10 packages, including the two new suites |
| `golangci-lint run ./...` | pass — 0 issues |
| `govulncheck ./...` | 14 vulnerabilities, all in the Go standard library at `go1.26.5` and all fixed by a later patch of the same series; 7 more in packages and modules the code does not call, all standard library as well. Nothing from the modules this phase added (finding F5, standalone, reproduced on the previous `main`) |
| `actionlint` | pass |
| `go mod tidy -diff` | pass (no change) |
| `gofmt -l internal/ cmd/` | clean except the pre-existing `internal/gh/{client,client_test,types}.go` drift, which predates the rewrite and is left alone |

**CI.** The `Test and Lint` workflow on this branch passed on the code commit
`cf0c9f4` in 1m29s (run
[38278594813](https://github.com/joangrigorov/ghspector/actions/runs/38278594813)) —
the same checks, run by the workflow's own steps, so the table above is not only
a local record.

### Run against GitHub

This is the first phase whose feature a stub server cannot referee: a stub
answers whatever it is asked, so a query the real schema rejects passes every
test. So the branch was run against the real API, read-only, and that run found a
bug the suite could not.

- **The search was dead and the app looked fine.** Every query came back
  `graphql: Field 'databaseId' doesn't exist on type 'Actor'` — the `Actor`
  interface carries the login and nothing else, while `databaseId` lives on
  `User`, `Bot` and the other implementors. The store fell back, so the lists
  still rendered; they were the fan-out's lists. The id is now read through the
  fragments of the concrete types, the two positions typed `User` read it
  directly, and `TestQueriesSelectAnActorsIDOnItsOwnType` pins both shapes for
  both queries.
- **An app's account has three spellings.** GraphQL reports a bot as
  `github-actions`, REST as `github-actions[bot]`, and the index matches only
  `author:app/github-actions` — the bare slug returns nothing with the scope
  qualifier or without it. The login is restored to the REST spelling from the
  `__typename` the `Bot` fragment carries, so the column reads the way it always
  did, and `appQualifier` turns a login ending in `[bot]` into `app/<slug>` on the
  way to the index, so a filter built from what is on screen matches.

What the run then showed, in a 140×40 pane, with the binary built from this
branch:

- **Pull requests**: three across the scope where the fan-out sees two — the third
  is an open pull request in a repository far outside the eight most recently
  pushed, which is the difference this phase buys. Bot authors read
  `github-actions[bot]` and `dependabot[bot]`, the REST spelling.
- **The filter**: `f`, a login, and the author role narrowed the list to the one
  pull request that app opened, with the filter line reading
  `Filter: state open, author @dependabot[bot]`; `x` cleared it and the full list
  came back. The screen reaches the index with `author:app/dependabot`.
- **Issues**: eleven open issues across two repositories, where the
  eight-repository fan-out sees eight. The three it cannot see live in a
  repository that is not among the eight most recently pushed — the same gap, on
  the other list.
- **Workflow runs**: unchanged on its fan-out, with the Load More row and the
  status glyphs, as the schema evidence says it must be.

The live run used the app's own credential path: it resolved a token through the
third source in the README's hierarchy (`gh auth token`) because the session's
environment has the GitHub CLI logged in. Every call was a read; no token was
printed, written, or passed anywhere it does not belong, and the temporary probe
files that held the live checks were deleted rather than committed.

### What stands in for browser verification

A terminal UI has no browser to drive, so the substitute is stated rather than
implied:

- **58 golden renders** across every screen at 80×24, 120×40 and 40×15, all
  unchanged by this phase — which is the point: the views did not move.
- **`teatest` runs through the real Bubble Tea runtime** for the tab, filter,
  pagination and navigation flows, unchanged and still passing.
- **Scripted tests that assert the requests a screen sent**, against a fixture
  server that serves REST and refuses `/graphql`, so the fallback the UI would use
  is the one under test.
- **The binary was built and run** at the project root, in a pty and then in a
  tmux pane, against live data as described above.

### Not verified

- Long-running behaviour: how the two allowances interact over an hour of
  polling, and whether the 1000-match cap on a search ever gets reached in a scope
  this size.
- Other accounts and organizations: an organization scope, a token with SSO
  authorization to complete, a scope where the index refuses for a reason other
  than the one this token hits. The fallback is tested with fixtures, not against
  a real refusal.
- Team reviewers (the union's other arm) and repo-level `RepoStats`, both
  deferred.
- Windows terminals, and screen sizes other than the ones the goldens pin.

## Deviations from the plan for this phase

1. **Workflow runs keep the REST fan-out.** The plan asked for all three lists on
   GraphQL. `Workflow.runs` is the schema's only run connection and it hangs off a
   single workflow file, and `Query.search` has no run index. The list therefore
   keeps the fan-out and the cap, and `docs/spec.md` §5.3 records the same
   evidence.
2. **The `limit > 8` cap was consolidated, not deleted.** The four duplicated
   fan-out bodies in the previous `internal/tui/update.go` are gone: one
   `repoTargets` helper and one `maxAggregateRepos = 8` constant now serve the runs
   list and the fallback. The cap itself survives where a fan-out survives.
3. **`golang.org/x/oauth2` is not a code dependency.** The transport writes the
   bearer header itself; the module appears in `go.mod` only because a dependency's
   test file imports it.
4. **A failed search is not retried immediately.** A refusal puts the search to
   sleep for 30 seconds, with the fallback serving the screen in the meantime.
5. **Deferred:** platform-specific `RepoStats` on `RepoRef`, automatic REST
   failure tracking, and multi-user caching. Each would add state with no caller
   yet.

## For the review gate

Release numbering is settled: one `0.4.0` for the whole rewrite, so the release
pull request stays open and unmerged and release-please keeps refreshing it as
phases land. It renumbers itself when it refreshes — the open one is #22, and #15
is the instance it replaced when Phase 4 merged — so the number is worth reading
from the pull requests rather than from this paragraph. Nothing in this branch
should be released on its own.

Two things worth a look on the review pass: whether the search index's extra
couple of hundred milliseconds per page is worth the scope-wide truth (it is a
round trip per page either way, but the fan-out runs in parallel), and whether
`maxAggregateRepos = 8` is still the right number for the runs list now that it is
the only fan-out left.
