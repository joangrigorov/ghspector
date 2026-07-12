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

### 2. TUI States and Key Interceptors
- **[`internal/tui/model.go`](internal/tui/model.go)**: Added `approvalPermissions` cache and `runApprovalState` to handle the modal overlay. Defined `approvalPermissionLoadedMsg`, `workflowRunApprovedMsg` message types, and the `selectedRunCanApprove()` helper.
- **[`internal/tui/update.go`](internal/tui/update.go)**: 
  - Added key interception for the approval confirmation modal (`y` / `Y` to confirm, `n` / `N` / `esc` to cancel).
  - Wired `checkApprovalPermissionCmd()` to trigger dynamically in the background during navigation (`up`/`down` movements in workflows list) and list reloads.
  - Handled `approvalPermissionLoadedMsg` to cache the permission state per workflow run.
  - Handled `workflowRunApprovedMsg` to perform list/job refreshes and render success status messages.
  - Mapped the `a` key binding under the main workflows view and the jobs view to open the approval confirmation modal.

### 3. Rendering and UI Overlay
- **[`internal/tui/view.go`](internal/tui/view.go)**:
  - Appended the `"a:Approve"` shortcut dynamically to the bottom footer when the selected run can be approved.
  - Implemented `renderApprovalModal()` using the existing `lipgloss` styles to display an overlay in the center of the terminal.
  - Center-placed the modal using `overlayModal` if the approval state is active.

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
ok      ghspector/internal/gh     (cached)
ok      ghspector/internal/tui    0.027s
```

### 2. Style and Quality Checks
We ran style checkers to ensure code quality:
- **`golangci-lint run`** completed with 0 issues.
