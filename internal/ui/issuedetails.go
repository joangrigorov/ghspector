package ui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/store"
)

// issueCommentsPageSize is how many comments one page of an issue's
// conversation asks for, matching the pull request conversation.
const issueCommentsPageSize = 30

// issueDataMsg carries the fetched issue.
type issueDataMsg struct {
	number int
	issue  store.Issue
	err    error
}

// IssueDetails is the issue screen: the description beside its metadata
// sidebar, with the conversation one key away.
//
// It is a route rather than a field of Main for the same reason the pull
// request screen is: its own fetches and its own lifetime. The session it holds
// is cancelled by the root when the route is popped, so a fetch for a screen
// nobody is looking at is abandoned rather than landing in discarded state.
type IssueDetails struct {
	ref    store.RepoRef
	number int
	issue  store.Issue

	loading bool
	err     error

	// descFocused is the Tab toggle between the description and the metadata
	// sidebar (spec §4.15). Only the description scrolls.
	descFocused bool
	descScroll  int

	// descLines is the description as it was last rendered, keyed on the body
	// itself. Every clamp measures the description, so without this a keystroke
	// pays for the whole markdown body.
	descLines renderedLines[string]

	sess *session
}

// newIssueDetails returns the Issue Details route for an issue the list already
// has, so the heading and the sidebar are populated before the fetch lands.
func newIssueDetails(deps Deps, issue store.Issue) (*IssueDetails, tea.Cmd) {
	d := &IssueDetails{
		ref:         issueRepo(issue),
		number:      issue.Number,
		issue:       issue,
		descFocused: true,
		sess:        newSession(deps.Ctx),
	}
	return d, d.load(deps)
}

// Title is the breadcrumb label.
func (d *IssueDetails) Title() string { return fmt.Sprintf("Issue #%d", d.number) }

// Scope is the keymap scope for one issue.
func (d *IssueDetails) Scope() keymap.Scope { return keymap.IssueDetails }

// Stop releases the requests this route has in flight.
func (d *IssueDetails) Stop() { d.sess.Stop() }

// Loading reports a fetch this route is waiting on.
func (d *IssueDetails) Loading() bool { return d.loading }

// load fetches the issue, which is where its body comes from: the list row
// carries the title and the state but not the description.
func (d *IssueDetails) load(deps Deps) tea.Cmd {
	ref, number := d.ref, d.number
	ctx := d.sess.Ctx()
	d.loading = true
	return func() tea.Msg {
		issue, err := deps.Store.Issue(ctx, ref, number)
		return issueDataMsg{number: number, issue: issue, err: err}
	}
}

// Update handles the messages this route owns.
func (d *IssueDetails) Update(deps Deps, msg tea.Msg) (Route, tea.Cmd) {
	switch msg := msg.(type) {
	case issueDataMsg:
		return d.apply(deps, msg)
	case pollMsg:
		// An issue's state and its comments are what change while it is open,
		// so a tick refreshes the issue rather than the whole screen.
		if d.loading {
			return d, nil
		}
		return d, d.load(deps)
	}
	return d, nil
}

// apply folds a fetched issue into the route.
func (d *IssueDetails) apply(deps Deps, msg issueDataMsg) (Route, tea.Cmd) {
	// A reply for an issue the user has already left is dropped.
	if msg.number != d.number {
		return d, nil
	}
	d.loading = false
	if msg.err != nil {
		d.err = msg.err
		return d, statusErr("Could not load the issue: " + msg.err.Error())
	}
	d.err = nil
	d.issue = msg.issue
	d.clamp(deps)
	return d, nil
}

// Act handles a keymap action.
func (d *IssueDetails) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionBack:
		return d, popCmd(), true
	case keymap.ActionToggleFocus:
		d.descFocused = !d.descFocused
		return d, nil, true
	case keymap.ActionDown:
		return d.scroll(deps, 1)
	case keymap.ActionUp:
		return d.scroll(deps, -1)
	case keymap.ActionScrollUp:
		return d.scroll(deps, -descStep)
	case keymap.ActionScrollDown:
		return d.scroll(deps, descStep)
	case keymap.ActionOpenBrowser:
		return d, browserCmd(issueURL(d.issue), "Opened the issue in the browser."), true
	case keymap.ActionComments:
		comments, cmd := newIssueComments(deps, d.ref, d.number, d.issue.Title)
		return d, pushRouteThen(comments, cmd), true
	case keymap.ActionRefresh:
		return d, tea.Batch(status("Refreshing issue..."), d.load(deps)), true
	}
	return d, nil, false
}

// scroll moves the description by delta lines. The sidebar holds no selection,
// so a keystroke while it has the focus changes nothing (spec §4.15).
func (d *IssueDetails) scroll(deps Deps, delta int) (Route, tea.Cmd, bool) {
	if d.descFocused {
		d.descScroll += delta
		d.clamp(deps)
	}
	return d, nil, true
}

// clamp keeps the scroll offset inside the description.
func (d *IssueDetails) clamp(deps Deps) {
	d.descScroll = clampOffset(d.descScroll, len(d.descriptionLines(deps, d.descriptionWidth(deps))))
}

// bodyWidth is the width the panes are laid out in: what a pane's rect works out
// to, so the scroll clamp counts the lines the pane actually shows.
func (d *IssueDetails) bodyWidth(deps Deps) int {
	if deps.Size.Width > 0 {
		return layout.ContentWidth(deps.Size.Width)
	}
	return defaultBodyWidth
}

// descriptionWidth is the width the description pane is drawn at, with the
// sidebar taking the rest of the box.
func (d *IssueDetails) descriptionWidth(deps Deps) int {
	desc, _ := detailColumns(d.bodyWidth(deps))
	return desc
}

// description is the text the description pane shows.
func (d *IssueDetails) description() string {
	if strings.TrimSpace(d.issue.Body) == "" {
		if d.loading {
			return "Loading issue..."
		}
		return "(No description provided.)"
	}
	return d.issue.Body
}

// View renders the heading and the description beside its sidebar.
func (d *IssueDetails) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	head := d.heading(deps, r.W)
	_, body := r.SplitRows(len(head))
	return layout.Fit(joinLines(append(head, d.body(deps, body))), r.W, r.H)
}

// heading is the issue's identity: its state, its number and title, and where
// it comes from.
func (d *IssueDetails) heading(deps Deps, width int) []string {
	theme := deps.Theme
	state, colour := issueStateLabel(theme, d.issue)

	title := fmt.Sprintf("Issue #%d: %s", d.number, d.issue.Title)
	if strings.TrimSpace(d.issue.Title) == "" {
		title = fmt.Sprintf("Issue #%d", d.number)
	}
	title = hyperlink(theme, title, issueURL(d.issue))

	repo := repoFullName(d.issue.Repository.FullName, d.ref)
	detail := "Repo: " + hyperlink(theme, repo, repoPageURL(repo)) +
		fmt.Sprintf(" | Author: @%s | Assignees: %s", issueAuthor(d.issue), orNone(issueAssignees(d.issue)))

	return []string{
		"  " + style(colour, bold).Render("["+state+"]") + " " +
			style(theme.Title, bold).Render(layout.Truncate(title, max(width-16, 8))),
		"  " + style(theme.Muted, normal).Render(layout.Truncate(detail, max(width-layout.Gutter, 8))),
		"  " + style(theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-layout.Gutter, 1))),
	}
}

// issueStateLabel is the state badge and the colour it is drawn in, using the
// vocabulary the issue list uses.
func issueStateLabel(theme Theme, issue store.Issue) (string, color.Color) {
	switch issue.State {
	case "open":
		return "OPEN", theme.StatusOK
	case "closed":
		return "CLOSED", theme.StatusFail
	}
	return strings.ToUpper(issue.State), theme.Muted
}

// body renders the description beside the metadata sidebar.
//
// Both panes produce exactly the height and the width they were given, so
// joining them row by row tiles the region exactly at every size. A width that
// cannot hold both drops the sidebar, because a sidebar too narrow to show a
// label and its value is not an answer.
func (d *IssueDetails) body(deps Deps, r layout.Rect) string {
	// The panes tile the rect they are given, so the rect is inset by the gutter
	// and the rendered block shifted into it, which puts the panes on the same
	// left edge as the heading above them.
	inner := paneRect(r)
	descW, sideW := detailColumns(inner.W)

	descLines := d.descriptionPane(deps, descW, inner.H)
	if sideW == 0 {
		return gutterBlock(layout.Fit(joinLines(padLines(descLines, descW, inner.H)), inner.W, inner.H), r.W, r.H)
	}

	columns := [][]string{
		padLines(descLines, descW, inner.H),
		sidebarPane(d.metadataPane(deps, sideW-sidebarPad, inner.H), sideW, inner.H),
	}
	separator := style(deps.Theme.HeaderRule, normal).Render("│")

	rows := make([]string, 0, inner.H)
	for row := 0; row < inner.H; row++ {
		rows = append(rows, columns[0][row]+separator+columns[1][row])
	}
	return gutterBlock(layout.Fit(joinLines(rows), inner.W, inner.H), r.W, r.H)
}

// descriptionPane renders the issue body, rendered as markdown and scrolled.
func (d *IssueDetails) descriptionPane(deps Deps, width, height int) []string {
	lines := d.descriptionLines(deps, width)
	start := clampOffset(d.descScroll, len(lines))
	visible := max(height-2, 0)
	return append([]string{paneTitle(deps.Theme, "DESCRIPTION", d.descFocused), ""},
		take(lines[start:], visible)...)
}

// descriptionLines is the body as rendered lines, from the cache when the body,
// the width and the palette are the ones it was rendered at. It is what clamp
// measures and what the pane draws, so neither pays for the body twice.
func (d *IssueDetails) descriptionLines(deps Deps, width int) []string {
	body := d.description()
	return d.descLines.linesFrom(body, markdownKeyOf(deps.Theme, width), func() []string {
		return deps.Markdown.Lines(deps.Theme, body, width)
	})
}

// metadataPane renders the issue sidebar: what the issue is, who is on it, and
// when it moved (spec §4.15).
func (d *IssueDetails) metadataPane(deps Deps, width, height int) []string {
	state, _ := issueStateLabel(deps.Theme, d.issue)
	lines := []string{paneTitle(deps.Theme, "DETAILS", !d.descFocused), ""}

	rows := [][2]string{
		{"State", state},
		{"Author", "@" + issueAuthor(d.issue)},
		{"Assignees", orNone(logins(d.issue.Assignees))},
		{"Milestone", orNone(milestoneName(d.issue.Milestone))},
		{"Labels", orNone(labelList(d.issue.Labels))},
		{"Created", formatTime(d.issue.CreatedAt)},
		{"Updated", formatTime(d.issue.UpdatedAt)},
		{"Closed", closedTime(d.issue)},
	}
	for _, row := range rows {
		lines = append(lines, metadataRow(deps.Theme, row[0], row[1], width)...)
	}
	return take(lines, height)
}

// closedTime renders the close timestamp, or the placeholder while the issue is
// open.
func closedTime(issue store.Issue) string {
	if issue.ClosedAt == nil {
		return "—"
	}
	return formatTime(*issue.ClosedAt)
}

// issueCommentsMsg carries the fetched conversation.
type issueCommentsMsg struct {
	number   int
	comments []store.IssueComment
	err      error
}

// IssueComments is the conversation of one issue, scrolled.
type IssueComments struct {
	ref    store.RepoRef
	number int
	title  string

	comments []store.IssueComment
	loading  bool
	loaded   bool
	err      error
	scroll   int

	// version counts the changes to the conversation, so the rendered lines
	// below are dropped when a fetch lands. Rendering every body on each frame
	// and each keystroke is what makes a long thread lag.
	version  int
	rendered renderedLines[int]

	sess *session
}

// newIssueComments returns the comments route for an issue, and the fetch of
// its conversation.
func newIssueComments(deps Deps, ref store.RepoRef, number int, title string) (*IssueComments, tea.Cmd) {
	c := &IssueComments{ref: ref, number: number, title: title, loading: true, sess: newSession(deps.Ctx)}
	return c, c.load(deps)
}

// Title is the breadcrumb label.
func (c *IssueComments) Title() string { return fmt.Sprintf("Comments #%d", c.number) }

// Scope is the keymap scope for one issue's conversation.
func (c *IssueComments) Scope() keymap.Scope { return keymap.IssueComments }

// Stop releases the requests this route has in flight.
func (c *IssueComments) Stop() { c.sess.Stop() }

// Loading reports a fetch this route is waiting on.
func (c *IssueComments) Loading() bool { return c.loading }

// load fetches the comments.
func (c *IssueComments) load(deps Deps) tea.Cmd {
	ref, number := c.ref, c.number
	ctx := c.sess.Ctx()
	c.loading = true
	return func() tea.Msg {
		list, err := deps.Store.Comments(ctx, ref, number, store.Page{Number: 1, Size: issueCommentsPageSize})
		return issueCommentsMsg{number: number, comments: list.Items, err: err}
	}
}

// Update handles the messages this route owns.
func (c *IssueComments) Update(deps Deps, msg tea.Msg) (Route, tea.Cmd) {
	switch msg := msg.(type) {
	case issueCommentsMsg:
		// A reply for an issue the user has already left is dropped.
		if msg.number != c.number {
			return c, nil
		}
		c.loading = false
		if msg.err != nil {
			c.err = msg.err
			return c, statusErr("Could not load the comments: " + msg.err.Error())
		}
		c.err = nil
		c.loaded = true
		c.comments = msg.comments
		c.version++
		c.clamp(deps)
		return c, nil
	}
	return c, nil
}

// Act handles a keymap action.
func (c *IssueComments) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionBack:
		return c, popCmd(), true
	case keymap.ActionRefresh:
		return c, tea.Batch(status("Refreshing comments..."), c.load(deps)), true
	case keymap.ActionDown:
		c.scroll++
		c.clamp(deps)
		return c, nil, true
	case keymap.ActionUp:
		c.scroll--
		c.clamp(deps)
		return c, nil, true
	case keymap.ActionScrollUp:
		c.scroll -= diffStep
		c.clamp(deps)
		return c, nil, true
	case keymap.ActionScrollDown:
		c.scroll += diffStep
		c.clamp(deps)
		return c, nil, true
	}
	return c, nil, false
}

// clamp keeps the scroll offset inside the rendered conversation.
func (c *IssueComments) clamp(deps Deps) {
	c.scroll = clampOffset(c.scroll, len(c.lines(deps)))
}

// lines is the conversation as rendered lines, from the cache when the data,
// the width and the palette are the ones it was rendered at.
func (c *IssueComments) lines(deps Deps) []string {
	key := markdownKeyOf(deps.Theme, c.bodyWidth(deps))
	return c.rendered.linesFrom(c.version, key, func() []string {
		return commentBlockLines(deps, c.comments, key.width)
	})
}

// bodyWidth is the width the conversation is wrapped to: the pane it is drawn
// in, which is the screen's content box.
func (c *IssueComments) bodyWidth(deps Deps) int {
	if deps.Size.Width > 0 {
		return layout.ContentWidth(deps.Size.Width)
	}
	return defaultBodyWidth
}

// View renders the heading and the conversation.
func (c *IssueComments) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	lines := c.heading(deps, r.W)
	if len(c.comments) == 0 {
		lines = append(lines, "", "  "+style(deps.Theme.Muted, normal).Render(c.emptyMessage()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	_, body := r.SplitRows(len(lines))
	content := c.lines(deps)
	start := clampOffset(c.scroll, len(content))
	lines = append(lines, gutterRows(joinLines(take(content[start:], body.H)), body.W, body.H)...)
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// heading names the issue the conversation belongs to, and links it.
func (c *IssueComments) heading(deps Deps, width int) []string {
	theme := deps.Theme
	title := fmt.Sprintf("Comments on issue #%d", c.number)
	if strings.TrimSpace(c.title) != "" {
		title += ": " + c.title
	}
	title = hyperlink(theme, title, issueCommentsURL(c.ref, c.number))
	return []string{
		"  " + style(theme.Title, bold).Render(layout.Truncate(title, max(width-layout.Gutter, 8))),
		"  " + style(theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-layout.Gutter, 1))),
	}
}

// emptyMessage describes a conversation with nothing in it, still loading, or
// unavailable.
func (c *IssueComments) emptyMessage() string {
	switch {
	case c.loading:
		return "Loading comments..."
	case c.err != nil:
		return "Could not load the comments"
	default:
		return "No comments"
	}
}
