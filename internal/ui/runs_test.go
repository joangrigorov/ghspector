package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/store"
)

func TestRunsTabLoadsTheScopeWideList(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(4))
	m := workflowsModel(t, srv, 100, 30)
	runs := runsTabOf(t, m)

	if len(runs.runs) != 5 {
		t.Fatalf("loaded %d runs, want 5: %v", len(runs.runs), runIDs(runs.runs))
	}
	if !runs.loaded || runs.loading {
		t.Errorf("loaded = %v, loading = %v, want loaded", runs.loaded, runs.loading)
	}
	// Running work sorts first, then finished work by recency, so the two
	// repositories are interleaved rather than grouped.
	want := []int64{899, 900, 898, 897, 800}
	if got := runIDs(runs.runs); !equalIDs(got, want) {
		t.Errorf("run order = %v, want %v", got, want)
	}
	for _, run := range runs.runs {
		if run.Repository.Name == "" {
			t.Errorf("run %d has no repository attached", run.ID)
		}
	}

	out := screen(t, m, 100, 30)
	for _, want := range []string{"api", "web", "Build 900", "octocat"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table is missing %q:\n%s", want, out)
		}
	}
}

func TestRunsTabCursorStopsAtTheEnds(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	m := workflowsModel(t, srv, 100, 30)

	up, _ := pressKey(t, m, "k")
	if got := runsTabOf(t, up).cursor; got != 0 {
		t.Errorf("cursor moved up from the first row to %d", got)
	}

	down := up
	for i := 0; i < 10; i++ {
		down, _ = pressKey(t, down, "j")
	}
	// Four runs and no further page, so the last row is the end of the list.
	if got := runsTabOf(t, down).cursor; got != 3 {
		t.Errorf("cursor = %d, want it pinned to the last row", got)
	}
}

func TestRunsTabLoadMoreSentinel(t *testing.T) {
	// Nine runs in the api repository: eight fill the page and the ninth is the
	// reason the count reports another page.
	srv := workflowServer(t, fixtureRuns(9))
	m := workflowsModel(t, srv, 100, 30)
	runs := runsTabOf(t, m)

	if !runs.hasMore {
		t.Fatal("hasMore = false with a run beyond the page, want true")
	}
	if got := runs.selectable(); got != len(runs.runs)+1 {
		t.Fatalf("selectable rows = %d, want the runs plus the sentinel", got)
	}
	if !strings.Contains(screen(t, m, 100, 30), "Load More") {
		t.Errorf("the sentinel row is not rendered:\n%s", screen(t, m, 100, 30))
	}

	// Walking to the sentinel and pressing enter fetches the next page, which
	// holds the ninth run: the list grows and the sentinel goes away, because
	// the count had nothing beyond it.
	loaded := m
	for i := 0; i < len(runs.runs); i++ {
		loaded, _ = pressKey(t, loaded, "j")
	}
	if got := runsTabOf(t, loaded).cursor; got != len(runs.runs) {
		t.Fatalf("cursor = %d, want the sentinel row", got)
	}
	loaded, cmd := pressKey(t, loaded, "enter")
	next := drive(t, loaded, cmd)
	after := runsTabOf(t, next)

	if after.hasMore {
		t.Error("the sentinel survived the last page")
	}
	if len(after.runs) != len(runs.runs)+1 {
		t.Errorf("the next page left %d runs, want %d", len(after.runs), len(runs.runs)+1)
	}
	if after.cursor > len(after.runs)-1 {
		t.Errorf("cursor = %d after the sentinel disappeared, want it clamped", after.cursor)
	}
	if strings.Contains(screen(t, next, 100, 30), "Load More") {
		t.Error("the sentinel is still rendered after the list ended")
	}
	if after.loaded != true {
		t.Error("the list is not marked loaded")
	}
}

func TestRunsTabRefreshClearsAndRefetches(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(4))
	m := workflowsModel(t, srv, 100, 30)
	m, _ = pressKey(t, m, "j")

	m, cmd := pressKey(t, m, "r")
	if runs := runsTabOf(t, m); !runs.loading || runs.runs != nil || runs.cursor != 0 {
		t.Fatalf("refresh did not clear the list: loading=%v runs=%d cursor=%d",
			runs.loading, len(runs.runs), runs.cursor)
	}
	next := drive(t, m, cmd)
	if got := len(runsTabOf(t, next).runs); got != 5 {
		t.Errorf("refresh loaded %d runs, want 5", got)
	}
	if !strings.Contains(screen(t, next, 100, 30), "Refreshing workflow runs") {
		t.Error("refresh did not announce itself in the footer")
	}
}

func TestRunsTabActorFilterTogglesWithMine(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(4))
	m := workflowsModel(t, srv, 100, 30)

	m, cmd := pressKey(t, m, "m")
	next := drive(t, m, cmd)
	if got := runsTabOf(t, next).actor; got != "octocat" {
		t.Fatalf("actor filter = %q, want the signed-in login", got)
	}
	if !strings.Contains(screen(t, next, 100, 30), "Filter: actor @octocat") {
		t.Error("the active filter is not stated on screen")
	}

	// Pressing it again stops showing only my runs.
	next, cmd = pressKey(t, next, "m")
	next = drive(t, next, cmd)
	if got := runsTabOf(t, next).actor; got != "" {
		t.Fatalf("actor filter = %q, want it cleared", got)
	}
}

func TestRunsTabRepositoryFilterFromThePicker(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(4))
	m := workflowsModel(t, srv, 100, 30)

	m, cmd := pressKey(t, m, "f")
	m = drive(t, m, cmd)
	if m.overlays.Empty() {
		t.Fatal("f did not open the filter menu")
	}

	// r in the menu scope picks the repository picker, which fetches the
	// repositories and opens the list above the menu.
	m, cmd = pressKey(t, m, "r")
	m = drive(t, m, cmd)
	if m.overlays.Len() != 2 {
		t.Fatalf("overlay depth = %d, want the picker above the menu", m.overlays.Len())
	}
	if !strings.Contains(screen(t, m, 100, 30), "Filter by repository") {
		t.Errorf("the picker is not on screen:\n%s", screen(t, m, 100, 30))
	}

	m, cmd = pressKey(t, m, "enter")
	m = drive(t, m, cmd)
	if got := runsTabOf(t, m).repo; got != fixtureRepo {
		t.Fatalf("repo filter = %q, want %q", got, fixtureRepo)
	}
	if !m.overlays.Empty() {
		t.Errorf("the overlays stayed open after a choice: %d", m.overlays.Len())
	}

	cleared, cmd := pressKey(t, m, "x")
	cleared = drive(t, cleared, cmd)
	runs := runsTabOf(t, cleared)
	if runs.actor != "" || runs.repo != "" {
		t.Fatalf("filters after x: actor=%q repo=%q", runs.actor, runs.repo)
	}
}

func TestRunsTabActorPromptIsPrefilledAndApplied(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(4))
	m := workflowsModel(t, srv, 100, 30)

	// Set a filter first, so the prompt has something to be pre-filled with.
	m, cmd := pressKey(t, m, "m")
	m = drive(t, m, cmd)

	m, cmd = pressKey(t, m, "f")
	m = drive(t, m, cmd)
	if !strings.Contains(screen(t, m, 100, 30), "Filter by user (actor)") {
		t.Fatal("the filter menu does not offer the actor filter")
	}

	m, cmd = pressKey(t, m, "u")
	m = drive(t, m, cmd)
	if !strings.Contains(screen(t, m, 100, 30), "octocat") {
		t.Errorf("the prompt is not pre-filled with the current filter:\n%s", screen(t, m, 100, 30))
	}

	// A backspace edits it, and submitting applies what is left.
	m, _ = pressKey(t, m, "backspace")
	m = typeKeys(t, m, "2")
	m, cmd = pressKey(t, m, "enter")
	m = drive(t, m, cmd)

	if got := runsTabOf(t, m).actor; got != "octoca2" {
		t.Fatalf("actor filter = %q, want the edited value", got)
	}
	if !m.overlays.Empty() {
		t.Errorf("overlays remain open after submitting: %d", m.overlays.Len())
	}
}

func TestRunsTabOpensJobsAndReturns(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(4))
	m := workflowsModel(t, srv, 100, 30)
	selected := runsTabOf(t, m).runs[0]

	m, cmd := pressKey(t, m, "enter")
	if msgs := messagesOf(t, cmd); len(msgs) == 0 {
		t.Fatal("enter produced no message")
	}
	m = drive(t, m, cmd)

	jobs := jobsRoute(t, m)
	if jobs.run.ID != selected.ID {
		t.Fatalf("the jobs route opened run %d, want %d", jobs.run.ID, selected.ID)
	}
	if m.routes.Len() != 2 {
		t.Fatalf("stack depth = %d, want 2", m.routes.Len())
	}
	out := screen(t, m, 100, 30)
	if !strings.Contains(out, "Workflow: Build "+strconv.FormatInt(selected.ID, 10)) {
		t.Errorf("the jobs screen does not name the run:\n%s", out)
	}

	// Esc returns to the list with the selection intact. The route hands the
	// stack a pop, which is what the runtime runs.
	back, cmd := pressKey(t, m, "esc")
	back = drive(t, back, cmd)
	if back.routes.Len() != 1 {
		t.Fatalf("stack depth after esc = %d, want 1", back.routes.Len())
	}
	if got := runsTabOf(t, back).cursor; got != 0 {
		t.Errorf("cursor = %d after returning, want 0", got)
	}
}

func TestRunsTabApprovalIsOfferedOnlyForWaitingRuns(t *testing.T) {
	// A queued run cannot be approved, so a does nothing and the footer does not
	// advertise the key.
	queued := []runSpec{{id: 1, repo: fixtureRepo, status: "queued", created: workflowNow}}
	srv := workflowServer(t, queued)
	m := workflowsModel(t, srv, 100, 30)

	if footerHasAction(m, keymap.ActionApprove) {
		t.Error("the footer offers approval for a run that cannot be approved")
	}
	quiet, cmd := pressKey(t, m, "a")
	if cmd != nil {
		t.Error("a produced a command for a queued run")
	}
	if !quiet.overlays.Empty() {
		t.Errorf("a opened an overlay for a queued run: %T", quiet.overlays.Top())
	}
}

func TestRunsTabApprovalForAWaitingRun(t *testing.T) {
	waiting := []runSpec{{id: 42, repo: fixtureRepo, status: "waiting", created: workflowNow}}
	srv := workflowServer(t, waiting)
	m := workflowsModel(t, srv, 100, 30)

	// Selecting the run asks whether it may be approved; the answer is what
	// makes the key available.
	runs := runsTabOf(t, m)
	if !runs.canApprove(runs.runs[0]) {
		t.Fatalf("the waiting run is not approvable: gates=%v", runs.perms)
	}
	if !footerHasAction(m, keymap.ActionApprove) {
		t.Error("the footer does not offer approval for a run the viewer may approve")
	}

	m, cmd := pressKey(t, m, "a")
	m = drive(t, m, cmd)
	if m.overlays.Empty() {
		t.Fatal("a did not open the approval overlay")
	}
	out := screen(t, m, 100, 30)
	if !strings.Contains(out, "Approve this run?") {
		t.Errorf("the overlay does not ask for confirmation:\n%s", out)
	}
	if !strings.Contains(out, "y/Y") || !strings.Contains(out, "w/W") {
		t.Errorf("the footer does not show the overlay's own keys:\n%s", out)
	}

	// Confirming approves the run and refreshes the list.
	m, cmd = pressKey(t, m, "y")
	m = drive(t, m, cmd)
	if !m.overlays.Empty() {
		t.Errorf("the overlay stayed open after confirming: %T", m.overlays.Top())
	}
	if !strings.Contains(screen(t, m, 100, 30), "Run approved") {
		t.Errorf("approval was not confirmed in the footer:\n%s", screen(t, m, 100, 30))
	}
}

func TestRunsTabApprovalFailureIsReported(t *testing.T) {
	waiting := []runSpec{{id: 42, repo: fixtureRepo, status: "waiting", created: workflowNow}}
	srv := workflowServer(t, waiting, failApproval())
	m := workflowsModel(t, srv, 100, 30)

	m, cmd := pressKey(t, m, "a")
	m = drive(t, m, cmd)
	m, cmd = pressKey(t, m, "y")
	m = drive(t, m, cmd)

	out := screen(t, m, 100, 30)
	if !strings.Contains(out, "Could not approve the run") {
		t.Errorf("a failed approval is not reported:\n%s", out)
	}
}

func TestRunsTabInternalActionRequiredOffersTheBrowser(t *testing.T) {
	// An internal run that needs approval cannot be approved through the API,
	// so the overlay says so and both keys lead to GitHub.
	internal := []runSpec{{id: 77, repo: fixtureRepo, status: "completed", conclusion: "action_required", created: workflowNow}}
	srv := workflowServer(t, internal)
	m := workflowsModel(t, srv, 100, 30)

	runs := runsTabOf(t, m)
	if !runs.canApprove(runs.runs[0]) {
		t.Fatalf("an internal action_required run is not offered: gates=%v", runs.perms)
	}
	m, cmd := pressKey(t, m, "a")
	m = drive(t, m, cmd)

	out := screen(t, m, 100, 30)
	if !strings.Contains(out, "manual approval") {
		t.Errorf("the overlay does not explain the API refusal:\n%s", out)
	}
	// The body is wrapped, so only an unbroken fragment can be searched for.
	if !strings.Contains(out, "allow it to be approved through the API") {
		t.Errorf("the overlay does not state the reason:\n%s", out)
	}
	if !strings.Contains(out, "Approve on GitHub") {
		t.Errorf("the footer does not offer the browser path:\n%s", out)
	}
}

func TestRunsTabForkActionRequiredNeedsWriteAccess(t *testing.T) {
	fork := []runSpec{{id: 88, repo: fixtureRepo, status: "completed", conclusion: "action_required", created: workflowNow, fork: true}}

	// The repository belongs to another account, so the viewer's permission on
	// it decides the answer rather than ownership.
	t.Run("write access is offered", func(t *testing.T) {
		srv := workflowServer(t, fork, withPermission("write"), withRepoOwner("acme"))
		m := workflowsModel(t, srv, 100, 30)
		runs := runsTabOf(t, m)
		if !runs.canApprove(runs.runs[0]) {
			t.Fatalf("a fork run on a writable repository is not offered: gates=%v", runs.perms)
		}
	})

	t.Run("read access is withheld with a reason", func(t *testing.T) {
		srv := workflowServer(t, fork, withPermission("read"), withRepoOwner("acme"))
		m := workflowsModel(t, srv, 100, 30)
		runs := runsTabOf(t, m)
		if runs.canApprove(runs.runs[0]) {
			t.Fatal("approval was offered without write access")
		}
		reason := runs.perms[88].Reason
		if !strings.Contains(reason, "write access") {
			t.Errorf("the reason does not explain the missing permission: %q", reason)
		}
		if footerHasAction(m, keymap.ActionApprove) {
			t.Error("the footer offers a key that does nothing")
		}
	})

	t.Run("the viewer's own repository is theirs to approve", func(t *testing.T) {
		srv := workflowServer(t, fork, withPermission("read"))
		m := workflowsModel(t, srv, 100, 30)
		runs := runsTabOf(t, m)
		if !runs.canApprove(runs.runs[0]) {
			t.Errorf("a run in the viewer's own repository is not offered: gates=%v", runs.perms)
		}
	})

	t.Run("a missing scope is reported before the permission is asked", func(t *testing.T) {
		srv := workflowServer(t, fork, withScopes("repo"), withRepoOwner("acme"))
		m := workflowsModel(t, srv, 100, 30)
		runs := runsTabOf(t, m)
		reason := runs.perms[88].Reason
		if !strings.Contains(reason, "workflow") || !strings.Contains(reason, "gh auth refresh") {
			t.Errorf("the reason does not name the missing scope and the fix: %q", reason)
		}
	})
}

func TestRunsTabKeepsApprovalAnswersPerRun(t *testing.T) {
	runs := []runSpec{
		{id: 1, repo: fixtureRepo, status: "waiting", created: workflowNow},
		{id: 2, repo: fixtureRepo, status: "completed", conclusion: "success", created: workflowNow.Add(-time.Minute)},
	}
	srv := workflowServer(t, runs)
	m := workflowsModel(t, srv, 100, 30)

	tab := runsTabOf(t, m)
	if !tab.canApprove(tab.runs[0]) {
		t.Fatal("the waiting run was not asked about")
	}
	// A run that needs nothing is never asked about, so it has no entry.
	if len(tab.perms) != 1 {
		t.Errorf("cached gates = %v, want only the run that needs one", tab.perms)
	}

	// A refresh keeps the answers: the same runs are on screen.
	refreshed, cmd := pressKey(t, m, "r")
	refreshed = drive(t, refreshed, cmd)
	if len(runsTabOf(t, refreshed).perms) != 1 {
		t.Error("a refresh dropped the cached answers")
	}
}

func TestRunsTabBrowserKeyNeedsASelection(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(2))
	m := workflowsModel(t, srv, 100, 30)

	// The browser is never opened in a test; the URL a run hands it is checked
	// by building the command and inspecting the message it produces.
	_, cmd := pressKey(t, m, "w")
	if cmd == nil {
		t.Fatal("w produced no command for a selected run")
	}
	if err := ValidateBrowserURL(runURL(runsTabOf(t, m).runs[0])); err != nil {
		t.Errorf("the run URL is not openable: %v", err)
	}

	// With an empty list there is nothing to open, and the key does nothing
	// rather than reporting a URL problem for a run that is not there.
	empty := workflowsModel(t, workflowServer(t, nil), 100, 30)
	if _, cmd := pressKey(t, empty, "w"); cmd != nil {
		t.Error("w produced a command with nothing selected")
	}
}

func TestRunsTabShowsAFetchFailure(t *testing.T) {
	// Every repository fails, so the store reports the first error instead of an
	// empty list.
	srv := workflowServer(t, fixtureRuns(2), failEveryRunList())
	m := workflowsModel(t, srv, 100, 30)

	out := screen(t, m, 100, 30)
	if !strings.Contains(out, "Could not load workflow runs") {
		t.Errorf("the failure is not reported on screen:\n%s", out)
	}
	if !strings.Contains(out, "no fixture") {
		t.Errorf("the failure does not carry the reason:\n%s", out)
	}
}

func TestRunsTabClampsAfterTheListShrinks(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(4))
	m := workflowsModel(t, srv, 100, 30)
	for i := 0; i < 4; i++ {
		m, _ = pressKey(t, m, "j")
	}

	// A shorter page arrives, which is what a filter change looks like by the
	// time the reply lands.
	main := m.routes.Top().(Main)
	next, _ := main.Update(m.deps, runsLoadedMsg{
		runs: []store.WorkflowRun{{ID: 1, Status: "queued"}},
		page: 1,
	})
	main = next.(Main)
	if main.runs.cursor != 0 {
		t.Errorf("cursor = %d after a shorter list arrived, want 0", main.runs.cursor)
	}
}

func TestRunsTabColumnsSurviveEveryWidth(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(3))
	for _, width := range []int{50, 60, 80, 100, 140, 200} {
		m := workflowsModel(t, srv, width, 24)
		out := screen(t, m, width, 24)
		if !strings.Contains(out, "WORKFLOW RUN") {
			t.Errorf("width %d: the run column header is gone:\n%s", width, out)
		}
	}
}

// runIDs is the IDs of a run list, in order.
func runIDs(runs []store.WorkflowRun) []int64 {
	out := make([]int64, len(runs))
	for i, r := range runs {
		out[i] = r.ID
	}
	return out
}

// equalIDs compares two ID lists.
func equalIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
