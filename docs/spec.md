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

### 2.6 Hyperlinks

The baseline wrote OSC 8 links with `renderHyperlink`; v2 keeps them and states the contract that makes them safe.

- [x] A name that points at a GitHub page is a link: the pull request title in the list and in the details header, and a comment's author.
- [x] A terminal that does not support OSC 8 prints the text and ignores the sequence, and a theme without the capability renders the text alone, byte for byte, so a golden render is unchanged.
- [x] A link is applied to the text and never to a row, so it does not enter a table's width arithmetic and a row cut short cannot leave the terminal linked.

**v2 note.** `ui.hyperlink` is the one place the sequence is written, and `Theme.Hyperlinks` is the capability flag (on for both real themes). Both properties above are enforced by `TestHyperlinkIsGatedOnTheTheme` and `TestHyperlinkIsInvisibleToWidthAndClosedByTruncation` rather than by convention. The baseline also linked the logs view's run title and job name; v2 leaves those plain, because `w` already opens the job from that screen and a link inside a header the user is standing in adds no way out.

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

**v2 note.** All nineteen states are on the stack: the nine routes up to and including `PRComments`, the four Issue routes (`IssueDetails`, `IssueComments`, its filter input and its filter type), plus the switcher, help, filter type, and repository select. The enum is gone; `RouteStack` gives a pushed route its own identity and `ActionBack` is route-authoritative, so a route steps inside itself before the stack pops.

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

- [x] `j` / `k` / `Down` / `Up` navigate, with the same Load More sentinel rule as workflows.
- [x] `Enter` on the sentinel loads the next PR page; `Enter` on a PR pushes PR Details with sub-tab Info and a fresh checks list.
- [x] `r` / `Ctrl+R` resets and refetches.
- [x] `s` cycles the state filter open → closed → all, then refetches.
- [x] `a` filters by author = current user.
- [x] `i` filters by assignee = current user.
- [x] `v` filters by requested reviewer = current user.
- [x] `f` opens the filter type overlay (free-text user, then pick author/assignee/reviewer).
- [x] `x` clears author, assignee, reviewer and repo filters and resets the state filter to `open`, then refetches.
- [ ] `x` also suppresses **all** filters when pressed on the PRs tab (it resets the repo filter for every tab). v2 scopes `x` to the tab it is pressed on: each tab owns its own filters, so a keystroke on one list cannot change what another list is showing.
- [x] `w` opens the selected PR's `HTMLURL` **browser**.

**v2 note on the three login keys.** `a`, `i` and `v` are three views of the same question — which pull requests involve me — so setting one clears the other two, and pressing the active one clears it. The free-text path (§4.6-4.7) follows the same rule. This is what the previous version did, and it is what makes the three keys read as a choice rather than three cumulative conditions.

### 4.3 Main — Issues tab

- [x] `j` / `k` / `Down` / `Up` navigate, with the Load More sentinel rule.
- [x] `Enter` on the sentinel loads the next issue page; `Enter` on an issue pushes Issue Details with the description focused.
- [x] `r` / `Ctrl+R` resets and refetches.
- [x] `s` cycles the state filter open → closed → all, then refetches.
- [x] `a` filters by author = current user.
- [x] `i` filters by assignee = current user.
- [x] `f` opens the filter type overlay (free-text user, then pick author/assignee).
- [x] `x` clears author, assignee and repo filters and resets the state filter to `open`, then refetches.
- [x] `w` opens the selected issue's `HTMLURL` **browser**.

**v2 note on the two login keys.** As on the PRs tab, `a` and `i` are two views of the same question, so setting one clears the other and pressing the active one clears it. The tab starts on `open`, the page size is 50, the list is aggregated across the context's repositories and merged by `updated_at` descending, and the rows the endpoint returns that are really pull requests are dropped by the transport before the tab ever sees them. The `x` key scopes to this tab, so it never changes what another list is showing (spec §4.2).

### 4.4 Filter type overlay

- [x] `u` / `U` opens the user filter text input. On the Workflows tab it is pre-filled with the current actor filter; on the other tabs it starts empty. All three prompts are delivered.
- [x] `r` / `R` opens the repository select overlay; if the repo list is empty it is fetched first (with a loading message).
- [x] `Esc` returns to the main view.
- [x] `Ctrl+C` quits.

### 4.5 Repository select overlay

- [x] `j` / `k` / `Down` / `Up` move the selection.
- [x] `Enter` sets the repo filter to the selected repository name, returns to the main view, and refetches the active tab.
- [x] `Esc` returns to the filter type overlay.
- [x] `Ctrl+C` quits.

### 4.6 User filter text input (PRs and Issues)

- [x] `Enter` accepts the typed username and advances to the filter type overlay. Delivered for the PRs and Issues tabs.
- [x] `Esc` returns to the main view.
- [x] `Ctrl+C` quits.
- [x] All other keys are forwarded to the text input.

### 4.7 Filter type overlay — PRs and Issues

- [x] PRs: `a` sets author, `i` sets assignee, `r` sets reviewer to the typed username, returns to the main view, and refetches.
- [x] Issues: `a` sets author, `i` sets assignee to the typed username, returns to the main view, and refetches. The issue picker offers two roles rather than three, because an issue has no requested reviewer.
- [x] `Esc` / `c` / `C` cancels back to the main view.

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

- [x] `Esc` / `Backspace` returns to the PR list.
- [x] `Tab` / `Shift+Tab` toggles focus between the description viewport and the checks sidebar.
- [x] `j` / `Down` scrolls the description down 1 when focused, otherwise moves to the next check.
- [x] `k` / `Up` scrolls the description up 1 when focused, otherwise moves to the previous check.
- [x] `u` scrolls the description up 6 (description focus only).
- [x] `d` scrolls the description down 6 (description focus only).
- [x] `Enter` on a check resolves the matching Actions run, by run ID extracted from the check URL, falling back to a case-insensitive name match; a match opens Jobs targeting that job name.
- [x] `Enter` on a check with a run ID but no matching run in the list opens Jobs for that run ID.
- [x] `Enter` on a check with no run ID opens the check URL in the **browser**.
- [x] `w` opens the PR URL when the description is focused, otherwise the selected check URL, in the **browser**.
- [x] `m` opens the merge overlay when the viewer has write permission, otherwise shows "You don't have write scopes (repo) to merge this PR."
- [x] `c` opens the comments view.
- [x] `C` opens the close-confirm overlay when the viewer has write permission, otherwise shows the equivalent scopes message.
- [x] `v` opens the commits view.
- [x] `D` opens the diff view.
- [x] `r` / `Ctrl+R` refreshes the PR details.
- [x] The view has five sub-tabs: Info, Commits, Files, Checks, Comments.

**v2 addition: the sub-tab strip.** The five views are one route with a numbered strip (`1` Info, `2` Commits, `3` Files, `4` Checks, `5` Comments) rather than five screens. The letters keep their v1 meanings (`v` commits, `D` diff, `c` comments) and the numbers are added, so either hand works. `Esc` steps back one level: from a sub-tab it lands on Info, and from Info it returns to the list. The Info sub-tab is two columns: the description, and a sidebar holding the details block above the checks. The sidebar is dropped when the terminal is too narrow for a label and its value, and the details block is dropped when the body is too short for it and a usable checks list.

**v2 addition: a check that cannot be resolved is not a dead end.** A check whose URL names a run opens that run's jobs; a check whose URL is not a workflow run (a coverage report, a linter bot) opens its own page in the browser instead of doing nothing.

### 4.11 PR Commits

- [x] `Esc` / `Backspace` returns to PR Details.
- [x] `j` / `k` / `Down` / `Up` navigate the commit list.
- [x] `Enter` fetches and shows commit details for the selected commit.

### 4.12 Commit Details

- [x] `Esc` / `Backspace` returns to PR Commits.
- [x] `j` / `k` / `Down` / `Up` navigate the changed-file list and update the diff viewport.
- [x] `u` / `d` scroll the diff by 3 lines.
- [x] `w` opens the commit in the **browser**.

### 4.13 PR Diff

- [x] `Esc` / `Backspace` returns to PR Details.
- [x] `j` / `k` / `Down` / `Up` navigate the file list and update the diff viewport.
- [x] `u` / `d` scroll the diff by 3 lines.
- [x] `w` opens `<PR URL>/files` in the **browser**.

### 4.14 PR Comments

- [x] `Esc` / `Backspace` returns to PR Details.
- [x] `r` / `Ctrl+R` refreshes the comments.
- [ ] Other keys are forwarded to the viewport for scrolling. v2 binds scrolling explicitly (`u`/`d`), the same as the log viewer (§4.9), so an unbound key does nothing.

### 4.15 Issue Details

- [x] `Esc` / `Backspace` returns to the issue list.
- [x] `Tab` / `Shift+Tab` toggles focus between the description and the metadata sidebar.
- [x] `j` / `k` scroll the description by 1 when focused.
- [x] `u` / `d` scroll the description by 6 when focused.
- [x] `w` opens the issue in the **browser**.
- [x] `c` opens the comments view.
- [x] `r` / `Ctrl+R` refreshes the issue details.
- [x] The metadata sidebar shows state, author, assignees, labels, milestone, and timestamps.

**v2 note.** The sidebar holds no selection, so a scroll key while it has the focus changes nothing; the toggle is what says which pane the keys act on. The route arrives with the issue the list row already carries, so the heading and the sidebar are populated before the fetch lands, and the description is where the fetch lands. A tick refreshes the issue, as the pull request screen's does for its checks.

### 4.16 Issue Comments

- [x] `Esc` / `Backspace` returns to Issue Details.
- [x] `r` / `Ctrl+R` refreshes the comments.
- [x] The conversation scrolls with `u` / `d`, the same pair the pull request conversation uses (§4.14).

### 4.17 Context switcher overlay

- [x] `j` / `k` / `Down` / `Up` move the selection through the target list (user first, then orgs, sorted as returned).
- [x] `Enter` loads the selected context: resets runs, pulls and issues lists, pagination, selection, repo cache, repo filter, and dashboard counters, then fetches the active tab.
- [x] `Esc` returns to the previous route.

**v2 note.** The switcher is a route, not a modal, so it is pushed and popped like any other screen and its cursor starts on the context in force. Selecting broadcasts `AccountChangedMsg`: every tab drops its list, pagination, selection and filters, the store's cache is invalidated in one call, and each tab on the stack fetches again. The two "targets" the baseline mixed into one list — accounts, organizations and repositories — are gone: the switcher lists owners, which is the only thing a context can be. `TestSwitcherSelectsAndCloses` covers the open, the move, the select, the header update, and the close.

### 4.18 Help overlay

- [x] Renders sections: Global Keys, Workflow Runs, Pull Requests, Pull Request Details, Workflow Jobs, Logs Viewer.
- [x] Renders the status legend.
- [x] `Esc` or `?` closes it.

**v2 note.** Every section is generated from the keymap table (`keymap.Scopes()`), so a route's section lists the keys that route actually handles: the baseline's six hand-written sections become the 25 declared scopes, and the footer, the help body, and the key handling cannot drift apart. The status legend leads the body (visible without scrolling) and each marker is drawn by `statusIndicator`, the same function that puts the marker in front of a run, job, check or log row, so the legend cannot name a symbol the tables do not draw.

### 4.19 Merge overlay

Method select (`mergeState == 1`):

- [x] `s` / `S` selects squash and advances to confirm.
- [x] `m` / `M` selects regular merge and advances to confirm.
- [x] `r` / `R` selects rebase and advances to confirm.
- [x] `Enter` advances using the configured default method.
- [x] `d` / `D` cycles the persisted default merge method squash → merge → rebase and writes it to the config file.
- [x] `Esc` / `c` / `C` cancels.
- [x] The default method label reflects `default_merge_method` from config.

Confirm merge (`mergeState == 2`):

- [x] `y` / `Y` performs the merge with the chosen method and shows the loading message "Merging pull request...".
- [x] `n` / `N` / `Esc` cancels without merging.

Confirm close (`mergeState == 4`):

- [x] `y` / `Y` closes the PR with the loading message "Closing pull request...".
- [x] `n` / `N` / `Esc` cancels.

**v2 note.** The config write edits the file as a YAML document, so keys this build does not know are preserved and the token is never rewritten. A session with no writable config path still cycles the default and says so: "Default merge method: merge (this session only)."

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
| Author / assignee / reviewer | PRs | **Client-side, after fetch** | Applied only to the fetched page; the three are exclusive |
| Author / assignee | Issues | **Client-side, after fetch** | Applied only to the fetched page |
| State (issue) | Issues | Server-side | Defaults to `open` |

- [x] PRs are filtered client-side after fetching, so a filter only sees the current page of the first 8 repositories.
- [x] Issues are filtered client-side after fetching, same limitation.
- [x] The repo filter clears when switching context.

### 5.2 Pagination

| Resource | Page size | Next page | End detection |
| --- | --- | --- | --- |
| Repositories | 100 (list), 15 (poll) | not paginated | — |
| Workflow runs | 8 | Load More sentinel | the repository's `total_count` |
| Pull requests | 8 | Load More sentinel | the search index's match count |
| Issues | 50 | Load More sentinel | the search index's match count |

- [x] Each list exposes a "Load More" row as the final selectable row while more pages exist. Delivered for workflow runs, pull requests and issues.
- [x] The Load More row is offered from the count the API reports rather than from a full page. A page that holds the last item ends the list even when it is full, which is what removes the phantom row the previous rule produced (v2, see [§7](#7-fix-in-v2)).
- [x] On the REST issues endpoint `HasMore` is still read from the raw page, before pull requests are dropped, so a full page keeps the sentinel even when the filtered list is short. That rule now applies to the fan-out fallback only.
- [x] The scope-wide pull request and issue lists come from one GraphQL search query per page, with the state and login filters applied by the API across the whole scope (v2). The cursor a page ends with starts the next one.
- [x] A scope the search index will not serve falls back to the REST fan-out, which is what keeps the screen working when the index refuses (v2).
- [x] A login filter reads an app the way the index does: a bot's login, which the API spells `<slug>[bot]`, becomes the `app/<slug>` qualifier, so a filter built from the login the list shows narrows to that app's entries. The index matches the bare slug the GraphQL API reports for a bot in no spelling, with or without the scope qualifier (v2).
- [x] Pull requests are sorted by `updated_at` descending, then by the higher number, so two pull requests touched in the same second have a stable order.
- [x] Workflow runs are sorted by a status priority, then recency.
- [x] Jobs are sorted by a job-specific priority.
- [x] Polled runs are merged into the existing list by run ID, then re-sorted.
- [x] Polled pull requests are merged by ID, then re-sorted, so a pull request whose state changed replaces its row.

**v2 note on the search index's limits.** The index is eventually consistent, so a write can take a few seconds to appear in a list; it holds at most 1000 matches for one query, which is also where it stops serving pages; and its requests draw on a separate allowance (documented as 30 per minute) on top of the point budget. The app asks for one search per page of a list and polls only the visible tab, so the allowance is not spent on lists nobody is looking at.

### 5.3 Cross-scope aggregation

- [x] Lists are aggregated across the first **8 repositories** of the context, sorted by `pushed_at` descending.
- [x] Polling uses the first **15 repositories** but only queries the first 8. v2 polls through the same 8-repository fan-out as the list, so there is one code path instead of two.
- [x] Each repository is queried concurrently, one request per repository.
- [x] Runs carry their repository embedded so the UI can render owner/repo columns.

**v2 note.** One cap, `store.maxAggregateRepos`, and one `repoTargets` helper serve the runs list and the fan-out fallback that pull requests and issues fall back to. The 15-repository poll list is gone.

**Fix in v2, delivered for two of the three lists.** Pull requests and issues are now one scope-wide GraphQL search query per page with real cursor pagination, the match count, and server-side filters; the fan-out behind them is the fallback rather than the path. **Workflow runs could not follow**, and this is a schema limit rather than a choice: the only run connection in GitHub's GraphQL schema is `Workflow.runs`, which hangs off a single workflow file, and `Query.search` has no run index (`SearchType` is discussion, issue, repository and user). So the runs list keeps the bounded fan-out, and `maxAggregateRepos = 8` now exists for that one list. At a five-second poll that is 1.6 requests a second against the shared five-a-second allowance, which leaves room for the scope lists' searches.

### 5.4 Polling model

- [x] A tick drives the spinner every 300 ms.
- [x] A poll tick drives data refresh. Interval by state: main + PRs tab → PRs interval; every other state → workflows interval.
- [x] Default intervals: workflows 5 s, PRs 10 s, issues 10 s.
- [x] `polling.workflows_interval_seconds`, `polling.prs_interval_seconds` and `polling.issues_interval_seconds` override them individually.
- [x] The legacy `polling_interval_seconds` sets all three, and takes effect before the per-tab keys.
- [x] Polling targets: main + Workflows → runs; main + PRs → pull requests; main + Issues → issues; Jobs → active jobs; Logs → logs.
- [x] Polling does not run on other routes.

**v2 note.** The root owns the cadence and the route owns what a tick means for its own data. A pull request detail screen posts its own tick handler: it refreshes the checks section, which is the part that changes while a pull request is open, rather than reloading the whole screen. The issue detail screen posts one too, and refreshes the issue itself.

**Fix delivered in v2:** the Issues tab has an interval of its own. The selector special-cases the Issues scope, so the tab no longer rides the workflows cadence, and `polling.issues_interval_seconds` retimes it.

---

## 6. CLI and configuration surface

### 6.1 CLI

- [x] `-v` / `-version`
- [x] `-org <name>`
- [x] `-user <name>`

**v2 note.** `cmd/ghspector.run(args, stdout)` takes its arguments and its version writer as parameters, so `cmd/ghspector/main_test.go` exercises the flags without a terminal: `-v` prints three stamps and exits 0, `-org` and `-user` each clear the other, and an unknown flag exits non-zero instead of being read as "no flags".

### 6.2 Config file

Path: `<os.UserConfigDir()>/ghspector/config.yaml` (Linux/macOS `~/.config/ghspector/config.yaml`, Windows `%APPDATA%\ghspector\config.yaml`).

- [x] `github_token` — token
- [x] `default_org` — org selected at startup
- [x] `default_account` — user selected at startup
- [x] `polling_interval_seconds` — legacy, sets all three intervals
- [x] `polling.workflows_interval_seconds`
- [x] `polling.prs_interval_seconds`
- [x] `polling.issues_interval_seconds` — added by the rewrite, so an existing file keeps working unchanged
- [x] `default_merge_method` — `squash` | `merge` | `rebase`, and is rewritten by the `d` key in the merge overlay
- [x] A warning is printed to stderr when the config file permissions allow group or other access.
- [x] The config file is written with mode `0600` and its directory with `0700`.
- [x] Keys `default_org`, `default_account`, `polling`, and `default_merge_method` are omitted when empty (`omitempty`).

**v2 addition:** a `version` key. Additive only.

**v2 note.** `auth.TestConfigFileSurface` pins the file itself: the modes it is created with, the stderr warning for a group-readable file, and the keys a token-only config leaves out. `auth.TestSetDefaultMergeMethod` pins the `d` key's write, which edits the file as a YAML document so a key this build does not know survives and the token is never decoded onto disk.

### 6.3 Token scopes

- [x] Required scopes for write actions are `repo` and `workflow`; the app reads `X-OAuth-Scopes` to determine them.
- [x] Merge and close are gated on repository write permission for the authenticated user.
- [x] Missing scopes produce the "You don't have write scopes (repo) to ..." status message.

**v2 note (finding F1, closed).** The baseline's `viewerCanMerge()` returned true when the `X-OAuth-Scopes` header was absent, so a token the API says nothing about was advertised as able to write and then failed at the API. `ui.decidePRWrite` fails closed instead: it asks the scope question first, and when the header is absent it asks the question that decides the matter — `GET /repos/{o}/{r}/collaborators/{user}/permission` — exactly as the run approval path already did. An unreadable permission is a refusal, not a grant. `internal/ui/prs_test.go` covers the refusal message, a permission of `read`, an unreadable permission, and the scoped path.

---

## 7. Fix in v2

These are behavior changes the rewrite is expected to make. They are not parity requirements, and each needs its own test.

- [x] Replace the 8-repository cap with scope-wide aggregation via GraphQL, with real cursor pagination and `total_count`. **Delivered for pull requests and issues**, which are one cursor-paginated search query per page and end on the count the index reports. **Not delivered for workflow runs, and not deliverable:** the schema's only run connection is `Workflow.runs`, which hangs off one workflow file, and search has no run index, so the runs list keeps the fan-out and the cap (see §5.3).
- [x] Move PR author/assignee/reviewer and issue author/assignee filters server-side (GraphQL supports all of them), so filtering is truthful across the whole scope rather than only the current page. **Delivered:** the filters are search qualifiers (`author:`, `assignee:`, `review-requested:`, `repo:`, `state:`), so a filtered list is what the API matched across the scope. The REST fallback still applies its own filter to the page it holds, because the REST list endpoints cannot express one.
- [x] Unify the workflow actor filter with the other user filters: today it is a `bool` flag plus a prefilled text input, while PRs and Issues use dedicated states.
- [x] Poll the Issues tab on its own interval. **Delivered:** `keymap.MainIssues` selects the Issues interval, and `polling.issues_interval_seconds` sets it.
- [ ] Replace the merge-method default cycle, which persists config on a keystroke, with an explicit save or an in-memory default that is written once. **Decided:** the cycle stays, because the method select is the only place it is chosen and a save key would duplicate the `Enter` that already advances. What the item was really about — the write clobbering the file — is fixed: `d` edits the file as a YAML document (`auth.SetDefaultMergeMethod`), so a key this build does not know survives and the token is never re-encoded; a session with no writable path cycles in memory and says "this session only".
- [x] Give the Jobs route an explicit origin instead of the `prevState` field, and give every pushed route an explicit `(owner, repo, number, runID)` rather than reading it back out of shared model fields.
- [x] Define and test a minimum terminal width with a graceful "terminal too small" state.
- [ ] Render pull request, issue and commit bodies as markdown. **Open:** planned as its own change, after the palette work, because it is the one place in the program that formats text it did not write. `charm.land/glamour/v2` v2.0.1 is available and depends only on the v2 Charm modules. The rules it has to follow: one renderer per (scheme, width), a style fixed at construction, the renderer built when the scheme or the width changes and reused, and no terminal query and no renderer construction inside a view. The baseline's markdown renderer called `lipgloss.HasDarkBackground()` inside the render path — one blocking probe per frame — which is why one version rendered markdown well and was unusable at the same time.

**v2 notes on the three closed items.**

- The filter menu, the user prompt, and the repo picker are one overlay set (`ui/filters.go`) keyed by `FilterKind`, and each tab accepts a kind through its own `acceptFilter`. The workflow actor filter is `FilterActor`, a peer of the PR author/assignee/reviewer filters; `showFilterInput` is gone.
- A pushed route owns its identity. `Jobs` carries the `store.RepoRef` and the run it lists, `Logs` carries the job it shows, and `PRDetails` carries the pull request number it is about; nothing reads the parent route's fields and nothing reads a `prevState`. `ActionBack` is route-authoritative, so Esc steps inside a route (the PR sub-tab strip) before the stack pops.
- `layout.TooSmall`, `layout.MinWidth` (50) and `layout.MinHeight` (12) gate the frame: below them the window draws a "Terminal too small" screen naming the current and required size, and every route still renders inside its rect so nothing is clipped mid-row. `TestTooSmallReplacesTheScreen`, `TestEveryScreenHasExactGeometry` and the `too_small_40x15` golden cover it.

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
| `GET /users/{o}/repos`, `GET /orgs/{o}/repos` | Repo list (`sort=pushed&direction=desc`) | REST (the list the run fan-out and the fallback cover) |
| `POST /graphql` (`search`) | Scope-wide pull request and issue lists, one query per page | GraphQL |
| `GET /repos/{o}/{r}/actions/runs` | Workflow runs, optional `actor` | REST (the schema has no scope-wide run connection; see §5.3) |
| `GET /repos/{o}/{r}/actions/runs/{id}` | Single run | REST |
| `GET /repos/{o}/{r}/actions/runs/{id}/jobs` | Jobs | REST |
| `GET /repos/{o}/{r}/actions/runs/{id}/attempts/{n}/jobs` | Jobs for an attempt | REST |
| `GET /repos/{o}/{r}/actions/jobs/{id}` | Single job | REST |
| `GET /repos/{o}/{r}/actions/jobs/{id}/logs` | Job logs (REST-only; errors arrive as XML) | REST |
| `GET /repos/{o}/{r}/pulls` | Pull requests, `state` (the fan-out fallback; the list itself is a search) | GraphQL search, REST fallback |
| `GET /repos/{o}/{r}/issues` | Issues, `state`; PRs filtered out client-side (the fan-out fallback; the list itself is a search that excludes them by query) | GraphQL search, REST fallback |
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
- [x] Every GraphQL query asks for `rateLimit { limit cost remaining resetAt }`, and the cost the API reports is what the next query reserves from the shared limiter (v2). The header renders whichever of the two transports has less left, since either can cut the app off.

---

## 10. Parallel anchors

These are the behaviors most worth porting directly as pure functions rather than rediscovering:

- [x] Log segmentation by step (`segmentLogs`): maps a raw log to step segments using group markers and step names, with a fuzzy name-matching score. v2: `internal/logs.Segment` and `MatchScore`, ported as pure functions with their own tests.
- [x] Log cleaning (`cleanLogForDisplay`): strips timestamps and ANSI for display. v2 keeps the timestamp on each line, because it is the only record of when a step produced its output; ANSI escapes and runner markers are stripped.
- [x] Timestamp parsing (`parseLogTimestamp`).
- [x] Status → indicator mapping and status priority sorting.
- [x] Actor matching (`matchActor`): case-insensitive, handling the `[bot]`-suffixed login a bare bot name may be filtered by.
- [x] Check deduplication to the latest run per check name.
- [x] Duration formatting.
- [x] Diff formatting with per-line styling.

**v2 note.** Where each anchor lives: log parsing, segmentation, cleaning and timestamp reading are pure functions in `internal/logs`; status priority sorting and the marker mapping are `store` sorts and `ui.statusIndicator`; actor matching is `store.matchActor`; check deduplication is `gh.filterLatestCheckRuns` behind `Store.CheckRuns`; diff styling is `ui.diffStyled`. The §9 rows below are the transport behaviors, and they belong to the same client the rewrite kept (`internal/gh`).

---

## 11. Parity checklist summary

| Section | Items | Checked |
| --- | --- | --- |
| Startup, context, chrome (§2.1–2.5) | 26 | 0 |
| Hyperlinks (§2.6) | 3 | 3 |
| Route map (§3) | 19 states mapped | 15 |
| Route behaviors (§4.1–4.20) | 131 | 108 |
| Filters / pagination / polling (§5.1–5.4) | 20 | 19 |
| CLI and config (§6.1–6.3) | 16 | 16 |
| Fix in v2 (§7) | 7 | 3 |
| Drift decisions (D1–D8) | 8 | 8 |
| API map (§9) | 24 endpoints, 6 client behaviors | 0 |
| Parallel anchors (§10) | 8 | 8 |

A box is checked when the behavior is implemented **and** covered by a test, so
the count follows the phases that ran the checklist. Phase 2 checked the
Workflows, Jobs and Logs rows it delivers (§4.1, §4.4–4.5, §4.8–4.9, §4.20, the
workflow rows of §5, and the anchors the logs package ports). Phase 3 checked
the pull request rows (§4.2, §4.6–4.7, §4.10–4.14, §4.17–4.19), the rows of §5
its poller touches, every row of §6, §10, the three §7 items the shell
delivered, and the new hyperlink contract in §2.6.

Two groups stay at zero on purpose, because their verification pass has not run:
§2.1–2.5 (startup, context, and chrome) and §9 (the transport). Both are
implemented and partly tested — the shell tests behind them are
`TestSplashUntilIdentityResolves`, `TestForbiddenShowsTheErrorScreen`,
`TestRetryFromTheErrorScreen`, `TestFooterRendersTheActiveScope`,
`TestQuitWorksFromEveryStage`, `TestStatusMessageAndExpiry`, and the transport
behaviors live in `internal/gh` — but the boxes are not ticked until each one
has been read against its own test. Ticking a row without that pass would turn
the checklist into a claim.

Sections 4 (route behaviors), 5 (filters/pagination/polling), and 6 (CLI/config) are the parity gate. Section 7 is the improvement backlog. Section 8 requires a written decision per item before the README is finalized.
