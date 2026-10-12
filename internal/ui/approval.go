package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/overlay"
	"github.com/joangrigorov/ghspector/internal/store"
)

// approvalGate is the answer to "may the viewer approve this run, and if not,
// why not". GitHub is the authority on the write itself; this only decides
// whether the action is offered, and it fails closed: a permission that cannot
// be established is not offered.
//
// The previous version's merge gate did the opposite — it offered merge and
// close when the scope header was missing (spec finding F1). The approval gate
// is the one that got this right, so it is the shape the merge gate is being
// moved onto.
type approvalGate struct {
	CanApprove bool
	// Reason explains a withheld action to the user. Empty when there is
	// nothing to say, which is the common case of a run that is not waiting.
	Reason string
}

// approvalMsg carries the gate for one run.
type approvalMsg struct {
	runID int64
	gate  approvalGate
}

// approveDoneMsg reports the outcome of an approval request.
type approveDoneMsg struct {
	runID int64
	err   error
}

// needsApproval reports whether a run is waiting for a person: paused on an
// environment, or refused by the API until someone approves it.
func needsApproval(run store.WorkflowRun) bool {
	return run.Status == "waiting" || run.Conclusion == "action_required"
}

// isForkRun reports whether a run came from a pull request from a fork. GitHub
// accepts API approval for those; a run from the repository's own pull request
// has to be approved in the browser.
func isForkRun(run store.WorkflowRun) bool {
	head := run.HeadRepository.FullName
	return head != "" && head != run.Repository.FullName
}

// apiCanApprove reports whether the API path can approve this run at all. It is
// false for a run from an internal pull request that needs approval, which is
// the case the approval overlay explains rather than attempts.
func apiCanApprove(run store.WorkflowRun) bool {
	return run.Conclusion != "action_required" || isForkRun(run)
}

// runRepo is the repository a run belongs to. A run carries it, so a route
// never reads it back out of shared state.
func runRepo(run store.WorkflowRun) store.RepoRef {
	owner := ""
	if run.Repository.Owner != nil {
		owner = run.Repository.Owner.Login
	}
	return store.RepoRef{Owner: owner, Repo: run.Repository.Name}
}

// runURL is the run's page, falling back to a constructed URL when the API did
// not supply one.
func runURL(run store.WorkflowRun) string {
	if run.HTMLURL != "" {
		return run.HTMLURL
	}
	if run.Repository.FullName == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/actions/runs/%d", run.Repository.FullName, run.ID)
}

// jobURL is the job's page, with the same fallback as runs.
func jobURL(run store.WorkflowRun, job store.WorkflowJob) string {
	if job.HTMLURL != "" {
		return job.HTMLURL
	}
	if run.Repository.FullName == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/actions/runs/%d/job/%d", run.Repository.FullName, run.ID, job.ID)
}

// canWrite reports whether a repository permission level allows a write action.
func canWrite(permission string) bool {
	switch permission {
	case "admin", "write", "maintain":
		return true
	}
	return false
}

// missingScopeReason explains how to grant the scopes an approval needs. It names
// the token source but never the token, and never a path.
func missingScopeReason(missing []string, tokenSource string) string {
	reason := "the token is missing the " + strings.Join(missing, ", ") + " scope"
	if len(missing) > 1 {
		reason = "the token is missing the " + strings.Join(missing, ", ") + " scopes"
	}
	if tokenSource != "" {
		reason += " (" + tokenSource + ")"
	}
	return reason + "; run: gh auth refresh -s " + strings.Join(missing, " -s ")
}

// checkApproval asks whether the viewer may approve a run, as a command so the
// answer arrives on the update loop.
func checkApproval(deps Deps, run store.WorkflowRun) tea.Cmd {
	if !needsApproval(run) {
		return nil
	}
	ref := runRepo(run)
	if !ref.Valid() {
		return nil
	}
	ctx := deps.Ctx
	return func() tea.Msg {
		return approvalMsg{runID: run.ID, gate: decideApproval(ctx, deps, run, ref)}
	}
}

// decideApproval is the gate itself.
func decideApproval(ctx context.Context, deps Deps, run store.WorkflowRun, ref store.RepoRef) approvalGate {
	// An internal run that needs approval cannot be approved through the API at
	// all, so the action is offered and leads to the browser. Nothing about the
	// token changes that, which is why it is decided first.
	if run.Conclusion == "action_required" && !isForkRun(run) {
		return approvalGate{CanApprove: true}
	}

	if missing := store.MissingScopes(deps.Store.Scopes()); len(missing) > 0 {
		return approvalGate{Reason: missingScopeReason(missing, deps.TokenSource)}
	}

	if run.Conclusion == "action_required" {
		// A run in the viewer's own account is theirs to approve.
		if strings.EqualFold(ref.Owner, deps.Account.Login) {
			return approvalGate{CanApprove: true}
		}
		permission, err := deps.Store.RepoPermission(ctx, ref, deps.Account.Login)
		if err != nil {
			return approvalGate{Reason: "cannot read your permission on " + ref.String()}
		}
		if !canWrite(permission) {
			return approvalGate{Reason: "you need write access to " + ref.String() + " to approve this run"}
		}
		return approvalGate{CanApprove: true}
	}

	// Waiting on an environment: the deployments endpoint knows whether this user
	// is the one the environment is waiting for.
	deployments, err := deps.Store.PendingDeployments(ctx, ref, run.ID)
	if err != nil {
		permission, permErr := deps.Store.RepoPermission(ctx, ref, deps.Account.Login)
		if permErr == nil && canWrite(permission) {
			return approvalGate{CanApprove: true}
		}
		return approvalGate{Reason: "cannot read the pending deployments for this run"}
	}
	for _, d := range deployments {
		if d.CurrentUserCanApprove {
			return approvalGate{CanApprove: true}
		}
	}
	return approvalGate{Reason: "no environment on this run is waiting for your approval"}
}

// approveOverlay is the confirmation for approving a run. Its body explains the
// path that applies: approving through the API, or approving on GitHub when the
// API refuses.
func approveOverlay(deps Deps, run store.WorkflowRun) overlay.Overlay {
	name := runName(run)

	if !apiCanApprove(run) {
		return overlay.Confirm{
			Scope:   keymap.ApproveRun,
			Heading: "Approve workflow run",
			Body: []string{
				"This internal run requires manual approval, but GitHub does not allow it to be approved through the API.",
				"Run: " + name,
				"Approve it on GitHub: y or w opens its approval page in the browser.",
			},
			// Both keys lead to the browser, which is the only path GitHub
			// offers for this run.
			OnYes:  tea.Batch(status("Opening the approval page in the browser."), browserCmd(runURL(run), "Opened approval page in browser.")),
			OnOpen: browserCmd(runURL(run), "Opened approval page in browser."),
		}
	}

	return overlay.Confirm{
		Scope:   keymap.ApproveRun,
		Heading: "Approve workflow run",
		Body: []string{
			"Approve this run?",
			"Run: " + name,
		},
		OnYes:  tea.Batch(status("Approving workflow run..."), approveCmd(deps, run)),
		OnOpen: browserCmd(runURL(run), "Opened approval page in browser."),
	}
}

// approveCmd approves a run: the environments it is waiting on when it is paused
// for a deployment, and the run itself otherwise.
func approveCmd(deps Deps, run store.WorkflowRun) tea.Cmd {
	ref := runRepo(run)
	ctx := deps.Ctx
	return func() tea.Msg {
		if run.Status == "waiting" {
			deployments, err := deps.Store.PendingDeployments(ctx, ref, run.ID)
			if err != nil {
				return approveDoneMsg{runID: run.ID, err: err}
			}
			envIDs := environmentIDs(deployments)
			if len(envIDs) == 0 {
				return approveDoneMsg{runID: run.ID, err: errors.New("no pending environments to approve")}
			}
			return approveDoneMsg{runID: run.ID, err: deps.Store.ApproveDeployments(ctx, ref, run.ID, envIDs, "Approved via ghspector")}
		}
		return approveDoneMsg{runID: run.ID, err: deps.Store.ApproveRun(ctx, ref, run.ID)}
	}
}

// environmentIDs picks the environments to approve: the ones waiting on this
// user, or every pending one when none names them.
func environmentIDs(deployments []store.PendingDeployment) []int64 {
	out := make([]int64, 0, len(deployments))
	for _, d := range deployments {
		if d.CurrentUserCanApprove {
			out = append(out, d.Environment.ID)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, d := range deployments {
		out = append(out, d.Environment.ID)
	}
	return out
}

// runName is the label a run is shown by. The display title is what a person
// recognises; the workflow name is the fallback.
func runName(run store.WorkflowRun) string {
	if run.DisplayTitle != "" {
		return run.DisplayTitle
	}
	if run.Name != "" {
		return run.Name
	}
	return fmt.Sprintf("run %d", run.ID)
}
