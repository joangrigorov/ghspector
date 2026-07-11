# Walkthrough: Issue Browser & Viewer Feature

This walkthrough documents the completion and verification of the read-only Issue Browser and Viewer feature implemented on the branch `feat/issue-browser`.

---

## Changes Made

### 1. API Client Wrapper (`internal/gh`)
- **`types.go`**: Defined the `Issue` struct modeling the GitHub Issues JSON payload. Added the `PullRequest` field placeholder to distinguish issues from pull requests.
- **`client.go`**: Implemented `GetIssuesWithState` to fetch issues from `/repos/{owner}/{repo}/issues` and filter out pull requests. Implemented `GetIssue` to query single issue details.

### 2. Main State & Cache (`internal/tui/model.go`)
- Added `viewIssueDetails`, `viewIssueComments`, `viewIssueFilterInput`, and `viewIssueFilterTypeSelect` view states.
- Extended the `mainTab` enum with `tabIssues` as the third tab.
- Added issues list caching, selection indices, pagination variables, filter queries, and viewport configurations.
- Added message types `issuesLoadedMsg`, `issueDetailsLoadedMsg`, and `issueCommentsLoadedMsg`.

### 3. Controller Logic & Updates (`internal/tui/update.go`)
- Updated the main `Update` function to handle the new view states, cycling through 3 tabs (`tabPRs` -> `tabWorkflows` -> `tabIssues`), and resizing viewports during terminal resize events.
- Wired keypress actions for issues state navigation, filters, reload actions, and web browser launch capabilities.
- Implemented concurrent data fetching commands for retrieving issues across repositories and pre-loading issue comments.

### 4. Layouts & Views (`internal/tui/view.go`)
- Updated the header with dynamic title page mappings ("Issues", "Issue Details", and "Issue Comments").
- Added table rendering logic for the issues list featuring state indicators (● for open, ○ for closed).
- Built double-column details layout with a Glamour-rendered markdown viewport on the left and a metadata sidebar on the right.
- Added a formatted issue comments viewer viewport with a border frame matching PR comments view.
- Added filter user input and type select overlay modals.

### 5. Verification & Unit Tests (`internal/tui/app_test.go`)
- Implemented `TestTUI_IssueViewer` testing issues loaded message updates, state transitions, and vertical navigation.
- Implemented `TestIssueStateFiltering` verifying the cycling behavior of state filters from open -> closed -> all -> open.

---

## Verification Results

### Automated Tests & Linting
Ran Go tests and `golangci-lint` check, both completing successfully:
```bash
$ go test ./...
ok      ghspector/internal/auth (cached)
ok      ghspector/internal/gh   (cached)
ok      ghspector/internal/tui  0.050s

$ golangci-lint run
# Clean run - no issues found
```

All 6 modified files have been staged and committed conventional-commit style on the branch `feat/issue-browser`.
