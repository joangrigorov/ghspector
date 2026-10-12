package ui

import (
	"bytes"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// The rendered stream is a barrier, not an assertion surface.
//
// teatest.WaitFor consumes what it reads and the v2 renderer emits cell diffs,
// so a second wait on the same frame hangs and a wait for text that only moved
// bytes can miss.
//
// readyMarker is the tab strip, which the first ready frame writes in one
// piece. A marker inside a list body would be unreliable: a list that arrives
// after the frame it replaces is patched cell by cell, so "no pull requests"
// can reach the stream split around a cursor move. Behaviour is asserted on the
// model the program exits with; the stream is used only to know that a screen
// has rendered.
const readyMarker = "Workflows │ Issues"

func startProgram(t *testing.T, srv *httptest.Server, w, h int) *teatest.TestModel {
	t.Helper()
	tm := teatest.NewTestModel(t, newModel(t, srv, w, h), teatest.WithInitialTermSize(w, h))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(readyMarker))
	}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
	return tm
}

// finish quits the program and returns the model it exited with.
func finish(t *testing.T, tm *teatest.TestModel) Model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Text: "q", Code: 'q'})
	tm.WaitFinished(t, teatest.WithFinalTimeout(10*time.Second))
	return tm.FinalModel(t, teatest.WithFinalTimeout(10*time.Second)).(Model)
}

// TestFlowThroughTheProgram drives the real Bubble Tea runtime, so the wiring
// between Init, the command loop and View is exercised rather than assumed.
func TestFlowThroughTheProgram(t *testing.T) {
	srv := ghServer(t, defaultFixtures())

	t.Run("tab then help then quit", func(t *testing.T) {
		tm := startProgram(t, srv, 100, 30)

		// The default tab is Pull Requests (spec D2); Tab moves to Workflows.
		tm.Send(tea.KeyPressMsg{Code: tea.KeyTab})
		tm.Type("?")

		final := finish(t, tm)
		if _, ok := final.routes.Top().(*Help); !ok {
			t.Fatalf("? did not open help through the runtime; top = %T", final.routes.Top())
		}
		if final.routes.Len() != 2 {
			t.Fatalf("stack depth = %d, want 2", final.routes.Len())
		}
		assertGeometry(t, "final frame", final.View().Content, 100, 30)
	})

	t.Run("help closes back onto the active tab", func(t *testing.T) {
		tm := startProgram(t, srv, 100, 30)

		tm.Send(tea.KeyPressMsg{Code: tea.KeyTab})
		tm.Type("?")
		tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})

		final := finish(t, tm)
		if got := final.routes.Top().Title(); got != "Workflows" {
			t.Fatalf("after closing help the route is %q, want Workflows", got)
		}
		if final.routes.Len() != 1 {
			t.Fatalf("stack depth = %d, want 1", final.routes.Len())
		}
	})

	t.Run("context switcher opens and closes", func(t *testing.T) {
		tm := startProgram(t, srv, 100, 30)

		tm.Type("o")
		tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})

		final := finish(t, tm)
		if got := final.routes.Top().Title(); got != "Pull Requests" {
			t.Fatalf("after closing the switcher the route is %q", got)
		}
	})

	t.Run("startup reaches the ready stage", func(t *testing.T) {
		tm := startProgram(t, srv, 80, 24)
		final := finish(t, tm)
		if final.stage != stageReady {
			t.Fatalf("stage = %v, want ready", final.stage)
		}
		if final.deps.Account.Login != "octocat" {
			t.Fatalf("viewer = %q, want octocat", final.deps.Account.Login)
		}
	})
}

// TestFlowResizeWhileRunning proves a resize mid-session is tracked and still
// renders an exact frame, which is where the previous version's hand-tuned
// constants failed.
func TestFlowResizeWhileRunning(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	tm := startProgram(t, srv, 100, 30)

	// Messages are processed in the order they are sent, so the last size wins
	// and every intermediate size is walked through the real renderer.
	for _, size := range [][2]int{{80, 24}, {60, 16}, {40, 15}, {120, 40}} {
		tm.Send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	}

	final := finish(t, tm)
	if final.width != 120 || final.height != 40 {
		t.Fatalf("final size = %dx%d, want 120x40", final.width, final.height)
	}
	assertGeometry(t, "resized frame", final.View().Content, 120, 40)
}

// TestFlowTooSmallThenRecovered checks the one resize sequence a user is most
// likely to hit: shrinking below the minimum and growing back.
func TestFlowTooSmallThenRecovered(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	tm := startProgram(t, srv, 100, 30)

	tm.Send(tea.WindowSizeMsg{Width: 30, Height: 8})
	tm.Send(tea.WindowSizeMsg{Width: 100, Height: 30})

	final := finish(t, tm)
	if final.stage != stageReady {
		t.Fatalf("stage = %v, want ready", final.stage)
	}
	assertGeometry(t, "recovered frame", final.View().Content, 100, 30)
	if !bytes.Contains([]byte(final.View().Content), []byte("No open pull requests found.")) {
		t.Error("the list did not come back after the terminal grew")
	}
}

// TestFlowFromTheRunListToTheLog drives the whole workflow path through the real
// runtime: the runs table, the job list and the log viewer are three stacked
// routes, and each one has to hand the next its fetch. The driver tests reach
// the same screens by running commands by hand, so this is where the wiring
// between Init, the command loop and the stack is exercised rather than assumed.
func TestFlowFromTheRunListToTheLog(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(8))
	const w, h = 120, 40

	tm := teatest.NewTestModel(t, newModel(t, srv, w, h), teatest.WithInitialTermSize(w, h))

	// Each marker is text that only exists once the screen it belongs to has its
	// data: the stream carries cell diffs, so waiting for text that is already on
	// screen would wait forever.
	waitFor := func(marker string) {
		t.Helper()
		teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
			return bytes.Contains(out, []byte(marker))
		}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
	}

	// The default tab is Pull Requests, and the run list is fetched when the
	// Workflows tab is first opened.
	waitFor(readyMarker)
	tm.Send(tea.KeyPressMsg{Code: tea.KeyTab})
	waitFor("Load More Workflow Runs...")

	// The highlighted run is the queued one, which always has a job list.
	tm.Send(press(t, "enter"))
	waitFor("JOB NAME")

	// The running job sorts first and has no log yet, so the finished job is the
	// row below it, and Enter opens its log on the step that failed.
	tm.Send(press(t, "j"))
	tm.Send(press(t, "enter"))
	waitFor("Job: build")

	final := finish(t, tm)

	if got := final.routes.Titles(); !slices.Equal(got, []string{"Workflows", "Jobs", "Logs"}) {
		t.Fatalf("breadcrumb = %v, want [Workflows Jobs Logs]", got)
	}
	logs, ok := final.routes.Top().(*Logs)
	if !ok {
		t.Fatalf("the top route is %T, want *ui.Logs", final.routes.Top())
	}
	if logs.step != 1 || logs.job.Name != "build" {
		t.Errorf("the viewer opened on step %d of %q, want step 1 of build", logs.step, logs.job.Name)
	}
	assertGeometry(t, "final frame", final.View().Content, w, h)
}

// TestFlowThroughThePullRequestScreens drives the pull request path through the
// real runtime: the list, the details view with its sub-tabs, and the merge
// overlays.
//
// Every marker is text written by a wholesale frame redraw — a route push, a
// sub-tab switch, an overlay composite — because the v2 renderer patches a list
// body cell by cell and a marker inside one can arrive split.
func TestFlowThroughThePullRequestScreens(t *testing.T) {
	srv := prServer(t)
	const w, h = 120, 40

	tm := teatest.NewTestModel(t, newModel(t, srv, w, h), teatest.WithInitialTermSize(w, h))
	waitFor := func(marker string) {
		t.Helper()
		teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
			return bytes.Contains(out, []byte(marker))
		}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
	}

	waitFor(readyMarker)

	// Enter pushes the details view; the pane titles are drawn with the push.
	tm.Send(press(t, "enter"))
	waitFor("DESCRIPTION")

	// The sub-tab strip switches the body; Esc steps back to Info without
	// leaving the route.
	tm.Send(press(t, "2"))
	waitFor("SHA")
	tm.Send(press(t, "esc"))
	waitFor("DETAILS")

	// A merge is two overlays: the method chooser, then the confirmation.
	tm.Send(press(t, "m"))
	waitFor("Squash and merge")
	tm.Send(press(t, "s"))
	waitFor("Confirm merge")

	// n cancels the confirmation. Nothing is waited for after it: closing a
	// modal rewrites the box's own rows, and the text underneath them was
	// already written before the box opened, so there is no new marker to wait
	// for. The keys are queued in order, and the assertions below say what
	// happened: the stack and the open overlays are only what they are if the
	// cancel closed the confirmation and the Esc popped the details view.
	tm.Send(press(t, "n"))
	tm.Send(press(t, "esc"))

	final := finish(t, tm)

	if got := final.routes.Titles(); !slices.Equal(got, []string{"Pull Requests"}) {
		t.Fatalf("breadcrumb = %v, want the list alone", got)
	}
	if final.overlays.Len() != 0 {
		t.Errorf("%d overlays are open at the end, want none", final.overlays.Len())
	}
	if got := prsTabOf(t, final).prs; len(got) != len(fixturePRs()) {
		t.Errorf("the list holds %d pull requests, want %d", len(got), len(fixturePRs()))
	}
	assertGeometry(t, "final frame", final.View().Content, w, h)
}

// TestFlowTabCyclingKeepsThePullRequestList checks the tab strip through the
// runtime: walking all the way around comes back to the list the session
// started on, and a tab that already has its data is not fetched again.
func TestFlowTabCyclingKeepsThePullRequestList(t *testing.T) {
	log := newRequestLog()
	srv := prServerWithLog(t, log)
	const w, h = 100, 30

	tm := teatest.NewTestModel(t, newModel(t, srv, w, h), teatest.WithInitialTermSize(w, h))
	waitFor := func(marker string) {
		t.Helper()
		teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
			return bytes.Contains(out, []byte(marker))
		}, teatest.WithDuration(10*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
	}

	waitFor(readyMarker)

	// The list's request is the barrier: a request that reached the server
	// cannot be patched out of the stream the way a cell can.
	listPath := "/repos/octocat/" + fixtureRepo + "/pulls"
	deadline := time.Now().Add(10 * time.Second)
	for log.count(listPath) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the list was never fetched from %s", listPath)
		}
		time.Sleep(5 * time.Millisecond)
	}
	before := log.count(listPath)

	// The tabs are walked in one go: the keys are queued in order, so the model
	// sees them in this order before the quit. The Issues tab is the only tab
	// whose body is a single fresh sentence, so it is the one marker here.
	tm.Send(press(t, "tab"))
	tm.Send(press(t, "tab"))
	waitFor("No issues loaded yet.")
	tm.Send(press(t, "tab"))

	final := finish(t, tm)

	if got := tab(t, final); got != TabPRs {
		t.Fatalf("after a full cycle the active tab is %v, want Pull Requests", got)
	}
	if got := len(prsTabOf(t, final).prs); got != len(fixturePRs()) {
		t.Errorf("the recycled tab holds %d pull requests, want %d", got, len(fixturePRs()))
	}
	if got := log.count(listPath); got != before {
		t.Errorf("returning to the tab refetched it (%d requests, was %d)", got, before)
	}
}
