package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/store"
)

// runsPageSize is how many runs one page of the scope-wide list asks each
// repository for. It matches the previous version's page size, so the Load More
// row appears after the same amount of scrolling.
const runsPageSize = 8

// runsTab is the Workflows tab: the scope-wide run list, its filters, and what
// is known about approving the selected run.
//
// It is a value with no identity of its own. Every method that changes it
// returns the new value, which is what makes a repaint reproducible and a test
// able to assert on state rather than on a mutation side effect.
type runsTab struct {
	runs    []store.WorkflowRun
	loading bool
	loaded  bool
	err     error
	page    int
	hasMore bool
	cursor  int
	// actor and repo are the active filters. An empty filter matches
	// everything, so the zero value means "unfiltered".
	actor string
	repo  string
	// perms records, per run ID, the answer to whether the viewer may approve
	// that run. An absent entry means the question has not been asked, which is
	// different from an answer of no, and the reason an answer carries is what
	// explains a withheld action.
	perms map[int64]approvalGate
}

// newRunsTab returns an unfiltered tab on its first page.
func newRunsTab() runsTab {
	return runsTab{page: 1, perms: map[int64]approvalGate{}}
}

// runsLoadedMsg carries one fetched page of runs. A poll reports page 0, which
// is folded into the list rather than replacing it.
type runsLoadedMsg struct {
	runs    []store.WorkflowRun
	hasMore bool
	page    int
	err     error
}

// load fetches one page of the scope-wide list for the tab's filters.
func (t runsTab) load(deps Deps, page int) tea.Cmd {
	owner := deps.Account.Owner
	q := store.RunQuery{
		Page:  store.Page{Number: page, Size: runsPageSize},
		Actor: t.actor,
		Repo:  t.repo,
	}
	ctx := deps.Ctx
	return func() tea.Msg {
		list, err := deps.Store.ScopeRuns(ctx, owner, q)
		if err != nil {
			return runsLoadedMsg{page: page, err: err}
		}
		return runsLoadedMsg{runs: list.Items, hasMore: list.HasMore, page: page}
	}
}

// start clears the list and refetches page 1. It is what opening the tab, a
// refresh, and a filter change all mean.
func (t runsTab) start(deps Deps, dropApprovals bool) (runsTab, tea.Cmd) {
	if dropApprovals {
		t.perms = map[int64]approvalGate{}
	}
	t.runs = nil
	t.hasMore = false
	t.cursor = 0
	t.page = 1
	t.loading = true
	t.err = nil
	return t, t.load(deps, 1)
}

// poll refreshes page 1 and folds the result into the list by run ID.
//
// A poll never clears what is on screen: a refresh that empties the list would
// make the table flicker once per interval, and the runs that are still there
// are not news. It is also the one fetch that carries no error to the footer —
// a poll failure is a background hiccup, and the next tick tries again.
func (t runsTab) poll(deps Deps) (runsTab, tea.Cmd) {
	if t.loading {
		return t, nil
	}
	return t, t.load(deps, 0)
}

// update handles the messages this tab owns.
func (t runsTab) update(deps Deps, msg tea.Msg) (runsTab, tea.Cmd) {
	switch msg := msg.(type) {
	case runsLoadedMsg:
		return t.apply(deps, msg)
	case FilterMsg:
		return t.acceptFilter(deps, msg.Kind, msg.Value)
	case approvalMsg:
		t.perms = withAnswer(t.perms, msg.runID, msg.gate)
		return t, nil
	case approveDoneMsg:
		if msg.err != nil {
			return t, statusErr("Could not approve the run: " + msg.err.Error())
		}
		return t, tea.Batch(status("Run approved."), t.load(deps, 0))
	}
	return t, nil
}

// apply folds a fetch result into the tab.
func (t runsTab) apply(deps Deps, msg runsLoadedMsg) (runsTab, tea.Cmd) {
	if msg.err != nil {
		t.loading = false
		t.err = msg.err
		if msg.page == 0 {
			return t, nil
		}
		return t, statusErr("Could not load workflow runs: " + msg.err.Error())
	}

	t.loading = false
	t.loaded = true
	t.err = nil

	switch msg.page {
	case 0:
		// A poll folds its result in rather than replacing the list.
		t.runs = store.MergeRuns(t.runs, msg.runs)
	case 1:
		t.runs = msg.runs
		t.hasMore = msg.hasMore
		t.cursor = 0
	default:
		t.runs = store.MergeRuns(t.runs, msg.runs)
		t.hasMore = msg.hasMore
	}
	t.clampCursor()
	return t, t.checkSelected(deps)
}

// withAnswer records an approval answer. The map is copied rather than written
// through, so a value that was handed out earlier is not changed underneath its
// holder.
func withAnswer(perms map[int64]approvalGate, runID int64, gate approvalGate) map[int64]approvalGate {
	next := make(map[int64]approvalGate, len(perms)+1)
	for id, answer := range perms {
		next[id] = answer
	}
	next[runID] = gate
	return next
}

// selectable is the number of rows the cursor can rest on: the runs, plus the
// Load More row while another page exists.
func (t runsTab) selectable() int {
	n := len(t.runs)
	if t.hasMore {
		n++
	}
	return n
}

// clampCursor keeps the selection inside the selectable rows.
func (t *runsTab) clampCursor() {
	if t.cursor > t.selectable()-1 {
		t.cursor = t.selectable() - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// selected returns the highlighted run. The Load More row is not a run, so it
// reports false.
func (t runsTab) selected() (store.WorkflowRun, bool) {
	if t.cursor < 0 || t.cursor >= len(t.runs) {
		return store.WorkflowRun{}, false
	}
	return t.runs[t.cursor], true
}

// onLoadMore reports whether the Load More row is highlighted.
func (t runsTab) onLoadMore() bool {
	return t.hasMore && t.cursor == len(t.runs)
}

// canApprove reports whether the viewer may approve a run: it has to be waiting
// for approval, and the permission check has to have come back positive.
func (t runsTab) canApprove(run store.WorkflowRun) bool {
	return needsApproval(run) && t.perms[run.ID].CanApprove
}

// checkSelected asks whether the viewer may approve the selected run, unless
// the answer is already in hand. The answer arrives as an approvalMsg.
func (t runsTab) checkSelected(deps Deps) tea.Cmd {
	run, ok := t.selected()
	if !ok || !needsApproval(run) {
		return nil
	}
	if _, asked := t.perms[run.ID]; asked {
		return nil
	}
	return checkApproval(deps, run)
}

// act handles a keymap action. It reports whether the tab consumed it.
func (t runsTab) act(deps Deps, action keymap.Action) (runsTab, tea.Cmd, bool) {
	switch action {
	case keymap.ActionDown:
		return t.move(deps, 1)
	case keymap.ActionUp:
		return t.move(deps, -1)
	case keymap.ActionOpen:
		return t.open(deps)
	case keymap.ActionRefresh:
		next, cmd := t.start(deps, false)
		return next, tea.Batch(status("Refreshing workflow runs..."), cmd), true
	case keymap.ActionFilterType:
		return t, pushOverlay(filterMenu(deps, t.actor)), true
	case keymap.ActionFilterMine:
		return t.filterByMine(deps)
	case keymap.ActionClearFilters:
		return t.clearFilters(deps)
	case keymap.ActionApprove:
		run, ok := t.selected()
		if !ok || !t.canApprove(run) {
			// The action is offered only for a run the viewer may approve, so
			// this key does nothing otherwise (spec §4.1).
			return t, nil, true
		}
		return t, pushOverlay(approveOverlay(deps, run)), true
	case keymap.ActionOpenBrowser:
		run, ok := t.selected()
		if !ok {
			return t, nil, true
		}
		return t, browserCmd(runURL(run), "Opened the run in the browser."), true
	}
	return t, nil, false
}

// move shifts the selection by delta rows and asks about the run that lands
// under the cursor.
func (t runsTab) move(deps Deps, delta int) (runsTab, tea.Cmd, bool) {
	if t.selectable() == 0 {
		return t, nil, true
	}
	next := t.cursor + delta
	if next < 0 {
		next = 0
	}
	if last := t.selectable() - 1; next > last {
		next = last
	}
	if next == t.cursor {
		return t, nil, true
	}
	t.cursor = next
	return t, t.checkSelected(deps), true
}

// open loads the next page from the Load More row, and opens the Jobs route
// from a run.
func (t runsTab) open(deps Deps) (runsTab, tea.Cmd, bool) {
	if t.onLoadMore() {
		if t.loading {
			return t, nil, true
		}
		t.page++
		t.loading = true
		return t, t.load(deps, t.page), true
	}
	run, ok := t.selected()
	if !ok {
		return t, nil, true
	}
	jobs, cmd := newJobs(deps, run)
	return t, pushRouteThen(jobs, cmd), true
}

// filterByMine toggles the actor filter between the signed-in login and no
// filter, which is what the key means: show my runs, or stop showing only mine.
func (t runsTab) filterByMine(deps Deps) (runsTab, tea.Cmd, bool) {
	if t.actor == deps.Account.Login {
		t.actor = ""
	} else {
		t.actor = deps.Account.Login
	}
	next, cmd := t.start(deps, true)
	return next, tea.Batch(status("Filter: "+displayFilter(next.actor)), cmd), true
}

// clearFilters drops both filters and refetches.
func (t runsTab) clearFilters(deps Deps) (runsTab, tea.Cmd, bool) {
	t.actor, t.repo = "", ""
	next, cmd := t.start(deps, true)
	return next, tea.Batch(status("Filters cleared."), cmd), true
}

// acceptFilter applies a filter chosen in an overlay and refetches.
func (t runsTab) acceptFilter(deps Deps, kind FilterKind, value string) (runsTab, tea.Cmd) {
	switch kind {
	case FilterActor:
		t.actor = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	case FilterRepo:
		t.repo = value
	default:
		return t, nil
	}
	next, cmd := t.start(deps, true)
	return next, tea.Batch(status("Filter: "+displayFilter(next.actor)+" "+displayFilter(next.repo)), cmd)
}

// filterLine states the active filters, so a narrowed list is never mistaken
// for a context that has no runs in it.
func (t runsTab) filterLine(deps Deps) string {
	parts := make([]string, 0, 2)
	if t.actor != "" {
		parts = append(parts, "actor @"+t.actor)
	}
	if t.repo != "" {
		parts = append(parts, "repo "+t.repo)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + style(deps.Theme.StatusWarn, normal).Render("Filter: "+strings.Join(parts, ", ")+" (x clears)")
}

// runColumns allocates the run table's columns for a row of width cells.
//
// The workflow name absorbs the slack, since it is the column a person reads.
// The event and actor columns are dropped before the table is squeezed, because
// a clipped column reads as a rendering fault.
func runColumns(width int) []tableColumn {
	return fitTable([]tableColumn{
		{Header: "ST", Column: layout.Column{Width: 3, Min: 3}},
		{Header: "REPOSITORY", Column: layout.Column{Width: 16, Min: 10}},
		{Header: "WORKFLOW RUN", Column: layout.Column{Width: 22, Min: 14, Flex: true}},
		{Header: "EVENT", Column: layout.Column{Width: 14}},
		{Header: "ACTOR", Column: layout.Column{Width: 16}},
		{Header: "DURATION", Column: layout.Column{Width: 11, Min: 9}},
	}, width)
}

// view renders the run table inside r.
func (t runsTab) view(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	cols := runColumns(r.W)

	lines := make([]string, 0, r.H)
	if f := t.filterLine(deps); f != "" {
		lines = append(lines, f, "")
	}
	lines = append(lines, style(deps.Theme.Muted, bold).Render(tableHeaderRow(cols)))

	if len(t.runs) == 0 {
		lines = append(lines, "", "  "+style(deps.Theme.Muted, normal).Render(t.message()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	visible := r.H - len(lines)
	if visible < 1 {
		return layout.Fit(joinLines(lines), r.W, r.H)
	}
	total := t.selectable()
	start, end := layout.Window(total, t.cursor, visible)
	for i := start; i < end; i++ {
		if i >= len(t.runs) {
			lines = append(lines, t.loadMoreRow(deps, r.W, i == t.cursor))
			continue
		}
		lines = append(lines, t.runRow(deps, t.runs[i], cols, r.W, i == t.cursor))
	}
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// message describes the empty table: still loading, failed, or genuinely empty.
func (t runsTab) message() string {
	switch {
	case t.loading:
		return "Loading workflow runs..."
	case t.err != nil:
		return "Could not load workflow runs: " + t.err.Error()
	default:
		return "No recent workflow runs found."
	}
}

// runRow renders one run.
func (t runsTab) runRow(deps Deps, run store.WorkflowRun, cols []tableColumn, width int, selected bool) string {
	row := tableRow(cols, []string{
		statusIndicator(deps.Theme, run.Status, run.Conclusion),
		run.Repository.Name,
		runName(run),
		run.Event,
		actorLogin(run),
		runDuration(deps.clock().Now(), run),
	})
	if selected {
		return selectedRow(deps.Theme, row, width)
	}
	return row
}

// loadMoreRow renders the sentinel that fetches the next page.
func (t runsTab) loadMoreRow(deps Deps, width int, selected bool) string {
	label := "  [ Load More Workflow Runs... ]"
	if t.loading {
		label = "  [ Loading More Workflow Runs... ]"
	}
	if selected {
		return selectedRow(deps.Theme, label, width)
	}
	return style(deps.Theme.Muted, normal).Render(label)
}

// statusIndicator is the one-cell state marker in front of a run or job row,
// coloured by how the item ended.
func statusIndicator(theme Theme, status, conclusion string) string {
	if status == "waiting" || conclusion == "action_required" {
		return style(theme.StatusWarn, normal).Render("◆")
	}
	switch status {
	case "queued":
		return style(theme.Muted, normal).Render("□")
	case "in_progress":
		return style(theme.Accent, normal).Render("■")
	}
	switch conclusion {
	case "success":
		return style(theme.StatusOK, normal).Render("■")
	case "failure", "timed_out", "startup_failure":
		return style(theme.StatusFail, normal).Render("■")
	}
	return style(theme.Muted, normal).Render("■")
}

// actorLogin is the login that started a run, or a placeholder when the API did
// not report one.
func actorLogin(run store.WorkflowRun) string {
	if run.Actor != nil && run.Actor.Login != "" {
		return run.Actor.Login
	}
	return "unknown"
}

// runDuration is how long a run took, or has been running for so far.
func runDuration(now time.Time, run store.WorkflowRun) string {
	switch run.Status {
	case "in_progress":
		if run.CreatedAt.IsZero() {
			return "running"
		}
		return formatDuration(now.Sub(run.CreatedAt))
	case "queued":
		return "queued"
	}
	if run.UpdatedAt.IsZero() || run.CreatedAt.IsZero() {
		return "—"
	}
	return formatDuration(run.UpdatedAt.Sub(run.CreatedAt))
}

// formatDuration renders an elapsed time compactly: 4s, 2m14s, 1h02m07s.
//
// The seconds field is always padded, because a duration column that changes
// width every second makes the table jump.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < 0 {
		d = 0
	}
	hours := int(d / time.Hour)
	minutes := int(d % time.Hour / time.Minute)
	seconds := int(d % time.Minute / time.Second)

	switch {
	case hours > 0:
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}
