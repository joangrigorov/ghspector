# Walkthrough - Workflow Approval Flow

We have successfully implemented the workflow run and environment deployment approval flow in `ghspector`. The implementation detects waiting workflows, checks for user permissions, and provides a confirmation modal with `Y/n` options.

## Changes Made

### 1. GitHub API Client additions
- **[`internal/gh/types.go`](internal/gh/types.go)**: Added definitions for `RepoPermissionResponse` and `PendingDeployment` payload mappings.
- **[`internal/gh/client.go`](internal/gh/client.go)**: Implemented new endpoints:
  - `GetRepoPermission`: Checks repository access role (`admin`, `write`, `maintain`) for the current user.
  - `GetPendingDeployments`: Fetches pending environments and whether the user is authorized to approve them.
  - `ApproveWorkflowRun`: Approves waiting runs from fork pull requests.
  - `ApprovePendingDeployments`: Approves environment deployments using a list of target environments.
  - `HasRequiredScopes`: Parses the cached token scopes and validates that `repo` and `workflow` permissions are present.

### 2. TUI States and Key Interceptors
- **[`internal/tui/model.go`](internal/tui/model.go)**: Added `approvalPermissions` cache and `runApprovalState` to handle the modal overlay. Defined `approvalPermissionLoadedMsg`, `workflowRunApprovedMsg` message types, and the `selectedRunCanApprove()` helper.
- **[`internal/tui/update.go`](internal/tui/update.go)**: 
  - Added key interception for the approval confirmation modal (`y` / `Y` / `Enter` to confirm, `n` / `N` / `esc` to cancel).
  - Wired `checkApprovalPermissionCmd()` to trigger dynamically in the background during navigation. It now inspects `HasRequiredScopes()` and returns a status error message indicating what command to run (e.g. `gh auth refresh -s repo -s workflow`) if scopes are missing.
  - Handled `approvalPermissionLoadedMsg` to cache the permission state per workflow run and display scope validation warnings.
  - Handled `workflowRunApprovedMsg` to perform list/job refreshes and render success status messages.

### 3. Rendering and UI Overlay
- **[`internal/tui/view.go`](internal/tui/view.go)**:
  - Appended the `"a:Approve"` shortcut dynamically to the bottom footer when the selected run can be approved.
  - Implemented a custom stylized Lipgloss box banner inside the jobs view (`viewJobs`) that warns when a run is awaiting approval and prompts the user on how to approve it.
  - Implemented `renderApprovalModal()` using the existing `lipgloss` styles to display an overlay in the center of the terminal.
  - Updated `renderFooter` to properly display error status messages (in red, prefixed with `"error:"`) and success/info messages (in green).
  - Updated `renderHelpView` to display scope details for the `a` keyboard shortcut.

### 4. Setup & Documentation Updates
- **[`internal/auth/auth.go`](internal/auth/auth.go)**: Updated login and scope refresh command suggestions to include both `repo` and `workflow` scopes.
- **[`README.md`](README.md)**: Updated configuration, merge, and authentication scope instructions to list both `repo` and `workflow` scopes.

---

## Verification Results

### 1. Automated Tests
We added coverage for both client and TUI updates:
- **`TestClient_GetRepoPermission`**, **`TestClient_GetPendingDeployments`**, **`TestClient_ApproveWorkflowRun`**, and **`TestClient_ApprovePendingDeployments`** in `internal/gh/client_test.go` were added and validated against HTTP mocks.
- **`TestWorkflowApprovalFlow`** in `internal/tui/app_test.go` was added to verify state transitions and event logic in the TUI loop.

Command run:
```bash
go test ./...
```
Result:
```
ok      ghspector/internal/auth   (cached)
ok      ghspector/internal/gh     0.009s
ok      ghspector/internal/tui    0.025s
```

### 2. Style and Quality Checks
We ran style checkers to ensure code quality:
- **`golangci-lint run`** completed with 0 issues.
- **`govulncheck ./...`** completed with no vulnerability findings.
