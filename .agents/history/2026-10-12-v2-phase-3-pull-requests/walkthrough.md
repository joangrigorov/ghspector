# feat(tui): Phase 3 — Pull requests vertical slice

Third implementation phase of the v2 TUI rewrite. Phase 1 rebuilt the shell
(#17); Phase 2 made the Workflows tab, the Jobs route and the Logs route real
(#18). This phase makes the Pull Requests tab real: the list, the detail screen
with its five sub-tabs, the checks that resolve back to the runs that produced
them, and the merge and close flows — plus the write gate that decides whether
either is offered.

Parent plan: the phased plan for the v2 rewrite (Phase 3 of 7). Issues remain a
placeholder.

## What changed

| Package | What it is |
| --- | --- |
| `internal/store/pulls.go` | `PullRequests` across the context's repositories: the same 8-repository fan-out as the runs list (`repoTargets`, one cap), `state` forwarded as a query param, author/assignee/reviewer re-checked client-side over the fetched page, sorted by `updated_at` descending then by the higher number, and poll results merged by ID so a pull request that changed state replaces its row. Plus `PullRequest` detail, `PRCommits`, `PRFiles`, `Comments`, `Commit`, `CheckRuns`, `Merge`, `Close`, `RepoPermission`. |
| `internal/ui/prs.go` | The Pull Requests tab: the table, the state cycle, the three "involves me" quick filters, the free-text filter chain, pagination via the `Load More` sentinel, and the empty and error states. |
| `internal/ui/prdetails.go` | The `PRDetails` route: a numbered sub-tab strip (Info, Commits, Files, Checks, Comments), the three-pane Info row, the merge-method select, the confirm-merge and confirm-close screens, and the write gate. |
| `internal/ui/prpanes.go` | The panes: description, metadata sidebar, checks, commit list, file list with its patch viewport, and the per-line diff styling. |
| `internal/ui/merge.go` | The merge and close flows as overlays plus the persisted default method, with the `d` key cycling it. |
| `internal/ui/hyperlink.go` | OSC 8 links, in one place, gated on the theme. |
| `internal/overlay/method.go` | The merge-method select, built on the shared `Menu`, so the flow is overlays rather than a modal state machine. |
| `internal/keymap` | `MainPRs`, `PRDetails`, `PRCommits`, `CommitDetails`, `PRDiff`, `PRComments`, `PRFilterType`, `MergeMethod`, `MergeConfirm`, `CloseConfirm` scope carry the phase's keys: the sub-tab numbers `1`–`5` alongside the v1 letters, and `u`/`d`/`PgUp`/`PgDn` for the new viewports. |
| `internal/auth` | `SetDefaultMergeMethod` edits the config file as a YAML document instead of re-encoding it. |
| `internal/ui/help.go` | The status legend now leads the help body, drawn by `statusIndicator` so it cannot name a symbol the tables do not draw. |
| `cmd/ghspector` | `run(args, stdout)` takes its arguments and version writer as parameters, which is what lets the CLI flags have tests. |

## Four defects found while building this

Each was found by exercising a screen rather than by reading it.

1. **The write gate failed open (finding F1).** The baseline's
   `viewerCanMerge()` answered "yes" whenever the `X-OAuth-Scopes` header was
   absent, so a token the API said nothing about was advertised as able to merge
   and then rejected at the API. `ui.decidePRWrite` asks the scope question
   first and, when the header is absent, asks the question that decides the
   matter — repository write permission for the authenticated user, the same
   question the run approval path already asked. An unreadable permission is a
   refusal. Covered by the refusal message, a `read` permission, an unreadable
   permission, and the scoped path in `internal/ui/prs_test.go`.
2. **A chain of overlays drew as one box with a doubled border.** `Stack.View`
   centred every layer at the same place, so opening the repository picker from
   the filter menu produced two boxes in the same cells and the only evidence of
   a stack was a doubled border. Layers now cascade two cells up and to the left
   per level of depth. The defect was visible in the **already merged** Phase 2
   golden `repo_picker_80x24`, which is how it was confirmed rather than
   theorised; that golden and `actor_prompt_80x24` were regenerated and read.
3. **The Info row could draw an empty pane's separator.** At 60 columns the
   metadata pane was too narrow to hold a label and a value on one line, so the
   row rendered `││` and a truncated answer. `infoColumns` now gives the
   metadata pane up entirely when three panes do not fit, hands the separator it
   would have used back to the description, and `infoView` skips a pane it is not
   drawing. A property test renders every width from 12 to 200 and asserts the
   row tiles the body exactly with no doubled separator.
4. **The test store throttled its own fixtures.** The shared limiter paces a
   person's requests one at a time; a UI test drives a whole screen's fetch at
   once, so a golden rendered "Loading…" instead of the data and one flow test
   stalled. UI tests now lift the limit through one helper (`testStore`), and the
   limiter keeps its own tests in `store`.

## Deliberate differences from v1

Each is recorded in `docs/spec.md` next to the item it changes:

- The five pull request views are one route with a numbered strip rather than
  five screens. The letters keep their v1 meanings, and `Esc` steps back one
  level: a sub-tab lands on Info, and Info returns to the list.
- The Info row is three panes in one row — description, metadata, checks — and
  the metadata pane is the first to give way when the terminal is too narrow.
- A check that is not a workflow run (a coverage report, a linter bot) opens its
  own page in the browser instead of doing nothing.
- `x` clears the filters of the tab it is pressed on. The baseline reset the
  repository filter for every tab at once.
- The merge-method default is still cycled with `d`, so a keystroke still writes
  config; what changed is that the write edits the file as a YAML document, so a
  key this build does not know survives and the token is never re-encoded. A
  session with no writable config path cycles in memory and says "this session
  only".
- The status legend leads the help body instead of trailing it, so it is on
  screen without scrolling.
- The baseline also linked the logs view's run title and job name; v2 leaves
  those plain, because `w` already opens the job from that screen.

## Security

**F1 is closed.** See defect 1. Merge and close are gated on repository write
permission; a missing scope produces the spec's message
("You don't have write scopes (repo) to merge this PR.") with the reason
appended, and never names the token or a path.

Already closed and unchanged: **F2** (the browser opener validates the scheme
and host) and **F4** (the credential source label carries the file name only).
**F3** was closed in Phase 2.

Still open: **F5** — 14 standard-library advisories at `go1.26.5`, fixed by the
`go1.26.6` toolchain. It is a standalone toolchain bump, not part of a phase,
and the CI step passes because it does not fail on standard-library advisories.

## Verification

This is a TUI, so browser verification does not apply and has no equivalent
here. The substitute is the same one the earlier phases used, stated plainly:
scripted `teatest` runs through the real Bubble Tea runtime (which render
through the real view path against mock GitHub servers), golden renders of every
screen, property tests for the geometry, and the unit tests below. What that
leaves unverified is a human's impression of the screens against live GitHub
with a real token — the colour theme, and how the flows feel — which is the
reader's step.

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race -count=1 ./...` | pass — 283 test functions with 130 subtests, 413 leaf runs, in the 9 packages that have tests |
| `golangci-lint run` | 0 issues |
| `govulncheck ./...` | 14 findings, all standard library (F5, pre-existing; identical on `main`) |
| `actionlint` | pass |
| `go mod tidy -diff` | clean |
| `gofmt -l` | clean except `internal/gh/{client,client_test,types}.go`, whose drift predates the rewrite and is left alone |
| `./ghspector -v` | prints `ghspector <version>`, `commit: <sha>`, `build date: <date>` and exits 0 |
| CI `Test and Lint` on the PR | pass — 1m00s |

The built binary was also run twice under a pty with a deliberately invalid
`GH_TOKEN`: it rendered the splash, reached GitHub, and drew the error screen
with the rejection message, the retry and quit hints and the token source label.
The first run was stopped by a timeout; the second, with `q` sent to the pty,
exited 0. Without a TTY it reports an open-TTY error and exits 1, which is the
expected shape for a TUI in a pipe. A live session against real pull requests
was not run: that needs a real token, and reading one out of the user's
credential store is out of bounds for this session.

Screen and flow coverage:

- **42 golden renders**: 18 new for the pull request screens
  (`TestGoldenPullRequestScreens`) — the list at three widths, the filtered list,
  the load-more row, the repository picker chain, Info at three widths including
  the read-only and 60-column variants, Commits, Files & diff, Checks, Comments,
  commit details, and the merge-method, confirm-merge and confirm-close
  screens. Shell and workflow goldens changed where a shared behaviour did: the
  three `main_prs_*` renders now show the table, `modal_80x24`,
  `repo_picker_80x24` and `actor_prompt_80x24` show the cascaded overlay chain,
  and both `help_*` renders show the status legend.
- **Scripted runtime flows** (`internal/ui/flow_test.go`): the list → details →
  Commits → back → merge-method → confirm path, then `n` and `Esc`, ending with
  the list intact, the overlay stack empty and the geometry exact; and a tab
  cycle that leaves the pull request list standing and issues no second fetch.
- **The write gate**: refused on a `read` permission, refused when the permission
  cannot be read, refused when the token lacks the scopes, and allowed on write.
- **The store's page semantics**: parametrized requests, the local filter applied
  to the fetched page, the state forward, the fan-out cap, and the merged poll.
- **Geometry**: every width from 12 to 200 tiles the Info row exactly; the
  overlay chain renders as two readable boxes two cells apart in both axes.
- **The CLI and the config file**: the version flag's three stamps and its exit
  code, `-org`/`-user` clearing each other, `0600`/`0700`, the stderr warning for
  a group-readable file, `omitempty` on the optional keys, and the merge-method
  write preserving keys it does not know.

## Notes for the review

- `./ghspector` at the project root is the binary for this commit, built
  locally and gitignored. `./ghspector -v` reports the commit it was built from.
- `docs/spec.md` was re-read row by row in this phase and now records what the
  phase checked (§4.2, §4.6–4.7, §4.10–4.14, §4.17–4.19, §5, §6, §7, §10) plus a
  new §2.6 for the hyperlink contract. Two groups stay unticked on purpose and
  are named in §11: §2.1–2.5 (startup, context, chrome) and §9 (the transport),
  whose verification pass has not run yet. Ticking them without that pass would
  turn the checklist into a claim.
- `docs/architecture.md` gains the Phase 3 build status and flips **F1** to
  closed.
- Not merged, by design: PR #15 (`chore(main): release 0.4.0`).
