# Technical Walkthrough: `ghspector` Go TUI

This document summarizes the technical details, codebase layout, authentication fallbacks, and test executions of the `ghspector` application.

## 1. Directory & Code Layout

- **[`cmd/ghspector/main.go`](cmd/ghspector/main.go)**: The application entrypoint. Sets up flags (`--org`, `--user`), runs the credential resolution, initializes the API client, and launches the Bubble Tea loop.
- **[`internal/auth/auth.go`](internal/auth/auth.go)**: Resolves tokens from environment variables (`GH_TOKEN`, `GITHUB_TOKEN`), `~/.config/ghspector/config.yaml` (with secure permissions), or via a sub-shell call to `gh auth token`. If no token is found, displays comprehensive login setup instructions.
- **[`internal/gh/types.go`](internal/gh/types.go)**: Implements all custom Go struct payloads representing GitHub API responses for workflow runs, jobs, steps, and rate limits.
- **[`internal/gh/client.go`](internal/gh/client.go)**: Contains the GitHub REST client interface. Provides methods to query orgs, users, repos, workflows, jobs, and log content, with strict header-parsing for rate limits.
- **[`internal/tui/model.go`](internal/tui/model.go)**: Models the Bubble Tea state, views (Splash, Main Table, Jobs, Logs, Switcher), and definitions of communication messages.
- **[`internal/tui/theme.go`](internal/tui/theme.go)**: Establishes a Lipgloss adaptive color theme suited to automatic Terminal background updates (Light vs Dark mode).
- **[`internal/tui/splash.go`](internal/tui/splash.go)**: Renders the loading screen featuring an ASCII cat wearing glasses and the smallcaps title "ghspector".
- **[`internal/tui/update.go`](internal/tui/update.go)**: Implements the state machine transitions (scrolling, selecting, context-switching) and polling goroutine coordination for active items.
- **[`internal/tui/view.go`](internal/tui/view.go)**: Draws the visual UI elements, status icons (using custom Unicode blocks without emojis), tables, details, log viewports, and context options.

## 2. Dynamic Features

- **Safe Concurrency Polling & Auto-Discovery**: Periodically requests status updates for active (queued/running) items on-screen. In the main view, it polls the first page of runs for the active repositories to dynamically detect newly queued or started runs without losing runs loaded from previous pages.
- **Clock Drift & Time Sync Alignment**: Automatically extracts and parses the standard `Date` header from GitHub HTTP response payloads. Computes the time offset between the local system clock and the GitHub API server clock, ensuring running workflows and jobs calculate elapsed times accurately without getting skewed by local clock drifts.
- **Log Streaming & Follow Mode**: The step logs browser implements an automatic tail/follow scroll behavior. When viewing logs for active jobs, the viewport stays locked to the bottom as new logs arrive. Manual scroll-up suspends follow mode, and scrolling back to the bottom automatically resumes it.
- **Fixed Headers & List Windowing**: Both the runs table and jobs list render only the subset of rows that fit in the terminal height (recalculated via a rolling viewport range: `height - 11` for main runs, and `height - 14` for jobs). Includes leading header newlines and bounds vertical padding to ensure the header and footer remain strictly fixed in place, visible on all terminals, and never scroll off-screen.
- **Deterministic Priority Sorting (Queued on Top)**: Runs and jobs are sorted deterministically using `sort.SliceStable`. Items are ordered by status priority first (`queued` at the very top, followed by `in_progress` / running, and then completed), then sorted chronologically (`CreatedAt` descending), and finally fallback to their unique GitHub ID descending. This prevents random shifting during background refreshes.
- **Layout Alignment & Safe Resizing**: Very long event tags (like `pull_request_target`) are automatically shortened to prevent field overflow. The fixed header logic automatically subtracts the rate limit indicator width (explicitly labeled as `Rate Limit: X/Y reqs` with reqs metric unit) from padding calculations and intelligently hides context strings if the terminal window size is too narrow, preventing line wraps (like the wrapped rate limit "floating green A" bug).
- **Status Key Legend**: A clean status explanation legend is displayed above the footer in runs and jobs views. The legend automatically hides responsively if the terminal width is less than 70 characters.
- **XML Log Error Parsing & Formatting**: If logs fail to load due to Azure/S3 storage errors (like `BlobNotFound`), the XML response is automatically unmarshaled to extract the structured code and description. The TUI footer formats any error status under a bold red `| error: ...` prefix instead of the default subdued message style.
- **Actor Attribution**: Added an `ACTOR` column to the main workflow runs list view. This identifies the GitHub user or bot (e.g. `github-actions[bot]`) who triggered the workflow execution.
- **Unicode Status Icons**:
  - Running: Orange block (`■`)
  - Successful: Green block (`■`)
  - Failed: Red block (`■`)
  - Queued: Yellow hollow block (`□`)
  - Waiting / Action Required (Approval Needed): Purple/magenta diamond (`◆`)
- **GoReleaser configuration (`.goreleaser.yaml`)**: Setup for multi-platform Go binary packaging across Unix/macOS/Windows architectures.

## 3. Verification & Testing

- Integration tests are written in:
  - **[`internal/auth/auth_test.go`](internal/auth/auth_test.go)**: Validates fallback prioritization and error responses.
  - **[`internal/gh/client_test.go`](internal/gh/client_test.go)**: Tests the REST APIs and rate-limit parsing headers using `httptest.Server`.
  - **[`internal/tui/app_test.go`](internal/tui/app_test.go)**: Asserts TUI navigation, view updates, and cursor selections against a mock API server.

All test suites run and compile successfully:
```bash
$ go test -v ./...
ok  	ghspector/internal/auth	(cached)
ok  	ghspector/internal/gh	0.007s
ok  	ghspector/internal/tui	0.005s
```
