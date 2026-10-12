# feat(tui): Phase 2 — Workflows vertical slice

Third phase of the v2 TUI rewrite. Phase 1 rebuilt the shell; this phase makes
the Workflows tab, the Jobs route and the Logs route real, and closes the log
read finding (F3). Pull Requests and Issues remain placeholders.

Parent plan: the phased plan for the v2 rewrite (Phase 2 of 7).

## What changed

| Package | What it is |
| --- | --- |
| `internal/logs` | Log handling as pure functions, ported from v1's `update.go`: `Parse` (timestamp split, ANSI and runner-marker stripping), `Clean`, `Segment` (assign each line to the step that produced it, keyed by step index), `MatchScore` (fuzzy group-title to step-name matching), `ParseTimestamp`. 15 tests. |
| `internal/store` | `ScopeRuns` (8-repository fan-out, status sort, merged poll results), `WorkflowRuns`, `Jobs` per attempt, `JobLogs`, `PendingDeployments`, `ApproveRun`, `ApproveDeployments`, `RepoPermission`, `MissingScopes`. |
| `internal/gh` | Job-log reads are capped at 8 MiB with a truncation note placed on a line boundary; error bodies are capped at 64 KiB (F3). |
| `internal/overlay` | `Menu`, `List` and `Prompt` join `Confirm`, so a filter menu, a repository picker and a text prompt are modal without new machinery. The stack composites through a `Compositor` (see the fixes below). |
| `internal/ui/table.go` | One table renderer for both lists: `layout.FitColumns` allocates the columns and drops optional ones before squeezing the flexible one, so a narrow terminal loses `EVENT` and `ACTOR` rather than clipping them. |
| `internal/ui/runs.go` | The Workflows tab: run list, actor and repository filters, pagination via the `Load More` sentinel as the last selectable row, poll merge, and a per-run approval answer with its reason. |
| `internal/ui/jobs.go` | The Jobs route: job table with step counts and durations, attempt cycling on `[` / `]`, the approval banner in place of the table for a run that is waiting, and `w` (job) / `v` (run) browser keys. |
| `internal/ui/logs.go` | The Logs route: steps sidebar, viewport, follow mode that re-engages when the bottom is reached, step selection opening on the step that failed, and the "not yet available" message behind a running job. |
| `internal/ui/approval.go` | The approval gate: fork runs and the viewer's own runs approve through the API; an internal `action_required` run explains that GitHub does not allow API approval and opens the browser instead; a missing scope names the fix without naming the token or a path. |
| `internal/ui/filters.go`, `prompt.go` | The filter menu, the repository picker (fetched when opened), and the actor prompt pre-filled with the active filter. |
| `internal/keymap` | `MainWorkflows`, `Jobs`, `Logs`, `FilterType`, `RepoSelect`, `UserInput` and `ApproveRun` carry every key the phase adds; `ActionOpenRunBrowser` separates `v` from `w` as spec D3 requires. |

## Two defects found while building this

Both were found by reading what a screen actually rendered rather than by
assuming it worked.

1. **Every modal erased the frame behind it and landed in the top-left corner.**
   `overlay.Stack.View` composed bare Lip Gloss layers onto a `Canvas`.
   `Layer.Draw` ignores its `X`/`Y` position, and drawing a styled string clears
   the area it is given — so a layer handed the whole canvas cleared the whole
   canvas, and the box was drawn at the origin. `Compositor` is the API that
   hands each layer its own rectangle; the stack now builds one. Two tests were
   added for the behaviours that were silently wrong and had no coverage: the
   frame around the box survives (`TestViewKeepsTheFrameAroundTheBox`) and the
   box is centred (`TestViewCentresTheBox`). Verified by regenerating the
   `modal_80x24` golden: the list behind the modal is now visible.
2. **The actor prompt rendered as `l`.** A `bubbles` text input with no width
   set draws only the first rune of its placeholder, so the empty filter prompt
   showed `l` for `login` and offered no visible field to type in. The input now
   has a width.

Both are Phase 1 code, and both are visible in the Phase 2 screens, so they are
fixed here rather than carried forward.

## Deliberate differences from v1

Each is recorded in `docs/spec.md` next to the item it changes:

- The `ESC` on the actor prompt closes the whole filter chain, not one level, so
  the user lands on the list they started from.
- The jobs list always pops to the route below it. v1 returned to PR Details
  when the job list was opened from there, which is the PR phase's problem.
- Scrolling in the log viewer is bound (`u`/`d`, `PgUp`/`PgDn`) rather than
  forwarded to a catch-all viewport.
- Polling uses the same 8-repository fan-out as the list, so there is one code
  path instead of a 15-repository poll limit and an 8-repository list limit.
- Tab switching fetches a tab the first time it is opened and keeps each tab's
  selection, rather than resetting every index and refetching on every switch.
- The log line keeps its timestamp: it is the only record of when a step
  produced its output, and the steps sidebar has no room for it.

## Security

**F3 is closed.** `gh.GetJobLogs` reads through a capped reader
(`maxLogBytes`, 8 MiB), appends a truncation note on a line boundary, and caps
error bodies at 64 KiB. Tested against an oversized body and for the
line-boundary rule. This mattered more here than in v1 because each open log
viewer polls the same endpoint.

Unchanged and still open: **F1** (the merge gate fails open; PR phase) and
**F5** (14 stdlib advisories, fixed by the `go1.26.6` toolchain; a standalone
toolchain bump, not part of a phase).

## Verification

A TUI cannot be checked in a browser, so the substitute is the test suite plus
rendered goldens; nothing in this phase was verified by looking at it in a
terminal. What that leaves unverified is the colour theme and the terminal
opening behaviour, both unchanged by this phase.

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race ./...` | pass — 224 test functions, 309 cases, in the 8 packages that have tests |
| `golangci-lint run` | 0 issues |
| `govulncheck ./...` | 14 findings, all stdlib (F5, pre-existing; identical on `main`) |
| `actionlint` | pass |
| `go mod tidy -diff` | clean |
| `gofmt -l` | clean except `internal/gh/{client,client_test,types}.go`, whose drift predates the rewrite and is left alone |

Screen coverage:

- 24 golden renders: 14 for the shell (`TestGoldenScreens`) and 10 for the
  workflow screens (`TestGoldenWorkflowScreens`) — the run table at two widths,
  the filtered table, the job list at two widths, the approval banner, the
  approval confirm, the log viewer, the filter menu, the repository picker and
  the actor prompt. Two shell goldens changed: `main_workflows_80x24` now shows
  the run table, and `modal_80x24` shows the list behind the modal and the
  overlay's own footer keys.
- The `list → jobs → logs` path runs through the real Bubble Tea runtime
  (`TestFlowFromTheRunListToTheLog`), including the breadcrumb and the route
  stack, because the driver tests reach the same screens by running commands by
  hand.
- The poll loop is asserted by releasing the tick a `fixedClock` hands out: the
  cadence per scope, the merge of a changed run, the drop of a stale token, the
  refusal to poll a finished job list or a finished job's log, and no polling at
  all when no interval is configured.
- Approval semantics: fork runs and the viewer's own runs approve through the
  API, an internal `action_required` run is offered the browser path, a
  withheld approval states the reason, and a missing scope names
  `gh auth refresh` without naming the token.
- Log reads are bounded against an oversized body at the transport layer
  (`internal/gh/logs_test.go`), and segmentation is pinned by the 15 tests in
  `internal/logs`.

## Notes for the review

- `./ghspector` at the project root is the binary for this commit, built
  locally and gitignored. `./ghspector -v` reports the commit it was built from.
- `.agents/history/2026-10-12-v2-phase-1-foundation-shell/` is Phase 1's
  walkthrough, written after that PR merged and therefore not in it; it is
  committed here.
- Not merged, by design: PR #15 (`chore(main): release 0.4.0`).
