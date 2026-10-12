# ghspector — behavioral specification & v2 parity checklist

This document is the acceptance test for the v2 rewrite. It records how the tool behaves **today** (read from the code, not from the README), so that "parity" is a checklist rather than a memory.

How to use it:

- Every checkbox is one testable behavior. The v2 rewrite is complete when all boxes are checked, or an unchecked box is explicitly deferred with a reason in the "Deferred" column.
- Where the README and the code disagree, the code is the baseline and the disagreement is recorded in [Known drift](#known-drift). Resolving a drift item is a decision, not an accident.
- Sections marked **Fix in v2** describe behavior the rewrite is expected to change. Those are not parity requirements.

Source of truth for the baseline: `internal/tui/update.go`, `internal/tui/view.go`, `internal/gh/client.go`, `internal/auth/auth.go`, `cmd/ghspector/main.go` at commit `3e64be8` (v0.3.0 line).

---

## 1. Scope

Four capability areas must reach parity:

| Area | Entry point | Write actions |
| --- | --- | --- |
| Workflow runs | Workflows tab | Approve waiting run, approve pending deployments |
| Jobs & logs | Enter on a run | none |
| Pull requests | PRs tab | Merge (squash/merge/rebase), close |
| Issues | Issues tab | none (read-only) |

Plus the cross-cutting surface: context switcher, filters, polling, rate-limit display, browser links, config file, CLI flags.

Explicitly **not** in scope for parity: the README's "Dashboard View" (§ Known drift D1).

---

## 2. Startup, context, and global chrome

### 2.1 Startup sequence

- [ ] Token resolution order: `GH_TOKEN` → `GITHUB_TOKEN` → config file `github_token` → `gh auth token`. If all fail: print instructions to stderr, exit code 1.
- [ ] `-v` / `-version` prints `ghspector <version>`, `commit: <commit>`, `build date: <date>` and exits 0.
- [ ] `-org <name>` overrides `DefaultOrg` and clears `DefaultAccount`; `-user <name>` does the reverse.
- [ ] Program starts in alternate screen mode.
- [ ] Splash screen renders the ASCII cat logo, the app name, a spinner, and the current `loadingMsg`.
- [ ] `fetchInitialDataCmd` runs at startup: `GET /user`, then `GET /user/orgs`.
- [ ] Target list = the authenticated user (first) followed by each org; `selectedTargetIdx` defaults to 0.
- [ ] If `DefaultOrg` matches an org in the list, that org is selected; else if `DefaultAccount` matches, that user is selected.
- [ ] After init, the active tab's list is fetched; the first list load (or an error) moves the app from splash to the main view.
- [ ] On splash, `Esc`/`Backspace` cancels the in-flight context, creates a fresh one, and returns to `prevState` (or the main view), clearing the loading flag.

### 2.2 Header (top bar)

- [ ] Shows the active context name and whether it is an org or a user account.
- [ ] Shows API rate-limit remaining/limit.
- [ ] Rate-limit indicator color reflects consumption.
- [ ] Shows a loading spinner while `isLoading` is true, with `loadingMsg`.

### 2.3 Footer (bottom bar)

- [ ] Renders the active route's key hints.
- [ ] `?:Help` is present on every route.
- [ ] Displays a transient status message; it auto-clears after 3 seconds (guarded by a monotonic ID so a newer message is not cleared by an older timer).
- [ ] Fills the terminal width; hints are laid out within that width.

### 2.4 Global keys (every route except splash)

- [ ] `?` toggles the help view; closing it returns to the previous route.
- [ ] `o` toggles the context switcher; closing it returns to the previous route.
- [ ] `q` and `Ctrl+C` cancel the root context and quit.
- [ ] `Ctrl+C` also quits from inside input and modal routes.

### 2.5 Error handling

- [ ] A fatal error renders a full-screen error view.
- [ ] In the error view, only `q` and `Ctrl+C` are accepted.
- [ ] A rate-limit error is recoverable: the tick handler clears the error, resets pagination and selection for all three tabs, and reloads the active tab once the reset time passes.
- [ ] A non-fatal error on a list load sets a status message and returns to the main view rather than the error screen.

---

## 3. Route map

The 19 `viewState` values map to routes as follows. v2 replaces the enum with a route stack; the mapping is what must be preserved.

| Current state | v2 route | Notes |
| --- | --- | --- |
| `viewSplash` | `Splash` | Transient; not stackable |
| `viewMain` | `Main` with `tab ∈ {PRs, Workflows, Issues}` | Tab is route state, and `mainTab` order is PRs, Workflows, Issues (initial tab is PRs) |
| `viewJobs` | `Jobs` pushed over `Main` or `PRDetails` | `prevState` decides where `Esc` returns |
| `viewLogs` | `Logs` pushed over `Jobs` | |
| `viewPRDetails` | `PRDetails` pushed over `Main` | Hosts the five PR sub-tabs |
| `viewPRCommits` | `PRCommits` pushed over `PRDetails` | |
| `viewCommitDetails` | `CommitDetails` pushed over `PRCommits` | |
| `viewPRDiff` | `PRDiff` pushed over `PRDetails` | |
| `viewPRComments` | `PRComments` pushed over `PRDetails` | |
| `viewIssueDetails` | `IssueDetails` pushed over `Main` | |
| `viewIssueComments` | `IssueComments` pushed over `IssueDetails` | |
| `viewSwitcher` | Overlay: context switcher | |
| `viewHelp` | Overlay: help | |
| `viewFilterTypeSelect` | Overlay: filter type | Shared by all three tabs |
| `viewRepoFilterSelect` | Overlay: repo select | |
| `viewPRFilterInput` | Overlay: text input (PR) | |
| `viewPRFilterTypeSelect` | Overlay: filter type (PR) | |
| `viewIssueFilterInput` | Overlay: text input (issues) | |
| `viewIssueFilterTypeSelect` | Overlay: filter type (issues) | |

Two modal machines are **not** in the enum and are stored as integer fields:

| Field | Values | Meaning |
| --- | --- | --- |
| `mergeState` | `0` none, `1` method select, `2` confirm merge, `4` confirm close | `3` "completed" is declared in a comment and never used |
| `runApprovalState` | `0` none, `1` confirm approval | |

One input route is a **bool** rather than a state: the workflow actor filter uses `showFilterInput`. v2 must unify this with the other filter inputs ([Fix in v2](#7-fix-in-v2)).

---

## 4. Route behaviors and keybindings

Legend: **browser** = opens a URL via `openBrowser` (`xdg-open` / `open` / `rundll32`).

### 4.1 Main — Workflows tab

- [x] `j` / `Down` moves down; blocked at `len(runs)-1` unless `hasMoreRuns`, in which case the "Load More" sentinel row is selectable.
- [x] `k` / `Up` moves up.
- [x] Moving the selection triggers a permission check for the newly selected run (`checkApprovalPermissionCmd`; v2: `runsTab.checkSelected`).
- [x] `Enter` on the Load More sentinel increments the page and fetches more runs.
- [x] `Enter` on a run pushes the Jobs route, clearing `jobs` and setting `selectedAttempt` to the run's `RunAttempt`.
- [x] `r` / `Ctrl+R` resets to page 1, clears the list and selection, and refetches.
- [x] `m` toggles the actor filter between the current user and empty, then refetches.
- [x] `f` opens the filter type overlay.
- [x] `w` opens the selected run's `HTMLURL` **browser**.
- [x] `a` opens the approval confirm overlay when the selected run needs approval and the viewer may approve it; otherwise nothing happens.
- [x] `x` clears the actor filter and the repo filter, then refetches.
- [x] `Tab` / `Shift+Tab` cycles tabs and fetches a tab the first time it is opened. v2 keeps each tab's own selection instead of resetting every index, and re-opening a tab shows what was already fetched, which polling keeps current.

### 4.2 Main — Pull Requests tab

- [ ] `j` / `k` / `Down` / `Up` navigate, with the same Load More sentinel rule as workflows.
- [ ] `Enter` on the sentinel loads the next PR page; `Enter` on a PR pushes PR Details with sub-tab Info and a fresh checks list.
- [ ] `r` / `Ctrl+R` resets and refetches.
- [ ] `s` cycles the state filter open → closed → all, then refetches.
- [ ] `a` filters by author = current user.
- [ ] `i` filters by assignee = current user.
- [ ] `v` filters by requested reviewer = current user.
- [ ] `f` opens the filter type overlay (free-text user, then pick author/assignee/reviewer).
- [ ] `x` clears author, assignee, reviewer and repo filters and resets the state filter to `open`, then refetches.
- [ ] `x` also suppresses **all** filters when pressed on the PRs tab (it resets the repo filter for every tab).
- [ ] `w` opens the selected PR's `HTMLURL` **browser**.

### 4.3 Main — Issues tab

- [ ] `j` / `k` / `Down` / `Up` navigate, with the Load More sentinel rule.
- [ ] `Enter` on the sentinel loads the next issue page; `Enter` on an issue pushes Issue Details with the description focused.
- [ ] `r` / `Ctrl+R` resets and refetches.
- [ ] `s` cycles the state filter open → closed → all, then refetches.
- [ ] `a` filters by author = current user.
- [ ] `i` filters by assignee = current user.
- [ ] `f` opens the filter type overlay (free-text user, then pick author/assignee).
- [ ] `x` clears author, assignee and repo filters and resets the state filter to `open`, then refetches.
- [ ] `w` opens the selected issue's `HTMLURL` **browser**.

### 4.4 Filter type overlay

- [x] `u` / `U` opens the user filter text input. On the Workflows tab it is pre-filled with the current actor filter; on the other tabs it starts empty. v2 delivers the Workflows prompt; the PR and Issue inputs arrive with their tabs (Phases 3 and 4).
- [x] `r` / `R` opens the repository select overlay; if the repo list is empty it is fetched first (with a loading message).
- [x] `Esc` returns to the main view.
- [x] `Ctrl+C` quits.

### 4.5 Repository select overlay

- [x] `j` / `k` / `Down` / `Up` move the selection.
- [x] `Enter` sets the repo filter to the selected repository name, returns to the main view, and refetches the active tab.
- [x] `Esc` returns to the filter type overlay.
- [x] `Ctrl+C` quits.

### 4.6 User filter text input (PRs and Issues)

- [ ] `Enter` accepts the typed username and advances to the filter type overlay.
- [ ] `Esc` returns to the main view.
- [ ] `Ctrl+C` quits.
- [ ] All other keys are forwarded to the text input.

### 4.7 Filter type overlay — PRs and Issues

- [ ] PRs: `a` sets author, `i` sets assignee, `r` sets reviewer to the typed username, returns to the main view, and refetches.
- [ ] Issues: `a` sets author, `i` sets assignee to the typed username, returns to the main view, and refetches.
- [ ] `Esc` / `c` / `C` cancels back to the main view.

### 4.8 Jobs

- [x] `j` / `k` / `Down` / `Up` navigate the job list.
- [x] `Enter` on a job opens Logs.
- [x] `Enter` on a job whose status is `in_progress` or `queued` shows the status message "Logs are not yet available for running jobs. Please wait for completion." and does not navigate.
- [x] `Esc` / `Backspace` returns to the route the job list was opened from. v2 always pops to the route below, which is the run list; the "opened from PR Details" origin arrives with Phase 3.
- [x] `a` opens the approval confirm overlay when the run may be approved.
- [x] `w` / `W` opens the **selected job** in the **browser** (see [Known drift D3](#known-drift)).
- [x] `v` / `V` opens the **parent workflow run** in the **browser**.
- [x] Job and run URLs fall back to constructed `https://github.com/<full_name>/actions/runs/<id>/job/<job_id>` when `HTMLURL` is empty.
- [x] `r` / `Ctrl+R` refetches jobs for the current attempt.
- [x] `[` switches to the previous attempt when `selectedAttempt > 1`.
- [x] `]` switches to the next attempt when `selectedAttempt < run.RunAttempt`.
- [x] Attempt switching resets the job selection and list, and shows the attempt number in the loading message.

### 4.9 Logs

- [x] `Esc` / `Backspace` returns to Jobs.
- [x] `r` / `Ctrl+R` refetches logs; a job that is still running answers with the same "not yet available" message, which v2 reports from the failed read rather than refusing up front.
- [x] `j` / `Down` selects the next step; `k` / `Up` selects the previous step.
- [x] Changing the selected step updates the rendered log content for that step.
- [x] `u` / `PageUp` scrolls up 3 lines and disables follow mode.
- [x] `d` / `PageDown` scrolls down 3 lines and re-enables follow mode on reaching the bottom.
- [x] `w` / `W` opens the job in the **browser**.
- [x] Follow mode keeps the viewport pinned to the newest output as logs are polled in.
- [ ] Any other key is forwarded to the viewport for scrolling, except the step-navigation keys. v2 binds scrolling explicitly (`u`/`d`, `PgUp`/`PgDn`) and has no catch-all viewport, so an unbound key does nothing.

### 4.10 PR Details

- [ ] `Esc` / `Backspace` returns to the PR list.
- [ ] `Tab` / `Shift+Tab` toggles focus between the description viewport and the checks sidebar.
- [ ] `j` / `Down` scrolls the description down 1 when focused, otherwise moves to the next check.
- [ ] `k` / `Up` scrolls the description up 1 when focused, otherwise moves to the previous check.
- [ ] `u` scrolls the description up 6 (description focus only).
- [ ] `d` scrolls the description down 6 (description focus only).
- [ ] `Enter` on a check resolves the matching Actions run, by run ID extracted from the check URL, falling back to a case-insensitive name match; a match opens Jobs targeting that job name.
- [ ] `Enter` on a check with a run ID but no matching run in the list opens Jobs for that run ID.
- [ ] `Enter` on a check with no run ID opens the check URL in the **browser**.
- [ ] `w` opens the PR URL when the description is focused, otherwise the selected check URL, in the **browser**.
- [ ] `m` opens the merge overlay when the viewer has write permission, otherwise shows "You don't have write scopes (repo) to merge this PR."
- [ ] `c` opens the comments view.
- [ ] `C` opens the close-confirm overlay when the viewer has write permission, otherwise shows the equivalent scopes message.
- [ ] `v` opens the commits view.
- [ ] `D` opens the diff view.
- [ ] `r` / `Ctrl+R` refreshes the PR details.
- [ ] The view has five sub-tabs: Info, Commits, Files, Checks, Comments.

### 4.11 PR Commits

- [ ] `Esc` / `Backspace` returns to PR Details.
- [ ] `j` / `k` / `Down` / `Up` navigate the commit list.
- [ ] `Enter` fetches and shows commit details for the selected commit.

### 4.12 Commit Details

- [ ] `Esc` / `Backspace` returns to PR Commits.
- [ ] `j` / `k` / `Down` / `Up` navigate the changed-file list and update the diff viewport.
- [ ] `u` / `d` scroll the diff by 3 lines.
- [ ] `w` opens the commit in the **browser**.

### 4.13 PR Diff

- [ ] `Esc` / `Backspace` returns to PR Details.
- [ ] `j` / `k` / `Down` / `Up` navigate the file list and update the diff viewport.
- [ ] `u` / `d` scroll the diff by 3 lines.
- [ ] `w` opens `<PR URL>/files` in the **browser**.

### 4.14 PR Comments

- [ ] `Esc` / `Backspace` returns to PR Details.
- [ ] `r` / `Ctrl+R` refreshes the comments.
- [ ] Other keys are forwarded to the viewport for scrolling.

### 4.15 Issue Details

- [ ] `Esc` / `Backspace` returns to the issue list.
- [ ] `Tab` / `Shift+Tab` toggles focus between the description and the metadata sidebar.
- [ ] `j` / `k` scroll the description by 1 when focused.
- [ ] `u` / `d` scroll the description by 6 when focused.
- [ ] `w` opens the issue in the **browser**.
- [ ] `c` opens the comments view.
- [ ] `r` / `Ctrl+R` refreshes the issue details.
- [ ] The metadata sidebar shows state, author, assignees, labels, milestone, and timestamps.

### 4.16 Issue Comments

- [ ] `Esc` / `Backspace` returns to Issue Details.
- [ ] `r` / `Ctrl+R` refreshes the comments.

### 4.17 Context switcher overlay

- [ ] `j` / `k` / `Down` / `Up` move the selection through the target list (user first, then orgs, sorted as returned).
- [ ] `Enter` loads the selected context: resets runs, pulls and issues lists, pagination, selection, repo cache, repo filter, and dashboard counters, then fetches the active tab.
- [ ] `Esc` returns to the previous route.

### 4.18 Help overlay

- [ ] Renders sections: Global Keys, Workflow Runs, Pull Requests, Pull Request Details, Workflow Jobs, Logs Viewer.
- [ ] Renders the status legend.
- [ ] `Esc` or `?` closes it.

### 4.19 Merge overlay

Method select (`mergeState == 1`):

- [ ] `s` / `S` selects squash and advances to confirm.
- [ ] `m` / `M` selects regular merge and advances to confirm.
- [ ] `r` / `R` selects rebase and advances to confirm.
- [ ] `Enter` advances using the configured default method.
- [ ] `d` / `D` cycles the persisted default merge method squash → merge → rebase and writes it to the config file.
- [ ] `Esc` / `c` / `C` cancels.
- [ ] The default method label reflects `default_merge_method` from config.

Confirm merge (`mergeState == 2`):

- [ ] `y` / `Y` performs the merge with the chosen method and shows the loading message "Merging pull request...".
- [ ] `n` / `N` / `Esc` cancels without merging.

Confirm close (`mergeState == 4`):

- [ ] `y` / `Y` closes the PR with the loading message "Closing pull request...".
- [ ] `n` / `N` / `Esc` cancels.

### 4.20 Run approval overlay

- [x] `y` / `Y` / `Enter` approves the run, showing "Approving workflow run...".
- [x] `w` / `W` opens the run's approval page in the **browser** and shows "Opened approval page in browser."
- [x] A run with conclusion `action_required` on a non-fork PR cannot be approved through the API: it opens the browser instead, and the overlay body explains this.
- [x] `n` / `N` / `Esc` cancels.
- [x] Fork PRs (`head_repository.full_name != repository.full_name`) use the API approval path.

---

## 5. Filters, pagination, and aggregation

### 5.1 Filter semantics

| Filter | Tab | Applied | Notes |
| --- | --- | --- | --- |
| Actor | Workflows | Server-side (`actor` query param) **and** client-side re-check | Client-side re-check uses a case-insensitive prefix/substring match |
| Repo | All | Client-side: narrows the repo list to one before fetching | Only exact name match |
| State (PR) | PRs | Server-side (`state` query param) | Defaults to `open` |
| Author / assignee / reviewer | PRs | **Client-side, after fetch** | Applied only to the fetched page |
| Author / assignee | Issues | **Client-side, after fetch** | Applied only to the fetched page |
| State (issue) | Issues | Server-side | Defaults to `open` |

- [ ] PRs are filtered client-side after fetching, so a filter only sees the current page of the first 8 repositories.
- [ ] Issues are filtered client-side after fetching, same limitation.
- [x] The repo filter clears when switching context.

### 5.2 Pagination

| Resource | Page size | Next page | End detection |
| --- | --- | --- | --- |
| Repositories | 100 (list), 15 (poll) | not paginated | — |
| Workflow runs | 8 | Load More sentinel | empty page |
| Pull requests | 8 | Load More sentinel | empty page |
| Issues | 50 | Load More sentinel | `len(page) == per_page` |

- [x] Each list exposes a "Load More" row as the final selectable row while more pages exist. Delivered for workflow runs; the PR and Issue lists arrive with their tabs.
- [ ] Pull requests are sorted by `updated_at` descending.
- [x] Workflow runs are sorted by a status priority, then recency.
- [x] Jobs are sorted by a job-specific priority.
- [x] Polled runs are merged into the existing list by run ID, then re-sorted.

### 5.3 Cross-scope aggregation

- [x] Lists are aggregated across the first **8 repositories** of the context, sorted by `pushed_at` descending.
- [ ] Polling uses the first **15 repositories** but only queries the first 8. v2 polls through the same 8-repository fan-out as the list, so there is one code path instead of two.
- [x] Each repository is queried concurrently, one request per repository.
- [x] Runs carry their repository embedded so the UI can render owner/repo columns.

**Fix in v2:** replace this fan-out with a scope-wide GraphQL query with real cursor pagination and `total_count` (see [§7](#7-fix-in-v2)).

### 5.4 Polling model

- [x] A tick drives the spinner every 300 ms.
- [x] A poll tick drives data refresh. Interval by state: main + PRs tab → PRs interval; every other state → workflows interval.
- [x] Default intervals: workflows 5 s, PRs 10 s.
- [x] `polling.workflows_interval_seconds` and `polling.prs_interval_seconds` override them individually.
- [x] The legacy `polling_interval_seconds` sets both, and takes effect before the per-tab keys.
- [x] Polling targets: main + Workflows → runs; main + PRs → pull requests; main + Issues → issues; Jobs → active jobs; Logs → logs. v2 delivers the Workflows, Jobs and Logs targets; the PR and Issue targets arrive with their tabs.
- [x] Polling does not run on other routes.

**Fix in v2:** the Issues tab currently polls on the workflows interval because the interval selector only special-cases the PRs tab.

---

## 6. CLI and configuration surface

### 6.1 CLI

- [ ] `-v` / `-version`
- [ ] `-org <name>`
- [ ] `-user <name>`

### 6.2 Config file

Path: `<os.UserConfigDir()>/ghspector/config.yaml` (Linux/macOS `~/.config/ghspector/config.yaml`, Windows `%APPDATA%\ghspector\config.yaml`).

- [ ] `github_token` — token
- [ ] `default_org` — org selected at startup
- [ ] `default_account` — user selected at startup
- [ ] `polling_interval_seconds` — legacy, sets both intervals
- [ ] `polling.workflows_interval_seconds`
- [ ] `polling.prs_interval_seconds`
- [ ] `default_merge_method` — `squash` | `merge` | `rebase`, and is rewritten by the `d` key in the merge overlay
- [ ] A warning is printed to stderr when the config file permissions allow group or other access.
- [ ] The config file is written with mode `0600` and its directory with `0700`.
- [ ] Keys `default_org`, `default_account`, `polling`, and `default_merge_method` are omitted when empty (`omitempty`).

**v2 addition:** a `version` key. Additive only.

### 6.3 Token scopes

- [ ] Required scopes for write actions are `repo` and `workflow`; the app reads `X-OAuth-Scopes` to determine them.
- [ ] Merge and close are gated on repository write permission for the authenticated user.
- [ ] Missing scopes produce the "You don't have write scopes (repo) to ..." status message.

---

## 7. Fix in v2

These are behavior changes the rewrite is expected to make. They are not parity requirements, and each needs its own test.

- [ ] Replace the 8-repository cap with scope-wide aggregation via GraphQL, with real cursor pagination and `total_count`.
- [ ] Move PR author/assignee/reviewer and issue author/assignee filters server-side (GraphQL supports all of them), so filtering is truthful across the whole scope rather than only the current page.
- [ ] Unify the workflow actor filter with the other user filters: today it is a `bool` flag plus a prefilled text input, while PRs and Issues use dedicated states.
- [ ] Poll the Issues tab on its own interval.
- [ ] Replace the merge-method default cycle, which persists config on a keystroke, with an explicit save or an in-memory default that is written once.
- [ ] Give the Jobs route an explicit origin instead of the `prevState` field, and give every pushed route an explicit `(owner, repo, number, runID)` rather than reading it back out of shared model fields.
- [ ] Define and test a minimum terminal width with a graceful "terminal too small" state.

---

## 8. Known drift

### D1 — README describes a Dashboard that does not exist

`README.md` documents a Dashboard view as the first tab and states `Tab` cycles "Dashboard → Workflows → Pull Requests". The code has three tabs in the order **PRs, Workflows, Issues**, with PRs as the initial tab. A `mainTab` dashboard value does not exist.

Dead remnants of the removed dashboard: `dashboardPRsCount`, `dashboardWorkflowsCount`, `dashboardStatsLoadedMsg` (whose handler sets the state to main), and `fetchDashboardStatsCmd` if present. Nothing emits `dashboardStatsLoadedMsg`.

**Decision for v2:** no Dashboard route. Remove the dead fields and message. Update the README.

### D2 — README global-keys table is stale

The README says `Tab` goes "Dashboard → Workflows → Pull Requests". Actual: PRs → Workflows → Issues.

**Decision for v2:** README follows the code.

### D3 — Jobs `w` and `v` are swapped between docs and code

Code: `w` opens the **selected job**, `v` opens the **parent run**. Both the README and the in-app help say `w` opens the run and `v` opens the job.

**Decision for v2:** pick one mapping, apply it to the code, the footer, the help overlay, and the README, and assert it with the keymap consistency test. Recommendation: `w` = open the highlighted job, `v` = open the parent run, matching the code so existing user muscle memory holds.

### D4 — README documents the PR close key as `c`

Code: `c` opens comments, `C` (shift) opens the close confirm. The in-app help documents both correctly; the README lists `c` as close and omits `C`.

**Decision for v2:** README corrected to `C`.

### D5 — README omits most list-view filter keys

The README's Workflow Runs table has only `j/k/Enter/r/w/m/f`, and its Pull Requests table has only `j/k/Enter/r/w`. Missing from the README entirely: `s` (state cycle), `a`/`i`/`v` (quick filters), `x` (clear filters), `f` (filter type), `[`/`]` (attempts), `a` (approve).

**Decision for v2:** the README keybinding tables are regenerated from the keymap and checked by the consistency test.

### D6 — Logs viewer documented as `u`/`d` only

The code also supports `j`/`k` for step navigation, `PageUp`/`PageDown` aliases, and `w` to open the job.

**Decision for v2:** README follows the code.

### D7 — Unreachable key handler

`viewPRCommits` handles `case "q"` by quitting. The global `q` handler runs before the state switch, so this arm is dead code.

**Decision for v2:** drop it.

### D8 — `mergeState == 3` is declared but unused

The field comment lists `3: completed`, which no handler produces.

**Decision for v2:** modeled states are `Method`, `ConfirmMerge`, `ConfirmClose`, with no phantom completed state.

### Recorded resolutions

Each drift item is resolved by construction: the new code has one place to
express the fact, and a test that fails when the two drift apart again.

| Item | Resolution | Enforced by |
| --- | --- | --- |
| D1 | No Dashboard route. The dead counter fields and `dashboardStatsLoadedMsg` are gone with `internal/tui`. | `internal/ui` has three tabs and no dashboard scope. |
| D2 | Tab order is PRs → Workflows → Issues, PRs first. The README global-keys table now says so. | `TestTabCyclingOrder`, `TestEveryScreenHasExactGeometry`. |
| D3 | `w`/`W` opens the highlighted job, `v`/`V` opens the parent run, matching the code. | `keymap.Jobs` plus `TestDocumentedKeysFromSpec`, which pins both keys to their actions. |
| D4 | `c` is comments, `C` is close. | `keymap.PRDetails` plus `TestDocumentedKeysFromSpec`. |
| D5 | Every list-view filter key is in the table, so the footer and the help overlay show them. | `TestFooterRendersTheActiveScope`, `keymap.Documented`. |
| D6 | Logs keeps `j`/`k` steps, `u`/`d` and `PgUp`/`PgDn` scroll, `w` opens the job. | `keymap.Logs`. |
| D7 | The unreachable `case "q"` is gone: quit is handled once, before any routing. | `TestQuitWorksFromEveryStage`, `TestCtrlCQuitsFromBehindAModal`. |
| D8 | No phantom completed merge state. | `keymap.MergeMethod`, `keymap.MergeConfirm`. |

---

## 9. Data and API map

REST calls made by the current implementation. `o` = owner, `r` = repo.

| Method + path | Purpose | v2 owner |
| --- | --- | --- |
| `GET /user` | Authenticated user | REST |
| `GET /user/orgs` | Context list | REST |
| `GET /users/{o}/repos`, `GET /orgs/{o}/repos` | Repo list (`sort=pushed&direction=desc`) | GraphQL (task: keep REST as fallback) |
| `GET /repos/{o}/{r}/actions/runs` | Workflow runs, optional `actor` | GraphQL |
| `GET /repos/{o}/{r}/actions/runs/{id}` | Single run | REST |
| `GET /repos/{o}/{r}/actions/runs/{id}/jobs` | Jobs | REST |
| `GET /repos/{o}/{r}/actions/runs/{id}/attempts/{n}/jobs` | Jobs for an attempt | REST |
| `GET /repos/{o}/{r}/actions/jobs/{id}` | Single job | REST |
| `GET /repos/{o}/{r}/actions/jobs/{id}/logs` | Job logs (REST-only; errors arrive as XML) | REST |
| `GET /repos/{o}/{r}/pulls` | Pull requests, `state` | GraphQL |
| `GET /repos/{o}/{r}/issues` | Issues, `state`; PRs filtered out client-side | GraphQL |
| `GET /repos/{o}/{r}/issues/{n}` | Single issue | REST |
| `GET /repos/{o}/{r}/pulls/{n}` | Single PR | REST |
| `GET /repos/{o}/{r}/issues/{n}/comments` | PR and issue comments | REST |
| `GET /repos/{o}/{r}/pulls/{n}/commits` | PR commits | REST |
| `GET /repos/{o}/{r}/pulls/{n}/files` | PR changed files with patches | REST |
| `GET /repos/{o}/{r}/commits/{sha}` | Commit + files | REST |
| `GET /repos/{o}/{r}/commits/{ref}/check-runs` | Checks, deduplicated to latest per name | REST |
| `PUT /repos/{o}/{r}/pulls/{n}/merge` | Merge | REST |
| `PATCH /repos/{o}/{r}/pulls/{n}` | Close | REST |
| `GET /repos/{o}/{r}/collaborators/{user}/permission` | Write-permission gate | REST |
| `GET /repos/{o}/{r}/actions/runs/{id}/pending_deployments` | Pending deployments | REST |
| `POST /repos/{o}/{r}/actions/runs/{id}/approve` | Approve run | REST |
| `POST /repos/{o}/{r}/actions/runs/{id}/pending_deployments` | Approve deployments | REST |

Cross-cutting client behavior to preserve:

- [ ] `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`.
- [ ] Rate-limit state parsed from `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.
- [ ] Server clock offset derived from the `Date` header, used for rate-limit countdown.
- [ ] If the rate limit is exhausted and resets in more than 30 s, return a rate-limited error rather than sleeping. If it resets sooner, wait for the reset subject to context cancellation.
- [ ] `429` → rate-limited error; `401`/`403` → forbidden error including the API message; other `4xx`/`5xx` → error including the API message.
- [ ] Job-log errors are parsed as XML (`Code`, `Message`) before falling back to the raw body.

---

## 10. Parallel anchors

These are the behaviors most worth porting directly as pure functions rather than rediscovering:

- [x] Log segmentation by step (`segmentLogs`): maps a raw log to step segments using group markers and step names, with a fuzzy name-matching score. v2: `internal/logs.Segment` and `MatchScore`, ported as pure functions with their own tests.
- [ ] Log cleaning (`cleanLogForDisplay`): strips timestamps and ANSI for display. v2 keeps the timestamp on each line, because it is the only record of when a step produced its output; ANSI escapes and runner markers are stripped.
- [x] Timestamp parsing (`parseLogTimestamp`).
- [x] Status → indicator mapping and status priority sorting.
- [x] Actor matching (`matchActor`): case-insensitive, handling the `[bot]`-suffixed login a bare bot name may be filtered by.
- [ ] Check deduplication to the latest run per check name.
- [x] Duration formatting.
- [ ] Diff formatting with per-line styling.

---

## 11. Parity checklist summary

| Section | Items | Checked |
| --- | --- | --- |
| Startup, context, chrome (§2) | 26 | 0 |
| Route map (§3) | 19 states mapped | 0 |
| Route behaviors (§4.1–4.20) | 131 | 45 |
| Filters / pagination / polling (§5.1–5.4) | 19 | 15 |
| CLI and config (§6.1–6.3) | 16 | 0 |
| Fix in v2 (§7) | 7 | 0 |
| Drift decisions (D1–D8) | 8 | 8 |
| API map (§9) | 24 endpoints, 6 client behaviors | 0 |
| Parallel anchors (§10) | 8 | 5 |

A box is checked when the behavior is implemented **and** covered by a test, so
the count follows the phases that ran the checklist: Phase 2 checked the
Workflows, Jobs and Logs rows it delivers (§4.1, §4.4–4.5, §4.8–4.9, §4.20, the
workflow rows of §5, and the anchors the logs package ports). The shell rows
(§2) and the CLI/config rows (§6) are delivered but are re-verified when their
phase runs the list. Drift decisions are checked by their recorded resolutions
above.

Sections 4 (route behaviors), 5 (filters/pagination/polling), and 6 (CLI/config) are the parity gate. Section 7 is the improvement backlog. Section 8 requires a written decision per item before the README is finalized.
