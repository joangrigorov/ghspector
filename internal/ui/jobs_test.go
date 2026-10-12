package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/store"
)

func TestJobsListsTheJobsOfTheRun(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := workflowsModel(t, srv, 110, 30)
	m = openJobs(t, m)
	out := screen(t, m, 110, 30)

	for _, want := range []string{
		fmt.Sprintf("Workflow: Build %d", jobsRoute(t, m).run.ID),
		"Repo: octocat/api",
		"Branch: main",
		"SHA: abcdef1",
		"JOB NAME",
		"lint",
		"build",
		"STEPS",
		// The skipped step is reported as completed with a skipped
		// conclusion, so all three count as completed.
		"3/3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the job list is missing %q:\n%s", want, out)
		}
	}

	// The running job sorts first, so the thing that is not finished is the
	// thing under the cursor.
	jobs := jobsRoute(t, m)
	if len(jobs.jobs) != 2 {
		t.Fatalf("loaded %d jobs, want 2", len(jobs.jobs))
	}
	if got := jobs.jobs[jobs.cursor].Name; got != "lint" {
		t.Errorf("the cursor starts on %q, want the running job", got)
	}
}

func TestJobsEnterOnARunningJobExplainsItself(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := openJobs(t, workflowsModel(t, srv, 110, 30))

	// The cursor starts on the running job (lint).
	m, cmd := pressKey(t, m, "enter")
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	m = drive(t, m, cmd)
	if m.routes.Len() != 2 {
		t.Fatalf("stack depth = %d, want the jobs route still on top", m.routes.Len())
	}
	if !strings.Contains(screen(t, m, 110, 30), "not yet available for running jobs") {
		t.Errorf("the refusal is not stated:\n%s", screen(t, m, 110, 30))
	}
}

func TestJobsEnterOnAFinishedJobOpensLogs(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := openJobs(t, workflowsModel(t, srv, 110, 30))

	// Down to the finished job, then into its log.
	m, _ = pressKey(t, m, "j")
	selected := jobsRoute(t, m).jobs[jobsRoute(t, m).cursor]
	m, cmd := pressKey(t, m, "enter")
	m = drive(t, m, cmd)

	logs := logsRoute(t, m)
	if logs.job.ID != selected.ID {
		t.Fatalf("the log viewer opened job %d, want %d", logs.job.ID, selected.ID)
	}
	if m.routes.Len() != 3 {
		t.Fatalf("stack depth = %d, want the log viewer above the job list", m.routes.Len())
	}
	if !strings.Contains(screen(t, m, 110, 30), "Job: "+selected.Name) {
		t.Errorf("the log viewer does not name the job:\n%s", screen(t, m, 110, 30))
	}

	// Esc returns to the job list: the route hands the stack a pop, which is
	// what the runtime would run.
	back, cmd := pressKey(t, m, "esc")
	back = drive(t, back, cmd)
	if back.routes.Len() != 2 {
		t.Fatalf("stack depth after esc = %d, want 2", back.routes.Len())
	}
	if _, ok := back.routes.Top().(*Jobs); !ok {
		t.Fatalf("esc landed on %T, want the job list", back.routes.Top())
	}
}

func TestJobsAttemptCycling(t *testing.T) {
	runs := []runSpec{{id: 900, repo: fixtureRepo, status: "completed", conclusion: "failure", attempt: 2, created: workflowNow}}
	srv := workflowServer(t, runs)
	m := openJobs(t, workflowsModel(t, srv, 110, 30))

	jobs := jobsRoute(t, m)
	if jobs.attempt != 2 {
		t.Fatalf("attempt = %d, want the run's attempt", jobs.attempt)
	}
	if !strings.Contains(screen(t, m, 110, 30), "Attempt 2 of 2") {
		t.Errorf("the attempt is not shown:\n%s", screen(t, m, 110, 30))
	}

	// ] is already at the last attempt, so it does nothing.
	same, cmd := pressKey(t, m, "]")
	if cmd != nil {
		t.Error("] produced a command at the last attempt")
	}
	if jobsRoute(t, same).attempt != 2 {
		t.Error("] changed the attempt past the last one")
	}

	// [ moves to the previous attempt, which is a different set of jobs.
	back, cmd := pressKey(t, m, "[")
	if jobsRoute(t, back).attempt != 1 {
		t.Fatalf("attempt after [ = %d, want 1", jobsRoute(t, back).attempt)
	}
	if jobs := jobsRoute(t, back); !jobs.loading || jobs.jobs != nil {
		t.Errorf("switching attempt did not clear the list: loading=%v jobs=%d", jobs.loading, len(jobs.jobs))
	}
	loaded := drive(t, back, cmd)
	if !strings.Contains(screen(t, loaded, 110, 30), "Attempt 1 of 2") {
		t.Errorf("the new attempt is not shown:\n%s", screen(t, loaded, 110, 30))
	}
	if !strings.Contains(screen(t, loaded, 110, 30), "Loading attempt 1 of 2") {
		t.Error("the switch did not announce itself in the footer")
	}

	// [ at the first attempt does nothing.
	first, cmd := pressKey(t, loaded, "[")
	if cmd != nil || jobsRoute(t, first).attempt != 1 {
		t.Error("[ moved past the first attempt")
	}
}

func TestJobsIgnoresAReplyForAnotherAttempt(t *testing.T) {
	runs := []runSpec{{id: 900, repo: fixtureRepo, status: "completed", conclusion: "success", attempt: 2, created: workflowNow}}
	srv := workflowServer(t, runs)
	m := openJobs(t, workflowsModel(t, srv, 110, 30))
	back, _ := pressKey(t, m, "[")

	// A reply for the attempt the user has left is dropped: the list on screen
	// belongs to the attempt that was asked for last.
	route, _ := jobsRoute(t, back).Update(back.deps, jobsMsg{jobs: jobsFixtureOfSize(3), attempt: 2})
	got := route.(*Jobs)
	if len(got.jobs) != 0 {
		t.Errorf("a reply for attempt 2 landed while attempt 1 is on screen: %d jobs", len(got.jobs))
	}
}

func TestJobsApprovalFromTheJobList(t *testing.T) {
	runs := []runSpec{{id: 900, repo: fixtureRepo, status: "waiting", created: workflowNow}}
	srv := workflowServer(t, runs)
	m := openJobs(t, workflowsModel(t, srv, 110, 30))

	jobs := jobsRoute(t, m)
	if !jobs.canApprove() {
		t.Fatalf("the waiting run is not approvable: gates=%v", jobs.perms)
	}
	if !footerHasAction(m, keymap.ActionApprove) {
		t.Error("the footer does not offer approval on the job list")
	}

	m, cmd := pressKey(t, m, "a")
	m = drive(t, m, cmd)
	if m.overlays.Empty() {
		t.Fatal("a did not open the approval overlay")
	}
	m, cmd = pressKey(t, m, "y")
	m = drive(t, m, cmd)
	if !strings.Contains(screen(t, m, 110, 30), "Run approved") {
		t.Errorf("approval was not confirmed:\n%s", screen(t, m, 110, 30))
	}
}

func TestJobsShowsTheApprovalBannerInsteadOfTheTable(t *testing.T) {
	runs := []runSpec{{id: 900, repo: fixtureRepo, status: "waiting", created: workflowNow}}
	srv := workflowServer(t, runs)
	m := openJobs(t, workflowsModel(t, srv, 110, 30))
	out := screen(t, m, 110, 30)

	for _, want := range []string{"Awaiting approval", "protected environment", "Press a to approve it now."} {
		if !strings.Contains(out, want) {
			t.Errorf("the banner is missing %q:\n%s", want, out)
		}
	}
	// The job table is replaced, not drawn underneath: approving is the only
	// thing to do on this screen.
	if strings.Contains(out, "JOB NAME") {
		t.Errorf("the job table is drawn under the banner:\n%s", out)
	}
}

func TestJobsInternalRunBannerExplainsTheLocalRun(t *testing.T) {
	runs := []runSpec{{id: 900, repo: fixtureRepo, status: "completed", conclusion: "action_required", created: workflowNow}}
	srv := workflowServer(t, runs)
	m := openJobs(t, workflowsModel(t, srv, 110, 30))
	out := screen(t, m, 110, 30)

	if !strings.Contains(out, "local pull request") {
		t.Errorf("the banner does not explain the state:\n%s", out)
	}
	// The run is offered, and the offer leads to the browser because the API
	// refuses this kind of run. The overlay that says so is asserted on the runs
	// screen.
	if !strings.Contains(out, "Press a to approve it now.") {
		t.Errorf("the banner does not offer the action:\n%s", out)
	}
}

func TestJobsWithheldApprovalExplainsItselfInTheBanner(t *testing.T) {
	// The environment the run is waiting on does not name the viewer as an
	// approver, so the gate withholds the action and the banner says why.
	runs := []runSpec{{id: 900, repo: fixtureRepo, status: "waiting", created: workflowNow}}
	srv := workflowServer(t, runs, withPendingDeployment(false))
	m := openJobs(t, workflowsModel(t, srv, 110, 30))
	out := screen(t, m, 110, 30)

	if !strings.Contains(out, "Your token cannot approve this run.") {
		t.Errorf("the banner does not explain the withheld approval:\n%s", out)
	}
	if footerHasAction(m, keymap.ActionApprove) {
		t.Error("the footer offers approval that is withheld")
	}
}

func TestJobsBrowserKeysSeparateJobFromRun(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := openJobs(t, workflowsModel(t, srv, 110, 30))

	job := jobsRoute(t, m).jobs[jobsRoute(t, m).cursor]
	run := jobsRoute(t, m).run

	// w opens the job, v opens the run (spec D3). The difference is in the URL
	// the command carries, which is checked by building both.
	jobLink := jobURL(run, job)
	runLink := runURL(run)
	if jobLink == runLink {
		t.Fatalf("the job and run links are the same: %q", jobLink)
	}
	if !strings.Contains(jobLink, "/job/") {
		t.Errorf("the job link %q is not a job URL", jobLink)
	}
	if strings.Contains(runLink, "/job/") {
		t.Errorf("the run link %q is a job URL", runLink)
	}

	for _, key := range []string{"w", "v"} {
		if _, cmd := pressKey(t, m, key); cmd == nil {
			t.Errorf("%s produced no command", key)
		}
	}
}

func TestJobsRefreshAndErrors(t *testing.T) {
	t.Run("refresh refetches", func(t *testing.T) {
		srv := workflowServer(t, fixtureRuns(2))
		m := openJobs(t, workflowsModel(t, srv, 110, 30))
		m, cmd := pressKey(t, m, "r")
		if !jobsRoute(t, m).loading {
			t.Error("refresh did not put the list back into its loading state")
		}
		loaded := drive(t, m, cmd)
		if len(jobsRoute(t, loaded).jobs) != 2 {
			t.Errorf("refresh loaded %d jobs, want 2", len(jobsRoute(t, loaded).jobs))
		}
	})

	t.Run("a fetch failure is reported", func(t *testing.T) {
		srv := workflowServer(t, fixtureRuns(2), failJobs())
		m := openJobs(t, workflowsModel(t, srv, 110, 30))
		out := screen(t, m, 110, 30)
		if !strings.Contains(out, "Could not load jobs") {
			t.Errorf("the failure is not reported:\n%s", out)
		}
	})
}

func TestJobsStopReleasesItsRequests(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(2))
	m := workflowsModel(t, srv, 110, 30)
	m, cmd := pressKey(t, m, "enter")
	m = driveShallow(t, m, cmd)

	jobs := jobsRoute(t, m)
	if err := jobs.sess.Ctx().Err(); err != nil {
		t.Fatalf("the route's context is already done: %v", err)
	}
	back, cmd := pressKey(t, m, "esc")
	back = drive(t, back, cmd)
	if back.routes.Len() != 1 {
		t.Fatalf("stack depth = %d, want 1", back.routes.Len())
	}
	if err := jobs.sess.Ctx().Err(); err == nil {
		t.Error("the popped route's requests were not cancelled")
	}
}

// openJobs opens the Jobs route from the runs list and drives its fetch to
// completion.
func openJobs(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := pressKey(t, m, "enter")
	m = drive(t, m, cmd)
	jobsRoute(t, m)
	return m
}

// driveShallow feeds the messages of a command without following the commands
// the model answers them with, which is how a test keeps a route's fetch in
// flight.
func driveShallow(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range messagesOf(t, cmd) {
		if _, ok := msg.(jobsMsg); ok {
			continue
		}
		next, _ := send(t, m, msg)
		m = next
	}
	return m
}

// jobsFixtureOfSize is a synthetic job list for the tests that need one.
func jobsFixtureOfSize(n int) []store.WorkflowJob {
	out := make([]store.WorkflowJob, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, store.WorkflowJob{
			ID:        int64(7000 + i),
			Name:      "job",
			Status:    "completed",
			StartedAt: workflowNow,
		})
	}
	return out
}
