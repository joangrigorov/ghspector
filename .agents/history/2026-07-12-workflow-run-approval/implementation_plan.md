## Goal Description
Implement the ability to approve workflow runs and environment deployments that are stuck waiting in `ghspector`. The features will:
1. Detect when a workflow run is waiting for approval (either a fork PR workflow run with status `action_required`/conclusion `action_required` or a protected environment deployment with status `waiting`).
2. Verify if the authenticated user's access token has the necessary permission to approve the run before allowing/rendering the option.
   - For fork PR runs: check if the user has `write`, `admin`, or `maintain` role on the repository (or is the repository owner).
   - For environment deployments: fetch the pending deployments for the run and inspect the `current_user_can_approve` field.
3. Prompt the user for confirmation via an interactive modal with Y/n before performing the approval.
4. Execute the approval using the GitHub REST API and refresh the TUI view upon completion.

---

## User Review Required
No breaking changes are introduced. The approval modal design follows the existing pattern used for pull request merge and close confirmations.

---

## Open Questions
None. The GitHub REST API endpoints and payloads for workflow approvals and pending deployments have been fully verified.

---

## Proposed Changes

### `internal/gh` (GitHub Client SDK)
We will add definitions for pending deployments and collaborator permission responses, and implement the necessary GET/POST API methods.

#### [MODIFY] internal/gh/types.go
Add structures for pending deployments and permission responses:
```go
// RepoPermissionResponse represents the response from the collaborator permission endpoint.
type RepoPermissionResponse struct {
	Permission string `json:"permission"`
}

// PendingDeployment represents a pending deployment for a workflow run.
type PendingDeployment struct {
	Environment struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"environment"`
	CurrentUserCanApprove bool `json:"current_user_can_approve"`
}
```

#### [MODIFY] internal/gh/client.go
Add endpoints to fetch repository collaborator permissions, pending deployments, and perform approvals:
```go
// GetRepoPermission checks a user's permission for a repository.
func (c *Client) GetRepoPermission(ctx context.Context, owner, repo, username string) (string, error) {
	var resp RepoPermissionResponse
	path := fmt.Sprintf("/repos/%s/%s/collaborators/%s/permission", owner, repo, username)
	err := c.doRequest(ctx, "GET", path, nil, &resp)
	if err != nil {
		return "", err
	}
	return resp.Permission, nil
}

// GetPendingDeployments fetches pending deployments for a workflow run.
func (c *Client) GetPendingDeployments(ctx context.Context, owner, repo string, runID int64) ([]PendingDeployment, error) {
	var resp []PendingDeployment
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/pending_deployments", owner, repo, runID)
	err := c.doRequest(ctx, "GET", path, nil, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ApproveWorkflowRun approves a workflow run from a fork.
func (c *Client) ApproveWorkflowRun(ctx context.Context, owner, repo string, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/approve", owner, repo, runID)
	return c.doRequestWithBody(ctx, "POST", path, nil, nil, nil)
}

type environmentApprovalRequest struct {
	EnvironmentIDs []int64 `json:"environment_ids"`
	State          string  `json:"state"` // approved or rejected
	Comment        string  `json:"comment"`
}

// ApprovePendingDeployments approves pending deployments for a workflow run.
func (c *Client) ApprovePendingDeployments(ctx context.Context, owner, repo string, runID int64, envIDs []int64, comment string) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/pending_deployments", owner, repo, runID)
	body := environmentApprovalRequest{
		EnvironmentIDs: envIDs,
		State:          "approved",
		Comment:        comment,
	}
	var response any
	return c.doRequestWithBody(ctx, "POST", path, nil, body, &response)
}
```

---

### `internal/tui` (TUI Model, Key Handlers, and Views)
We will introduce cache maps and confirm state in the `Model`, handle async permission checks on highlight/refresh, intercept keys for run approval modals, and update render footers and modals.

#### [MODIFY] internal/tui/model.go
Add state fields and message types:
```go
// In Model struct:
	// Approval confirmation state
	approvalPermissions map[int64]bool // caches runID -> canApprove
	runApprovalState    int            // 0: none, 1: confirm approval

// In InitModel:
		approvalPermissions: make(map[int64]bool),
		runApprovalState:    0,

// In Message types:
type approvalPermissionLoadedMsg struct {
	runID      int64
	canApprove bool
	err        error
}

type workflowRunApprovedMsg struct {
	runID int64
	err   error
}
```

We will also add helper methods on `Model` in `model.go`:
```go
// selectedRunCanApprove returns true if the selected workflow run requires approval and the user has permission to do so.
func (m Model) selectedRunCanApprove() bool {
	var run gh.WorkflowRun
	if m.state == viewMain && m.activeTab == tabWorkflows {
		if m.selectedRunIdx >= 0 && m.selectedRunIdx < len(m.runs) {
			run = m.runs[m.selectedRunIdx]
		} else {
			return false
		}
	} else if m.state == viewJobs {
		run = m.getRun()
	} else {
		return false
	}

	if run.ID == 0 {
		return false
	}
	
	needsApproval := (run.Status == "waiting" || run.Conclusion == "action_required")
	return needsApproval && m.approvalPermissions[run.ID]
}
```

#### [MODIFY] internal/tui/update.go
1. Intercept keys at the beginning of `Update()`:
```go
	// Intercept keys for workflow run approval confirmation
	if m.runApprovalState > 0 {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch m.runApprovalState {
			case 1: // confirm approval
				switch keyMsg.String() {
				case "y", "Y":
					m.isLoading = true
					m.loadingMsg = "Approving workflow run..."
					m.runApprovalState = 0 // reset
					run := m.getRun()
					return m, m.approveWorkflowRunCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID, run.Status, run.Conclusion)
				case "n", "N", "esc":
					m.runApprovalState = 0
					return m, nil
				}
			}
		}
		return m, nil
	}
```

2. Add TUI Commands for fetching permissions and triggering approval:
```go
func (m Model) checkApprovalPermissionCmd() tea.Cmd {
	var run gh.WorkflowRun
	if m.state == viewMain && m.activeTab == tabWorkflows {
		if m.selectedRunIdx >= 0 && m.selectedRunIdx < len(m.runs) {
			run = m.runs[m.selectedRunIdx]
		} else {
			return nil
		}
	} else if m.state == viewJobs {
		run = m.getRun()
	} else {
		return nil
	}

	if run.ID == 0 {
		return nil
	}

	status := run.Status
	conclusion := run.Conclusion
	needsApproval := (status == "waiting" || conclusion == "action_required")
	if !needsApproval {
		return nil
	}

	if _, cached := m.approvalPermissions[run.ID]; cached {
		return nil
	}

	owner := run.Repository.Owner.Login
	repo := run.Repository.Name
	if owner == "" || repo == "" {
		return nil
	}

	return func() tea.Msg {
		if conclusion == "action_required" {
			// Fork PR approval: verify repo write permission
			if m.currentUser != "" && strings.EqualFold(owner, m.currentUser) {
				return approvalPermissionLoadedMsg{runID: run.ID, canApprove: true}
			}
			perm, err := m.client.GetRepoPermission(m.ctx, owner, repo, m.currentUser)
			if err != nil {
				return approvalPermissionLoadedMsg{runID: run.ID, canApprove: false, err: err}
			}
			canApprove := (perm == "admin" || perm == "write" || perm == "maintain")
			return approvalPermissionLoadedMsg{runID: run.ID, canApprove: canApprove}
		}

		if status == "waiting" {
			// Environment deployment approval: check deployment permissions
			deployments, err := m.client.GetPendingDeployments(m.ctx, owner, repo, run.ID)
			if err != nil {
				// Fallback to collaborator permission
				perm, permErr := m.client.GetRepoPermission(m.ctx, owner, repo, m.currentUser)
				if permErr == nil && (perm == "admin" || perm == "write" || perm == "maintain") {
					return approvalPermissionLoadedMsg{runID: run.ID, canApprove: true}
				}
				return approvalPermissionLoadedMsg{runID: run.ID, canApprove: false, err: err}
			}
			canApprove := false
			for _, d := range deployments {
				if d.CurrentUserCanApprove {
					canApprove = true
					break
				}
			}
			return approvalPermissionLoadedMsg{runID: run.ID, canApprove: canApprove}
		}

		return approvalPermissionLoadedMsg{runID: run.ID, canApprove: false}
	}
}

func (m Model) approveWorkflowRunCmd(owner, repo string, runID int64, status, conclusion string) tea.Cmd {
	return func() tea.Msg {
		if conclusion == "action_required" {
			err := m.client.ApproveWorkflowRun(m.ctx, owner, repo, runID)
			return workflowRunApprovedMsg{runID: runID, err: err}
		}
		if status == "waiting" {
			deployments, err := m.client.GetPendingDeployments(m.ctx, owner, repo, runID)
			if err != nil {
				return workflowRunApprovedMsg{runID: runID, err: err}
			}
			var envIDs []int64
			for _, d := range deployments {
				if d.CurrentUserCanApprove {
					envIDs = append(envIDs, d.Environment.ID)
				}
			}
			if len(envIDs) == 0 {
				for _, d := range deployments {
					envIDs = append(envIDs, d.Environment.ID)
				}
			}
			if len(envIDs) == 0 {
				return workflowRunApprovedMsg{runID: runID, err: fmt.Errorf("no pending environments found to approve")}
			}
			err = m.client.ApprovePendingDeployments(m.ctx, owner, repo, runID, envIDs, "Approved via ghspector")
			return workflowRunApprovedMsg{runID: runID, err: err}
		}
		return workflowRunApprovedMsg{runID: runID, err: fmt.Errorf("workflow run does not require approval")}
	}
}
```

3. Trigger `checkApprovalPermissionCmd()` inside `Update` on navigation/refresh:
- In `viewMain` for key movements (`j`, `k`, `up`, `down`) and actions (`r`, `x`, `m`).
- Inside `runsLoadedMsg` / `runsPolledMsg` / `jobsLoadedMsg` message handlers.

4. Handle the new messages:
- `approvalPermissionLoadedMsg`: stores the value in `m.approvalPermissions`.
- `workflowRunApprovedMsg`: sets loading state back to false, updates `m.statusMsg`, clears cache, and triggers list or job refreshes.

5. Map the `a` keyboard shortcut:
- In `viewMain` (when `activeTab == tabWorkflows`), if `m.selectedRunCanApprove()`, map `a` to `m.runApprovalState = 1`.
- In `viewJobs`, if `m.selectedRunCanApprove()`, map `a` to `m.runApprovalState = 1`.

#### [MODIFY] internal/tui/view.go
1. Render footer instructions dynamically:
   - If `m.selectedRunCanApprove()` is true, append `"a:Approve Run"` to footer shortcuts in both `renderMainView` and `renderJobsView`.
2. Add modal drawing:
```go
func (m Model) renderApprovalModal() string {
	var modalText strings.Builder
	lineStyle := lipgloss.NewStyle().Width(46)
	
	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│              APPROVE WORKFLOW RUN            │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("  Are you sure you want to approve this run?") + "│\n")
	run := m.getRun()
	runName := run.Name
	if runName == "" && run.DisplayTitle != "" {
		runName = run.DisplayTitle
	}
	if len(runName) > 40 {
		runName = runName[:37] + "..."
	}
	modalText.WriteString("│" + lineStyle.Render(fmt.Sprintf("  Run: %s", runName)) + "│\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusSuccessful.Render("[Y]")+" Yes, approve run") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusFailed.Render("[n]")+" No, cancel") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}
```
3. Overlay modal:
   - Apply `overlayModal(viewStr, m.renderApprovalModal(), m.width, m.height, 48)` in both `renderMainView` and `renderJobsView` if `m.runApprovalState > 0`.

---

## Verification Plan

### Automated Tests
We will add new tests to:
1. `internal/gh/client_test.go`:
   - `TestClient_GetRepoPermission`: Mock `/collaborators/{username}/permission` and verify values.
   - `TestClient_GetPendingDeployments`: Mock `/actions/runs/{id}/pending_deployments` and verify lists.
   - `TestClient_ApproveWorkflowRun` & `TestClient_ApprovePendingDeployments`: Verify successful approvals.
2. `internal/tui/app_test.go`:
   - `TestWorkflowApprovalFlow`: Simulates navigating to a waiting run, triggering permission check, displaying `"a:Approve Run"`, pressing `"a"`, confirming via `"y"`, triggering approval, and verifying refresh actions.

Run tests:
```bash
go test ./...
```

### Manual Verification
1. Open `ghspector`.
2. Locate a workflow run that requires approval (e.g. from a fork PR or awaiting environment approval).
3. If the token has permissions:
   - Confirm that `◆ waiting` is shown.
   - Verify `a:Approve Run` is visible in the bottom footer.
   - Press `a`. Verify the "APPROVE WORKFLOW RUN" modal overlays in the center of the terminal.
   - Press `n` or `esc` to close. Verify it closes.
   - Press `a` again, then press `y`. Verify status updates to "Approving workflow run..." and then successfully refreshes the list with updated status.
4. If the token does not have permissions:
   - Verify `a:Approve Run` is **not** visible.
   - Verify pressing `a` has no effect.
