package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

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

// threadIndent is the indent an inline thread is drawn at, inside the indent a
// top-level contribution uses, because a thread is part of the review above it.
const threadIndent = "    "

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
// authors wrote it.
func conversationLines(deps Deps, conv conversation, width int) []string {
	var out []string
	for i, entry := range conv.entries {
		if i > 0 {
			out = append(out, "", style(deps.Theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-2, 1))), "")
		}
		out = append(out, entryLines(deps, entry, width)...)
	}
	if conv.truncated {
		out = append(out, "", "  "+style(deps.Theme.Muted, normal).Render(truncatedNote))
	}
	return out
}

// entryLines renders one contribution.
func entryLines(deps Deps, entry conversationEntry, width int) []string {
	switch {
	case entry.comment != nil:
		return commentBlock(deps, *entry.comment, width)
	case entry.review != nil:
		return reviewLines(deps, *entry.review, entry.threads, width)
	default:
		var out []string
		for _, thread := range entry.threads {
			out = append(out, threadLines(deps, thread, width)...)
		}
		return out
	}
}

// reviewLines renders a review: who wrote it, what they concluded, the summary
// they left, and the threads they wrote under it.
func reviewLines(deps Deps, review store.Review, threads []reviewThread, width int) []string {
	author := userLogin(review.User)
	head := authorLine(deps, author, review.HTMLURL, reviewTime(review), reviewStateLabel(deps.Theme, review))
	out := cardLines(deps, author, width, head, "", bodyLines(deps, review.Body, "  ", width))
	for _, thread := range threads {
		out = append(out, threadLines(deps, thread, width)...)
	}
	return out
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
func threadLines(deps Deps, thread reviewThread, width int) []string {
	theme := deps.Theme
	rule := style(theme.HeaderRule, normal)

	place := thread.path
	if thread.line > 0 {
		place = fmt.Sprintf("%s:%d", thread.path, thread.line)
	}
	place = hyperlink(theme, style(theme.Muted, bold).Render(place), threadURL(thread))

	titleWidth := max(width-len(threadIndent)-frameCells, 8)
	inner := threadIndent + rule.Render(frameRail)
	innerWidth := max(width-layout.Width(inner)-frameCells, 8)

	out := []string{
		"",
		threadIndent + rule.Render(frameTop) + layout.Truncate(place, titleWidth),
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

	return append(out, threadIndent+rule.Render(frameClose))
}

// rootCard draws the remark a thread started from: its heading, marked so the
// eye finds where the thread began, its body, and the rule that closes it. The
// card wears its author's wash, and an answer follows the rule.
func rootCard(deps Deps, comment store.ReviewComment, indent string, innerWidth, width int) []string {
	theme := deps.Theme
	author := userLogin(comment.User)

	head := indent + layout.Truncate(style(theme.Accent, normal).Render(rootMark)+" "+
		authorHeading(deps, author, comment.HTMLURL, comment.CreatedAt, ""), innerWidth)
	out := cardLines(deps, author, width, head, indent, bodyLines(deps, comment.Body, indent+"  ", width))

	foot := indent + style(theme.HeaderRule, normal).Render(strings.Repeat("─", max(width-layout.Width(indent), 1)))
	return append(out, paintLine(authorTint(theme, author), width, foot))
}

// replyCard draws one answer as a frame of its own, nested inside the thread's
// rail and titled by who wrote it. Its foot is a rule that doubles as the
// horizontal line under the remark. The card wears its author's wash.
func replyCard(deps Deps, comment store.ReviewComment, indent string, width int) []string {
	theme := deps.Theme
	rule := style(theme.HeaderRule, normal)
	author := userLogin(comment.User)
	tint := authorTint(theme, author)

	head := indent + rule.Render(frameTop) + layout.Truncate(
		authorHeading(deps, author, comment.HTMLURL, comment.CreatedAt, ""),
		max(width-layout.Width(indent)-frameCells, 8))

	out := cardLines(deps, author, width, head, indent+rule.Render(frameRail),
		bodyLines(deps, comment.Body, indent+rule.Render(frameRail)+"  ", width))
	foot := indent + rule.Render(frameClose+strings.Repeat("─", max(width-layout.Width(indent)-2, 1)))
	return append(out, paintLine(tint, width, foot))
}

// cardLines draws one contribution as a wash in its author's shade: the heading,
// a row of air, then the body. The air row carries the rail the contribution is
// drawn inside, so a framed card does not break its own frame. A palette with no
// washes hands the lines back as they are, so a plain render gains the spacing
// and nothing else.
func cardLines(deps Deps, login string, width int, head, air string, body []string) []string {
	tint := authorTint(deps.Theme, login)
	out := []string{paintLine(tint, width, head)}
	if len(body) == 0 {
		return out
	}
	out = append(out, paintLine(tint, width, air))
	for _, line := range body {
		out = append(out, paintLine(tint, width, line))
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
	return "  " + authorHeading(deps, author, url, at, state)
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
