package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestRenderedLinesHoldWhatTheyRendered covers the cache the panes share: the
// lines come back untouched while the revision, the width and the palette are
// the ones they were rendered at, and any of the three changing renders again.
func TestRenderedLinesHoldWhatTheyRendered(t *testing.T) {
	var cache renderedLines[int]
	key := markdownKeyOf(DarkTheme(), 72)
	renders := 0
	render := func() []string {
		renders++
		return []string{"body"}
	}

	first := cache.linesFrom(1, key, render)
	if renders != 1 {
		t.Fatalf("the first call rendered %d times, want once", renders)
	}
	if again := cache.linesFrom(1, key, render); renders != 1 {
		t.Errorf("an unchanged revision rendered again (%d renders)", renders)
	} else if &again[0] != &first[0] {
		t.Error("an unchanged revision did not return the lines it held")
	}

	cache.linesFrom(2, key, render)
	if renders != 2 {
		t.Errorf("a new revision rendered %d times, want twice", renders)
	}
	cache.linesFrom(2, markdownKeyOf(DarkTheme(), 60), render)
	if renders != 3 {
		t.Errorf("a new width rendered %d times, want three", renders)
	}
	cache.linesFrom(2, markdownKeyOf(PlainTheme(), 60), render)
	if renders != 4 {
		t.Errorf("a new palette rendered %d times, want four", renders)
	}
}

// TestPRCommentsRenderTheConversationOnce is the regression the report was
// about: the pane draws dozens of markdown bodies, and a keystroke used to
// render every one of them twice before the frame was even drawn. Now a key
// renders nothing, and the two things that change what the pane looks like —
// new data and a new width — render it again.
func TestPRCommentsRenderTheConversationOnce(t *testing.T) {
	srv := prServer(t, withReviews(manyReviews(8)...), withInlineComments())
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	m = pressAndDrive(t, m, "5")

	details := prDetailsRoute(t, m)
	if len(details.commentLines(m.deps)) == 0 {
		t.Fatal("the conversation rendered to nothing")
	}

	renders := m.deps.Markdown.renders
	for _, key := range []string{"down", "down", "j", "d", "u", "k", "up"} {
		m = pressAndDrive(t, m, key)
		screen(t, m, 120, 40)
	}
	if got := m.deps.Markdown.renders; got != renders {
		t.Errorf("%d markdown renders for seven keystrokes, want none", got-renders)
	}

	// A refresh replaces the lists the lines were rendered from, so the next
	// frame renders them again rather than drawing what the old data looked
	// like.
	before := m.deps.Markdown.renders
	m = pressAndDrive(t, m, "r")
	if got := m.deps.Markdown.renders; got <= before {
		t.Errorf("a refresh rendered %d times, want the conversation rendered again", got-before)
	}

	// So does a resize, which re-wraps every body to the new width.
	before = m.deps.Markdown.renders
	next, _ := send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next
	screen(t, m, 100, 30)
	if got := m.deps.Markdown.renders; got <= before {
		t.Errorf("a resize rendered %d times, want the conversation rendered again", got-before)
	}
}

// TestIssueCommentsRenderTheThreadOnce is the same property on the issue side:
// a long thread is rendered when it arrives, and an arrow key over it costs no
// markdown work at all.
func TestIssueCommentsRenderTheThreadOnce(t *testing.T) {
	srv := prServer(t, withComments(12))
	m := openIssueDetails(t, issuesModel(t, srv, 100, 30))
	m = pressAndDrive(t, m, "c")

	comments := issueCommentsRoute(t, m)
	if len(comments.lines(m.deps)) == 0 {
		t.Fatal("the thread rendered to nothing")
	}

	renders := m.deps.Markdown.renders
	for _, key := range []string{"down", "j", "up", "k"} {
		m = pressAndDrive(t, m, key)
		screen(t, m, 100, 30)
	}
	if got := m.deps.Markdown.renders; got != renders {
		t.Errorf("%d markdown renders for four keystrokes, want none", got-renders)
	}
}

// TestPRDescriptionRenderIsCached covers the body the Info pane draws and every
// clamp measures: the description is markdown too, so a keystroke on any
// sub-tab used to pay for it.
func TestPRDescriptionRenderIsCached(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))

	details := prDetailsRoute(t, m)
	if len(details.descriptionLines(m.deps, details.descriptionWidth(m.deps))) == 0 {
		t.Fatal("the description rendered to nothing")
	}

	renders := m.deps.Markdown.renders
	for i := 0; i < 5; i++ {
		m = pressAndDrive(t, m, "down")
		screen(t, m, 120, 40)
	}
	if got := m.deps.Markdown.renders; got != renders {
		t.Errorf("%d markdown renders for five keystrokes on the description, want none", got-renders)
	}
}
