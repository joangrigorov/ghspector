# feat(tui): the Issues vertical slice

Phase 4 of the v2 rewrite. The third list becomes real: the Issues tab, the issue
detail screen, and the conversation. With this phase every screen the previous
version had is on the stack, and the three top-level lists have the same shape —
filters, pagination, polling, a Load More sentinel.

Parent plan: the phased plan for the v2 rewrite
(`.agents/history/2026-10-12-v2-phase-0-spec-and-prep/implementation_plan.md`,
phase 4). Branch `feat/v2-issues`, branched from `main` at `fb8427a` (the squash
of #20).

## What changed

| File | What it is |
| --- | --- |
| `internal/store/issues.go` | New. `ScopeIssues` aggregates the context's repositories with the `repoTargets` fan-out the runs and pull request lists use: one goroutine per repository, the repository embedded on each row, `HasMore` OR-ed across pages, and the first error returned only when every repository failed. `MergeIssues` folds a poll into the list by ID, and `SortIssues` orders by `updated_at` descending with the higher number breaking a tie. |
| `internal/store/store.go` | `IssueQuery` gains `Repo` and `Assignee` (so the issue filters are one query object, like `PRQuery`), and the `Store` interface gains `ScopeIssues` next to `Issues`. |
| `internal/store/rest.go` | `REST.Issues` applies `filterIssues` inside its cached fetch, so the page stays reusable across filter changes and the filters see the merged list rather than one repository's page. |
| `internal/gh/client_test.go` | The pull-request exclusion in `GetIssuesWithState` gets a transport test of its own, and asserts that `HasMore` comes from the raw page. |
| `internal/ui/issues.go` | New. `issuesTab`: the scope-wide list at page size 50, the open → closed → all state cycle, the two exclusive login filters, the free-text filter path through the login prompt and the two-role picker, the Load More sentinel, the poll, and the browser key. |
| `internal/ui/issuedetails.go` | New. `IssueDetails` (description beside the metadata sidebar, Tab focus toggle, 1-line and 6-line scrolling, `w`, `c`, `r`, its own poll) and `IssueComments` (the conversation, scrolled with `u`/`d`, refreshable). |
| `internal/ui/mainroute.go` | `Main` hosts `issuesTab` in every path the other two tabs have: content, actions, data messages, the account change, the poll tick and the lazy fetch on first open. The tab-strip placeholder and the `tabMeta.Empty` field are gone. |
| `internal/ui/prdetails.go`, `prpanes.go` | The shared pieces the issue screen needed: `milestoneTitle`/`labelNames` became `milestoneName`/`labelList` over `store.Milestone`/`store.Label`, `commentLines` became the free `commentBlockLines`, and `infoColumns` became `detailColumns` because two screens now split a row the same way. |
| `internal/auth/auth.go` | `polling.issues_interval_seconds` (default 10 s). The legacy `polling_interval_seconds` now sets all three intervals; the per-tab keys still win. |
| `internal/ui/model.go`, `cmd/ghspector/main.go` | `PollIssues` through `Options`, `Deps` and `pollInterval`, which now selects on `keymap.MainIssues`. |
| `internal/keymap/keymap.go` | `IssueComments` gains `u`/`d`, so a thread scrolls the way the pull request conversation does. `docs/keybindings.md` is regenerated from the table. |
| `internal/ui/{issues_test.go,prs_test.go,golden_test.go,flow_test.go,poll_test.go}` | The issue tests, the issue fixtures on the shared list-screen fixture server, 16 new goldens, a `teatest` run through the issue screens, and the poll cadence test. |
| `internal/store/issues_test.go`, `internal/auth/auth_test.go` | Store tests for the fan-out, the sort, the merge, the filters, `HasMore`, the partial-failure rule and the empty context; auth tests for the three-interval resolution rule. |
| `docs/{spec,architecture}.md`, `README.md` | The phase 4 exit criteria are ticked, the §5.4 "fix in v2" is recorded as delivered, the architecture build status gains the phase, and the README gains the third polling key and the issue capability. |

## How to read it

**The list is the same screen as the other two, on purpose.** `issuesTab` is
`prsTab` with the reviewer filter removed and the page size the spec names: the
same `load`/`start`/`poll`/`apply`/`move`/`open`/`filterBy`/`clearFilters` shape,
the same Load More sentinel, the same "one of the login filters at a time" rule,
so a person who has used the PRs tab already knows this one. The one real
difference is at the transport: the issues endpoint returns pull requests too, and
the store drops them before the tab sees them.

**The exclusion is asserted three times, at three layers.** The transport test
serves a page with a pull request in it and requires it to be dropped; the store
test requires the same of `ScopeIssues` and of the *raw* page length for
`HasMore`; the UI test counts the rows on screen and requires the title of the
entry that is really a pull request to be absent from the frame. A fixture that
silently stopped including a pull request would fail the UI test, which is the
point of testing the count rather than only the absence.

**The two description-beside-a-sidebar screens share one column split.** The
issue sidebar holds the same eight rows the pull request details block holds, so
`detailColumns` and `metadataRow` are shared rather than copied, and the issue
screen drops the sidebar at exactly the width the pull request Info row does.
`metadataRow`'s wrapping behaviour — a value too long for the sidebar is
continued on an indented line, and a single word longer than the value column is
broken — is what the 60×16 golden shows for `enhancement`.

**Focus, not selection.** The issue sidebar has nothing to select, so Tab is what
tells the user which pane the scroll keys act on: with the sidebar focused, `j`,
`k`, `u` and `d` change nothing, and the pane title carries the `▶` marker. The
scripted test pins both the step sizes and the freeze.

## Verification

The seven checks, on the final commit of the branch:

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race -count=1 ./...` | pass — 324 test functions, 483 leaf runs, 9 packages |
| `golangci-lint run ./...` | pass — 0 issues |
| `govulncheck ./...` | 14 vulnerabilities, all in the Go standard library at `go1.26.5` and all fixed by `go1.26.6` (finding F5, standalone, reproduced on the previous `main`) |
| `actionlint` | pass |
| `go mod tidy -diff` | pass (no change) |
| `gofmt -l internal/ cmd/` | clean except the pre-existing `internal/gh/{client,client_test,types}.go` drift |

Because this is a terminal program, browser verification has no equivalent here.
What stands in for it:

- **58 golden renders**, 16 of them new (the list at three widths, the filtered
  and closed lists, the Load More sentinel, the three overlay screens, the
  details at three widths, the sidebar focus, and the conversation at two
  widths). The new files were read rather than only regenerated; the narrow
  frames are the ones that matter: at 60×16 the issue list still draws `ST`,
  `ISSUE` and `TITLE`, and the details screen keeps its 24-cell sidebar with the
  description beside it.
- **A `teatest` run through the real Bubble Tea runtime**
  (`TestFlowThroughTheIssueScreens`): the tab strip, the first fetch, Enter into
  the details, Tab, `c` into the conversation, and two Escs back to the list,
  asserting the breadcrumb, the loaded list and the frame geometry on the model
  the program exits with. `TestFlowTabCyclingKeepsThePullRequestList` was
  extended: the issue list is fetched once when the tab is first opened, and
  returning to the PRs tab does not refetch it.
- **Scripted tests that assert the requests, not only the pixels**: the state
  cycle against a request log, the repo picker against the fan-out it narrows,
  and the refresh keys against the request counts.
- **A layout property sweep** over seven widths for both new screens, where every
  frame must be exactly the size it was given.
- **The binary was built and run.** `./ghspector` at the project root is built
  from this branch. In a pty it starts, enters the alternate screen, sets the
  window title, draws the splash and its version line, and exits cleanly on `q`.
  It was not driven against live data: the API base URL is deliberately not
  configurable outside tests, and no real token is available in this environment,
  so the screens themselves are verified by the two mechanisms above.

Not verified, and worth a look on the review pass: how the issue list and the
sidebar read in a real terminal at a size other than the ones the goldens pin,
and whether 50 issues per page feels right on a slow connection.

## Open question for the review

**Release numbering.** This is the fourth phase merged without a release, so
`main` now carries four phases of work that release-please describes as
`0.4.0` (PR #15, `chore(main): release 0.4.0`, still open and deliberately not
merged). Two ways forward, and the choice is not mine to make:

1. Cut one release for the whole rewrite once the phases that remain (GraphQL,
   markdown, the final review pass) have landed. PR #15 is superseded and should
   be closed rather than merged, and the number is then chosen once, against a
   version of the app that is whole.
2. Resume per-phase numbering now: close #15, and let the next release PR carry
   the four phases as one `0.4.0`, then one number per phase after that.

Either way, merging #15 as it stands would publish a `0.4.0` whose notes describe
only the first phase's worth of work.
