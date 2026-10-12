package ui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogsOpensOnTheStepThatFailed(t *testing.T) {
	m := openLogs(t, 110, 24, workflowServer(t, fixtureRuns(3)))

	logs := logsRoute(t, m)
	if logs.step != 1 {
		t.Fatalf("selected step = %d, want the failing step", logs.step)
	}
	if !logs.follow {
		t.Error("the viewer did not start in follow mode")
	}

	out := screen(t, m, 110, 24)
	for _, want := range []string{"Job: build", "STEPS", "Run tests", "go test ./...", "FAIL ghspector/internal/ui"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log view is missing %q:\n%s", want, out)
		}
	}
	// The output of another step is not on screen.
	if strings.Contains(out, "Preparing the runner") {
		t.Errorf("another step's output is on screen:\n%s", out)
	}
}

func TestLogsStepSelectionChangesTheOutput(t *testing.T) {
	m := openLogs(t, 110, 24, workflowServer(t, fixtureRuns(3)))

	m, _ = pressKey(t, m, "k")
	first := screen(t, m, 110, 24)
	if got := logsRoute(t, m).step; got != 0 {
		t.Fatalf("step after k = %d, want 0", got)
	}
	if !strings.Contains(first, "Preparing the runner") {
		t.Errorf("the first step's output is not shown:\n%s", first)
	}
	if strings.Contains(first, "FAIL ghspector/internal/ui") {
		t.Errorf("the failing step's output stayed on screen:\n%s", first)
	}

	// k at the first step does nothing.
	same, _ := pressKey(t, m, "k")
	if got := logsRoute(t, same).step; got != 0 {
		t.Errorf("step after a second k = %d, want it pinned at 0", got)
	}

	// j walks back down through the steps.
	down, _ := pressKey(t, m, "j")
	second, _ := pressKey(t, down, "j")
	if got := logsRoute(t, second).step; got != 2 {
		t.Fatalf("step after two j presses = %d, want 2", got)
	}
}

func TestLogsSkippedStepHasNoOutput(t *testing.T) {
	m := openLogs(t, 110, 24, workflowServer(t, fixtureRuns(3)))
	m, _ = pressKey(t, m, "j")

	out := screen(t, m, 110, 24)
	if got := logsRoute(t, m).step; got != 2 {
		t.Fatalf("step = %d, want the skipped one", got)
	}
	if !strings.Contains(out, "Upload coverage") {
		t.Errorf("the skipped step is not in the sidebar:\n%s", out)
	}
	if !strings.Contains(out, "No logs available for this step") {
		t.Errorf("a step with no output is not explained:\n%s", out)
	}
}

func TestLogsFollowModePinsToTheNewestOutput(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3), withLog(longLog(50)))
	m := openLogs(t, 110, 24, srv)

	logs := logsRoute(t, m)
	if !logs.follow {
		t.Fatal("the viewer did not start in follow mode")
	}
	visible := logs.visibleLines(m.deps)
	out := screen(t, m, 110, 24)
	if !strings.Contains(out, "output line 050") {
		t.Errorf("follow mode is not at the end of the log:\n%s", out)
	}
	if strings.Contains(out, "output line 001") {
		t.Errorf("follow mode is not scrolled to the end:\n%s", out)
	}

	// u detaches from the end.
	m, _ = pressKey(t, m, "u")
	if logsRoute(t, m).follow {
		t.Error("u did not leave follow mode")
	}
	out = screen(t, m, 110, 24)
	if strings.Contains(out, "output line 050") {
		t.Errorf("u did not scroll away from the end:\n%s", out)
	}
	if !strings.Contains(out, "output line 030") {
		t.Errorf("u did not scroll by a screenful of lines:\n%s", out)
	}

	// u again, then d twice: reaching the end turns follow mode back on.
	m, _ = pressKey(t, m, "u")
	m, _ = pressKey(t, m, "d")
	m, _ = pressKey(t, m, "d")
	logs = logsRoute(t, m)
	if !logs.follow {
		t.Fatal("reaching the end of the log did not re-enable follow mode")
	}
	if got := logs.top(visible); got != logs.scrollLimit(visible) {
		t.Errorf("top = %d, want the end of the log at %d", got, logs.scrollLimit(visible))
	}
	if !strings.Contains(screen(t, m, 110, 24), "output line 050") {
		t.Error("follow mode is not back at the end")
	}
}

func TestLogsScrollUpStopsAtTheBeginning(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3), withLog(longLog(50)))
	m := openLogs(t, 110, 24, srv)
	// Far past the start: the offset is clamped rather than going negative.
	for i := 0; i < 30; i++ {
		m, _ = pressKey(t, m, "u")
	}
	logs := logsRoute(t, m)
	if logs.offset != 0 {
		t.Errorf("offset = %d, want it clamped at the start", logs.offset)
	}
	if !strings.Contains(screen(t, m, 110, 24), "Run tests") {
		t.Errorf("the start of the log is not on screen:\n%s", screen(t, m, 110, 24))
	}
}

func TestLogsClipsLongLines(t *testing.T) {
	line := strings.Repeat("x", 300)
	body := "2024-05-01T10:01:00.0000000Z ##[group]Run tests\n" +
		"2024-05-01T10:01:00.0000000Z " + line + "\n" +
		"2024-05-01T10:01:30.0000000Z ##[endgroup]\n"
	srv := workflowServer(t, fixtureRuns(3), withLog(body))
	m := openLogs(t, 110, 24, srv)

	// The frame is exactly the geometry, which is what clipping buys: a wrapped
	// line would make the number of rendered rows depend on the text.
	out := screen(t, m, 110, 24)
	if !strings.Contains(out, "…") {
		t.Errorf("the long line is not shown as clipped:\n%s", out)
	}
	if strings.Contains(out, line[:120]) {
		t.Error("the whole long line is on screen, so it was not clipped")
	}
}

func TestLogsReportsUnavailableOutput(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3), failLogs())
	m := openLogs(t, 110, 24, srv)

	out := screen(t, m, 110, 24)
	// A running job answers with a missing blob rather than an empty log, which
	// is a normal state rather than a failure to explain.
	if !strings.Contains(out, "not yet available for running jobs") {
		t.Errorf("the missing log is not explained:\n%s", out)
	}
	// The screen is still the log viewer, so the message is on it rather than
	// replacing it.
	if _, ok := m.routes.Top().(*Logs); !ok {
		t.Fatalf("top route is %T, want the log viewer", m.routes.Top())
	}
}

func TestLogsPollsOnlyWhileTheJobCanProduceOutput(t *testing.T) {
	t.Run("a running job is refetched", func(t *testing.T) {
		srv := workflowServer(t, fixtureRuns(3))
		m := openLogs(t, 110, 24, srv)
		logs := logsRoute(t, m)
		logs.job.Status = "in_progress"

		next, cmd := logs.poll(m.deps)
		if cmd == nil {
			t.Fatal("a running job's log was not refetched")
		}
		if _, ok := next.(*Logs); !ok {
			t.Fatalf("poll returned %T", next)
		}
	})

	t.Run("a finished job is left alone", func(t *testing.T) {
		srv := workflowServer(t, fixtureRuns(3))
		m := openLogs(t, 110, 24, srv)
		if _, cmd := logsRoute(t, m).poll(m.deps); cmd != nil {
			t.Error("a finished job's log was refetched")
		}
	})
}

func TestLogsHeadingNamesTheJob(t *testing.T) {
	runs := []runSpec{{id: 900, repo: fixtureRepo, status: "completed", conclusion: "failure", attempt: 2, created: workflowNow}}
	srv := workflowServer(t, runs)
	m := openLogs(t, 110, 24, srv)

	if !strings.Contains(screen(t, m, 110, 24), "Job: build (attempt 2)") {
		t.Errorf("the heading does not name the job and attempt:\n%s", screen(t, m, 110, 24))
	}
}

func TestLogsBrowsesTheJob(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := openLogs(t, 110, 24, srv)

	logs := logsRoute(t, m)
	if err := ValidateBrowserURL(jobURL(logs.run, logs.job)); err != nil {
		t.Fatalf("the job URL is not openable: %v", err)
	}
	if _, cmd := pressKey(t, m, "w"); cmd == nil {
		t.Error("w produced no command")
	}
}

func TestLogsStopsItsRequestsWhenPopped(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := openLogs(t, 110, 24, srv)
	logs := logsRoute(t, m)

	back, _ := pressKey(t, m, "esc")
	if back.routes.Len() != 2 {
		t.Fatalf("stack depth = %d, want the job list", back.routes.Len())
	}
	if err := logs.sess.Ctx().Err(); err == nil {
		t.Error("the popped log viewer's requests were not cancelled")
	}
}

// openLogs drives the model from the runs list into the log viewer of the
// finished job.
func openLogs(t *testing.T, w, h int, srv *httptest.Server) Model {
	t.Helper()
	return openLogsFrom(t, openJobs(t, workflowsModel(t, srv, w, h)))
}

// openLogsFrom opens the log viewer of the finished job from a model that is
// already on the job list, so a test that configured the model keeps its
// settings.
func openLogsFrom(t *testing.T, m Model) Model {
	t.Helper()
	// The running job sorts first, so the finished one is the row below it.
	m, _ = pressKey(t, m, "j")
	m, cmd := pressKey(t, m, "enter")
	m = drive(t, m, cmd)
	logsRoute(t, m)
	return m
}
