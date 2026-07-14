# Implementation Plan - TUI Layout, Styling, Merge Defaults, and Repository Filtering

This plan outlines the enhancements to `ghspector` TUI views to maximize screen space utilization, refine visual styles, make shortcut hints dynamic/floating, clarify Shift/uppercase shortcuts, support configurable default merge methods, and implement repository-level filtering via a scrollable selection modal.

---

## Iteration 1: TUI Layout, Styling, Defaults, and Repo Filtering

### Goals
1. **Maximize Workflow Jobs Table**:
   - Make the `JOB NAME` column scale dynamically relative to screen width (`m.width`).
   - Add a `STEPS` column to show step progress (e.g. `3/5` completed).
2. **Shift Shortcut Clarification**:
   - Update PR details footer key hints from `D:Diff` and `C:Close PR` to `Shift+D:Diff` and `Shift+C:Close PR`.
3. **Floating Bottom Bar / Hints Layout**:
   - Extend the bottom bar width to 100% of the screen.
   - Automatically inject `?:Help` (or `?:Close Help` inside help view) if missing from the keybinds list.
   - Align `Esc` and `?` shortcuts to the left, and float all other navigation/action shortcuts to the right.
   - Display status messages next to the left-aligned shortcuts.
4. **Header Styling**:
   - Add an adaptive background color (`#eaeaea` for Light, `#262626` for Dark) to the top header bar.
   - Ensure rate limit status colors and the loading spinner correctly inherit/apply the background.
   - Add a horizontal border line (`hr` equivalent) directly under the header.
5. **Configurable Default Merge Method**:
   - Modify the PR merge method modal so hitting `Enter` immediately confirms and proceeds with the configured default method (e.g., Squash).
   - Allow cycling the default method (Squash → Regular → Rebase) inside the selection screen by pressing `d`/`D`.
   - Persist the selected default method to the user's `config.yaml` file under `default_merge_method`.
6. **Repository Filtering Modal & Combined Filters**:
   - Intercept the `f` (filter) key in the main view across all tabs (workflows, PRs, and issues) to open a choice modal:
     - `[U] Filter by User`
     - `[R] Filter by Repository`
   - If filtering by Repository, display a scrollable selection modal containing the target's repositories (loaded asynchronously in the background).
   - Handle pagination/scrolling for repository list if there are many repositories.
   - **Combined Filters**: The repository filter and the user filter can be active simultaneously (combining the search criteria). 
   - Apply the repository filter to the active tab, query only the selected repository, and display the active repo filter alongside the user filter in the filters bar.

### Proposed Changes

#### Configuration Updates
##### [MODIFY] [internal/auth/auth.go](internal/auth/auth.go)
- Add `DefaultMergeMethod string` to the `Config` struct (unmarshal/marshal from YAML key `default_merge_method`).

```diff
 // Config holds the configuration options.
 type Config struct {
 	GitHubToken            string        `yaml:"github_token"`
 	DefaultOrg             string        `yaml:"default_org,omitempty"`
 	DefaultAccount         string        `yaml:"default_account,omitempty"`
 	PollingIntervalSeconds int           `yaml:"polling_interval_seconds,omitempty"`
 	Polling                PollingConfig `yaml:"polling,omitempty"`
+	DefaultMergeMethod     string        `yaml:"default_merge_method,omitempty"`
 	TokenSource            string        `yaml:"-"`
 }
```

#### Theme Style Additions
##### [MODIFY] [internal/tui/theme.go](internal/tui/theme.go)
- Add `Header`, `HeaderTitle`, `HeaderSubtitle` styles and `HeaderBg` color field to `Theme` struct.
- Initialize them in `GetTheme()` using an adaptive color matching the terminal theme mode.

#### View State Additions
##### [MODIFY] [internal/tui/model.go](internal/tui/model.go)
- Add new `viewState` constants: `viewFilterTypeSelect` and `viewRepoFilterSelect`.
- Add fields to cache repositories and track selection/scrolling:
  - `repos []gh.Repository`
  - `selectedRepoIdx int`
  - `repoStartIndex int`
  - `filterRepo string`

#### View Render Updates
##### [MODIFY] [internal/tui/view.go](internal/tui/view.go)
- **`renderHeader`**: Render header content using new backgrounds, and append the border line.
- **`renderFooter`**: Maximize width, auto-inject missing `?` key, float `Esc` and `?` left (with status), and float other keys right.
- **`renderJobsView`**: Maximize columns (dynamic width calculation for job name column) and append the `STEPS` column.
- **`renderPRDetailsView`**: Rename `D:Diff` -> `Shift+D:Diff` and `C:Close PR` -> `Shift+C:Close PR`.
- **`renderMergeModal`**: Show the active default method, and instructions on how to cycle it (`[d]`) or merge with it (`[Enter]`).
- **`renderFilterTypeSelectModal`** [NEW]: A modal to choose between User and Repo filters.
- **`renderRepoFilterSelectModal`** [NEW]: A scrollable modal showing repositories for selection.
- Update `renderMainView`, `renderPullsView`, and `renderIssuesView` to display the active `Repo: <name>` filter.

#### Update Logic & Background Fetching
##### [MODIFY] [internal/tui/update.go](internal/tui/update.go)
- **Background Fetching**:
  - Implement a `fetchReposCmd` to load up to 100 repositories of the active target org/user.
  - Trigger `fetchReposCmd` on initial load and whenever target account context changes.
- **Key Interceptors**:
  - Handle `viewFilterTypeSelect` and `viewRepoFilterSelect` states.
  - In `viewMain`, when `f` is pressed, navigate to `viewFilterTypeSelect` instead of opening the user filter input directly.
  - In `viewRepoFilterSelect`, handle arrow keys / `j`/`k` to scroll repositories, `Enter` to select and refetch data, and `Esc` to go back.
  - In `viewPRDetails`, when in `mergeState == 1` (choose merge method):
    - Handle `enter` to select the default configured merge method.
    - Handle `d`/`D` to cycle the default merge method and automatically call `auth.SaveConfig` to write to config.yaml.
- **Data Filtering**:
  - In `fetchRunsCmd`, `fetchPullsCmd`, and `fetchIssuesCmd`, check if `m.filterRepo` is set. If yes, query only that repository (filtering the list of repositories queried).
  - In `x` key handler, set `m.filterRepo = ""` to clear repo filters.

---

## Iteration 2: Closed Issues Pagination Bug Fix

### Goals
- Resolve a bug where filtering by closed issues prematurely hid the "Load More" button after loading 3 items.
- The bug occurred because the GitHub Issues API returns both issues and pull requests in its response, which is then filtered client-side. If a page returned 8 items but they all got filtered out as pull requests, the length of the returned list was `0`. The TUI interpreted this `0` length as "no more issues" and hid the "Load More" button.
- Fix: Modify the GitHub client to return a `hasMore` boolean along with the filtered list indicating if the raw response count equaled the requested limit. Keep the "Load More" button visible as long as `hasMore` is true.

### Proposed Changes

#### Client Implementation
##### [MODIFY] [internal/gh/client.go](internal/gh/client.go)
- Update `GetIssuesWithState` signature to return `([]Issue, bool, error)`.
- Determine `hasMore` as `len(allIssues) == perPage` before filtering out pull requests.

#### Message Definitions
##### [MODIFY] [internal/tui/model.go](internal/tui/model.go)
- Add `hasMore bool` to `issuesLoadedMsg` struct.

#### Update Logic
##### [MODIFY] [internal/tui/update.go](internal/tui/update.go)
- Update `fetchIssuesCmd` to track `anyHasMore` across repositories and pass it inside `issuesLoadedMsg`.
- Update `case issuesLoadedMsg:` handler to set `m.hasMoreIssues = msg.hasMore`.

---

## Verification Plan

### Automated Tests
- Run `go test ./...` to ensure all tests pass (including new `TestTUI_IssuesPaginationPRFiltering` test verifying the pagination fixes).

### Manual Verification
- Filter by closed issues on a repository with a large number of issues and pull requests.
- Verify that pressing `Load More` repeatedly continues loading items, and the button does not disappear prematurely when a page has only pull requests.
