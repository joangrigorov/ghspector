package ui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/layout"
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

// TestConversationGroupsAThreadAcrossReviews is the shape GitHub actually
// returns: a reply is posted as part of a new review, so the remarks of one
// thread name different reviews. Grouped one review at a time, the reply was
// drawn as a thread of its own, with its own location and its own hunk excerpt,
// while the comment it answered sat alone in the earlier review.
func TestConversationGroupsAThreadAcrossReviews(t *testing.T) {
	root := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)
	reply := anInline(101, 11, "octocat", "internal/ui/table.go", 12, conversationNow.Add(time.Minute))
	reply.InReplyToID = 100

	conv := newConversation(
		nil,
		[]store.Review{
			aReview(10, "carol", "CHANGES_REQUESTED", conversationNow),
			aReview(11, "octocat", "COMMENTED", conversationNow.Add(time.Minute)),
		},
		[]store.ReviewComment{root, reply},
		false,
	)

	want := []string{"review:carol", "review:octocat"}
	if got := conversationKinds(conv); !slices.Equal(got, want) {
		t.Fatalf("conversation reads %v, want %v", got, want)
	}
	threads := conv.entries[0].threads
	if len(threads) != 1 {
		t.Fatalf("the review that opened the thread carries %d threads, want 1", len(threads))
	}
	if got := len(threads[0].comments); got != 2 {
		t.Fatalf("the thread holds %d comments, want the reply under it too", got)
	}
	if got := threads[0].comments[1].ID; got != 101 {
		t.Errorf("the remark under the thread is %d, want the reply 101", got)
	}
	if got := threads[0].review; got != 10 {
		t.Errorf("the thread belongs to review %d, want the review that opened it (10)", got)
	}
	if got := conv.entries[1].threads; len(got) != 0 {
		t.Errorf("the review that only replied carries %d threads, want none: the thread lives where it was written", len(got))
	}
}

// TestConversationFollowsAChainOfReplies covers an answer to an answer: the
// exchange is one thread however deep it goes, it reads from the comment it
// started from, and its remarks are in the order they were made whatever order
// the API returned them in.
func TestConversationFollowsAChainOfReplies(t *testing.T) {
	root := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)
	first := anInline(101, 11, "octocat", "internal/ui/table.go", 12, conversationNow.Add(time.Minute))
	first.InReplyToID = 100
	second := anInline(102, 11, "carol", "internal/ui/table.go", 12, conversationNow.Add(2*time.Minute))
	second.InReplyToID = 101

	conv := newConversation(
		nil,
		[]store.Review{
			aReview(10, "carol", "COMMENTED", conversationNow),
			aReview(11, "octocat", "COMMENTED", conversationNow.Add(time.Minute)),
		},
		[]store.ReviewComment{second, root, first},
		false,
	)

	if len(conv.entries) != 2 || len(conv.entries[0].threads) != 1 {
		t.Fatalf("conversation reads %v, want one thread under the first review", conversationKinds(conv))
	}
	thread := conv.entries[0].threads[0]
	if got := len(thread.comments); got != 3 {
		t.Fatalf("the thread holds %d comments, want the whole exchange", got)
	}
	for i, want := range []int64{100, 101, 102} {
		if got := thread.comments[i].ID; got != want {
			t.Errorf("remark %d of the thread is %d, want %d", i, got, want)
		}
	}
	if thread.line != 12 || thread.path != "internal/ui/table.go" {
		t.Errorf("the thread is placed at %s:%d, want where it was written", thread.path, thread.line)
	}
}

// TestConversationGathersRepliesToAMissingComment covers the page that was cut
// below the comment an exchange started from: the replies that name the same
// absent remark still read as one thread, placed by the earliest of them, rather
// than as a thread each.
func TestConversationGathersRepliesToAMissingComment(t *testing.T) {
	firstReply := anInline(101, 11, "octocat", "internal/ui/table.go", 12, conversationNow.Add(time.Minute))
	firstReply.InReplyToID = 404
	secondReply := anInline(102, 11, "carol", "internal/ui/table.go", 20, conversationNow.Add(2*time.Minute))
	secondReply.InReplyToID = 404

	conv := newConversation(
		nil,
		[]store.Review{aReview(11, "octocat", "COMMENTED", conversationNow)},
		[]store.ReviewComment{secondReply, firstReply},
		false,
	)

	if len(conv.entries) != 1 {
		t.Fatalf("conversation reads %v, want the one review", conversationKinds(conv))
	}
	threads := conv.entries[0].threads
	if len(threads) != 1 {
		t.Fatalf("the replies to one absent remark read as %d threads, want 1", len(threads))
	}
	if got := len(threads[0].comments); got != 2 {
		t.Errorf("the thread holds %d comments, want both replies", got)
	}
	if got := threads[0].comments[0].ID; got != 101 {
		t.Errorf("the thread opens with %d, want the earliest reply 101", got)
	}
	if threads[0].line != 12 {
		t.Errorf("the thread is placed at line %d, want the first reply's line 12", threads[0].line)
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
	// A hunk longer than the excerpt gives up its head: what a remark needs is
	// the lines just above the one it was written against.
	hunk := "@@ -1,9 +1,10 @@\n one\n two\n three\n four\n five\n six\n seven\n eight\n"
	want := []string{" three", " four", " five", " six", " seven", " eight"}
	if got := hunkLines(hunk); !slices.Equal(got, want) {
		t.Errorf("hunk tail = %q, want %q", got, want)
	}

	// A short hunk is shown whole, header and all: it says where in the file the
	// remark was written.
	short := "@@ -1,2 +1,3 @@\n one\n two\n"
	if got, want := hunkLines(short), []string{"@@ -1,2 +1,3 @@", " one", " two"}; !slices.Equal(got, want) {
		t.Errorf("short hunk = %q, want %q", got, want)
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

// TestThreadsAreDrawnInFrames covers the shape of a thread on screen: a frame
// titled by where the thread is, the lines it was written against in a frame of
// their own, the remark the thread started from out on the thread's rail, and
// each reply in a frame one rail deeper, so an answer reads as belonging to the
// remark above it.
func TestThreadsAreDrawnInFrames(t *testing.T) {
	root := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)
	reply := anInline(101, 10, "octocat", "internal/ui/table.go", 12, conversationNow.Add(time.Minute))
	reply.InReplyToID = 100

	const width = 100
	lines := conversationLines(plainDeps(), newConversation(
		nil,
		[]store.Review{aReview(10, "carol", "APPROVED", conversationNow)},
		[]store.ReviewComment{root, reply},
		false,
	), width)
	joined := strings.Join(lines, "\n")
	// The frames are structure, so the assertions read the characters the pane
	// draws and ignore the weights the bold names carry.
	plain := ansi.Strip(joined)
	var stripped []string
	for _, line := range lines {
		stripped = append(stripped, ansi.Strip(line))
	}

	for _, want := range []string{
		// The thread's frame, titled by the location.
		threadIndent + frameTop + "internal/ui/table.go:12",
		threadIndent + frameClose,
		// The snippet, in a frame inside the thread's rail.
		threadIndent + frameRail + frameTop + "@@ -10,4 +10,5 @@",
		threadIndent + frameRail + frameClose,
		// The remark the thread started from, marked, out on the thread's rail.
		threadIndent + frameRail + rootMark + " carol",
		// The reply, in a frame of its own one rail further in, its heading and
		// body separated by air.
		threadIndent + frameRail + frameTop + "octocat",
		threadIndent + frameRail + frameRail + "\n" + threadIndent + frameRail + frameRail + "  inline octocat",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("the thread is missing %q:\n%s", want, plain)
		}
	}

	// The remark's heading, a row of air that keeps the thread's rail running,
	// then the body.
	rows := strings.Split(plain, "\n")
	heading := -1
	for i, row := range rows {
		if strings.HasPrefix(strings.TrimRight(row, " "), threadIndent+frameRail+rootMark) {
			heading = i
			break
		}
	}
	if heading < 0 || heading+2 >= len(rows) {
		t.Fatalf("the remark's heading is not where the frame draws it:\n%s", plain)
	}
	if air := strings.TrimRight(rows[heading+1], " "); air != strings.TrimRight(threadIndent+frameRail, " ") {
		t.Errorf("the row under the heading is %q, want the thread's rail", air)
	}
	if !strings.Contains(rows[heading+2], "inline carol") {
		t.Errorf("the body does not follow the air row: %q", rows[heading+2])
	}

	// Every remark is closed by a rule: the remark's own line, and the rule that
	// forms the foot of the reply's frame.
	rules := 0
	for _, line := range stripped {
		line = strings.TrimRight(line, " ")
		if strings.HasPrefix(line, threadIndent+frameBar+"  ─") || strings.HasPrefix(line, threadIndent+frameRail+frameClose+"─") {
			rules++
		}
	}
	if rules != 2 {
		t.Errorf("the remarks are closed by %d rules, want the remark's line and the reply's foot:\n%s", rules, plain)
	}

	if first, nested := strings.Index(plain, threadIndent+frameTop), strings.Index(plain, threadIndent+frameRail+frameTop); nested <= first {
		t.Errorf("the reply frame is not nested inside the thread frame (%d, %d):\n%s", first, nested, plain)
	}

	for i, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("line %d is %d cells wide, want at most %d: %q", i, got, width, line)
		}
	}

	// A frame is characters, so it needs no link sequence to hold together.
	if strings.Contains(joined, "\x1b]8;") {
		t.Errorf("a conversation without hyperlinks carries OSC 8 sequences:\n%q", joined)
	}
}

// TestWashesPaintTheCommentBodyAndNothingElse covers the coloured render: the
// wash is the box of a contribution's own text — no heading, no row of air, and
// nothing outside the box — it is a background and nothing else, so the same text
// sits in the same cells as the plain render, and two authors in one thread do
// not share a shade.
func TestWashesPaintTheCommentBodyAndNothingElse(t *testing.T) {
	theme := DarkTheme()
	conv := newConversation(
		nil,
		[]store.Review{aReview(10, "carol", "APPROVED", conversationNow)},
		[]store.ReviewComment{
			anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow),
			func() store.ReviewComment {
				reply := anInline(101, 10, "octocat", "internal/ui/table.go", 12, conversationNow.Add(time.Minute))
				reply.InReplyToID = 100
				return reply
			}(),
		},
		false,
	)

	const width = 100
	painted := conversationLines(Deps{Theme: theme, Markdown: NewMarkdown()}, conv, width)
	plain := conversationLines(plainDeps(), conv, width)
	if len(painted) != len(plain) {
		t.Fatalf("the coloured render has %d rows and the plain one %d", len(painted), len(plain))
	}

	shades := map[string]string{
		"carol":   paintCode(authorTint(theme, "carol")),
		"octocat": paintCode(authorTint(theme, "octocat")),
	}
	if shades["carol"] == "" || shades["carol"] == shades["octocat"] {
		t.Fatalf("the two authors of one thread share the shade %q", shades["carol"])
	}
	// The bodies the fixtures write, and whose shade each of them wears.
	bodies := map[string]string{
		"review carol":   "carol",
		"inline carol":   "carol",
		"inline octocat": "octocat",
	}
	// bodyOf names the contribution a row is the body of, by the text the
	// fixtures give that body. A row that is not one of them is a heading, a row
	// of air, a frame or a rule, and carries no wash at all.
	bodyOf := func(text string) (string, bool) {
		for body, login := range bodies {
			if strings.Contains(text, body) {
				return login, true
			}
		}
		return "", false
	}

	washed := map[string]bool{}
	for i := range painted {
		// A wash is a background and nothing else, so the painted row carries the
		// same text as the plain one.
		got := strings.TrimRight(ansi.Strip(painted[i]), " ")
		want := strings.TrimRight(ansi.Strip(plain[i]), " ")
		if got != want {
			t.Errorf("row %d reads %q painted and %q plain, want the paint to change no text", i, got, want)
		}
		if width := ansi.StringWidth(painted[i]); width > 100 {
			t.Errorf("row %d is %d cells wide, want at most the pane's 100", i, width)
		}
		worn := ""
		for login, code := range shades {
			if strings.Contains(painted[i], code) {
				worn = login
			}
		}
		login, isBody := bodyOf(ansi.Strip(painted[i]))
		switch {
		case isBody && worn != login:
			t.Errorf("row %d is %s's body and wears %q, want their shade %q: %q", i, login, worn, shades[login], got)
		case isBody:
			washed[login] = true
		case worn != "":
			t.Errorf("row %d is not a comment's body and wears %s's shade: %q", i, worn, got)
		}
	}
	for login := range shades {
		if !washed[login] {
			t.Errorf("no body is painted in %s's shade", login)
		}
	}
}

// TestAWashIsTheCommentsOwnBox covers the extent of the wash: it is the box the
// body's own text occupies — it opens at the column the card puts its text at,
// ends on the last cell of the block's longest line, and is that one box on every
// line of the block, so the short line of the fixture is carried out to the long
// one's end rather than left with a wash of its own. The heading above the box and
// the pane's content past it keep the terminal's colour.
func TestAWashIsTheCommentsOwnBox(t *testing.T) {
	const width = 100
	theme := DarkTheme()
	comment := aComment(1, "alice", conversationNow)
	// Two lines of different lengths, so a wash that followed each line rather than
	// the block would be caught here. A nil renderer wraps the body plainly, which
	// is what makes the text's own extent predictable.
	const longest = "and a line long enough to be the widest of them"
	comment.Body = "a short line\n" + longest
	conv := newConversation([]store.IssueComment{comment}, nil, nil, false)

	lines := conversationLines(Deps{Theme: theme}, conv, width)
	code := paintCode(authorTint(theme, "alice"))
	if code == "" {
		t.Fatal("the dark palette gives alice no shade")
	}
	// The box: the column the text starts in, out to the end of the longest line.
	box := layout.Width(cardIndent) + ansi.StringWidth(longest)
	if box >= width {
		t.Fatalf("the fixture's longest line reaches the pane, so this proves nothing")
	}
	painted := 0
	for i, line := range lines {
		if !strings.Contains(line, code) {
			continue
		}
		painted++
		if got := ansi.StringWidth(ansi.Strip(line)); got != box {
			t.Errorf("row %d is painted out to %d cells, want the block's own box of %d: %q", i, got, box, ansi.Strip(line))
		}
		// The wash starts where the text does: the cells before it are the card's
		// indent, and they keep the terminal's own background.
		if start := ansi.StringWidth(ansi.Strip(line[:strings.Index(line, code)])); start != layout.Width(cardIndent) {
			t.Errorf("row %d opens its wash at cell %d, want the text's own %d: %q", i, start, layout.Width(cardIndent), ansi.Strip(line))
		}
	}
	if painted != 2 {
		t.Errorf("%d rows are painted, want the body's two", painted)
	}
	// The heading is above the box and carries no colour of its own.
	for _, line := range lines {
		if strings.Contains(ansi.Strip(line), "alice") && strings.Contains(line, "\x1b[48") {
			t.Errorf("the heading that names the author carries a wash: %q", ansi.Strip(line))
		}
	}
}

// TestAResolvedThreadIsFoldedUntilItIsOpened covers the resolution state: a
// thread GitHub reports as resolved is one row carrying a `[show]` button that
// says what is folded away, and opening it brings the whole thread back with the
// button turned into `[hide]`.
func TestAResolvedThreadIsFoldedUntilItIsOpened(t *testing.T) {
	root := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)
	reply := anInline(101, 10, "octocat", "internal/ui/table.go", 12, conversationNow.Add(time.Minute))
	reply.InReplyToID = 100

	const width = 100
	conv := newConversation(
		nil,
		[]store.Review{aReview(10, "carol", "APPROVED", conversationNow)},
		[]store.ReviewComment{root, reply},
		false,
	)
	view := threadView{resolved: map[int64]bool{100: true, 101: true}}

	folded, controls := conversationLinesOf(plainDeps(), conv, width, view)
	if len(controls) != 1 {
		t.Fatalf("the folded conversation carries %d controls, want 1", len(controls))
	}
	if got := controls[0]; got.thread != 100 || got.row < 0 || got.row >= len(folded) {
		t.Fatalf("the control is %+v, want the thread's root on a drawn row", got)
	}
	row := ansi.Strip(folded[controls[0].row])
	for _, want := range []string{
		threadShowLabel, "internal/ui/table.go:12", "carol", "2 remarks", "resolved",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the folded row is missing %q: %q", want, row)
		}
	}
	if strings.Contains(row, "inline carol") {
		t.Errorf("a folded thread shows its remarks: %q", row)
	}
	// Folding is what the resolution state does; the conversation is otherwise
	// the one the plain render drew.
	plain := strings.Join(conversationLines(plainDeps(), conv, width), "\n")
	if !strings.Contains(ansi.Strip(plain), "inline carol") {
		t.Fatalf("the unfolded render does not hold the thread:\n%s", ansi.Strip(plain))
	}

	// Opened: the frame is back, the button says `[hide]`, and it is still the
	// control the arrows land on.
	view.expanded = map[int64]bool{100: true}
	opened, controls := conversationLinesOf(plainDeps(), conv, width, view)
	if len(controls) != 1 {
		t.Fatalf("the opened conversation carries %d controls, want the one button", len(controls))
	}
	joined := ansi.Strip(strings.Join(opened, "\n"))
	for _, want := range []string{threadHideLabel, frameTop + "internal/ui/table.go:12", "inline carol", "inline octocat"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the opened thread is missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, threadShowLabel) {
		t.Errorf("an opened thread still offers %q:\n%s", threadShowLabel, joined)
	}
	if got := ansi.Strip(opened[controls[0].row]); !strings.Contains(got, threadHideLabel) {
		t.Errorf("the control is not on the row that carries the button: %q", got)
	}
}

// TestAFoldedThreadHoldsItsWashUntilItIsOpened covers the coloured fold: a folded
// thread is a summary row with no wash on it, and opening it brings back the
// remark's body in its author's shade — from the column the body's text starts in,
// with the heading above it left plain. Both palettes answer, because a shade that
// only works on one background is a shade that disappears on the other.
func TestAFoldedThreadHoldsItsWashUntilItIsOpened(t *testing.T) {
	palettes := map[string]Theme{"dark": DarkTheme(), "light": LightTheme()}
	for name, theme := range palettes {
		t.Run(name, func(t *testing.T) {
			root := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)
			conv := newConversation(
				nil,
				[]store.Review{aReview(10, "carol", "APPROVED", conversationNow)},
				[]store.ReviewComment{root},
				false,
			)

			const width = 100
			deps := Deps{Theme: theme, Markdown: NewMarkdown()}
			view := threadView{resolved: map[int64]bool{100: true}}
			code := paintCode(authorTint(theme, "carol"))
			if code == "" {
				t.Fatalf("the %s palette gives carol no shade", name)
			}

			folded, controls := conversationLinesOf(deps, conv, width, view)
			if len(controls) != 1 {
				t.Fatalf("the folded conversation carries %d controls, want 1", len(controls))
			}
			// A folded thread is a summary row, not a comment's body: the row the
			// button is on carries no wash, and the remark it folded is not drawn.
			if row := folded[controls[0].row]; strings.Contains(row, "\x1b[48") {
				t.Errorf("the folded row carries a wash: %q", ansi.Strip(row))
			}
			if strings.Contains(ansi.Strip(strings.Join(folded, "\n")), "inline carol") {
				t.Error("the folded conversation still draws the remark's body")
			}

			// Opened, the remark the fold hid is drawn with its body in carol's
			// shade, and its heading still carries none.
			view.expanded = map[int64]bool{100: true}
			opened, _ := conversationLinesOf(deps, conv, width, view)
			body := ""
			for _, line := range opened {
				switch {
				case strings.Contains(ansi.Strip(line), "inline carol"):
					body = line
				case strings.Contains(ansi.Strip(line), rootMark) && strings.Contains(line, "\x1b[48"):
					t.Errorf("the remark's heading carries a wash: %q", ansi.Strip(line))
				}
			}
			if body == "" {
				t.Fatal("the opened conversation does not draw the remark's body")
			}
			if !strings.Contains(body, code) {
				t.Errorf("the opened remark's body does not wear carol's shade %q: %q", code, body)
			}
			if strings.HasPrefix(body, code) {
				t.Errorf("the wash starts before the body's own text: %q", ansi.Strip(body))
			}
		})
	}
}

// TestThreadsWithNoResolutionCarryNoControl covers the degraded case: a page
// whose resolution query did not answer has nothing to fold, so every thread is
// drawn in full and there is no control for the arrows to move to.
func TestThreadsWithNoResolutionCarryNoControl(t *testing.T) {
	root := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)

	conv := newConversation(
		nil,
		[]store.Review{aReview(10, "carol", "APPROVED", conversationNow)},
		[]store.ReviewComment{root},
		false,
	)

	for name, view := range map[string]threadView{
		"no answer":      {},
		"nothing folded": {resolved: map[int64]bool{}},
		"another thread": {resolved: map[int64]bool{999: true}},
	} {
		lines, controls := conversationLinesOf(plainDeps(), conv, 100, view)
		if len(controls) != 0 {
			t.Errorf("%s: the conversation carries %d controls, want none", name, len(controls))
		}
		joined := ansi.Strip(strings.Join(lines, "\n"))
		if !strings.Contains(joined, "inline carol") || strings.Contains(joined, threadShowLabel) {
			t.Errorf("%s: the thread is not drawn in full:\n%s", name, joined)
		}
	}
}

// TestFoldedThreadsOfferTheirControlsInOrder covers what the arrows walk: one
// control per folded thread, reported on the row it was drawn on, in the order
// the threads appear — which is the order the reader meets them.
func TestFoldedThreadsOfferTheirControlsInOrder(t *testing.T) {
	first := anInline(100, 10, "carol", "internal/ui/table.go", 12, conversationNow)
	second := anInline(102, 10, "carol", "internal/ui/prpanes.go", 4, conversationNow.Add(time.Minute))

	const width = 100
	conv := newConversation(
		[]store.IssueComment{aComment(1, "alice", conversationNow.Add(-time.Hour))},
		[]store.Review{aReview(10, "carol", "APPROVED", conversationNow)},
		[]store.ReviewComment{first, second},
		false,
	)
	view := threadView{resolved: map[int64]bool{100: true, 102: true}}

	lines, controls := conversationLinesOf(plainDeps(), conv, width, view)
	if len(controls) != 2 {
		t.Fatalf("the conversation carries %d controls, want 2", len(controls))
	}
	for i, want := range []int64{100, 102} {
		if controls[i].thread != want {
			t.Errorf("control %d is thread %d, want %d", i, controls[i].thread, want)
		}
		if controls[i].row >= len(lines) {
			t.Fatalf("control %d is on row %d of %d", i, controls[i].row, len(lines))
		}
	}
	if controls[0].row >= controls[1].row {
		t.Errorf("the controls are on rows %d and %d, want them in reading order", controls[0].row, controls[1].row)
	}
	// Each control is on the row that carries its own location.
	for i, want := range []string{"internal/ui/table.go:12", "internal/ui/prpanes.go:4"} {
		row := ansi.Strip(lines[controls[i].row])
		if !strings.Contains(row, want) || !strings.Contains(row, threadShowLabel) {
			t.Errorf("control %d is on %q, want the row naming %s", i, row, want)
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
