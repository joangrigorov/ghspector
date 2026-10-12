package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/joangrigorov/ghspector/internal/store"
)

// BenchmarkConversationRender measures what one change to the conversation
// costs: every markdown body wrapped and padded, which is the whole reason the
// pane caches its rendered lines (render.go).
//
// The fixture is the shape of the pull request the lag was reported from — 48
// reviews carrying long bodies and 41 inline remarks, ~44 KB of markdown — and
// one render of it measured 163 ms, against 30 µs to assemble the same entries.
func BenchmarkConversationRender(b *testing.B) {
	comments, reviews, inline := reviewHeavyFixture(48, 41)
	conv := newConversation(comments, reviews, inline, false)
	deps := Deps{Theme: DarkTheme(), Markdown: NewMarkdown()}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = conversationLines(deps, conv, 136)
	}
}

// benchmarkBodies returns the bodies of the reported pull request: dozens of
// reviews whose bodies are long markdown documents, and inline remarks under
// them.
func reviewHeavyFixture(reviews, inline int) ([]store.IssueComment, []store.Review, []store.ReviewComment) {
	body := strings.Repeat(
		"**Finding.** The staged path stays out of any transaction, and the read query\n"+
			"now owns one of its own. The ID map keeps the search backend's relevance\n"+
			"order across the two stages, and dropping the middleware is coherent.\n\n"+
			"- the group filter is indexed by the projection but never applied\n"+
			"- filter values are spliced into the filter expression unquoted\n"+
			"- the term branch is dead code, together with its doc comment\n\n", 6)

	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var revs []store.Review
	for i := 0; i < reviews; i++ {
		stamp := at.Add(time.Duration(i) * time.Minute)
		revs = append(revs, store.Review{
			ID:          int64(3000 + i),
			User:        &store.User{Login: "review-bot[bot]"},
			Body:        body,
			State:       "COMMENTED",
			HTMLURL:     "https://github.com/x/y/pull/1#pullrequestreview-1",
			SubmittedAt: &stamp,
		})
	}

	var inlineComments []store.ReviewComment
	for i := 0; i < inline; i++ {
		stamp := at.Add(time.Duration(i) * time.Minute)
		inlineComments = append(inlineComments, store.ReviewComment{
			ID:                  int64(4000 + i),
			PullRequestReviewID: int64(3000 + i%reviews),
			User:                &store.User{Login: "review-bot[bot]"},
			Body:                strings.Repeat("The term branch ignores every other filter the endpoint accepts, so the results come back wider than the caller asked for.\n\n", 3),
			Path:                fmt.Sprintf("internal/service/search/handler.go:%d", i),
			Line:                84,
			DiffHunk:            "@@ -10,4 +10,5 @@\n func width() int {\n-    return 40\n+    return tableWidth\n }\n",
			CreatedAt:           stamp,
		})
	}
	return nil, revs, inlineComments
}
