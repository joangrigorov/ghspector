package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/store"
)

// conversation is a pull request's discussion, assembled from the three lists
// the API keeps it in: conversation comments, reviews, and the inline comments
// those reviews carry.
//
// A review-heavy pull request has nothing in the first list and everything in
// the other two, which is why the Comments sub-tab reads all three. The
// conversation GitHub shows is the three merged in the order they were written,
// with each review's inline threads under the review they belong to.
type conversation struct {
	entries []conversationEntry
	// truncated says one of the three pages came back full, so what is on
	// screen is part of a longer discussion rather than all of it.
	truncated bool
}

// conversationEntry is one contribution: a comment, or a review with the threads
// written under it.
type conversationEntry struct {
	at      time.Time
	comment *store.IssueComment
	review  *store.Review
	threads []reviewThread
}

// reviewThread is one exchange of inline comments: where it was written, the
// lines it was written against, and the remarks themselves in the order they
// were made.
type reviewThread struct {
	// review is the review that carried the comment the thread started from,
	// which is where the thread is drawn. A reply carries the review that
	// posted it, so the two are usually different.
	review   int64
	path     string
	line     int
	hunk     []string
	comments []store.ReviewComment
}

// first is the remark the thread is placed by: the comment it started from, or
// the earliest reply when the root is not on the page.
func (t reviewThread) first() store.ReviewComment {
	if len(t.comments) == 0 {
		return store.ReviewComment{}
	}
	return t.comments[0]
}

// rootID is the remark the thread is placed by, as an id. It is what a reader's
// expansion of a folded thread is remembered against, so opening a thread
// survives a re-render.
func (t reviewThread) rootID() int64 { return t.first().ID }

// threadView is what the conversation needs to know about resolution: which
// remarks belong to a thread the schema reports as resolved, and which resolved
// threads the reader has opened.
//
// The resolution is keyed by remark rather than by thread because the
// conversation groups the remarks itself, from the REST payload, and a thread
// whose root is not on the page is placed by the earliest reply it holds. Naming
// every remark of a resolved thread answers "is this thread resolved" the same
// way however the page was cut. The focused control is deliberately not part of
// this: moving the focus changes what a row looks like rather than what is in
// it, and re-rendering every body to move a highlight is the cost the rendered
// lines are cached to avoid.
type threadView struct {
	// resolved names the remarks that belong to a resolved thread.
	resolved map[int64]bool
	// expanded names the resolved threads the reader has opened, by rootID.
	expanded map[int64]bool
}

// isResolved reports whether the schema placed any of the thread's remarks in a
// resolved thread. A thread the answer does not name, which is every thread when
// the query has not answered at all, is not resolved.
func (v threadView) isResolved(thread reviewThread) bool {
	for _, comment := range thread.comments {
		if v.resolved[comment.ID] {
			return true
		}
	}
	return false
}

// isFolded reports whether a thread is drawn as its button row alone: it is
// resolved, and the reader has not opened it.
func (v threadView) isFolded(thread reviewThread) bool {
	return v.isResolved(thread) && !v.expanded[thread.rootID()]
}

// threadControl is one control of a conversation: the thread its button folds,
// and the row it was drawn on. The rows come from the render, so they always
// match the lines on screen, and the thread is what a toggle acts on.
type threadControl struct {
	thread int64
	row    int
}

// The two labels of the control that folds a resolved thread away and opens it
// again. They are named here because the rendered row, the help text and the
// tests all say the same two words.
const (
	threadShowLabel = "[show]"
	threadHideLabel = "[hide]"
)

// threadIndent is the indent an inline thread is drawn at, inside the indent a
// top-level contribution uses, because a thread is part of the review above it.
const threadIndent = "    "

// cardIndent is how far a contribution indents its own text: a heading, the row of
// air under it, and a body all start here, and it is the column a comment's wash
// begins at.
const cardIndent = "  "

// contentRight is the last cell a route's content reaches. A route is handed the
// screen less the gutter on its right, so a card's box stops on the route's last
// usable cell and the margin past it keeps the terminal's own colour.
func contentRight(width int) int { return width - 1 }

// hunkTail is how much of a diff hunk is shown above an inline comment. GitHub
// sends a few lines of context around the commented line, and the tail of that
// is what a remark like "this should be a const" needs to mean anything; six
// lines give the frame something to hold without pushing the thread off screen.
const hunkTail = 6

// truncatedNote is what the pane says when a page came back full.
const truncatedNote = "The API returned a full page; the rest is on GitHub (w opens it)."

// newConversation merges the three lists into reading order.
//
// A pending review is dropped: it is a draft its author has not submitted, it
// has no submission time to place it by, and there is nothing here that could
// submit it. The inline comments it carries are drafts too, so they go with it
// rather than standing in as orphans.
func newConversation(comments []store.IssueComment, reviews []store.Review, reviewComments []store.ReviewComment, truncated bool) conversation {
	byID := make(map[int64]store.Review, len(reviews))
	drafts := make(map[int64]bool, len(reviews))
	for _, review := range reviews {
		byID[review.ID] = review
		if isPendingReview(review) {
			drafts[review.ID] = true
		}
	}

	submitted := make([]store.ReviewComment, 0, len(reviewComments))
	for _, comment := range reviewComments {
		if drafts[comment.PullRequestReviewID] {
			continue
		}
		submitted = append(submitted, comment)
	}

	// Threads belong to the review their first comment came from; a thread
	// whose review is not in the page — it was submitted after the page was
	// cut, or the token cannot see the review — is still something somebody
	// wrote, so it is drawn on its own rather than dropped.
	owned := make(map[int64][]reviewThread, len(reviews))
	var loose []reviewThread
	for _, thread := range groupThreads(submitted) {
		if _, ok := byID[thread.review]; !ok {
			loose = append(loose, thread)
			continue
		}
		owned[thread.review] = append(owned[thread.review], thread)
	}

	conv := conversation{truncated: truncated}
	for i := range comments {
		comment := comments[i]
		conv.entries = append(conv.entries, conversationEntry{at: comment.CreatedAt, comment: &comment})
	}
	for i := range reviews {
		review := reviews[i]
		if isPendingReview(review) {
			continue
		}
		conv.entries = append(conv.entries, conversationEntry{
			at:      reviewTime(review),
			review:  &review,
			threads: owned[review.ID],
		})
	}
	for _, thread := range loose {
		conv.entries = append(conv.entries, conversationEntry{at: threadTime(thread), threads: []reviewThread{thread}})
	}
	sortByTime(conv.entries)
	return conv
}

// empty reports whether the conversation holds nothing to draw.
func (c conversation) empty() bool { return len(c.entries) == 0 }

// isPendingReview reports a review its author has not submitted yet.
func isPendingReview(review store.Review) bool {
	return review.State == "PENDING" || review.SubmittedAt == nil
}

// reviewTime is when a review entered the conversation.
func reviewTime(review store.Review) time.Time {
	if review.SubmittedAt == nil {
		return time.Time{}
	}
	return *review.SubmittedAt
}

// threadTime is when a thread's first comment was written.
func threadTime(thread reviewThread) time.Time {
	if len(thread.comments) == 0 {
		return time.Time{}
	}
	return thread.comments[0].CreatedAt
}

// sortByTime puts the entries in the order they happened. An entry with no
// timestamp sorts last rather than dragging the conversation to the top of the
// pane, which an unknown time would otherwise do.
func sortByTime(entries []conversationEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].at, entries[j].at
		if a.IsZero() {
			return false
		}
		if b.IsZero() {
			return true
		}
		return a.Before(b)
	})
}

// groupThreads gathers the inline comments into the exchanges they were written
// in, in the order the threads were opened.
//
// A thread is identified by the comment it started from, not by the review that
// carried each remark. GitHub posts a reply as part of a *new* review, so the
// reply names the review that posted it while in_reply_to_id names the comment
// it answers; on the pull request this was reported from, all 25 replies arrived
// in a later review than the comment they answer and 13 of the 16 threads
// spanned more than one review. Grouping one review at a time therefore drew
// those 41 comments as 41 threads, each with its own location and hunk excerpt.
//
// A reply whose root is not in the page — the page was cut, or the token cannot
// see the comment — is gathered under the comment it names, so an exchange stays
// together even when what it started from is missing.
func groupThreads(comments []store.ReviewComment) []reviewThread {
	byID := make(map[int64]store.ReviewComment, len(comments))
	for _, comment := range comments {
		byID[comment.ID] = comment
	}

	grouped := make(map[int64][]store.ReviewComment, len(comments))
	var roots []int64
	for _, comment := range comments {
		root := threadRoot(comment, byID)
		if _, ok := grouped[root]; !ok {
			roots = append(roots, root)
		}
		grouped[root] = append(grouped[root], comment)
	}

	threads := make([]reviewThread, 0, len(roots))
	for _, root := range roots {
		exchange := grouped[root]
		sortCommentsByTime(exchange)
		// The comment the thread started from names where it is; a reply says
		// where it was written, which is not the same place once the lines have
		// moved.
		head := exchange[0]
		if first, ok := byID[root]; ok {
			head = first
		}
		threads = append(threads, reviewThread{
			review:   head.PullRequestReviewID,
			path:     head.Path,
			line:     commentLocation(head),
			hunk:     hunkLines(head.DiffHunk),
			comments: exchange,
		})
	}
	sortThreadsByTime(threads)
	return threads
}

// threadRoot is the comment an exchange started from: the ancestor of a reply,
// or the reply's own parent when that comment is not in the page, so replies to
// the same missing comment still read as one exchange.
func threadRoot(comment store.ReviewComment, byID map[int64]store.ReviewComment) int64 {
	root := comment
	for step := 0; step < len(byID); step++ {
		if root.InReplyToID == 0 {
			return root.ID
		}
		parent, ok := byID[root.InReplyToID]
		if !ok {
			// The comment being answered is not on the page, so the answer is
			// as close to the root as this page can get.
			return root.InReplyToID
		}
		root = parent
	}
	// A page of comments that answers its own ancestors in a cycle is not
	// something the API produces; stopping here keeps the walk bounded.
	return root.ID
}

// sortCommentsByTime puts an exchange in the order it was written.
func sortCommentsByTime(comments []store.ReviewComment) {
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
}

// sortThreadsByTime puts the threads of one review in the order they were
// opened, with a thread of unknown time last.
func sortThreadsByTime(threads []reviewThread) {
	sort.SliceStable(threads, func(i, j int) bool {
		a, b := threadTime(threads[i]), threadTime(threads[j])
		if a.IsZero() {
			return false
		}
		if b.IsZero() {
			return true
		}
		return a.Before(b)
	})
}

// commentLocation is the line a comment is attached to. GitHub reports the line
// as zero once the diff has moved past it, and keeps the original then, which is
// where the comment still lives.
func commentLocation(comment store.ReviewComment) int {
	if comment.Line > 0 {
		return comment.Line
	}
	return comment.OriginalLine
}

// hunkLines is the tail of a diff hunk, which is the part just above the line
// the comment was written against. The hunk header is dropped when it is not in
// the tail: it names offsets, and the comment is what the reader is here for.
func hunkLines(hunk string) []string {
	if strings.TrimSpace(hunk) == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(hunk, "\r\n", "\n"), "\n"), "\n")
	if len(lines) > hunkTail {
		lines = lines[len(lines)-hunkTail:]
	}
	if len(lines) == 1 && strings.HasPrefix(lines[0], "@@") {
		return nil
	}
	return lines
}

// conversationLines renders the conversation: one block per contribution, a rule
// between them, and the bodies as markdown, so a discussion reads the way its
// authors wrote it. It is the render for a conversation whose threads are all
// drawn in full, which is what a page with no resolution data shows.
func conversationLines(deps Deps, conv conversation, width int) []string {
	lines, _ := conversationLinesOf(deps, conv, width, threadView{})
	return lines
}

// conversationLinesOf renders the conversation and reports the controls it drew,
// so the pane can put the focus on one of them.
func conversationLinesOf(deps Deps, conv conversation, width int, view threadView) ([]string, []threadControl) {
	var (
		out      []string
		controls []threadControl
	)
	for i, entry := range conv.entries {
		if i > 0 {
			out = append(out, "", style(deps.Theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-2, 1))), "")
		}
		out, controls = appendEntry(out, controls, deps, entry, width, view)
	}
	if conv.truncated {
		out = append(out, "", "  "+style(deps.Theme.Muted, normal).Render(truncatedNote))
	}
	return out, controls
}

// appendEntry draws one contribution onto the conversation and records where its
// controls landed, offset by what is already drawn.
func appendEntry(out []string, controls []threadControl, deps Deps, entry conversationEntry, width int, view threadView) ([]string, []threadControl) {
	switch {
	case entry.comment != nil:
		return append(out, commentBlock(deps, *entry.comment, width)...), controls
	case entry.review != nil:
		return appendReview(out, controls, deps, *entry.review, entry.threads, width, view)
	default:
		return appendThreads(out, controls, deps, entry.threads, width, view)
	}
}

// appendThreads draws each thread of an entry and records the rows its buttons
// landed on. Offset by the lines already drawn, those rows are the lines of the
// conversation the controls sit on.
func appendThreads(out []string, controls []threadControl, deps Deps, threads []reviewThread, width int, view threadView) ([]string, []threadControl) {
	for _, thread := range threads {
		lines, drawn := threadLines(deps, thread, width, view)
		controls = offsetControls(drawn, len(out), controls)
		out = append(out, lines...)
	}
	return out, controls
}

// offsetControls moves the rows a block reported onto the conversation.
func offsetControls(drawn []threadControl, at int, controls []threadControl) []threadControl {
	for _, control := range drawn {
		control.row += at
		controls = append(controls, control)
	}
	return controls
}

// appendReview draws a review: who wrote it, what they concluded, the summary
// they left, and the threads they wrote under it, and records where those
// threads' controls landed.
func appendReview(out []string, controls []threadControl, deps Deps, review store.Review, threads []reviewThread, width int, view threadView) ([]string, []threadControl) {
	author := userLogin(review.User)
	head := authorLine(deps, author, review.HTMLURL, reviewTime(review), reviewStateLabel(deps.Theme, review))
	out = append(out, cardLines(deps, author, cardIndent, head, "",
		bodyLines(deps, review.Body, cardIndent, width), contentRight(width))...)
	return appendThreads(out, controls, deps, threads, width, view)
}

// The frame an inline thread is drawn in.
//
// A thread is a titled container: the title is where it was written, the content
// runs down a left rail, and a reply is a frame of its own nested one level
// inside that rail, so an answer reads as belonging to the remark above it. The
// frames are box characters and one colour, which is what lets them survive on a
// terminal that paints nothing at all.
const (
	frameTop   = "┌─ "
	frameRail  = "│  "
	frameClose = "└─"
	// frameBar is the rail on its own, drawn between two segments of a thread.
	frameBar = "│"
	// frameCells is the width frameTop and frameRail occupy, and therefore what
	// the content inside a frame loses to it.
	frameCells = 3

	// rootMark sits on the remark a thread started from. A reply carries no mark
	// of its own: the frame it is drawn in says what it is.
	rootMark = "●"
)

// threadLines renders one inline thread: the lines it was written against, then
// the exchange itself, inside a frame titled by where the thread is. The title
// is a link to the thread on GitHub, which is what a reader wants from it: the
// remark, in the diff it was written against.
//
// A resolved thread is drawn as one row with a `[show]` button until the reader
// opens it, which is what keeps a settled conversation out of the way of the
// remarks still being made. The controls returned are the rows of this block that
// carry a button, which is what the arrows move between.
func threadLines(deps Deps, thread reviewThread, width int, view threadView) ([]string, []threadControl) {
	if view.isFolded(thread) {
		return foldedThreadLines(deps, thread, width)
	}
	theme := deps.Theme
	rule := style(theme.HeaderRule, normal)

	place := threadPlace(theme, thread)

	// A thread the schema reports as resolved carries the button that folds it
	// away again, and the location gives up the cells it takes.
	button := ""
	if view.isResolved(thread) {
		button = "  " + threadButton(theme, threadHideLabel)
	}

	titleWidth := max(width-len(threadIndent)-frameCells-layout.Width(button), 8)
	inner := threadIndent + rule.Render(frameRail)
	innerWidth := max(width-layout.Width(inner)-frameCells, 8)

	out := []string{
		"",
		threadIndent + rule.Render(frameTop) + layout.Truncate(place, titleWidth) + button,
	}
	var controls []threadControl
	if button != "" {
		controls = append(controls, threadControl{thread: thread.rootID(), row: 1})
	}

	if len(thread.hunk) > 0 {
		out = append(out, snippetLines(deps, thread.hunk, inner, innerWidth)...)
		out = append(out, threadIndent+rule.Render(frameBar))
	}

	for i := range thread.comments {
		comment := thread.comments[i]
		if i > 0 {
			out = append(out, threadIndent+rule.Render(frameBar))
			out = append(out, replyCard(deps, comment, inner, width)...)
			continue
		}
		out = append(out, rootCard(deps, comment, inner, innerWidth, width)...)
	}

	return append(out, threadIndent+rule.Render(frameClose)), controls
}

// foldedThreadLines draws a resolved thread as a single row: the button that
// opens it, then what the thread is — where it was written, who wrote it first,
// how much of it there is — and the state that folded it away. The button leads
// the row, so the controls of a conversation line up in one column.
//
// The row is a summary rather than a comment, so it carries no wash: opening the
// thread puts its remark's body back in its author's shade.
func foldedThreadLines(deps Deps, thread reviewThread, width int) ([]string, []threadControl) {
	theme := deps.Theme
	button := threadButton(theme, threadShowLabel)
	summary := threadSummary(deps, thread)
	room := max(width-len(threadIndent)-layout.Width(button)-1, 8)

	row := threadIndent + button + " " + layout.Truncate(summary, room)
	return []string{"", row}, []threadControl{{thread: thread.rootID(), row: 1}}
}

// threadPlace is where a thread was written, as the frame's title and a folded
// row both name it: the path and line, linked to the thread on GitHub.
func threadPlace(theme Theme, thread reviewThread) string {
	place := thread.path
	if thread.line > 0 {
		place = fmt.Sprintf("%s:%d", thread.path, thread.line)
	}
	return hyperlink(theme, style(theme.Muted, bold).Render(place), threadURL(thread))
}

// threadSummary is what a folded thread says about itself: where it was written,
// who opened it, how many remarks it holds, and the state that folded it.
func threadSummary(deps Deps, thread reviewThread) string {
	theme := deps.Theme
	parts := []string{threadPlace(theme, thread)}
	if author := userLogin(thread.first().User); author != "" {
		parts = append(parts, style(theme.Muted, normal).Render(author))
	}
	parts = append(parts, style(theme.Muted, normal).Render(remarkCount(len(thread.comments))))
	parts = append(parts, style(theme.StatusOK, normal).Render("resolved"))
	return strings.Join(parts, style(theme.Muted, normal).Render(" · "))
}

// remarkCount names how many remarks a folded thread holds.
func remarkCount(n int) string {
	if n == 1 {
		return "1 remark"
	}
	return strconv.Itoa(n) + " remarks"
}

// threadButton is the control that folds a resolved thread away and opens it
// again. It is the one thing on its row that does something, so it carries the
// accent; which of the controls holds the keyboard is the row highlight the rest
// of the app uses.
func threadButton(theme Theme, label string) string {
	return style(theme.Accent, normal).Render(label)
}

// rootCard draws the remark a thread started from: its heading, marked so the
// eye finds where the thread began, its body, and the rule that closes it. The
// body wears its author's wash, and an answer follows the rule.
func rootCard(deps Deps, comment store.ReviewComment, indent string, innerWidth, width int) []string {
	theme := deps.Theme
	author := userLogin(comment.User)
	text := indent + cardIndent

	head := indent + layout.Truncate(style(theme.Accent, normal).Render(rootMark)+" "+
		authorHeading(deps, author, comment.HTMLURL, comment.CreatedAt, ""), innerWidth)
	out := cardLines(deps, author, text, head, indent, bodyLines(deps, comment.Body, text, width), contentRight(width))

	foot := indent + style(theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-layout.Width(indent), 1)))
	return append(out, foot)
}

// replyCard draws one answer as a frame of its own, nested inside the thread's
// rail and titled by who wrote it. Its foot is a rule that doubles as the
// horizontal line under the remark. The body wears its author's wash.
func replyCard(deps Deps, comment store.ReviewComment, indent string, width int) []string {
	theme := deps.Theme
	rule := style(theme.HeaderRule, normal)
	author := userLogin(comment.User)
	text := indent + rule.Render(frameRail) + cardIndent

	head := indent + rule.Render(frameTop) + layout.Truncate(
		authorHeading(deps, author, comment.HTMLURL, comment.CreatedAt, ""),
		max(width-layout.Width(indent)-frameCells, 8))

	out := cardLines(deps, author, text, head, indent+rule.Render(frameRail),
		bodyLines(deps, comment.Body, text, width), contentRight(width))
	foot := indent + rule.Render(frameClose+strings.Repeat("─", max(width-layout.Width(indent)-2, 1)))
	return append(out, foot)
}

// cardLines draws one contribution: its heading, a row of air, then its body.
//
// Only the body is washed, and the wash is the box the comment's own text
// occupies: from textPrefix, the column the card puts its text at, out to the end
// of the block's longest line. The heading that names the author, the row of air
// under it and the rules around it stay plain, as does the pane's content beyond
// the box, so a wash on this screen marks a comment's text and nothing else. A
// palette with no washes hands the lines back as they are, so a plain render gains
// the spacing and nothing else.
//
// limit is the pane's own edge: the box never reaches past it.
func cardLines(deps Deps, login, textPrefix, head, air string, body []string, limit int) []string {
	out := []string{head}
	if len(body) == 0 {
		return out
	}
	out = append(out, air)
	return append(out, bodyBox(deps, login, layout.Width(textPrefix), limit, body)...)
}

// bodyBox paints a comment's body in its author's shade. The wash is the box the
// text occupies, and it is the same box on every line of the block: it opens at
// the column the text starts in and ends on the last cell of the block's longest
// line, so a line shorter than the box — the last line of a paragraph, the blank
// line between two of them — is carried out to it rather than left with a gap
// between two lines that wear one colour.
func bodyBox(deps Deps, login string, textLeft, limit int, body []string) []string {
	right := textLeft - 1
	for _, line := range body {
		if text := strings.TrimRight(ansi.Strip(line), " "); text != "" {
			right = min(limit, max(right, ansi.StringWidth(text)-1))
		}
	}
	tint := authorTint(deps.Theme, login)
	out := make([]string, 0, len(body))
	for _, line := range body {
		out = append(out, paintSpan(tint, textLeft, right, line))
	}
	return out
}

// snippetLines renders the lines a remark was written against as a small frame
// of its own, so an excerpt of code is told apart from prose at a glance. The
// first line rides in the frame's corner, which saves the excerpt a row of its
// own.
func snippetLines(deps Deps, hunk []string, indent string, inner int) []string {
	rule := style(deps.Theme.HeaderRule, normal)
	styled := diffStyled(deps.Theme, hunk)
	out := make([]string, 0, len(styled)+1)
	for i, line := range styled {
		if i == 0 {
			out = append(out, indent+rule.Render(frameTop)+layout.Truncate(line, inner))
			continue
		}
		out = append(out, indent+rule.Render(frameRail)+layout.Truncate(line, inner))
	}
	return append(out, indent+rule.Render(frameClose))
}

// bodyLines renders a contribution's markdown under the prefix its frame puts it
// at, wrapped to what the pane has left. An empty body is no lines at all, so a
// card of a heading only has no air under it.
func bodyLines(deps Deps, body, prefix string, width int) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var out []string
	for _, line := range deps.Markdown.Lines(deps.Theme, body, max(width-layout.Width(prefix), 8)) {
		out = append(out, prefix+line)
	}
	return out
}

// authorLine is the heading of a contribution: who wrote it, the conclusion a
// review came to when there is one, and when it arrived. Both the name and the
// time are links to the contribution when the API supplied a URL for it, so the
// whole of what names it opens it.
func authorLine(deps Deps, author, url string, at time.Time, state string) string {
	return cardIndent + authorHeading(deps, author, url, at, state)
}

// authorHeading is authorLine without its indent, so a frame can title itself
// with the same heading.
func authorHeading(deps Deps, author, url string, at time.Time, state string) string {
	name := hyperlink(deps.Theme, style(deps.Theme.Title, bold).Render(author), url)
	out := name
	if state != "" {
		out += "  " + state
	}
	stamp := style(deps.Theme.Muted, normal).Render("  " + formatTime(at))
	return out + hyperlink(deps.Theme, stamp, url)
}

// reviewStateLabel is a review's conclusion in the badge vocabulary the rest of
// the app uses for a state that matters: approved, changes requested, or a
// reviewer who left comments without a verdict.
func reviewStateLabel(theme Theme, review store.Review) string {
	switch review.State {
	case "APPROVED":
		return style(theme.StatusOK, bold).Render("[approved]")
	case "CHANGES_REQUESTED":
		return style(theme.StatusFail, bold).Render("[changes requested]")
	case "COMMENTED":
		return style(theme.Muted, bold).Render("[reviewed]")
	case "":
		return ""
	}
	return style(theme.Muted, bold).Render("[" + strings.ToLower(review.State) + "]")
}
