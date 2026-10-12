package ui

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// The rendered stream is a barrier, not an assertion surface.
//
// teatest.WaitFor consumes what it reads and the v2 renderer emits cell diffs,
// so a second wait on the same frame hangs and a wait for text that only moved
// bytes can miss. Behaviour is asserted on the model the program exits with;
// the stream is used only to know that startup has rendered.
func startProgram(t *testing.T, srv *httptest.Server, w, h int) *teatest.TestModel {
	t.Helper()
	tm := teatest.NewTestModel(t, newModel(t, srv, w, h), teatest.WithInitialTermSize(w, h))
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("No pull requests loaded yet."))
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
	if !bytes.Contains([]byte(final.View().Content), []byte("No pull requests loaded yet.")) {
		t.Error("the list did not come back after the terminal grew")
	}
}
