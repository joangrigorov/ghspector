package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
	"ghspector/internal/logs"
	"ghspector/internal/store"
)

// Log viewer geometry. The steps sidebar is a fixed column because step names
// are short and the log needs the room; the gap separates the two panes without
// drawing a box around the output.
const (
	logSidebarWidth = 32
	logGapWidth     = 3
	// logScrollStep is how many lines one scroll key moves.
	logScrollStep = 3
	// logHeadingRows is the height of the heading above the log: the job line
	// and the blank under it.
	logHeadingRows = 2
)

// Logs is the log viewer for one job: the steps of the job on the left, the
// output of the selected step on the right.
//
// The step segmentation lives in internal/logs, which is pure text handling.
// This route decides which step is selected, where the viewport sits, and
// whether it follows new output.
type Logs struct {
	run store.WorkflowRun
	job store.WorkflowJob
	ref store.RepoRef
	// segments holds the output of each step, keyed by the step's index in the
	// job's step list, in display form.
	segments map[int]string
	// step is the selected step, lines is its output, already split.
	step  int
	lines []string
	// offset is the first visible line, used when follow mode is off.
	offset int
	// follow pins the viewport to the newest output.
	follow  bool
	loading bool
	loaded  bool
	err     error
	sess    *session
}

// newLogs returns the Logs route for a job and the command that fetches its
// log.
func newLogs(deps Deps, run store.WorkflowRun, job store.WorkflowJob) (*Logs, tea.Cmd) {
	l := &Logs{
		run:     run,
		job:     job,
		ref:     runRepo(run),
		loading: true,
		follow:  true,
		sess:    newSession(deps.Ctx),
	}
	return l, l.load(deps, false)
}

// Title is the breadcrumb label.
func (l *Logs) Title() string { return "Logs" }

// Scope is the keymap scope for the viewer.
func (l *Logs) Scope() keymap.Scope { return keymap.Logs }

// Stop releases the requests this route has in flight.
func (l *Logs) Stop() { l.sess.Stop() }

// logsMsg carries a fetched job log.
type logsMsg struct {
	raw  string
	err  error
	poll bool
}

// load fetches the job's log.
func (l *Logs) load(deps Deps, poll bool) tea.Cmd {
	ref, jobID := l.ref, l.job.ID
	ctx := l.sess.Ctx()
	return func() tea.Msg {
		raw, err := deps.Store.JobLogs(ctx, ref, jobID)
		if err != nil {
			return logsMsg{err: err, poll: poll}
		}
		return logsMsg{raw: raw, poll: poll}
	}
}

// Update handles the messages this route owns.
func (l *Logs) Update(deps Deps, msg tea.Msg) (Route, tea.Cmd) {
	switch msg := msg.(type) {
	case logsMsg:
		return l.apply(msg)
	case pollMsg:
		return l.poll(deps)
	}
	return l, nil
}

// apply folds a fetched log into the viewer.
func (l *Logs) apply(msg logsMsg) (Route, tea.Cmd) {
	if msg.err != nil {
		l.loading = false
		if msg.poll {
			return l, nil
		}
		l.err = msg.err
		return l, statusErr(logFailure(msg.err))
	}

	first := !l.loaded
	l.loading = false
	l.loaded = true
	l.err = nil
	l.segments = logs.Segment(msg.raw, logSteps(l.job.Steps))
	if first {
		// Opening the viewer lands on the step that failed, which is the one a
		// person came to read, and follows the end of its output.
		l.step = firstFailure(l.job.Steps)
		l.follow = true
	}
	l.selectStep(l.step)
	return l, nil
}

// poll refetches the log while the job can still produce output. A completed
// job's log cannot change, so the tick costs nothing.
func (l *Logs) poll(deps Deps) (Route, tea.Cmd) {
	if l.loading || l.job.Status == "completed" {
		return l, nil
	}
	return l, l.load(deps, true)
}

// selectStep shows the output of one step.
func (l *Logs) selectStep(index int) {
	if index < 0 {
		index = 0
	}
	if index > len(l.job.Steps)-1 {
		index = len(l.job.Steps) - 1
	}
	if index < 0 {
		index = 0
	}
	// A different step is a different body of text, so the previous offset
	// means nothing: the viewport returns to the top unless follow mode is on,
	// in which case it stays pinned to the end.
	if index != l.step && !l.follow {
		l.offset = 0
	}
	l.step = index
	l.lines = splitLines(l.segments[index])
}

// Act handles a keymap action.
func (l *Logs) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionBack:
		return l, popCmd(), true
	case keymap.ActionRefresh:
		l.loading = true
		return l, tea.Batch(status("Refreshing logs..."), l.load(deps, false)), true
	case keymap.ActionNextStep:
		l.selectStep(l.step + 1)
		l.follow = true
		return l, nil, true
	case keymap.ActionPrevStep:
		l.selectStep(l.step - 1)
		l.follow = true
		return l, nil, true
	case keymap.ActionScrollUp:
		visible := l.visibleLines(deps)
		top := l.top(visible)
		l.follow = false
		l.offset = max(top-logScrollStep, 0)
		return l, nil, true
	case keymap.ActionScrollDown:
		visible := l.visibleLines(deps)
		limit := l.scrollLimit(visible)
		next := min(l.top(visible)+logScrollStep, limit)
		// Reaching the end of the log turns follow mode back on, so the newest
		// output stays on screen as it arrives.
		l.offset = next
		l.follow = next >= limit
		return l, nil, true
	case keymap.ActionOpenBrowser:
		return l, browserCmd(jobURL(l.run, l.job), "Opened the job in the browser."), true
	}
	return l, nil, false
}

// visibleLines is how many log lines fit on screen, derived from the terminal
// height the root publishes.
//
// The route cannot measure itself: it is handed a rect when it renders, and the
// viewport position has to be settled when a key arrives. The floor keeps a
// tiny terminal from producing a zero-row viewport.
func (l *Logs) visibleLines(deps Deps) int {
	height := deps.Size.Height - layout.HeaderHeight - layout.FooterHeight - logHeadingRows
	if height < 3 {
		height = 3
	}
	return height
}

// scrollLimit is the largest usable offset: the last line sits at the bottom of
// the viewport.
func (l *Logs) scrollLimit(visible int) int {
	return max(len(l.lines)-visible, 0)
}

// top is the first line the viewport shows: the end of the log in follow mode,
// otherwise the stored offset, bounded so the viewport never runs past the end.
func (l *Logs) top(visible int) int {
	if l.follow {
		return l.scrollLimit(visible)
	}
	return clampInt(l.offset, 0, l.scrollLimit(visible))
}

// View renders the steps sidebar and the log viewport.
func (l *Logs) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	head := l.heading(deps)
	body := r.H - len(head)
	if body < 1 {
		return layout.Fit(joinLines(head), r.W, r.H)
	}

	sidebarWidth := logSidebarWidth
	if sidebarWidth > r.W-20 {
		sidebarWidth = max(r.W/3, 8)
	}
	viewportWidth := max(r.W-sidebarWidth-logGapWidth, 8)

	gap := strings.Repeat(" ", logGapWidth)
	left := layout.Fit(l.stepsView(deps, sidebarWidth), sidebarWidth, body)
	right := layout.Fit(l.logView(deps, viewportWidth), viewportWidth, body)

	rows := make([]string, 0, len(head)+body)
	rows = append(rows, head...)
	rows = append(rows, joinBlocks(left, gap, right, body)...)
	return layout.Fit(joinLines(rows), r.W, r.H)
}

// heading is the identity of the job whose log is on screen.
func (l *Logs) heading(deps Deps) []string {
	theme := deps.Theme
	attempt := ""
	if l.run.RunAttempt > 1 {
		attempt = " (attempt " + strconv.Itoa(l.attempt()) + ")"
	}
	return []string{
		"  " + style(theme.Title, bold).Render("Job: "+l.job.Name+attempt),
		"",
	}
}

// attempt is the attempt the run was on when this job was opened.
func (l *Logs) attempt() int {
	if l.run.RunAttempt < 1 {
		return 1
	}
	return l.run.RunAttempt
}

// stepsView renders the step list with the selected step marked.
//
// The rows are built the way every other list in the program builds them: the
// shared indent, then the shared marker on the highlighted one. A step list
// looks like the tables it sits next to, so a highlight reads the same way
// wherever it appears.
func (l *Logs) stepsView(deps Deps, width int) string {
	theme := deps.Theme
	rows := []string{style(theme.Muted, bold).Render(layout.Truncate(tableIndent+"STEPS", width))}
	if len(l.job.Steps) == 0 {
		rows = append(rows, tableIndent+style(theme.Muted, normal).Render("(no steps reported)"))
		return joinLines(rows)
	}
	// The gutter, the state indicator and the space after it take four cells of
	// the row, the same four every other list leaves for them.
	nameWidth := max(width-4, 4)
	for i, step := range l.job.Steps {
		row := tableIndent + statusIndicator(theme, step.Status, step.Conclusion) + " " +
			layout.Truncate(step.Name, nameWidth)
		if i == l.step {
			rows = append(rows, selectedRow(theme, row, width))
			continue
		}
		rows = append(rows, row)
	}
	return joinLines(rows)
}

// logView renders the visible slice of the selected step's output.
func (l *Logs) logView(deps Deps, width int) string {
	switch {
	case l.loading && !l.loaded:
		return style(deps.Theme.Muted, normal).Render("Loading logs...")
	case l.err != nil && !l.loaded:
		return style(deps.Theme.StatusFail, normal).Render(l.err.Error())
	case len(l.lines) == 0:
		return style(deps.Theme.Muted, normal).Render("(No logs available for this step.)")
	}

	visible := l.visibleLines(deps)
	start := l.top(visible)
	end := min(start+visible, len(l.lines))

	rows := make([]string, 0, end-start)
	for _, line := range l.lines[start:end] {
		// Long lines are clipped rather than wrapped, which is what the
		// previous version's viewport did: a wrapped line makes the number of
		// rendered rows depend on the text, and every scroll offset then means
		// something different. Scroll keys move log lines, so log lines are
		// what the viewport shows.
		rows = append(rows, layout.Truncate(line, width))
	}
	return joinLines(rows)
}

// joinBlocks places two fixed-height blocks side by side.
func joinBlocks(left, gap, right string, height int) []string {
	leftRows := strings.Split(left, "\n")
	rightRows := strings.Split(right, "\n")
	rows := make([]string, 0, height)
	for i := 0; i < height; i++ {
		l, r := "", ""
		if i < len(leftRows) {
			l = leftRows[i]
		}
		if i < len(rightRows) {
			r = rightRows[i]
		}
		rows = append(rows, l+gap+r)
	}
	return rows
}

// logSteps converts the job's steps into the form the segmentation needs.
func logSteps(steps []store.JobStep) []logs.Step {
	out := make([]logs.Step, 0, len(steps))
	for _, s := range steps {
		out = append(out, logs.Step{
			Name:        s.Name,
			Conclusion:  s.Conclusion,
			StartedAt:   s.StartedAt,
			CompletedAt: s.CompletedAt,
		})
	}
	return out
}

// firstFailure is the index of the step that failed, or zero when none did.
func firstFailure(steps []store.JobStep) int {
	for i, s := range steps {
		if s.Conclusion == "failure" {
			return i
		}
	}
	return 0
}

// splitLines splits a segment into display lines.
func splitLines(segment string) []string {
	if segment == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(segment, "\n"), "\n")
	return lines
}

// logFailure explains a log fetch that failed.
//
// A running job has no log yet, and GitHub answers that with a 404 for the blob
// rather than an empty log. That is a normal state, so it is reported as one
// instead of as an error.
func logFailure(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "BlobNotFound") || strings.Contains(msg, "status 404") {
		return "Logs are not yet available for running jobs. Please wait for the job to complete."
	}
	return "Could not load logs: " + msg
}

// clampInt bounds v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
