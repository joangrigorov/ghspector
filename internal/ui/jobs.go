package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/store"
)

// jobsPageSize is how many jobs of one attempt the list asks for. A run has a
// handful of jobs; the page exists so the request is bounded rather than
// assumed small.
const jobsPageSize = 30

// Jobs is the job list of one workflow run, on one attempt of it.
//
// It is a route rather than a field of Main because it is a screen of its own:
// it has its own selection, its own fetch, and its own lifetime. The session it
// holds is cancelled by the root when the route is popped, so a fetch for a
// screen nobody is looking at is abandoned instead of landing in state that has
// been discarded.
type Jobs struct {
	run     store.WorkflowRun
	ref     store.RepoRef
	attempt int
	jobs    []store.WorkflowJob
	loading bool
	err     error
	cursor  int
	// target is a job name to select once the list arrives. A pull request
	// check that resolves to a run names the job it came from, and the person
	// who followed it meant that job rather than the first one.
	target string
	// perms records whether the viewer may approve this run, and why not when
	// they may not.
	perms map[int64]approvalGate
	sess  *session
}

// newJobs returns the Jobs route for a run, on the attempt the run reports, and
// the commands that fetch its jobs and ask about approving it.
//
// The attempt is kept exactly as the run reports it. Zero means the run did not
// report one, which the API reads as "the latest attempt"; the display treats it
// as the first.
func newJobs(deps Deps, run store.WorkflowRun) (*Jobs, tea.Cmd) {
	return newJobsTargeting(deps, run, "")
}

// newJobsTargeting is newJobs with a job name to select when the list loads.
func newJobsTargeting(deps Deps, run store.WorkflowRun, target string) (*Jobs, tea.Cmd) {
	j := &Jobs{
		run:     run,
		ref:     runRepo(run),
		attempt: run.RunAttempt,
		target:  target,
		loading: true,
		perms:   map[int64]approvalGate{},
		sess:    newSession(deps.Ctx),
	}
	return j, tea.Batch(j.load(deps, false), checkApproval(deps, run))
}

// newJobsAt returns the Jobs route for a run ID whose run is not in hand, which
// is what a pull request check with a run ID but no matching run in the list
// produces. The run itself is fetched for the heading before the jobs are.
func newJobsAt(deps Deps, ref store.RepoRef, runID int64, attempt int, target string) (*Jobs, tea.Cmd) {
	j := &Jobs{
		ref:     ref,
		attempt: attempt,
		target:  target,
		loading: true,
		perms:   map[int64]approvalGate{},
		sess:    newSession(deps.Ctx),
	}
	j.run = store.WorkflowRun{
		ID:         runID,
		RunAttempt: attempt,
		Repository: store.Repository{
			Name:     ref.Repo,
			FullName: ref.String(),
			Owner:    &store.User{Login: ref.Owner},
		},
	}
	return j, tea.Batch(j.loadRun(deps), j.load(deps, false))
}

// runMsg carries a run fetched for a route that was opened from its ID alone.
type runMsg struct {
	run store.WorkflowRun
	err error
}

// loadRun fetches the run behind this route's ID.
func (j *Jobs) loadRun(deps Deps) tea.Cmd {
	ref, runID := j.ref, j.run.ID
	ctx := j.sess.Ctx()
	return func() tea.Msg {
		run, err := deps.Store.WorkflowRun(ctx, ref, runID)
		if err != nil {
			return runMsg{err: err}
		}
		return runMsg{run: run}
	}
}

// Title is the breadcrumb label.
func (j *Jobs) Title() string { return "Jobs" }

// Scope is the keymap scope for the job list.
func (j *Jobs) Scope() keymap.Scope { return keymap.Jobs }

// Stop releases the requests this route has in flight. The root calls it as the
// route leaves the stack.
func (j *Jobs) Stop() { j.sess.Stop() }

// Loading reports a fetch this route is waiting on.
func (j *Jobs) Loading() bool { return j.loading }

// Available narrows the footer: approve is offered only when the viewer may
// approve this run.
func (j *Jobs) Available(action keymap.Action) bool {
	if action != keymap.ActionApprove {
		return true
	}
	return j.canApprove()
}

// jobsMsg carries the jobs of one attempt. A poll result is merged rather than
// replacing the list, and its failure is not reported: a background refresh
// that fails is not news for the person reading the screen.
type jobsMsg struct {
	jobs    []store.WorkflowJob
	attempt int
	err     error
	poll    bool
}

// load fetches the job list for the current attempt.
func (j *Jobs) load(deps Deps, poll bool) tea.Cmd {
	ref, runID, attempt := j.ref, j.run.ID, j.attempt
	ctx := j.sess.Ctx()
	return func() tea.Msg {
		list, err := deps.Store.Jobs(ctx, ref, runID, attempt, store.Page{Number: 1, Size: jobsPageSize})
		if err != nil {
			return jobsMsg{attempt: attempt, err: err, poll: poll}
		}
		return jobsMsg{jobs: list.Items, attempt: attempt, poll: poll}
	}
}

// Update handles the messages this route owns.
func (j *Jobs) Update(deps Deps, msg tea.Msg) (Route, tea.Cmd) {
	switch msg := msg.(type) {
	case jobsMsg:
		return j.apply(msg)
	case runMsg:
		if msg.err != nil {
			return j, nil
		}
		j.run = msg.run
		j.ref = runRepo(msg.run)
		return j, checkApproval(deps, msg.run)
	case approvalMsg:
		j.perms = withAnswer(j.perms, msg.runID, msg.gate)
		return j, nil
	case approveDoneMsg:
		if msg.err != nil {
			return j, statusErr("Could not approve the run: " + msg.err.Error())
		}
		j.loading = true
		return j, tea.Batch(status("Run approved."), j.load(deps, false))
	case pollMsg:
		return j.poll(deps)
	}
	return j, nil
}

// apply folds a fetch result into the route.
func (j *Jobs) apply(msg jobsMsg) (Route, tea.Cmd) {
	// A reply for an attempt the user has already left is dropped rather than
	// shown: the list on screen belongs to the attempt that was asked for last.
	if msg.attempt != j.attempt {
		return j, nil
	}
	if msg.err != nil {
		j.loading = false
		if msg.poll {
			return j, nil
		}
		j.err = msg.err
		return j, statusErr("Could not load jobs: " + msg.err.Error())
	}

	selected := j.selectedID()
	j.loading = false
	j.err = nil
	j.jobs = msg.jobs

	if j.target != "" && selected == 0 {
		// The first arrival aims the cursor at the job the check named, so the
		// route opens on the job the user came for.
		j.cursor = indexOfJob(j.jobs, j.target)
		j.clampCursor()
		return j, nil
	}

	// A poll replaces the list in place, so the cursor follows the job it was
	// on rather than the row number it was on.
	j.cursor = 0
	for i, job := range j.jobs {
		if job.ID == selected {
			j.cursor = i
			break
		}
	}
	j.clampCursor()
	return j, nil
}

// indexOfJob finds the job a check name refers to, by an exact name match first
// and then by a case-insensitive part match, which is how a check named after
// its workflow meets a job named after its step.
func indexOfJob(jobs []store.WorkflowJob, target string) int {
	if target == "" {
		return 0
	}
	want := strings.ToLower(target)
	for i, job := range jobs {
		if job.Name == target {
			return i
		}
	}
	for i, job := range jobs {
		name := strings.ToLower(job.Name)
		if name == want || strings.Contains(name, want) || strings.Contains(want, name) {
			return i
		}
	}
	return 0
}

// poll refreshes the job list while any job can still change state.
func (j *Jobs) poll(deps Deps) (Route, tea.Cmd) {
	if j.loading || !j.hasActive() {
		return j, nil
	}
	return j, j.load(deps, true)
}

// hasActive reports whether any job is still queued or running.
func (j *Jobs) hasActive() bool {
	for _, job := range j.jobs {
		if job.Status == "queued" || job.Status == "in_progress" {
			return true
		}
	}
	return false
}

// selected returns the highlighted job.
func (j *Jobs) selected() (store.WorkflowJob, bool) {
	if j.cursor < 0 || j.cursor >= len(j.jobs) {
		return store.WorkflowJob{}, false
	}
	return j.jobs[j.cursor], true
}

// selectedID is the highlighted job's ID, or zero when nothing is selected.
func (j *Jobs) selectedID() int64 {
	job, ok := j.selected()
	if !ok {
		return 0
	}
	return job.ID
}

// clampCursor keeps the selection inside the list.
func (j *Jobs) clampCursor() {
	if j.cursor > len(j.jobs)-1 {
		j.cursor = len(j.jobs) - 1
	}
	if j.cursor < 0 {
		j.cursor = 0
	}
}

// canApprove reports whether the viewer may approve this run.
func (j *Jobs) canApprove() bool {
	return needsApproval(j.run) && j.perms[j.run.ID].CanApprove
}

// Act handles a keymap action.
func (j *Jobs) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionDown:
		if j.cursor < len(j.jobs)-1 {
			j.cursor++
		}
		return j, nil, true
	case keymap.ActionUp:
		if j.cursor > 0 {
			j.cursor--
		}
		return j, nil, true
	case keymap.ActionOpen:
		return j.openLogs(deps)
	case keymap.ActionRefresh:
		j.loading = true
		return j, tea.Batch(status("Refreshing jobs..."), j.load(deps, false)), true
	case keymap.ActionBack:
		return j, popCmd(), true
	case keymap.ActionApprove:
		if !j.canApprove() {
			return j, nil, true
		}
		return j, pushOverlay(approveOverlay(deps, j.run)), true
	case keymap.ActionOpenBrowser:
		job, ok := j.selected()
		if !ok {
			return j, nil, true
		}
		return j, browserCmd(jobURL(j.run, job), "Opened the job in the browser."), true
	case keymap.ActionOpenRunBrowser:
		return j, browserCmd(runURL(j.run), "Opened the run in the browser."), true
	case keymap.ActionNextAttempt:
		return j.switchAttempt(deps, 1)
	case keymap.ActionPrevAttempt:
		return j.switchAttempt(deps, -1)
	}
	return j, nil, false
}

// openLogs opens the log viewer for the selected job. A job that is still
// running has no log yet, so the key explains that instead of opening a screen
// that would show an error.
func (j *Jobs) openLogs(deps Deps) (Route, tea.Cmd, bool) {
	job, ok := j.selected()
	if !ok {
		return j, nil, true
	}
	if job.Status == "in_progress" || job.Status == "queued" {
		return j, status("Logs are not yet available for running jobs. Please wait for completion."), true
	}
	logs, cmd := newLogs(deps, j.run, job)
	return j, pushRouteThen(logs, cmd), true
}

// switchAttempt moves to another attempt of the run. A different attempt is a
// different set of jobs, so the list and the selection are reset.
//
// The bounds match the previous version's: the lowest attempt is the first, and
// the highest is the one the run reports, so an attempt that does not exist is
// never requested.
func (j *Jobs) switchAttempt(deps Deps, delta int) (Route, tea.Cmd, bool) {
	next := j.attempt + delta
	if next < 1 || next > j.maxAttempt() {
		return j, nil, true
	}
	j.attempt = next
	j.jobs = nil
	j.cursor = 0
	j.loading = true
	label := fmt.Sprintf("Loading attempt %d of %d...", next, j.maxAttempt())
	return j, tea.Batch(status(label), j.load(deps, false)), true
}

// maxAttempt is the highest attempt the run reports. A run that does not report
// one is treated as having a single attempt.
func (j *Jobs) maxAttempt() int {
	if j.run.RunAttempt < 1 {
		return 1
	}
	return j.run.RunAttempt
}

// View renders the run's heading and its job table.
func (j *Jobs) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	lines := j.heading(deps, r.W)

	if needsApproval(j.run) {
		lines = append(lines, "", approvalBanner(deps, j.run, j.canApprove()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	cols := jobColumns(r.W)
	lines = append(lines, style(deps.Theme.Muted, bold).Render(tableHeaderRow(cols)))

	if len(j.jobs) == 0 {
		lines = append(lines, "", "  "+style(deps.Theme.Muted, normal).Render(j.message()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	visible := r.H - len(lines)
	if visible < 1 {
		return layout.Fit(joinLines(lines), r.W, r.H)
	}
	start, end := layout.Window(len(j.jobs), j.cursor, visible)
	for i := start; i < end; i++ {
		lines = append(lines, j.jobRow(deps, j.jobs[i], cols, r.W, i == j.cursor))
	}
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// heading is the run's identity: what it is, where it ran, and which attempt is
// on screen.
func (j *Jobs) heading(deps Deps, width int) []string {
	theme := deps.Theme
	title := style(theme.Title, bold).Render("Workflow: " + runName(j.run))

	sha := j.run.HeadSHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	if sha == "" {
		sha = "unknown"
	}
	repo := j.run.Repository.FullName
	if repo == "" {
		repo = j.ref.String()
	}
	branch := j.run.HeadBranch
	if branch == "" {
		branch = "unknown"
	}
	detail := fmt.Sprintf("Repo: %s | Branch: %s | SHA: %s", repo, branch, sha)
	if j.maxAttempt() > 1 {
		detail += fmt.Sprintf(" | Attempt %d of %d ([ / ] switch)", j.attempt, j.maxAttempt())
	}
	return []string{
		"  " + title,
		"  " + style(theme.Muted, normal).Render(layout.Truncate(detail, width-4)),
		"  " + style(theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-4, 1))),
	}
}

// message describes the empty table.
func (j *Jobs) message() string {
	switch {
	case j.loading && j.attempt > 0:
		return fmt.Sprintf("Loading jobs (attempt %d)...", j.attempt)
	case j.loading:
		return "Loading jobs..."
	case j.err != nil:
		return "Could not load jobs: " + j.err.Error()
	default:
		return "No jobs found for this workflow run."
	}
}

// jobRow renders one job.
func (j *Jobs) jobRow(deps Deps, job store.WorkflowJob, cols []tableColumn, width int, selected bool) string {
	completed := 0
	for _, step := range job.Steps {
		if step.Status == "completed" {
			completed++
		}
	}
	started := "N/A"
	if !job.StartedAt.IsZero() {
		started = job.StartedAt.Format("15:04:05")
	}
	row := tableRow(cols, []string{
		statusIndicator(deps.Theme, job.Status, job.Conclusion),
		job.Name,
		fmt.Sprintf("%d/%d", completed, len(job.Steps)),
		started,
		jobDuration(deps, job),
	})
	if selected {
		return selectedRow(deps.Theme, row, width)
	}
	return row
}

// jobDuration is how long a job took, or has been running for so far.
func jobDuration(deps Deps, job store.WorkflowJob) string {
	switch job.Status {
	case "queued":
		return "queued"
	case "in_progress":
		if job.StartedAt.IsZero() {
			return "running"
		}
		return formatDuration(deps.clock().Now().Sub(job.StartedAt))
	}
	if job.StartedAt.IsZero() || job.CompletedAt.IsZero() {
		return "N/A"
	}
	return formatDuration(job.CompletedAt.Sub(job.StartedAt))
}

// jobColumns allocates the job table's columns for a row of width cells.
func jobColumns(width int) []tableColumn {
	return fitTable([]tableColumn{
		{Header: "ST", Column: layout.Column{Width: 3, Min: 3}},
		{Header: "JOB NAME", Column: layout.Column{Width: 24, Min: 14, Flex: true}},
		{Header: "STEPS", Column: layout.Column{Width: 7, Min: 5}},
		{Header: "STARTED", Column: layout.Column{Width: 9, Min: 8}},
		{Header: "DURATION", Column: layout.Column{Width: 10, Min: 8}},
	}, width)
}

// approvalBanner explains why a run is not running and what the viewer can do
// about it. It is drawn as a bordered box so it reads as an interruption rather
// than as another table row.
//
// The box sizes itself to the paragraph, and the caller's frame clips it on a
// narrow terminal, so a banner never draws outside the body.
func approvalBanner(deps Deps, run store.WorkflowRun, canApprove bool) string {
	theme := deps.Theme

	lines := []string{style(theme.StatusWarn, bold).Render("Awaiting approval"), ""}
	switch {
	case run.Conclusion == "action_required" && isForkRun(run):
		lines = append(lines,
			"This run was triggered by a pull request from a fork and",
			"needs a maintainer's approval before it will start.")
	case run.Conclusion == "action_required":
		lines = append(lines,
			"This run was triggered by a local pull request and needs",
			"approval to execute (bot trigger or policy).")
	default:
		lines = append(lines,
			"This run has passed its initial checks and is waiting for",
			"approval to deploy to a protected environment.")
	}
	lines = append(lines, "")

	switch {
	case canApprove:
		lines = append(lines, "Press a to approve it now.")
	case run.Conclusion == "action_required" && !isForkRun(run):
		lines = append(lines,
			style(theme.StatusFail, normal).Render("GitHub does not allow this run to be approved through the API."),
			"Approve it on GitHub with w.")
	default:
		lines = append(lines, style(theme.StatusFail, normal).Render("Your token cannot approve this run."))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 2)
	if theme.StatusWarn != nil {
		box = box.BorderForeground(theme.StatusWarn)
	}
	// The box is indented to the same gutter as the heading above it.
	return "  " + strings.ReplaceAll(box.Render(strings.Join(lines, "\n")), "\n", "\n  ")
}
