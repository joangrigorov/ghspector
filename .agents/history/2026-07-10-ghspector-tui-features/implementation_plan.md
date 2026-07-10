# Implementation Plan - GhSpector New Features

This plan outlines the design and implementation steps for adding the following features to **ghspector**:
1. Clickable links to workflows and jobs (both terminal-native OSC 8 links and keyboard shortcuts).
2. Browse previous attempts of a workflow run.
3. Filtering workflow runs by actor (both "mine" and a custom actor name).
4. A dedicated Help Screen overlay containing a full shortcuts guide and the status icon legend, leaving only essential actions in the footer.

---

## Goal Description
Enhance `ghspector` with convenient browser link navigation, historical run attempt browsing, robust actor filtering, and a clean, clutter-free help screen UI.

## User Review Required
No breaking configurations or dependencies are introduced. The layout changes will rearrange footer legends and introduce a help screen overlay (`?` hotkey).

## Open Questions
None. All design requirements have been aligned and finalized.

---

## Proposed Changes

### TUI Core & Views

#### [MODIFY] [internal/tui/model.go](internal/tui/model.go)
* Add `viewHelp` to the `viewState` enum.
* Add fields to `Model`:
  * `filterActor string`: The current actor search query.
  * `showFilterInput bool`: Toggle state to render actor filter text input.
  * `textInput textinput.Model`: Charmbubble textinput component.
  * `currentUser string`: Logged-in username.
  * `selectedAttempt int`: Currently viewed run attempt number (default is `run.RunAttempt`).

#### [MODIFY] [internal/tui/view.go](internal/tui/view.go)
* Update `View()` dispatcher to support `viewHelp`.
* Implement `renderHelpView()` to display:
  * Categorized hotkeys (Global, Main, Jobs, Logs, Switcher).
  * Status icon legend (running, success, failed, queued, waiting).
* Remove the legend from `renderMainView()` and `renderJobsView()` to prevent UI clutter.
* Shorten standard footers to display only the most critical navigation keys (e.g. including `?:Help` contextually).
* Implement `renderHyperlink(text, url string) string` helper mapping to OSC 8 escape sequences `\x1b]8;;url\x1b\\text\x1b]8;;\x1b\\`.
* Apply `renderHyperlink` to the workflow run titles, jobs list names, and details headers.

#### [NEW] [internal/tui/browser.go](internal/tui/browser.go)
* Create cross-platform function `openBrowser(url string) error` using `exec.Command` mapping to system-specific openers (`xdg-open` for Linux, `open` for macOS, `rundll32` for Windows).

#### [MODIFY] [internal/tui/update.go](internal/tui/update.go)
* Support global `?` toggle key that pushes the current state to `prevState` and flips to `viewHelp`. In `viewHelp`, `Esc` or `?` restores the previous state.
* Forward key events to `textInput` when `showFilterInput` is true.
* Implement custom filter prompt input lifecycle:
  * Pressing `f` in `viewMain` activates input.
  * `Enter` triggers a fresh `fetchRunsCmd()` using the inputted name.
  * `Esc` or empty text clears the filter.
* Implement quick filter key `m`:
  * Toggles between filtering runs by `currentUser` (from initial payload) and no filter.
* Update `fetchRunsCmd()` and `pollRunsCmd()` to fetch runs with `m.filterActor` passed as a query parameter.
* Implement attempts switcher:
  * In `viewJobs`: Pressing `[`/`]` cycles `selectedAttempt` (1 to `run.RunAttempt`).
  * Trigger `fetchJobsCmd` with the target attempt number.
* Handle link keybindings:
  * In `viewMain`: `w` opens current workflow run URL in browser.
  * In `viewJobs`: `w` opens workflow run URL, `v` opens selected job URL in browser.

---

### GitHub API Wrapper

#### [MODIFY] [internal/gh/types.go](internal/gh/types.go)
* Add `RunAttempt int \`json:"run_attempt"\`` to `WorkflowRun` struct.

#### [MODIFY] [internal/gh/client.go](internal/gh/client.go)
* Update `GetWorkflowRuns` signature to accept `actor string` query filter.
* Add `GetWorkflowRunAttemptJobs(ctx context.Context, owner, repo string, runID int64, attempt int) ([]WorkflowJob, error)` for target attempt retrieval.

---

## Verification Plan

### Automated Tests
Run all unit and integration tests with:
```bash
go test ./...
```

The following new test coverage will be added:
1. **Client API Tests** (in [client_test.go](internal/gh/client_test.go)):
   * `TestClient_GetWorkflowRuns_ActorFilter`: Verify that passing an `actor` query parameter properly appends `?actor=USERNAME` to the HTTP request URL.
   * `TestClient_GetWorkflowRunAttemptJobs`: Mock the attempt jobs endpoint `/repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}/jobs` and verify jobs list extraction.
2. **TUI Component/State Tests** (in [app_test.go](internal/tui/app_test.go)):
   * `TestTUI_HelpScreen`: Test entering and exiting `viewHelp` with `?` and `Esc` key inputs and verify view output matches shortcuts list.
   * `TestTUI_ActorFilter`: Test toggling actor filter via `m` (own runs) and typing a name via `f` (custom actor) and forwarding to text input.
   * `TestTUI_AttemptNavigation`: Test selecting run with multiple attempts, entering jobs view, and using `[` / `]` keys to cycle through attempts, validating that job fetches are triggered for specific attempts.

### Manual Verification
1. Open the TUI:
   * Verify rate limits and startup items load.
2. Filter tests:
   * Press `m` to toggle your own runs. Confirm only runs matching your login are shown.
   * Press `f` and type a distinct GitHub user's name. Confirm the filtered runs update.
   * Clear the filter (press `f` then `Enter` on empty, or `Esc`).
3. Help page:
   * Press `?` on the main page. Verify the shortcuts help sheet and status legend render.
   * Press `Esc` or `?` to close.
4. URLs test:
   * Select a run, press `w`. Verify the default browser opens the GitHub actions workflow run.
   * Enter jobs view. Select a job, press `v`. Verify the browser opens the job details page.
5. Attempt switching:
   * Select a workflow run that had multiple attempts.
   * Confirm the attempt count indicator (e.g. `Attempt X of Y`) displays in the jobs view.
   * Press `[` and `]` to cycle through attempts. Confirm jobs update contextually.
