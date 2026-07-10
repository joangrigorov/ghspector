# Walkthrough - TUI & API Enhancements

This walkthrough documents the implemented enhancements for **ghspector**.

## Changes Made

### 1. Clickable Links to Workflows and Jobs
* **Browser Opener Utility**: Added [browser.go](internal/tui/browser.go) with a cross-platform helper to launch the default system browser via command line execution (`xdg-open` on Linux, `open` on macOS, `rundll32` on Windows).
* **OSC 8 Hyperlinks**: Created helper `renderHyperlink` to wrap text in terminal-native OSC 8 hyperlink escape sequences. Applied this to:
  * Workflow run names in the main list.
  * Job names in the jobs list.
  * Selected workflow run title in the jobs list header.
* **Shortcut Keys**:
  * Added `w` on the main page to open the selected workflow run URL.
  * Added `w` in jobs view to open the workflow run URL, and `v` to open the selected job URL in the default browser.

### 2. Viewing Previous Attempts of a Workflow Run
* **API Extension**: Added `RunAttempt` to `WorkflowRun` struct in [types.go](internal/gh/types.go) and implemented `GetWorkflowRunAttemptJobs` in [client.go](internal/gh/client.go) to query attempt-specific jobs: `/repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}/jobs`.
* **Cycle Controls**: Added keybindings `[` and `]` in the jobs view. Pressing them updates the `selectedAttempt` state (bounded between `1` and `run.RunAttempt`) and triggers a jobs reload for that attempt.
* **UI Indicator**: Renders `Attempt X of Y (use [ / ] to switch)` in the header of the jobs list when `Y > 1`.

### 3. Actor Filtering
* **API Parameter**: Updated `GetWorkflowRuns` signature to pass the actor filter `?actor=USERNAME` parameter directly to the GitHub API.
* **Client-Side Filter**: Implemented a robust client-side filter fallback in the TUI when processing loaded and polled runs using a `matchActor` helper. This ensures correct filtering even if GitHub API returns runs triggered by other actors.
* **Bot Matcher**: The `matchActor` function matches user login names case-insensitively, and handles bot suffix matching (e.g., matching input `dependabot` with `dependabot[bot]`).
* **Quick Filter Toggle**: Pressing `m` toggles filtering by the currently authenticated user (`currentUser`).
* **Custom Name Input**: Pressing `f` opens a text input prompt at the bottom of the main runs list powered by `bubbles/textinput`. Keypresses are routed to it until the user presses `Enter` to apply or `Esc` to cancel.
* **Header Context**: Renders `(Filter: @username)` suffix in the header org/user title block when a filter is active.

### 4. Dedicated Help Screen Overlay
* **Help View State**: Added `viewHelp` state and `?` keybinding. Pressing `?` switches the TUI context to an overlay showing:
  * Fully categorized keyboard shortcut documentation (Global, Main view, Jobs view, Logs viewer).
  * Status icon indicator legend (running, success, failed, queued, waiting).
* **Responsiveness**: The status legend displays vertically stacked items on narrow screens (< 70 character width) and inline on wider ones.
* **Decluttering**: Removed the legend from the bottom of the main list and jobs list, keeping the footer keys list short and tidy.

---

## Verification & Automated Tests

All tests compile and run successfully:
```bash
go test ./...
```

### New Test Coverage
1. **Client API Tests** (in [client_test.go](internal/gh/client_test.go)):
   * `TestClient_GetWorkflowRuns_ActorFilter`: Verifies that passing an `actor` query parameter appends `?actor=USERNAME` to the HTTP request URL.
   * `TestClient_GetWorkflowRunAttemptJobs`: Mocks the attempt-specific jobs endpoint and verifies response list parsing.
2. **TUI Tests** (in [app_test.go](internal/tui/app_test.go)):
   * `TestTUI_HelpScreen`: Verifies entering/exiting the Help view and that layout elements (shortcuts, legend) render correctly.
   * `TestTUI_ActorFilter`: Verifies toggling `m` sets/clears filter Actor state, and typing/submitting filter input updates state and triggers API commands.
   * `TestTUI_AttemptNavigation`: Verifies cycle commands `[`/`]` update selected attempt and trigger target API queries.
   * `Test_MatchActor`: Verifies case-insensitive username filtering and `[bot]` suffix auto-matching.
