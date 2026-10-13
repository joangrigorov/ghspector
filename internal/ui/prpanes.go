package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/store"
)

// commitsView is the Commits sub-tab: the pull request's commits, one per row,
// with the selection the Enter key acts on.
func (p *PRDetails) commitsView(deps Deps, r layout.Rect) string {
	theme := deps.Theme
	lines := []string{paneTitle(theme, "COMMITS", true), ""}

	if len(p.commits) == 0 {
		lines = append(lines, "  "+style(theme.Muted, normal).Render(p.loadMessage(prSectionCommits, "No commits")))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	cols := commitColumns(r.W)
	lines = append(lines, style(theme.Muted, bold).Render(tableHeaderRow(cols)))

	visible := max(r.H-len(lines), 1)
	start, end := layout.Window(len(p.commits), p.commitCursor, visible)
	for i := start; i < end; i++ {
		lines = append(lines, commitRow(theme, p.commits[i], cols, r.W, i == p.commitCursor))
	}
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// commitColumns allocates the commit table's columns.
func commitColumns(width int) []tableColumn {
	return fitTable([]tableColumn{
		{Header: "SHA", Column: layout.Column{Width: 9, Min: 8}},
		{Header: "MESSAGE", Column: layout.Column{Width: 40, Min: 16, Flex: true}},
		{Header: "AUTHOR", Column: layout.Column{Width: 18}},
		{Header: "DATE", Column: layout.Column{Width: 17}},
	}, width)
}

// commitRow renders one commit.
func commitRow(theme Theme, commit store.RepositoryCommit, cols []tableColumn, width int, selected bool) string {
	row := tableRow(cols, []string{
		shortSHA(commit.SHA),
		commitSubject(commit),
		commitAuthor(commit),
		formatTime(commit.Commit.Author.Date),
	})
	if selected {
		return selectedRow(theme, row, width)
	}
	return row
}

// shortSHA is the seven-character form of a commit hash.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// commitSubject is the first line of a commit message.
func commitSubject(commit store.RepositoryCommit) string {
	message := strings.TrimSpace(commit.Commit.Message)
	if idx := strings.IndexByte(message, '\n'); idx != -1 {
		message = message[:idx]
	}
	if message == "" {
		return "(no message)"
	}
	return message
}

// commitBody is what a commit message says below its subject, which the heading
// of the commit screen already shows. It is empty for the common one-line
// message, and that is what keeps the message from taking rows it does not have.
func commitBody(commit store.RepositoryCommit) string {
	message := strings.ReplaceAll(commit.Commit.Message, "\r\n", "\n")
	idx := strings.IndexByte(message, '\n')
	if idx == -1 {
		return ""
	}
	return strings.TrimSpace(message[idx+1:])
}

// messageLines is a commit message's body, rendered for the width the commit
// screen's body has. Returning nil for an empty body is what tells the layout to
// give the whole region to the files.
func (c *CommitDetails) messageLines(deps Deps, width int) []string {
	body := commitBody(c.commit)
	if body == "" {
		return nil
	}
	return indentLines(deps.Markdown.Lines(deps.Theme, body, max(width-2, 8)), "  ")
}

// indentLines indents every line by a prefix, keeping blank lines blank so the
// block does not gain trailing spaces.
func indentLines(lines []string, prefix string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, prefix+line)
	}
	return out
}

// commitAuthor is the account that authored a commit, falling back to the
// name in the commit itself for a commit with no linked account.
func commitAuthor(commit store.RepositoryCommit) string {
	if commit.Author != nil && commit.Author.Login != "" {
		return commit.Author.Login
	}
	if commit.Commit.Author.Name != "" {
		return commit.Commit.Author.Name
	}
	return "unknown"
}

// filesView is the Files sub-tab: the changed files on the left, the selected
// file's patch on the right.
func (p *PRDetails) filesView(deps Deps, r layout.Rect) string {
	theme := deps.Theme
	lines := []string{paneTitle(theme, "FILES & DIFF", true), ""}
	if len(p.files) == 0 {
		lines = append(lines, "  "+style(theme.Muted, normal).Render(p.loadMessage(prSectionFiles, "No changed files")))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	_, body := r.SplitRows(len(lines))
	fileW, diffW := fileDiffColumns(body.W)

	list := p.fileList(deps, fileW, body.H)
	patch := p.diffPane(deps, diffW, body.H)

	columns := [][]string{padLines(list, fileW, body.H), padLines(patch, diffW, body.H)}
	separator := style(theme.HeaderRule, normal).Render("│")
	rows := make([]string, 0, body.H)
	for row := 0; row < body.H; row++ {
		rows = append(rows, columns[0][row]+separator+columns[1][row])
	}
	lines = append(lines, joinLines(rows))
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// fileList renders the changed files with their line counts.
func (p *PRDetails) fileList(deps Deps, width, height int) []string {
	return commitFileList(deps, p.files, p.fileCursor, width, height)
}

// fileColumns allocates the changed-file table's columns.
func fileColumns(width int) []tableColumn {
	return fitTable([]tableColumn{
		{Header: "ST", Column: layout.Column{Width: 4, Min: 4}},
		{Header: "FILE", Column: layout.Column{Width: 28, Min: 10, Flex: true}},
		{Header: "+", Column: layout.Column{Width: 5, Min: 4}},
		{Header: "-", Column: layout.Column{Width: 5, Min: 4}},
	}, width)
}

// fileRow renders one changed file.
func fileRow(theme Theme, file store.CommitFile, cols []tableColumn, width int, selected bool) string {
	row := tableRow(cols, []string{
		fileStatus(file.Status),
		file.Filename,
		fmt.Sprintf("+%d", file.Additions),
		fmt.Sprintf("-%d", file.Deletions),
	})
	if selected {
		return selectedRow(theme, row, width)
	}
	return row
}

// fileStatus abbreviates the status GitHub reports for a changed file.
func fileStatus(status string) string {
	switch status {
	case "added":
		return "A"
	case "removed", "deleted":
		return "D"
	case "renamed":
		return "R"
	case "copied":
		return "C"
	default:
		return "M"
	}
}

// diffPane renders the selected file's patch, scrolled.
func (p *PRDetails) diffPane(deps Deps, width, height int) []string {
	file, ok := p.selectedFile()
	if !ok {
		return []string{style(deps.Theme.Muted, normal).Render("No file selected")}
	}
	return diffBody(deps, file, width, height, p.diffScroll)
}

// diffLines splits a unified diff into lines with the trailing blank line
// removed. The lines carry no styling, so they can be counted and scrolled;
// diffStyled applies the line-level colour when they are drawn.
func diffLines(patch string) []string {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	out := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// diffStyled colours a patch line by its first character, which is what makes a
// patch readable at a glance: additions green, deletions red, hunk headers
// accented, and everything else plain.
func diffStyled(theme Theme, lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			out = append(out, style(theme.Muted, bold).Render(line))
		case strings.HasPrefix(line, "@@"):
			out = append(out, style(theme.Accent, normal).Render(line))
		case strings.HasPrefix(line, "+"):
			out = append(out, style(theme.StatusOK, normal).Render(line))
		case strings.HasPrefix(line, "-"):
			out = append(out, style(theme.StatusFail, normal).Render(line))
		default:
			out = append(out, line)
		}
	}
	return out
}

// fileDiffColumns splits the Files sub-tab into the file list and the patch.
func fileDiffColumns(width int) (list, diff int) {
	if width < 1 {
		return 1, 0
	}
	if width <= fileDiffSepWidth {
		return width, 0
	}
	usable := width - fileDiffSepWidth
	if usable < fileListMinWidth+diffMinWidth {
		list = usable / 2
		return list, usable - list
	}
	list = min(max(usable/3, fileListMinWidth), fileListMaxWidth)
	return list, usable - list
}

// Geometry of the Files sub-tab.
const (
	fileDiffSepWidth = 1
	fileListMinWidth = 24
	fileListMaxWidth = 60
	diffMinWidth     = 20
)

// loadMessage describes an empty or failed sub-tab, so every sub-tab says the
// same thing about the same situation.
func (p *PRDetails) loadMessage(section prSection, empty string) string {
	switch {
	case p.loading && !p.loaded[section]:
		return "Loading..."
	case !p.loaded[section] && p.failed[section] != nil:
		return "Could not load: " + p.failed[section].Error()
	default:
		return empty
	}
}

// commentsView is the Comments sub-tab: the conversation, scrolled.
//
// The row the arrows are on is drawn highlighted here rather than by the render,
// because the focus changes what a row looks like and not what is in it: the
// conversation is cached line by line, and re-rendering every body to move a
// highlight is exactly the cost that cache exists to avoid.
func (p *PRDetails) commentsView(deps Deps, r layout.Rect) string {
	lines := []string{paneTitle(deps.Theme, "COMMENTS", true), ""}
	conv := p.conversation()
	if conv.empty() {
		lines = append(lines, "  "+style(deps.Theme.Muted, normal).Render(p.conversationMessage()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	_, body := r.SplitRows(len(lines))
	content := p.commentLines(deps)
	start := clampOffset(p.commentScroll, len(content))
	// The cached lines are handed to the pane one by one, and the row the focus
	// landed on is drawn as it is handed over. The focus cannot be written into
	// the slice itself: it shares the cache's array, so a marked row would stay
	// marked and the next render would mark it again.
	focus := p.focusedControlRow(start, body.H)
	for i, row := range take(content[start:], body.H) {
		if i == focus {
			row = focusRow(deps.Theme, row)
		}
		lines = append(lines, row)
	}
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// focusedControlRow is the row of the viewport the focused button is on, or -1
// when there is no button or it is scrolled out of sight.
func (p *PRDetails) focusedControlRow(start, height int) int {
	if p.threadFocus < 0 || p.threadFocus >= len(p.threadControls) {
		return -1
	}
	row := p.threadControls[p.threadFocus].row - start
	if row < 0 || row >= height {
		return -1
	}
	return row
}

// focusRow draws the row a control's focus is on: the shared marker in the
// gutter and the row's own text in the accent, the same treatment a selected row
// gets in every list, so a highlight means one thing on every screen.
//
// The marker takes the cells of the indent the row already carries, so a control
// keeps the column it is drawn in when the focus lands on it, and the frame
// characters below it stay in the column the frame draws them in. A washed row
// opens with the escape that starts its paint, ahead of that indent, so the
// indent is cut where it stands and the paint is kept.
func focusRow(theme Theme, row string) string {
	prefix := highlightPrefix(theme)
	row = strings.ReplaceAll(row, ansi.ResetStyle, ansi.ResetStyle+prefix)
	row = strings.TrimSuffix(row, prefix)
	indent := selectMarker + strings.Repeat(" ", max(layout.Width(threadIndent)-layout.Width(selectMarker), 0))
	return prefix + indent + dropIndent(row, threadIndent) + ansi.ResetStyle
}

// dropIndent removes the indent a row opens with. The indent leads the cells of a
// plain row and follows the paint of a washed one, so it is cut where it stands
// rather than from the front of the string. A row that lost its indent already is
// handed back as it is.
func dropIndent(row, indent string) string {
	at := strings.Index(row, indent)
	if at < 0 || layout.Width(row[:at]) > 0 {
		return row
	}
	return row[:at] + row[at+len(indent):]
}

// conversation is the discussion the Comments sub-tab shows: the conversation
// comments, the reviews, and the inline threads the reviews carry, in the order
// they were written.
func (p *PRDetails) conversation() conversation {
	return newConversation(p.comments, p.reviews, p.reviewComments, p.conversationHasMore)
}

// commentLines is the conversation as rendered lines, from the cache when the
// data, the width and the palette are the ones it was rendered at. It is what
// the pane draws and what the scroll is clamped against, so a keystroke renders
// nothing unless something changed.
//
// The buttons the render drew are recorded beside the lines, because the two
// only make sense together: a control's row is a line of the conversation on
// screen. Rendering the same conversation again is what moves them.
func (p *PRDetails) commentLines(deps Deps) []string {
	key := markdownKeyOf(deps.Theme, p.bodyWidth(deps))
	return p.convLines.linesFrom(p.convVersion, key, func() []string {
		lines, controls := conversationLinesOf(deps, p.conversation(), key.width, p.threadView())
		p.threadControls = controls
		return lines
	})
}

// threadView is the resolution state the conversation is rendered with.
func (p *PRDetails) threadView() threadView {
	return threadView{resolved: p.threadsResolved, expanded: p.threadsExpanded}
}

// commentViewport is how many conversation rows the pane shows, derived the way
// commentsView derives it: the terminal less the chrome, the pull request's own
// heading and sub-tab strip, and the pane's title with the blank under it. The
// focus needs it outside a render, to scroll a control into view.
func (p *PRDetails) commentViewport(deps Deps) int {
	height := deps.Size.Height
	if height <= 0 {
		return defaultCommentViewport
	}
	body := height - layout.HeaderHeight - layout.FooterHeight - prHeadRows - prPaneRows
	return max(body, 1)
}

// conversationMessage is what the Comments sub-tab says when it holds no
// entries. The tab reads three lists, so it reports the first one that is still
// loading or failed, and says the discussion is empty only once all three
// arrived.
func (p *PRDetails) conversationMessage() string {
	for _, section := range conversationSections {
		if !p.loaded[section] {
			return p.loadMessage(section, "Loading...")
		}
	}
	return "No comments or reviews"
}

// commentBlockLines renders a conversation as lines, one block per comment.
// Every body is rendered as markdown, so a conversation reads the way its author
// wrote it, and the issue screen and the pull request's standalone comments both
// read it.
func commentBlockLines(deps Deps, comments []store.IssueComment, width int) []string {
	var out []string
	for i, comment := range comments {
		if i > 0 {
			out = append(out, "", style(deps.Theme.HeaderRule, normal).Render(strings.Repeat("─", max(cardBox(0, width), 1))), "")
		}
		out = append(out, commentBlock(deps, comment, width)...)
	}
	return out
}

// commentBlock renders one comment: who wrote it, when, and the body as
// markdown.
func commentBlock(deps Deps, comment store.IssueComment, width int) []string {
	author := userLogin(comment.User)
	box := cardBox(0, width)
	head := authorLine(deps, author, comment.HTMLURL, comment.CreatedAt, "")
	return cardLines(deps, author, box, head, "",
		bodyLines(deps, comment.Body, cardIndent, box-cardPadding))
}

// userLogin is the login of the user who wrote something, with a placeholder
// when the API omitted it.
func userLogin(user *store.User) string {
	if user != nil && user.Login != "" {
		return user.Login
	}
	return "unknown"
}

// CommitDetails is one commit: its message, the files it changed, and the diff
// of the selected one.
type CommitDetails struct {
	ref    store.RepoRef
	commit store.RepositoryCommit
	files  []store.CommitFile

	loading bool
	err     error

	fileCursor int
	diffScroll int

	sess *session
}

// commitMsg carries a fetched commit.
type commitMsg struct {
	sha   string
	files []store.CommitFile
	err   error
}

// newCommitDetails returns the Commit Details route for a commit the commit
// list already has, and the fetch of the files it changed.
func newCommitDetails(deps Deps, ref store.RepoRef, commit store.RepositoryCommit) (*CommitDetails, tea.Cmd) {
	c := &CommitDetails{ref: ref, commit: commit, loading: true, sess: newSession(deps.Ctx)}
	return c, c.load(deps)
}

// load fetches the commit and the files it changed.
func (c *CommitDetails) load(deps Deps) tea.Cmd {
	ref, sha := c.ref, c.commit.SHA
	ctx := c.sess.Ctx()
	return func() tea.Msg {
		_, files, err := deps.Store.Commit(ctx, ref, sha)
		return commitMsg{sha: sha, files: files, err: err}
	}
}

// Title is the breadcrumb label.
func (c *CommitDetails) Title() string { return "Commit " + shortSHA(c.commit.SHA) }

// Scope is the keymap scope for one commit.
func (c *CommitDetails) Scope() keymap.Scope { return keymap.CommitDetails }

// Stop releases the requests this route has in flight.
func (c *CommitDetails) Stop() { c.sess.Stop() }

// Loading reports a fetch this route is waiting on.
func (c *CommitDetails) Loading() bool { return c.loading }

// Update handles the messages this route owns.
func (c *CommitDetails) Update(deps Deps, msg tea.Msg) (Route, tea.Cmd) {
	switch msg := msg.(type) {
	case commitMsg:
		// A reply for a commit the user has already left is dropped.
		if msg.sha != c.commit.SHA {
			return c, nil
		}
		c.loading = false
		if msg.err != nil {
			c.err = msg.err
			return c, statusErr("Could not load the commit: " + msg.err.Error())
		}
		c.err = nil
		c.files = msg.files
		c.clamp()
		return c, nil
	}
	return c, nil
}

// Act handles a keymap action.
func (c *CommitDetails) Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionBack:
		return c, popCmd(), true
	case keymap.ActionDown:
		c.fileCursor++
		c.diffScroll = 0
		c.clamp()
		return c, nil, true
	case keymap.ActionUp:
		c.fileCursor--
		c.diffScroll = 0
		c.clamp()
		return c, nil, true
	case keymap.ActionScrollUp:
		c.diffScroll -= diffStep
		c.clamp()
		return c, nil, true
	case keymap.ActionScrollDown:
		c.diffScroll += diffStep
		c.clamp()
		return c, nil, true
	case keymap.ActionOpenBrowser:
		url := c.commit.HTMLURL
		if url == "" {
			url = commitURL(c.ref, c.commit.SHA)
		}
		return c, browserCmd(url, "Opened the commit in the browser."), true
	}
	return c, nil, false
}

// clamp keeps the file selection and the scroll offset inside the content.
func (c *CommitDetails) clamp() {
	c.fileCursor = clampIndex(c.fileCursor, len(c.files))
	c.diffScroll = clampOffset(c.diffScroll, len(diffLines(c.diffText())))
}

// diffText is the patch on screen.
func (c *CommitDetails) diffText() string {
	if c.fileCursor >= 0 && c.fileCursor < len(c.files) {
		return c.files[c.fileCursor].Patch
	}
	return ""
}

// commitURL is a commit's page, constructed when the API did not supply one.
func commitURL(ref store.RepoRef, sha string) string {
	if !ref.Valid() || sha == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/commit/%s", ref.String(), sha)
}

// View renders the commit's identity, its changed files, and the patch of the
// selected one.
func (c *CommitDetails) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	theme := deps.Theme
	lines := []string{
		"  " + style(theme.Title, bold).Render(layout.Truncate(commitSubject(c.commit), max(r.W-14, 8))),
		"  " + style(theme.Muted, normal).Render(fmt.Sprintf("%s | %s | %s",
			shortSHA(c.commit.SHA), commitAuthor(c.commit), formatTime(c.commit.Commit.Author.Date))),
		"  " + style(theme.HeaderRule, normal).Render(strings.Repeat("─", max(r.W-layout.Gutter, 1))),
		"",
	}
	if c.err != nil {
		lines = append(lines, "  "+style(theme.Muted, normal).Render("Could not load the commit: "+c.err.Error()))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}
	if len(c.files) == 0 {
		message := "No changed files"
		if c.loading {
			message = "Loading commit..."
		}
		lines = append(lines, "  "+style(theme.Muted, normal).Render(message))
		return layout.Fit(joinLines(lines), r.W, r.H)
	}

	_, body := r.SplitRows(len(lines))
	// The message and the file panes are one block, so they are shifted into the
	// content box together; the heading above carries the same gutter.
	inner := paneRect(body)
	// The commit message is as long as its author made it, so it takes the rows
	// it needs up to a fair share of the region and the files keep the rest.
	message := c.messageLines(deps, inner.W)
	msgRect, rest := inner.SplitRowsFair(len(message))
	msg := layout.Fit(joinLines(message), msgRect.W, msgRect.H)

	fileW, diffW := fileDiffColumns(rest.W)
	list := commitFileList(deps, c.files, c.fileCursor, fileW, rest.H)
	patch := diffBody(deps, c.files[c.fileCursor], diffW, rest.H, c.diffScroll)

	columns := [][]string{padLines(list, fileW, rest.H), padLines(patch, diffW, rest.H)}
	separator := style(theme.HeaderRule, normal).Render("│")
	rows := make([]string, 0, rest.H)
	for row := 0; row < rest.H; row++ {
		rows = append(rows, columns[0][row]+separator+columns[1][row])
	}
	lines = append(lines, gutterRows(msg+"\n"+joinLines(rows), body.W, body.H)...)
	return layout.Fit(joinLines(lines), r.W, r.H)
}

// commitFileList renders a changed-file list with its selection, shared by the
// commit view and the pull request Files sub-tab.
func commitFileList(deps Deps, files []store.CommitFile, cursor, width, height int) []string {
	theme := deps.Theme
	cols := fileColumns(width)
	lines := []string{style(theme.Muted, bold).Render(tableHeaderRow(cols))}
	visible := max(height-len(lines), 1)
	start, end := layout.Window(len(files), cursor, visible)
	for i := start; i < end; i++ {
		lines = append(lines, fileRow(theme, files[i], cols, width, i == cursor))
	}
	return lines
}

// diffBody renders a file's patch with its name above it, scrolled.
func diffBody(deps Deps, file store.CommitFile, width, height, scroll int) []string {
	header := style(deps.Theme.Title, bold).Render(layout.Truncate(file.Filename, width))
	lines := diffLines(file.Patch)
	if len(lines) == 0 {
		return []string{header, "", style(deps.Theme.Muted, normal).Render("  No textual diff (binary or too large).")}
	}
	start := clampOffset(scroll, len(lines))
	return append([]string{header, ""}, take(diffStyled(deps.Theme, lines[start:]), max(height-2, 0))...)
}
