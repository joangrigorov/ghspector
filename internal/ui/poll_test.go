package ui

import (
	"net/http/httptest"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/store"
)

// The poll loop is asserted by releasing the tick the model asked for, so the
// tests state the cadence and the effect rather than sleeping through an
// interval.

const (
	testWorkflowsInterval = 5 * time.Second
	testPRsInterval       = 10 * time.Second
	testIssuesInterval    = 20 * time.Second
)

// pollingModel is a ready model with polling enabled on a clock the test
// releases by hand.
func pollingModel(t *testing.T, srv *httptest.Server, w, h int) (Model, *fixedClock) {
	t.Helper()
	clock := newFixedClock(workflowNow)
	m := readyModel(t, srv, w, h, func(o *Options) {
		o.Clock = clock
		o.PollWorkflows = testWorkflowsInterval
		o.PollPRs = testPRsInterval
		o.PollIssues = testIssuesInterval
	})
	return m, clock
}

func TestPollTickIsArmedForTheActiveTab(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m, clock := pollingModel(t, srv, 100, 30)

	// The interval is recorded when the tick is asked for, which is when the
	// command runs.
	armAndRelease(t, m, clock)

	// The default tab is Pull Requests, which runs on the slower cadence.
	if got := clock.waited(); len(got) != 1 || got[0] != testPRsInterval {
		t.Fatalf("the pull request tab waited %v, want %v", got, testPRsInterval)
	}

	// The Workflows tab has its own cadence.
	m, _ = pressKey(t, m, "tab")
	armAndRelease(t, m, clock)
	if got := clock.waited(); len(got) != 2 || got[1] != testWorkflowsInterval {
		t.Fatalf("the workflows tab waited %v, want %v", got, testWorkflowsInterval)
	}

	// The Issues tab has an interval of its own, which is the v2 fix spec §5.4
	// asks for: the previous version polled it on the workflows cadence.
	m, _ = pressKey(t, m, "tab")
	if m.routes.Top().Scope() != keymap.MainIssues {
		t.Fatalf("scope = %q, want the issue tab", m.routes.Top().Scope())
	}
	armAndRelease(t, m, clock)
	if got := clock.waited(); len(got) != 3 || got[2] != testIssuesInterval {
		t.Fatalf("the issue tab waited %v, want %v", got, testIssuesInterval)
	}
}

// TestPollTickRefreshesTheIssueList is the spec §5.4 target for the third list:
// main + Issues → issues. The poll folds its page into the list rather than
// clearing it, so the table does not empty once per interval.
func TestPollTickRefreshesTheIssueList(t *testing.T) {
	srv := prServer(t)
	clock := newFixedClock(workflowNow)
	m := issuesModel(t, srv, 120, 40, func(o *Options) {
		o.Clock = clock
		o.PollIssues = testIssuesInterval
	})

	before := issuesTabOf(t, m)
	if len(before.issues) == 0 {
		t.Fatal("the issue list is empty")
	}

	poll, ok := clock.release(t, m.armPoll()).(pollMsg)
	if !ok {
		t.Fatal("the released tick did not produce a poll message")
	}
	next, cmd := send(t, m, poll)
	if cmd == nil {
		t.Fatal("the poll produced no command")
	}
	refreshed := drive(t, next, cmd)

	issues := issuesTabOf(t, refreshed)
	if len(issues.issues) != len(before.issues) {
		t.Errorf("the poll changed the list from %d issues to %d", len(before.issues), len(issues.issues))
	}
	if issues.loading {
		t.Error("the poll left the list in its loading state")
	}
}

// armAndRelease arms a poll tick and releases it, which is what asks the clock
// for its interval.
func armAndRelease(t *testing.T, m Model, clock *fixedClock) tea.Msg {
	t.Helper()
	cmd := m.armPoll()
	if cmd == nil {
		t.Fatal("polling was not armed")
	}
	msg := clock.release(t, cmd)
	if _, ok := msg.(pollMsg); !ok {
		t.Fatalf("the tick produced %T, want a poll message", msg)
	}
	return msg
}

func TestPollTickRefreshesTheRunsList(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m, clock := pollingModel(t, srv, 100, 30)
	m, cmd := pressKey(t, m, "tab")
	m = drive(t, m, cmd)

	before := len(runsTabOf(t, m).runs)
	if before == 0 {
		t.Fatal("the runs list is empty")
	}

	// The tick the model armed is released, and the message it produces is fed
	// back the way the runtime would.
	poll, ok := clock.release(t, m.armPoll()).(pollMsg)
	if !ok {
		t.Fatalf("the released tick did not produce a poll message")
	}
	next, cmd := send(t, m, poll)
	if cmd == nil {
		t.Fatal("the poll produced no command")
	}
	refreshed := drive(t, next, cmd)

	runs := runsTabOf(t, refreshed)
	if len(runs.runs) != before {
		t.Errorf("the poll changed the list from %d runs to %d", before, len(runs.runs))
	}
	if runs.loading {
		t.Error("the poll left the list in its loading state")
	}
	// A poll folds its result into the list rather than clearing it first: a
	// table that empties once per interval flickers.
	if len(runs.runs) == 0 {
		t.Error("the poll emptied the list")
	}
}

func TestPollTickMergesARunThatChanged(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m, clock := pollingModel(t, srv, 100, 30)
	m, cmd := pressKey(t, m, "tab")
	m = drive(t, m, cmd)

	before := runsTabOf(t, m).runs
	selected := before[0].ID

	// A poll reply carrying the same run in a new state replaces its row
	// instead of adding a second one.
	main := m.routes.Top().(Main)
	updated := before[0]
	updated.Status = "completed"
	updated.Conclusion = "success"
	next, _ := main.Update(m.deps, runsLoadedMsg{runs: []store.WorkflowRun{updated}})
	main = next.(Main)

	after := main.runs.runs
	if len(after) != len(before) {
		t.Fatalf("the list grew from %d runs to %d", len(before), len(after))
	}
	var found bool
	for _, run := range after {
		if run.ID == selected {
			found = true
			if run.Status != "completed" {
				t.Errorf("run %d still reports status %q", run.ID, run.Status)
			}
		}
	}
	if !found {
		t.Errorf("run %d disappeared from the list", selected)
	}
	_ = clock
}

func TestPollTickIsDroppedWhenItIsStale(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m, _ := pollingModel(t, srv, 100, 30)
	m, cmd := pressKey(t, m, "tab")
	m = drive(t, m, cmd)

	// A token no live session holds belongs to a state that has been left
	// behind, so it starts no fetch.
	stale, cmd := send(t, m, pollMsg{token: 999999})
	if cmd != nil {
		t.Error("a stale tick produced a command")
	}
	if stale.routes.Len() != 1 {
		t.Errorf("a stale tick changed the route stack: %d", stale.routes.Len())
	}
}

func TestPollTickIsIgnoredBeforeStartupFinishes(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	clock := newFixedClock(workflowNow)
	m := newModel(t, srv, 100, 30, func(o *Options) {
		o.Clock = clock
		o.PollWorkflows = testWorkflowsInterval
		o.PollPRs = testPRsInterval
	})
	if m.stage != stageLoading {
		t.Fatalf("stage = %v, want the splash", m.stage)
	}

	tick := clock.release(t, m.armPoll())
	if _, cmd := send(t, m, tick); cmd != nil {
		t.Error("the splash answered a poll tick with a fetch")
	}
}

func TestPollTickRefreshesTheJobList(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m, clock := pollingModel(t, srv, 110, 24)
	m, cmd := pressKey(t, m, "tab")
	m = drive(t, m, cmd)
	m = openJobs(t, m)

	// The fixture's first job is the running one, so a tick is worth acting on.
	if !jobsRoute(t, m).hasActive() {
		t.Fatal("the fixture has no running job to poll")
	}
	poll, ok := clock.release(t, m.armPoll()).(pollMsg)
	if !ok {
		t.Fatal("the released tick did not produce a poll message")
	}
	next, cmd := send(t, m, poll)
	if cmd == nil {
		t.Fatal("a tick on the job list produced no fetch")
	}
	refreshed := drive(t, next, cmd)
	if len(jobsRoute(t, refreshed).jobs) != 2 {
		t.Errorf("the poll changed the job list to %d jobs", len(jobsRoute(t, refreshed).jobs))
	}

	// A list with nothing running is left alone.
	done := jobsRoute(t, refreshed)
	for i := range done.jobs {
		done.jobs[i].Status = "completed"
	}
	if _, cmd := done.poll(refreshed.deps); cmd != nil {
		t.Error("a finished job list was refetched")
	}
}

func TestPollTickRefreshesTheLogOfARunningJob(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m, clock := pollingModel(t, srv, 110, 24)
	m, cmd := pressKey(t, m, "tab")
	m = drive(t, m, cmd)
	m = openLogsFrom(t, openJobs(t, m))

	// Arming the poll is what makes the session token live, which is what a tick
	// from the runtime carries.
	poll, ok := clock.release(t, m.armPoll()).(pollMsg)
	if !ok {
		t.Fatal("the released tick did not produce a poll message")
	}
	if _, tick := send(t, m, poll); tick == nil {
		t.Error("a live tick was dropped instead of re-arming the next one")
	}

	// The viewed job is finished, so the tick is not worth acting on.
	logs := logsRoute(t, m)
	if logs.job.Status == "in_progress" {
		t.Fatal("the fixture's viewed job is still running")
	}
	if _, cmd := logs.poll(m.deps); cmd != nil {
		t.Error("a finished job's log was refetched on a tick")
	}

	// While the job can still produce output, the log is refetched.
	logs.job.Status = "in_progress"
	if _, cmd := logs.poll(m.deps); cmd == nil {
		t.Error("a running job's log was not refetched")
	}
}

func TestPollingStopsWhenTheIntervalIsZero(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := readyModel(t, srv, 100, 30) // no intervals configured
	if cmd := m.armPoll(); cmd != nil {
		t.Error("polling was armed with no interval configured")
	}
	if m.pollToken != 0 {
		t.Errorf("poll token = %d, want no live session", m.pollToken)
	}
}
