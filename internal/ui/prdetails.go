package ui

import (
	"context"
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
	"ghspector/internal/store"
)

// prTab is one of the five sub-tabs of the pull request details view.
type prTab int

const (
	prTabInfo prTab = iota
	prTabCommits
	prTabFiles
	prTabChecks
	prTabComments
)

// prTabCount is the number of sub-tabs. Kept explicit so the strip reads
// clearly.
const prTabCount = 5

// prTabMeta ties each sub-tab to its label and the keymap scope that is active
// while it is on screen. The scope is what makes the footer describe the keys
// that work on the tab the user is looking at: a commit list has no merge key,
// and a description has no file list.
var prTabMeta = [prTabCount]struct {
	Name  string
	Scope keymap.Scope
}{
	prTabInfo:     {Name: "Info", Scope: keymap.PRDetails},
	prTabCommits:  {Name: "Commits", Scope: keymap.PRCommits},
	prTabFiles:    {Name: "Files", Scope: keymap.PRDiff},
	prTabChecks:   {Name: "Checks", Scope: keymap.PRDetails},
	prTabComments: {Name: "Comments", Scope: keymap.PRComments},
}

// prSection names a piece of the pull request data a sub-tab needs, so one
// message type and one loaded-set cover every fetch.
type prSection int

const (
	prSectionDetail prSection = iota
	prSectionRuns
	prSectionChecks
	prSectionCommits
	prSectionFiles
	prSectionComments
)

// prDataMsg carries one fetched section of the pull request data.
type prDataMsg struct {
	section  prSection
	pr       store.PullRequest
	runs     []store.WorkflowRun
	checks   []store.CheckRun
	commits  []store.RepositoryCommit
	files    []store.CommitFile
	comments []store.IssueComment
	err      error
}

// prWriteGate is the answer to "may the viewer merge or close this pull
// request, and if not, why not". GitHub is the authority on the write itself;
// this only decides whether the action is offered, and it fails closed: a
// permission that cannot be established is not offered.
type prWriteGate struct {
	CanWrite bool
	// Reason explains a withheld action. Empty means either "not asked yet" or
	// "allowed".
	Reason string
}

// prGateMsg carries the gate for one pull request.
type prGateMsg struct {
	number int
	gate   prWriteGate
}

// blocked is the message a withheld write action shows. The first sentence is
// the one the spec names; the detail says which permission was missing, so the
// message is actionable rather than merely negative.
func (g prWriteGate) blocked(what string) string {
	base := "You don't have write scopes (repo) to " + what + "."
	if g.Reason == "" {
		return base
	}
	return base + " " + g.Reason
}

// mergeDoneMsg reports the outcome of a merge.
type mergeDoneMsg struct {
	number int
	err    error
}

// closeDoneMsg reports the outcome of closing a pull request.
type closeDoneMsg struct {
	number int
	err    error
}

// mergeDefaultMsg reports the merge method written to the config file, so the
// route remembers the new default without reading the overlay back.
type mergeDefaultMsg struct{ method string }

// Page sizes for the pull request sub-views.
const (
	commitsPageSize  = 30
	filesPageSize    = 100
	commentsPageSize = 30
	// checkRunsPageSize is how many of a repository's runs the details view
	// holds for the check-to-run lookup. A pull request's runs are recent by
	// construction.
	checkRunsPageSize = 30
)

// Scroll steps, in lines: the description moves six, a diff three, matching the
// previous version.
const (
	descStep = 6
	diffStep = 3
	// defaultBodyWidth stands in for the description width before the first
	// window size arrives, so a scroll clamp has something sane to work with.
	defaultBodyWidth = 100
)

// PRDetails is the pull request screen: the description, the metadata sidebar,
// the checks, and the four sub-views that hang off it.
//
// It is a route rather than a field of Main because it is a screen of its own:
// its own selection, its own fetches, its own lifetime. The session it holds is
// cancelled by the root when the route is popped, so a fetch for a screen
// nobody is looking at is abandoned rather than landing in discarded state.
type PRDetails struct {
	number int
	ref    store.RepoRef
	pr     store.PullRequest
	tab    prTab

	runs     []store.WorkflowRun
	checks   []store.CheckRun
	commits  []store.RepositoryCommit
	files    []store.CommitFile
	comments []store.IssueComment

	loaded  map[prSection]bool
	loading bool
	err     error

	// descFocused is the Tab toggle between the description viewport and the
	// checks sidebar (spec §4.10).
	descFocused bool
	// descScroll, commentScroll and diffScroll are line offsets into the
	// wrapped description, the rendered comments, and the selected file's
	// patch.
	descScroll    int
	commentScroll int
	diffScroll    int

	// checkCursor is shared by the Info sidebar and the Checks sub-tab, so
	// switching between them keeps the selection.
	checkCursor  int
	commitCursor int
	fileCursor   int

	mergeMethod string
	gate        prWriteGate
	sess        *session
}

// newPRDetails returns the PR Details route for a pull request the list already
// has, so the heading is populated before the first fetch lands.
func newPRDetails(deps Deps, pr store.PullRequest) (*PRDetails, tea.Cmd) {
	p := &PRDetails{
		number:      pr.Number,
		ref:         prRepo(pr),
		pr:          pr,
		descFocused: true,
		mergeMethod: deps.MergeMethod,
		loaded:      map[prSection]bool{},
		sess:        newSession(deps.Ctx),
	}
	return p, tea.Batch(p.loadInfo(deps), checkPRWrite(deps, p.ref, p.number))
}

// Title is the breadcrumb label.
func (p *PRDetails) Title() string { return fmt.Sprintf("PR #%d", p.number) }

// Scope is the keymap scope of the sub-tab on screen.
func (p *PRDetails) Scope() keymap.Scope { return prTabMeta[p.tab].Scope }

// Stop releases the requests this route has in flight.
func (p *PRDetails) Stop() { p.sess.Stop() }

// Available narrows the footer: merge and close are offered only once the
// viewer is known to have write permission, so a key that would refuse is never
// advertised.
func (p *PRDetails) Available(action keymap.Action) bool {
	switch action {
	case keymap.ActionMerge, keymap.ActionClose:
		return p.gate.CanWrite
	}
	return true
}

// loadInfo fetches everything the Info and Checks sub-tabs show: the pull
// request, its checks, and the repository's recent runs, which a check is
// resolved against.
func (p *PRDetails) loadInfo(deps Deps) tea.Cmd {
	cmds := []tea.Cmd{p.load(deps, prSectionDetail)}
	for _, section := range []prSection{prSectionChecks, prSectionRuns} {
		if !p.loaded[section] {
			cmds = append(cmds, p.load(deps, section))
		}
	}
	return tea.Batch(cmds...)
}

// load fetches one section.
func (p *PRDetails) load(deps Deps, section prSection) tea.Cmd {
	ref, number, sha := p.ref, p.number, p.pr.Head.SHA
	ctx := p.sess.Ctx()
	p.loading = true
	return func() tea.Msg {
		switch section {
		case prSectionDetail:
			pr, err := deps.Store.PullRequest(ctx, ref, number)
			return prDataMsg{section: section, pr: pr, err: err}
		case prSectionRuns:
			list, err := deps.Store.WorkflowRuns(ctx, ref, store.RunQuery{Page: store.Page{Number: 1, Size: checkRunsPageSize}})
			return prDataMsg{section: section, runs: list.Items, err: err}
		case prSectionChecks:
			checks, err := deps.Store.CheckRuns(ctx, ref, sha)
			return prDataMsg{section: section, checks: checks, err: err}
		case prSectionCommits:
			list, err := deps.Store.PullRequestCommits(ctx, ref, number, store.Page{Number: 1, Size: commitsPageSize})
			return prDataMsg{section: section, commits: list.Items, err: err}
		case prSectionFiles:
			list, err := deps.Store.PullRequestFiles(ctx, ref, number, store.Page{Number: 1, Size: filesPageSize})
			return prDataMsg{section: section, files: list.Items, err: err}
		case prSectionComments:
			list, err := deps.Store.Comments(ctx, ref, number, store.Page{Number: 1, Size: commentsPageSize})
			return prDataMsg{section: section, comments: list.Items, err: err}
		}
		return prDataMsg{section: section}
	}
}

// loadTab fetches the active sub-tab's data when it is not loaded yet.
func (p *PRDetails) loadTab(deps Deps) tea.Cmd {
	switch p.tab {
	case prTabInfo, prTabChecks:
		return p.loadInfo(deps)
	case prTabCommits:
		if p.loaded[prSectionCommits] {
			return nil
		}
		return p.load(deps, prSectionCommits)
	case prTabFiles:
		if p.loaded[prSectionFiles] {
			return nil
		}
		return p.load(deps, prSectionFiles)
	case prTabComments:
		if p.loaded[prSectionComments] {
			return nil
		}
		return p.load(deps, prSectionComments)
	}
	return nil
}

// checkPRWrite asks whether the viewer may merge or close this pull request.
//
// It fails closed. The previous version offered merge whenever the token scope
// header was absent, which advertises an action that then fails at the API, and
// it asked about the coarse token scope rather than repository permission
// (spec finding F1). The question here is the one the run approval path already
// asks.
func checkPRWrite(deps Deps, ref store.RepoRef, number int) tea.Cmd {
	if !ref.Valid() {
		return nil
	}
	ctx := deps.Ctx
	return func() tea.Msg {
		return prGateMsg{number: number, gate: decidePRWrite(ctx, deps, ref)}
	}
}

// decidePRWrite is the gate itself.
func decidePRWrite(ctx context.Context, deps Deps, ref store.RepoRef) prWriteGate {
	if missing := store.MissingScopes(deps.Store.Scopes()); len(missing) > 0 {
		return prWriteGate{Reason: missingScopeReason(missing, deps.TokenSource)}
	}
	permission, err := deps.Store.RepoPermission(ctx, ref, deps.Account.Login)
	if err != nil {
		return prWriteGate{Reason: "cannot read your permission on " + ref.String()}
	}
	if !canWrite(permission) {
		return prWriteGate{Reason: "you need write access to " + ref.String()}
	}
	return prWriteGate{CanWrite: true}
}

// Update handles the messages this route owns.
func (p *PRDetails) Update(deps Deps, msg tea.Msg) (Route, tea.Cmd) {
	switch msg := msg.(type) {
	case prDataMsg:
		return p.apply(deps, msg)
	case prGateMsg:
		if msg.number == p.number {
			p.gate = msg.gate
		}
		return p, nil
	case mergeDefaultMsg:
		p.mergeMethod = msg.method
		return p, nil
	case mergeDoneMsg:
		if msg.number != p.number {
			return p, nil
		}
		if msg.err != nil {
			return p, statusErr("Could not merge the pull request: " + msg.err.Error())
		}
		return p, tea.Batch(status("Pull request merged."), p.load(deps, prSectionDetail))
	case closeDoneMsg:
		if msg.number != p.number {
			return p, nil
		}
		if msg.err != nil {
			return p, statusErr("Could not close the pull request: " + msg.err.Error())
		}
		return p, tea.Batch(status("Pull request closed."), p.load(deps, prSectionDetail))
	case pollMsg:
		// A pull request's checks are the thing that changes while it is open,
		// so a tick refreshes them rather than the whole screen.
		if p.loading {
			return p, nil
		}
		return p, p.load(deps, prSectionChecks)
	}
	return p, nil
}

// apply folds a fetched section into the route.
func (p *PRDetails) apply(deps Deps, msg prDataMsg) (Route, tea.Cmd) {
	p.loading = false
	if msg.err != nil {
		p.err = msg.err
		return p, statusErr("Could not load the pull request details: " + msg.err.Error())
	}
	p.err = nil
	p.loaded[msg.section] = true

	switch msg.section {
	case prSectionDetail:
		p.pr = msg.pr
	case prSectionRuns:
		p.runs = msg.runs
	case prSectionChecks:
		p.checks = msg.checks
	case prSectionCommits:
		p.commits = msg.commits
	case prSectionFiles:
		p.files = msg.files
	case prSectionComments:
		p.comments = msg.comments
	}
	p.clamp(deps)
	return p, nil
}

// Act handles a keymap action.
func (p *PRDetails) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionBack:
		if p.tab != prTabInfo {
			return p.switchTab(deps, prTabInfo)
		}
		return p, popCmd(), true
	case keymap.ActionTabInfo:
		return p.switchTab(deps, prTabInfo)
	case keymap.ActionCommits:
		return p.switchTab(deps, prTabCommits)
	case keymap.ActionDiff:
		return p.switchTab(deps, prTabFiles)
	case keymap.ActionTabChecks:
		return p.switchTab(deps, prTabChecks)
	case keymap.ActionComments:
		return p.switchTab(deps, prTabComments)
	case keymap.ActionToggleFocus:
		if p.tab == prTabInfo || p.tab == prTabChecks {
			p.descFocused = !p.descFocused
		}
		return p, nil, true
	case keymap.ActionDown:
		return p.move(deps, 1)
	case keymap.ActionUp:
		return p.move(deps, -1)
	case keymap.ActionScrollUp:
		return p.scroll(deps, -1)
	case keymap.ActionScrollDown:
		return p.scroll(deps, 1)
	case keymap.ActionOpen:
		return p.open(deps)
	case keymap.ActionOpenBrowser:
		return p.openBrowser(deps)
	case keymap.ActionMerge:
		return p.merge(deps)
	case keymap.ActionClose:
		return p.close(deps)
	case keymap.ActionRefresh:
		return p.refresh(deps)
	}
	return p, nil, false
}

// switchTab moves to a sub-tab and fetches what it needs.
func (p *PRDetails) switchTab(deps Deps, tab prTab) (Route, tea.Cmd, bool) {
	if tab == p.tab {
		return p, nil, true
	}
	p.tab = tab
	// The Info sub-tab is the one that reads the description, so landing on it
	// puts the focus back there.
	if tab == prTabInfo {
		p.descFocused = true
	}
	return p, p.loadTab(deps), true
}

// move shifts the selection inside the active sub-tab, or scrolls the
// description while it has the focus.
func (p *PRDetails) move(deps Deps, delta int) (Route, tea.Cmd, bool) {
	switch p.tab {
	case prTabInfo:
		if p.descFocused {
			p.descScroll += delta
			p.clamp(deps)
			return p, nil, true
		}
		p.checkCursor += delta
	case prTabChecks:
		p.checkCursor += delta
	case prTabCommits:
		p.commitCursor += delta
	case prTabFiles:
		p.fileCursor += delta
		p.diffScroll = 0
	case prTabComments:
		p.commentScroll += delta
	}
	p.clamp(deps)
	return p, nil, true
}

// scroll moves the viewport of the active sub-tab by delta steps.
func (p *PRDetails) scroll(deps Deps, delta int) (Route, tea.Cmd, bool) {
	switch p.tab {
	case prTabInfo:
		// The description is the only scrolling pane, and only while it has
		// the focus (spec §4.10).
		if !p.descFocused {
			return p, nil, true
		}
		p.descScroll += delta * descStep
	case prTabFiles:
		p.diffScroll += delta * diffStep
	case prTabComments:
		p.commentScroll += delta * diffStep
	}
	p.clamp(deps)
	return p, nil, true
}

// clamp keeps every cursor and scroll offset inside its content.
func (p *PRDetails) clamp(deps Deps) {
	p.checkCursor = clampIndex(p.checkCursor, len(p.checks))
	p.commitCursor = clampIndex(p.commitCursor, len(p.commits))
	p.fileCursor = clampIndex(p.fileCursor, len(p.files))
	p.descScroll = clampOffset(p.descScroll, len(wrapLines(p.description(), p.bodyWidth(deps))))
	p.commentScroll = clampOffset(p.commentScroll, len(p.commentLines(deps, p.bodyWidth(deps))))
	p.diffScroll = clampOffset(p.diffScroll, len(diffLines(p.diffText())))
}

// bodyWidth is the width the panes are laid out in. The body rect spans the
// whole terminal, so the terminal width is what the panes are sized from.
func (p *PRDetails) bodyWidth(deps Deps) int {
	if deps.Size.Width > 0 {
		return deps.Size.Width
	}
	return defaultBodyWidth
}

// description is the text the description pane shows.
func (p *PRDetails) description() string {
	if strings.TrimSpace(p.pr.Body) == "" {
		return "(No description provided.)"
	}
	return p.pr.Body
}

// diffText is the patch on screen.
func (p *PRDetails) diffText() string {
	if file, ok := p.selectedFile(); ok {
		return file.Patch
	}
	return ""
}

// clampIndex brings an index into [0, n-1], or zero when the list is empty.
func clampIndex(index, n int) int {
	if n == 0 {
		return 0
	}
	return min(max(index, 0), n-1)
}

// clampOffset brings a line offset into [0, n-1] so the last line stays
// reachable and the first line stays at the top.
func clampOffset(offset, n int) int {
	return min(max(offset, 0), max(n-1, 0))
}

// open acts on the selection of the active sub-tab.
func (p *PRDetails) open(deps Deps) (Route, tea.Cmd, bool) {
	switch p.tab {
	case prTabInfo, prTabChecks:
		return p.openCheck(deps)
	case prTabCommits:
		commit, ok := p.selectedCommit()
		if !ok {
			return p, nil, true
		}
		details, cmd := newCommitDetails(deps, p.ref, commit)
		return p, pushRouteThen(details, cmd), true
	}
	return p, nil, true
}

// openCheck follows the selected check: to the workflow run it belongs to
// inside the app, or to its page in the browser when there is nothing to open
// (spec §4.10).
func (p *PRDetails) openCheck(deps Deps) (Route, tea.Cmd, bool) {
	check, ok := p.selectedCheck()
	if !ok {
		return p, nil, true
	}

	runID := int64(0)
	if isActionsCheck(check) {
		runID = runIDFromURL(check.HTMLURL)
	}
	if run, found := p.matchRun(runID, check.Name); found {
		jobs, cmd := newJobsTargeting(deps, run, check.Name)
		return p, pushRouteThen(jobs, cmd), true
	}
	if runID > 0 {
		// The check names a run the recent list does not hold, which is common
		// for a run from an earlier push. It is still reachable by ID.
		jobs, cmd := newJobsAt(deps, p.ref, runID, 0, check.Name)
		return p, pushRouteThen(jobs, cmd), true
	}
	if check.HTMLURL == "" {
		return p, status("This check has no page to open."), true
	}
	return p, browserCmd(check.HTMLURL, "Opened the check in the browser."), true
}

// matchRun finds the run a check belongs to: by run ID when the check names
// one, then by a case-insensitive name match.
func (p *PRDetails) matchRun(runID int64, name string) (store.WorkflowRun, bool) {
	if runID > 0 {
		for _, run := range p.runs {
			if run.ID == runID {
				return run, true
			}
		}
	}
	if name == "" {
		return store.WorkflowRun{}, false
	}
	want := strings.ToLower(name)
	for _, run := range p.runs {
		have := strings.ToLower(run.Name)
		if have == want || strings.Contains(want, have) || strings.Contains(have, want) {
			return run, true
		}
	}
	return store.WorkflowRun{}, false
}

// openBrowser opens the page the active sub-tab points at. On the Info
// sub-tab the focused pane decides: the description means the pull request, the
// checks sidebar means the selected check.
func (p *PRDetails) openBrowser(deps Deps) (Route, tea.Cmd, bool) {
	switch p.tab {
	case prTabInfo:
		if p.descFocused {
			return p, browserCmd(prURL(p.pr), "Opened the pull request in the browser."), true
		}
		return p, p.checkBrowser(), true
	case prTabChecks:
		return p, p.checkBrowser(), true
	case prTabFiles:
		url := prURL(p.pr)
		if url != "" {
			url += "/files"
		}
		return p, browserCmd(url, "Opened the changed files in the browser."), true
	}
	return p, browserCmd(prURL(p.pr), "Opened the pull request in the browser."), true
}

// checkBrowser opens the selected check's page.
func (p *PRDetails) checkBrowser() tea.Cmd {
	check, ok := p.selectedCheck()
	if !ok || check.HTMLURL == "" {
		return status("This check has no page to open.")
	}
	return browserCmd(check.HTMLURL, "Opened the check in the browser.")
}

// merge opens the merge overlay, or explains why it is withheld.
func (p *PRDetails) merge(deps Deps) (Route, tea.Cmd, bool) {
	if !p.gate.CanWrite {
		return p, statusErr(p.gate.blocked("merge this PR")), true
	}
	return p, pushOverlay(mergeMethodOverlay(deps, p.target(), p.mergeMethod)), true
}

// close opens the close confirmation, or explains why it is withheld.
func (p *PRDetails) close(deps Deps) (Route, tea.Cmd, bool) {
	if !p.gate.CanWrite {
		return p, statusErr(p.gate.blocked("close this PR")), true
	}
	return p, pushOverlay(closeConfirmOverlay(deps, p.target())), true
}

// target is the pull request a merge or close applies to.
func (p *PRDetails) target() mergeTarget {
	return mergeTarget{ref: p.ref, number: p.number, title: p.pr.Title, body: p.pr.Body}
}

// itoa renders an int without pulling fmt into the overlay builders.
func itoa(n int) string { return strconv.Itoa(n) }

// refresh refetches the active sub-tab's data.
func (p *PRDetails) refresh(deps Deps) (Route, tea.Cmd, bool) {
	for _, section := range p.activeSections() {
		delete(p.loaded, section)
	}
	return p, tea.Batch(status("Refreshing pull request..."), p.loadTab(deps)), true
}

// activeSections is the data the active sub-tab reads.
func (p *PRDetails) activeSections() []prSection {
	switch p.tab {
	case prTabInfo, prTabChecks:
		return []prSection{prSectionDetail, prSectionChecks}
	case prTabCommits:
		return []prSection{prSectionCommits}
	case prTabFiles:
		return []prSection{prSectionFiles}
	case prTabComments:
		return []prSection{prSectionComments}
	}
	return nil
}

// selectedCheck is the highlighted check.
func (p *PRDetails) selectedCheck() (store.CheckRun, bool) {
	if p.checkCursor < 0 || p.checkCursor >= len(p.checks) {
		return store.CheckRun{}, false
	}
	return p.checks[p.checkCursor], true
}

// selectedCommit is the highlighted commit.
func (p *PRDetails) selectedCommit() (store.RepositoryCommit, bool) {
	if p.commitCursor < 0 || p.commitCursor >= len(p.commits) {
		return store.RepositoryCommit{}, false
	}
	return p.commits[p.commitCursor], true
}

// selectedFile is the highlighted changed file.
func (p *PRDetails) selectedFile() (store.CommitFile, bool) {
	if p.fileCursor < 0 || p.fileCursor >= len(p.files) {
		return store.CommitFile{}, false
	}
	return p.files[p.fileCursor], true
}

// View renders the heading, the sub-tab strip, and the active sub-tab.
func (p *PRDetails) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	head := p.heading(deps, r.W)
	head = append(head, p.tabStrip(deps), "")

	_, body := r.SplitRows(len(head))
	lines := append(head, p.body(deps, body))
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// heading is the pull request's identity: its state, its number and title, and
// where it comes from and goes to.
func (p *PRDetails) heading(deps Deps, width int) []string {
	theme := deps.Theme
	state, colour := prStateLabel(theme, p.pr)

	title := fmt.Sprintf("PR #%d: %s", p.number, p.pr.Title)
	if strings.TrimSpace(p.pr.Title) == "" {
		title = fmt.Sprintf("PR #%d", p.number)
	}
	title = hyperlink(theme, title, prURL(p.pr))

	repo := p.pr.Repository.FullName
	if repo == "" {
		repo = p.ref.String()
	}
	detail := fmt.Sprintf("Repo: %s | Author: @%s | Source: %s → Base: %s",
		repo, prAuthor(p.pr), refOr(p.pr.Head.Ref), refOr(p.pr.Base.Ref))

	return []string{
		"  " + style(colour, bold).Render("["+state+"]") + " " +
			style(theme.Title, bold).Render(layout.Truncate(title, max(width-16, 8))),
		"  " + style(theme.Muted, normal).Render(layout.Truncate(detail, max(width-4, 8))),
		"  " + style(theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-4, 1))),
	}
}

// refOr renders a branch name, with a placeholder when the API omitted it.
func refOr(ref string) string {
	if ref == "" {
		return "unknown"
	}
	return ref
}

// prStateLabel is the state badge and the colour it is drawn in, using the
// vocabulary the previous version used.
func prStateLabel(theme Theme, pr store.PullRequest) (string, color.Color) {
	switch {
	case pr.Draft:
		return "DRAFT", theme.Muted
	case pr.State == "open":
		return "OPEN", theme.StatusOK
	case pr.MergedAt != nil:
		return "MERGED", theme.Accent
	case pr.State == "closed":
		return "CLOSED", theme.StatusFail
	}
	return strings.ToUpper(pr.State), theme.Muted
}

// tabStrip renders the five sub-tabs with their keys, the active one
// highlighted, so the strip doubles as the way to reach each one.
func (p *PRDetails) tabStrip(deps Deps) string {
	theme := deps.Theme
	parts := make([]string, 0, prTabCount)
	for i := 0; i < prTabCount; i++ {
		label := fmt.Sprintf(" %d:%s ", i+1, prTabMeta[prTab(i)].Name)
		if prTab(i) == p.tab {
			parts = append(parts, style(theme.Selected, bold).Render(label))
			continue
		}
		parts = append(parts, style(theme.Muted, normal).Render(label))
	}
	return " " + strings.Join(parts, style(theme.HeaderRule, normal).Render("│"))
}

// body renders the active sub-tab.
func (p *PRDetails) body(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	switch p.tab {
	case prTabCommits:
		return p.commitsView(deps, r)
	case prTabFiles:
		return p.filesView(deps, r)
	case prTabChecks:
		return p.checksView(deps, r)
	case prTabComments:
		return p.commentsView(deps, r)
	default:
		return p.infoView(deps, r)
	}
}

// infoView renders the three panes: the description, the metadata, and the
// checks.
//
// Every pane produces exactly the region's height, already padded to its own
// width, so joining them row by row tiles the region exactly at every size.
func (p *PRDetails) infoView(deps Deps, r layout.Rect) string {
	descW, metaW, checksW := infoColumns(r.W)

	// A pane the width cannot hold is not drawn at all, and neither is the
	// separator that would have introduced it. infoColumns gives the width such
	// a pane would have taken back to the description, so the row still fills
	// the body.
	type pane struct {
		width int
		lines []string
	}
	drawn := make([]pane, 0, 3)
	if descW > 0 {
		drawn = append(drawn, pane{descW, p.descriptionPane(deps, descW, r.H)})
	}
	if metaW > 0 {
		drawn = append(drawn, pane{metaW, p.metadataPane(deps, metaW, r.H)})
	}
	if checksW > 0 {
		drawn = append(drawn, pane{checksW, p.checksPane(deps, checksW, r.H)})
	}
	if len(drawn) == 0 {
		return layout.Fit("", r.W, r.H)
	}

	columns := make([][]string, len(drawn))
	for i, pane := range drawn {
		columns[i] = padLines(pane.lines, pane.width, r.H)
	}
	separator := style(deps.Theme.HeaderRule, normal).Render("│")

	rows := make([]string, 0, r.H)
	for row := 0; row < r.H; row++ {
		cells := make([]string, 0, len(columns))
		for _, column := range columns {
			cells = append(cells, column[row])
		}
		rows = append(rows, strings.Join(cells, separator))
	}
	return layout.Fit(joinLines(rows), r.W, r.H)
}

// paneTitle renders a pane heading, marking the pane that owns the keyboard.
func paneTitle(theme Theme, name string, focused bool) string {
	if focused {
		return style(theme.Accent, bold).Render("▶ " + name)
	}
	return style(theme.Muted, bold).Render(name)
}

// descriptionPane renders the pull request body, wrapped and scrolled.
func (p *PRDetails) descriptionPane(deps Deps, width, height int) []string {
	lines := wrapLines(p.description(), width)
	start := clampOffset(p.descScroll, len(lines))
	visible := max(height-2, 0)
	return append([]string{paneTitle(deps.Theme, "DESCRIPTION", p.descFocused), ""},
		take(lines[start:], visible)...)
}

// metadataPane renders the details sidebar.
func (p *PRDetails) metadataPane(deps Deps, width, height int) []string {
	state, _ := prStateLabel(deps.Theme, p.pr)
	lines := []string{paneTitle(deps.Theme, "DETAILS", false), ""}

	rows := [][2]string{
		{"State", state},
		{"Author", "@" + prAuthor(p.pr)},
		{"Assignees", orNone(logins(p.pr.Assignees))},
		{"Reviewers", orNone(logins(p.pr.RequestedReviewers))},
		{"Milestone", orNone(milestoneTitle(p.pr))},
		{"Labels", orNone(labelNames(p.pr))},
		{"Created", formatTime(p.pr.CreatedAt)},
		{"Updated", formatTime(p.pr.UpdatedAt)},
	}
	for _, row := range rows {
		lines = append(lines, metadataRow(deps.Theme, row[0], row[1], width)...)
	}
	return take(lines, height)
}

// metadataRow renders one label/value pair, continuing a long value on
// continuation lines indented under it.
func metadataRow(theme Theme, label, value string, width int) []string {
	const labelWidth = 11
	const indent = "  "

	prefix := indent + style(theme.Muted, normal).Render(layout.PadRight(label+":", labelWidth)) + " "
	valueWidth := max(width-len(indent)-labelWidth-1, 4)

	chunks := wrapLines(value, valueWidth)
	out := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if i == 0 {
			out = append(out, prefix+style(theme.Text, normal).Render(chunk))
			continue
		}
		out = append(out, indent+strings.Repeat(" ", labelWidth+1)+style(theme.Text, normal).Render(chunk))
	}
	return out
}

// checksPane renders the checks list beside the description.
func (p *PRDetails) checksPane(deps Deps, width, height int) []string {
	lines := []string{paneTitle(deps.Theme, "CHECKS", !p.descFocused), ""}
	if len(p.checks) == 0 {
		return append(lines, style(deps.Theme.Muted, normal).Render("  "+p.checksMessage()))
	}
	visible := max(height-2, 1)
	start, end := layout.Window(len(p.checks), p.checkCursor, visible)
	for i := start; i < end; i++ {
		lines = append(lines, p.checkRow(deps, p.checks[i], width, i == p.checkCursor && !p.descFocused))
	}
	return take(lines, height)
}

// checksView is the Checks sub-tab: the same list, across the whole width.
func (p *PRDetails) checksView(deps Deps, r layout.Rect) string {
	lines := []string{paneTitle(deps.Theme, "CHECKS", true), ""}
	if len(p.checks) == 0 {
		lines = append(lines, "  "+style(deps.Theme.Muted, normal).Render(p.checksMessage()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}
	visible := max(r.H-len(lines), 1)
	start, end := layout.Window(len(p.checks), p.checkCursor, visible)
	for i := start; i < end; i++ {
		lines = append(lines, p.checkRow(deps, p.checks[i], r.W, i == p.checkCursor))
	}
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// checkRow renders one check: its state, its name, and the app that reported
// it.
func (p *PRDetails) checkRow(deps Deps, check store.CheckRun, width int, selected bool) string {
	name := check.Name
	if app := checkAppName(check); app != "" {
		name += " — " + app
	}
	marker := "  "
	if selected {
		marker = "> "
	}
	row := marker + statusIndicator(deps.Theme, check.Status, check.Conclusion) + " " +
		style(deps.Theme.Text, normal).Render(layout.Truncate(name, max(width-4, 4)))
	if selected {
		return style(deps.Theme.Selected, bold).Render(row)
	}
	return row
}

// checksMessage describes an empty checks pane.
func (p *PRDetails) checksMessage() string {
	switch {
	case p.loading && !p.loaded[prSectionChecks]:
		return "Loading checks..."
	case p.err != nil:
		return "Checks unavailable"
	default:
		return "No checks"
	}
}

// infoColumns splits the Info sub-tab's width into the description pane, the
// metadata pane and the checks pane.
//
// The widths of the panes plus one column per separator drawn between them
// always add up to width, so the row tiles the body exactly at every terminal
// size. The description takes the slack, so the two sidebars get what their
// widest rows need rather than a fraction of the screen, and the metadata pane
// is the first to be given up when there is not enough room for all three: the
// description is what the screen is for, and the checks are what the user acts
// on.
func infoColumns(width int) (desc, meta, checks int) {
	if width < 1 {
		width = 1
	}
	if width <= 2*infoSepWidth {
		// Room for one pane and nothing else.
		return width, 0, 0
	}

	usable := width - 2*infoSepWidth
	if usable < descMinWidth+checksMinWidth {
		checks = usable / 2
		desc = usable - checks
	} else {
		checks = min(max(usable/3, checksMinWidth), checksMaxWidth)
		meta = min(max(usable-checks-descMinWidth, metaMinWidth), metaMaxWidth)
		desc = usable - meta - checks

		if desc < descMinWidth {
			if cut := min(descMinWidth-desc, meta-metaMinWidth); cut > 0 {
				meta -= cut
			}
			desc = usable - meta - checks
		}
		if desc < descMinWidth {
			if cut := min(descMinWidth-desc, checks-checksMinWidth); cut > 0 {
				checks -= cut
			}
			desc = usable - meta - checks
		}
		if desc < descMinWidth {
			// Not enough room for three panes: the metadata pane gives way
			// entirely, which leaves the description and the checks.
			desc += meta
			meta = 0
		}
	}

	// A pane that is not drawn takes its separator with it, so the description
	// grows into the gap rather than leaving a hole.
	if meta == 0 {
		desc += infoSepWidth
	}
	if checks == 0 {
		desc += infoSepWidth
	}
	return desc, meta, checks
}

// Pane geometry: the separator, the narrowest each pane may become, and the
// widest the two sidebars may grow.
//
// The metadata pane's minimum is the width at which a label and a value both
// still fit on one line. Below that the pane holds "  State:      OP" rather
// than an answer, so it is dropped instead of squeezed.
const (
	infoSepWidth   = 1
	descMinWidth   = 20
	metaMinWidth   = 22
	checksMinWidth = 22
	checksMaxWidth = 38
	metaMaxWidth   = 26
)

// padLines fits lines into exactly height lines of exactly width cells, so the
// panes can be joined row by row.
func padLines(lines []string, width, height int) []string {
	out := make([]string, 0, height)
	blank := strings.Repeat(" ", max(width, 0))
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, layout.Fit(lines[i], width, 1))
			continue
		}
		out = append(out, blank)
	}
	return out
}

// take keeps at most n lines.
func take(lines []string, n int) []string {
	if n < 0 {
		n = 0
	}
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

// wrapLines wraps a multi-line body to width and returns its lines. Blank lines
// are preserved, so a description's paragraphs stay apart.
func wrapLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(paragraph) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(layout.Wrap(paragraph, width), "\n")...)
	}
	// A body that ends in a newline produces trailing blank lines, which read
	// as blank rows at the bottom of every description.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// logins renders a user list the way the sidebars show it.
func logins(users []store.User) string {
	if len(users) == 0 {
		return ""
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, "@"+u.Login)
	}
	return strings.Join(names, ", ")
}

// milestoneTitle is the milestone's name, or empty when there is none.
func milestoneTitle(pr store.PullRequest) string {
	if pr.Milestone == nil {
		return ""
	}
	return pr.Milestone.Title
}

// labelNames joins a pull request's labels.
func labelNames(pr store.PullRequest) string {
	if len(pr.Labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		names = append(names, l.Name)
	}
	return strings.Join(names, ", ")
}

// orNone renders an empty value as the placeholder the sidebars use.
func orNone(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// formatTime renders a timestamp without a timezone suffix, or a placeholder
// when the API did not supply one.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04")
}

// checkAppName is the check's app in the form the checks list shows: the name
// when there is one, otherwise the slug.
func checkAppName(check store.CheckRun) string {
	if check.App == nil {
		return ""
	}
	if check.App.Name != "" {
		return check.App.Name
	}
	return check.App.Slug
}

// isActionsCheck reports whether a check was reported by GitHub Actions, which
// is what makes it something the app can open as a workflow run.
func isActionsCheck(check store.CheckRun) bool {
	if check.App == nil {
		return false
	}
	slug := strings.ToLower(check.App.Slug)
	name := strings.ToLower(check.App.Name)
	return slug == "github-actions" || strings.Contains(name, "github actions") || strings.Contains(slug, "action")
}

// runIDFromURL reads the workflow run ID out of a check's URL, which looks like
// https://github.com/o/r/actions/runs/12345/job/67890. Zero means the URL names
// no run.
func runIDFromURL(url string) int64 {
	const marker = "/actions/runs/"
	idx := strings.Index(url, marker)
	if idx == -1 {
		return 0
	}
	sub := url[idx+len(marker):]
	var id int64
	for i := 0; i < len(sub); i++ {
		if sub[i] < '0' || sub[i] > '9' {
			break
		}
		id = id*10 + int64(sub[i]-'0')
	}
	return id
}
