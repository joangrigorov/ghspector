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

// reviewThread is one file's worth of inline comments from one review: where
// they are, the lines they were written against, and the exchange itself.
type reviewThread struct {
	path     string
	line     int
	hunk     []string
	comments []store.ReviewComment
}

// threadIndent is the indent an inline thread is drawn at, inside the indent a
// top-level contribution uses, because a thread is part of the review above it.
const threadIndent = "    "

// hunkTail is how much of a diff hunk is shown above an inline comment. Three
// lines is the context GitHub's own view shows around a commented line, and it
// is what makes a remark like "this should be a const" mean something.
const hunkTail = 3

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
	for _, review := range reviews {
		byID[review.ID] = review
	}

	grouped := map[int64][]store.ReviewComment{}
	var orphans []store.ReviewComment
	for _, comment := range reviewComments {
		if _, ok := byID[comment.PullRequestReviewID]; !ok {
			orphans = append(orphans, comment)
			continue
		}
		grouped[comment.PullRequestReviewID] = append(grouped[comment.PullRequestReviewID], comment)
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
			threads: groupThreads(grouped[review.ID]),
		})
	}
	// A thread whose review is not in the page — it was submitted after the page
	// was cut, or the token cannot see the review — is still something somebody
	// wrote, so it is drawn on its own rather than dropped.
	for _, thread := range groupThreads(orphans) {
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

// groupThreads splits one review's inline comments into the threads they belong
// to, keeping the order the API returned them in: a comment, and its replies
// after it.
func groupThreads(comments []store.ReviewComment) []reviewThread {
	var threads []reviewThread
	index := make(map[int64]int, len(comments))
	for _, comment := range comments {
		if comment.InReplyToID != 0 {
			if at, ok := index[comment.InReplyToID]; ok {
				threads[at].comments = append(threads[at].comments, comment)
				continue
			}
		}
		threads = append(threads, reviewThread{
			path:     comment.Path,
			line:     commentLocation(comment),
			hunk:     hunkLines(comment.DiffHunk),
			comments: []store.ReviewComment{comment},
		})
		index[comment.ID] = len(threads) - 1
	}
	return threads
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
	out := []string{authorLine(deps, userLogin(review.User), review.HTMLURL, reviewTime(review), reviewStateLabel(deps.Theme, review))}
	if strings.TrimSpace(review.Body) != "" {
		for _, line := range deps.Markdown.Lines(deps.Theme, review.Body, max(width-4, 8)) {
			out = append(out, "  "+line)
		}
	}
	for _, thread := range threads {
		out = append(out, threadLines(deps, thread, width)...)
	}
	return out
}

// threadLines renders one inline thread: where it is, the lines it was written
// against, then the exchange itself.
func threadLines(deps Deps, thread reviewThread, width int) []string {
	theme := deps.Theme
	place := thread.path
	if thread.line > 0 {
		place = fmt.Sprintf("%s:%d", thread.path, thread.line)
	}

	out := []string{"", threadIndent + style(theme.Muted, bold).Render(layout.Truncate(place, max(width-len(threadIndent), 8)))}
	for _, line := range thread.hunk {
		out = append(out, threadIndent+diffStyled(theme, []string{line})[0])
	}
	for _, comment := range thread.comments {
		out = append(out, threadIndent+authorLine(deps, userLogin(comment.User), comment.HTMLURL, comment.CreatedAt, ""))
		for _, line := range deps.Markdown.Lines(theme, comment.Body, max(width-layout.Width(threadIndent)-2, 8)) {
			out = append(out, threadIndent+"  "+line)
		}
	}
	return out
}

// authorLine is the heading of a contribution: who wrote it, the conclusion a
// review came to when there is one, and when it arrived. The name is a link to
// the contribution when the API supplied a URL for it.
func authorLine(deps Deps, author, url string, at time.Time, state string) string {
	name := style(deps.Theme.Title, bold).Render(author)
	if url != "" {
		name = hyperlink(deps.Theme, name, url)
	}
	out := "  " + name
	if state != "" {
		out += "  " + state
	}
	return out + style(deps.Theme.Muted, normal).Render("  "+formatTime(at))
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
