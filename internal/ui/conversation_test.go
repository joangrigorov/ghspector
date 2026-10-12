package ui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/store"
)

// conversationNow is the fixed clock the conversation tests are written
// against.
var conversationNow = time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

// plainDeps is a Deps with no colours and no markdown renderer: every body
// falls back to wrapped plain text, which is what makes the rendered lines
// assertable as strings.
func plainDeps() Deps { return Deps{Theme: PlainTheme()} }

// aComment is one conversation comment by an author at a time.
func aComment(id int64, author string, at time.Time) store.IssueComment {
	return store.IssueComment{
		ID:        id,
		Body:      "comment " + author,
		User:      &store.User{Login: author},
		CreatedAt: at,
	}
}

// aReview is one review by an author at a time. An empty state or a zero time
// asks for the field to be left out, which is what makes a draft.
func aReview(id int64, author, state string, at time.Time) store.Review {
	review := store.Review{
		ID:      id,
		User:    &store.User{Login: author},
		Body:    "review " + author,
		State:   state,
		HTMLURL: "https://github.com/octocat/api/pull/7#pullrequestreview-" + strconv.FormatInt(id, 10),
	}
	if !at.IsZero() {
		submitted := at
		review.SubmittedAt = &submitted
	}
	return review
}

// anInline is one inline review comment on a path and line.
func anInline(id, review int64, author, path string, line int, at time.Time) store.ReviewComment {
	return store.ReviewComment{
		ID:                  id,
		PullRequestReviewID: review,
		User:                &store.User{Login: author},
		Body:                "inline " + author,
		Path:                path,
		Line:                line,
		DiffHunk:            "@@ -10,4 +10,5 @@\n func width() int {\n-    return 40\n+    return tableWidth\n }\n",
		CreatedAt:           at,
	}
}

// conversationKinds names the entries in the order they are drawn, so a test
// says what the pane reads without depending on how a block is rendered.
func conversationKinds(conv conversation) []string {
	out := make([]string, 0, len(conv.entries))
	for _, entry := range conv.entries {
		switch {
		case entry.comment != nil:
			out = append(out, "comment:"+userLogin(entry.comment.User))
		case entry.review != nil:
			out = append(out, "review:"+userLogin(entry.review.User))
		default:
			out = append(out, "thread:"+entry.threads[0].path)
		}
	}
	return out
}

// TestConversationInterleavesTheThreeLists is the merge the Comments sub-tab
// exists for: the API keeps comments, reviews and inline comments apart, and the
// pane reads them in the order they happened.
func TestConversationInterleavesTheThreeLists(t *testing.T) {
	conv := newConversation(
		[]store.IssueComment{
			aComment(1, "alice", conversationNow),
			aComment(2, "octocat", conversationNow.Add(2*time.Hour)),
		},
		[]store.Review{aReview(10, "carol", "APPROVED", conversationNow.Add(time.Hour))},
		[]store.ReviewComment{anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow.Add(time.Hour))},
		false,
	)

	want := []string{"comment:alice", "review:carol", "comment:octocat"}
	if got := conversationKinds(conv); !slices.Equal(got, want) {
		t.Errorf("conversation reads %v, want %v", got, want)
	}
	if conv.truncated {
		t.Error("a conversation of whole pages reports itself truncated")
	}
}

// TestConversationSortsAnUnknownTimeLast covers the contribution the API gave no
// timestamp: it belongs at the end of the discussion rather than above
// everything, which an unset time would otherwise do.
func TestConversationSortsAnUnknownTimeLast(t *testing.T) {
	conv := newConversation(
		[]store.IssueComment{
			{ID: 1, Body: "no timestamp", User: &store.User{Login: "ghost"}},
			aComment(2, "alice", conversationNow),
		},
		nil, nil, false,
	)

	want := []string{"comment:alice", "comment:ghost"}
	if got := conversationKinds(conv); !slices.Equal(got, want) {
		t.Errorf("conversation reads %v, want %v", got, want)
	}
}

// TestConversationDropsAPendingReview covers the draft: an unsubmitted review
// and the inline comments it carries are not part of the discussion, and a
// review with no submission time is a draft whatever state it reports.
func TestConversationDropsAPendingReview(t *testing.T) {
	conv := newConversation(
		nil,
		[]store.Review{
			aReview(10, "carol", "PENDING", time.Time{}),
			aReview(11, "dave", "COMMENTED", time.Time{}),
		},
		[]store.ReviewComment{anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)},
		false,
	)

	if !conv.empty() {
		t.Fatalf("a pending review is drawn: %v", conversationKinds(conv))
	}
	for _, review := range []store.Review{aReview(10, "carol", "PENDING", time.Time{}), aReview(11, "dave", "COMMENTED", time.Time{})} {
		if !isPendingReview(review) {
			t.Errorf("review %d with no submission time is not treated as a draft", review.ID)
		}
	}
}

// TestConversationKeepsAThreadWhoseReviewIsMissing covers the inline comment
// whose review is not in the page: it is still something somebody wrote, so it
// is drawn on its own rather than dropped.
func TestConversationKeepsAThreadWhoseReviewIsMissing(t *testing.T) {
	conv := newConversation(
		nil, nil,
		[]store.ReviewComment{anInline(100, 999, "carol", "internal/ui/table.go", 12, conversationNow)},
		false,
	)

	want := []string{"thread:internal/ui/table.go"}
	if got := conversationKinds(conv); !slices.Equal(got, want) {
		t.Fatalf("conversation reads %v, want %v", got, want)
	}
	thread := conv.entries[0].threads[0]
	if thread.line != 12 || len(thread.comments) != 1 {
		t.Errorf("thread = %+v, want line 12 with one comment", thread)
	}
}

// TestConversationGroupsRepliesUnderTheirThread covers the exchange: a reply
// belongs under the comment it answers, and a reply whose parent is not in the
// page opens a thread of its own rather than vanishing.
func TestConversationGroupsRepliesUnderTheirThread(t *testing.T) {
	reply := anInline(101, 10, "octocat", "internal/ui/table.go", 12, conversationNow.Add(time.Minute))
	reply.InReplyToID = 100
	orphan := anInline(102, 10, "dave", "internal/ui/table.go", 12, conversationNow.Add(2*time.Minute))
	orphan.InReplyToID = 404

	conv := newConversation(
		nil,
		[]store.Review{aReview(10, "carol", "CHANGES_REQUESTED", conversationNow)},
		[]store.ReviewComment{
			anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow),
			reply,
			anInline(103, 10, "carol", "internal/ui/prpanes.go", 4, conversationNow),
			orphan,
		},
		false,
	)

	if len(conv.entries) != 1 {
		t.Fatalf("conversation has %d entries, want the one review", len(conv.entries))
	}
	threads := conv.entries[0].threads
	if len(threads) != 3 {
		t.Fatalf("review carries %d threads, want 3", len(threads))
	}
	if got := len(threads[0].comments); got != 2 {
		t.Errorf("the thread with a reply has %d comments, want 2", got)
	}
	if got := threads[0].comments[1].ID; got != 101 {
		t.Errorf("the reply under the thread is %d, want 101", got)
	}
	if got := threads[2].comments[0].ID; got != 102 {
		t.Errorf("the reply with no parent on the page is %d, want 102 drawn on its own", got)
	}
}

// TestCommentLocationFallsBackToTheOriginalLine covers the comment the diff has
// moved past: the API reports no current line and keeps the original one, which
// is where the remark still belongs.
func TestCommentLocationFallsBackToTheOriginalLine(t *testing.T) {
	moved := anInline(100, 10, "carol", "internal/ui/table.go", 0, conversationNow)
	moved.OriginalLine = 7
	if got := commentLocation(moved); got != 7 {
		t.Errorf("comment location = %d, want the original line 7", got)
	}
	if got := commentLocation(anInline(101, 10, "carol", "internal/ui/table.go", 12, conversationNow)); got != 12 {
		t.Errorf("comment location = %d, want the current line 12", got)
	}
}

// TestHunkLinesKeepsTheContextAboveTheComment covers the excerpt: the tail of
// the hunk is the part just above the remark, and a hunk that is nothing but its
// header has no context to show.
func TestHunkLinesKeepsTheContextAboveTheComment(t *testing.T) {
	hunk := "@@ -1,5 +1,6 @@\n one\n two\n three\n four\n five\n"
	want := []string{" three", " four", " five"}
	if got := hunkLines(hunk); !slices.Equal(got, want) {
		t.Errorf("hunk tail = %q, want %q", got, want)
	}

	for name, hunk := range map[string]string{
		"a lone header": "@@ -1,3 +1,4 @@",
		"no hunk":       "",
		"only spaces":   "   \n",
	} {
		if got := hunkLines(hunk); got != nil {
			t.Errorf("%s renders %q, want nothing", name, got)
		}
	}
}

// TestConversationLinesSayWhenAPageWasCut covers the note: a page that came back
// full is part of a longer discussion, and the pane says so rather than showing
// a fraction of it as the whole.
func TestConversationLinesSayWhenAPageWasCut(t *testing.T) {
	comments := []store.IssueComment{aComment(1, "alice", conversationNow)}

	cut := strings.Join(conversationLines(plainDeps(), newConversation(comments, nil, nil, true), 80), "\n")
	if !strings.Contains(cut, truncatedNote) {
		t.Errorf("a truncated conversation does not say so:\n%s", cut)
	}

	whole := strings.Join(conversationLines(plainDeps(), newConversation(comments, nil, nil, false), 80), "\n")
	if strings.Contains(whole, truncatedNote) {
		t.Errorf("a whole conversation reports itself truncated:\n%s", whole)
	}
}

// TestConversationLinesPlaceAnInlineThread covers what an inline remark reads
// like: the file and line it was written against, the hunk it was written
// against, and the exchange itself underneath.
func TestConversationLinesPlaceAnInlineThread(t *testing.T) {
	thread := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)
	out := strings.Join(conversationLines(plainDeps(), newConversation(
		nil,
		[]store.Review{aReview(10, "carol", "APPROVED", conversationNow)},
		[]store.ReviewComment{thread},
		false,
	), 100), "\n")

	for _, want := range []string{
		"carol",
		"[approved]",
		"internal/ui/table.go:12",
		"-    return 40",
		"+    return tableWidth",
		"inline carol",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the thread is missing %q:\n%s", want, out)
		}
	}
}

// TestReviewStateLabel covers the badge: the vocabulary the rest of the app uses
// for a state that matters, and the plain fallback for one it does not know.
func TestReviewStateLabel(t *testing.T) {
	for _, tc := range []struct{ state, want string }{
		{"APPROVED", "[approved]"},
		{"CHANGES_REQUESTED", "[changes requested]"},
		{"COMMENTED", "[reviewed]"},
		{"DISMISSED", "[dismissed]"},
		{"", ""},
	} {
		review := store.Review{State: tc.state}
		// The badge is bold whatever the palette, so the label is read without
		// escapes.
		if got := ansi.Strip(reviewStateLabel(PlainTheme(), review)); got != tc.want {
			t.Errorf("label for %q = %q, want %q", tc.state, got, tc.want)
		}
	}
}
