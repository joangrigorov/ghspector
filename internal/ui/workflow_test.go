package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
)

// The workflow fixtures are built from a list of runs rather than from a static
// JSON blob, so each test states the states it cares about. A test that needs a
// waiting run says so by naming one.

// workflowNow is the instant the workflow fixtures are built around.
var workflowNow = time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

// fixedClock is a Clock a test owns: it reports one fixed instant, so a
// duration on screen is a function of the fixtures, and it fires only the ticks
// a test releases.
type fixedClock struct {
	mu    sync.Mutex
	at    time.Time
	waits []time.Duration
	fire  chan time.Time
}

// newFixedClock returns a clock stopped at one instant.
func newFixedClock(at time.Time) *fixedClock {
	return &fixedClock{at: at, fire: make(chan time.Time, 8)}
}

// Now reports the fixed instant.
func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// After records the wait and returns the channel a test releases.
func (c *fixedClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	return c.fire
}

// waited returns the intervals a command has asked to wait for.
func (c *fixedClock) waited() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]time.Duration, len(c.waits))
	copy(out, c.waits)
	return out
}

// release runs a waiting command and hands it the tick it is waiting for.
func (c *fixedClock) release(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to release")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case c.fire <- c.Now():
	case <-time.After(2 * time.Second):
		t.Fatal("the command never asked for a tick")
	}
	select {
	case msg := <-done:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("the tick did not produce a message")
		return nil
	}
}

// The two fixture repositories, most recently pushed first.
const (
	fixtureRepo = "api"
	secondRepo  = "web"
)

// runSpec is one run in a fixture.
type runSpec struct {
	id         int64
	repo       string
	status     string
	conclusion string
	attempt    int
	created    time.Time
	// fork marks a run that came from a pull request from a fork, which is the
	// case GitHub lets the API approve.
	fork bool
}

// fixtureRuns builds the standard run list: apiCount runs in the first
// repository, cycling through the states a row can be drawn in, plus one run in
// the second repository so the list has to merge two of them.
//
// The page size is 8, so apiCount=8 makes the Load More row appear.
func fixtureRuns(apiCount int) []runSpec {
	states := []struct{ status, conclusion string }{
		{"waiting", ""},
		{"in_progress", ""},
		{"completed", "failure"},
		{"completed", "success"},
		{"queued", ""},
		{"completed", "cancelled"},
	}
	specs := make([]runSpec, 0, apiCount+1)
	for i := 0; i < apiCount; i++ {
		state := states[i%len(states)]
		specs = append(specs, runSpec{
			id:         int64(900 - i),
			repo:       fixtureRepo,
			status:     state.status,
			conclusion: state.conclusion,
			attempt:    i%2 + 1,
			created:    workflowNow.Add(-time.Duration(i) * time.Minute),
		})
	}
	return append(specs, runSpec{
		id:         800,
		repo:       secondRepo,
		status:     "completed",
		conclusion: "success",
		attempt:    1,
		created:    workflowNow.Add(-time.Hour),
	})
}

// serverConfig is the fixture server's behaviour, which tests vary to reach the
// states a happy path never does.
type serverConfig struct {
	// permission is what the collaborators endpoint reports.
	permission string
	// scopes is the X-OAuth-Scopes header. Empty means the API does not report
	// scopes, which the gate treats as unknown rather than insufficient.
	scopes string
	// failRuns makes every run list fail.
	failRuns bool
	// failApproval makes the approve endpoints fail.
	failApproval bool
	// owner is the account the fixture repositories belong to. It is not
	// always the viewer: a repository the viewer owns takes the "it is yours to
	// approve" path instead of the permission check.
	owner string
	// log is the body the job log endpoint returns.
	log string
	// failJobs makes every job list request fail.
	failJobs bool
	// canApproveDeployment is what the pending deployments endpoint reports.
	canApproveDeployment bool
	// failLogs makes the log endpoint answer with a missing blob.
	failLogs bool
}

// serverOption adjusts the fixture server.
type serverOption func(*serverConfig)

// withPermission sets the repository permission the API reports.
func withPermission(permission string) serverOption {
	return func(c *serverConfig) { c.permission = permission }
}

// withScopes sets the OAuth scopes the API reports.
func withScopes(scopes string) serverOption {
	return func(c *serverConfig) { c.scopes = scopes }
}

// failEveryRunList makes every run list request fail, which is what an
// unreachable context looks like.
func failEveryRunList() serverOption {
	return func(c *serverConfig) { c.failRuns = true }
}

// failJobs makes every job list request fail.
func failJobs() serverOption {
	return func(c *serverConfig) { c.failJobs = true }
}

// failApproval makes the approval write fail.
func failApproval() serverOption {
	return func(c *serverConfig) { c.failApproval = true }
}

// withRepoOwner puts the fixture repositories in another account's name.
func withRepoOwner(owner string) serverOption {
	return func(c *serverConfig) { c.owner = owner }
}

// withPendingDeployment sets whether the environment on the run reports that
// the viewer may approve it.
func withPendingDeployment(canApprove bool) serverOption {
	return func(c *serverConfig) { c.canApproveDeployment = canApprove }
}

// failLogs makes the log endpoint answer the way it does for a job that is
// still running: a missing blob rather than an empty log.
func failLogs() serverOption {
	return func(c *serverConfig) { c.failLogs = true }
}

// withLog replaces the job log the transport reads.
func withLog(body string) serverOption {
	return func(c *serverConfig) { c.log = body }
}

// workflowServer serves the workflow endpoints for a fixed run list. Page 2 of
// every run list is empty, which is how the list ends.
//
// The endpoint paths are the ones the transport builds: repositories for the
// context, a run list per repository, one job list per attempt, one log per
// job, and the endpoints the approval gate reads.
func workflowServer(t *testing.T, runs []runSpec, opts ...serverOption) *httptest.Server {
	t.Helper()
	config := serverConfig{
		permission:           "write",
		owner:                "octocat",
		log:                  jobLogBody,
		canApproveDeployment: true,
	}
	for _, opt := range opts {
		opt(&config)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if config.scopes != "" {
			w.Header().Set("X-OAuth-Scopes", config.scopes)
		}
		switch {
		case path == "/user":
			writeBody(w, `{"login":"octocat","id":1}`)
		case path == "/user/orgs":
			writeBody(w, `[{"login":"acme","id":2}]`)
		case path == "/users/octocat/repos":
			writeBody(w, reposBody(t, config.owner))
		case strings.HasSuffix(path, "/actions/runs"):
			if config.failRuns {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			if r.URL.Query().Get("page") == "2" {
				writeBody(w, runsBody(t, config.owner))
				return
			}
			writeBody(w, runsBody(t, config.owner, forRepo(runs, repoFromRunPath(path))...))
		case strings.HasSuffix(path, "/jobs"):
			if config.failJobs {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			writeBody(w, jobsBody(t))
		case strings.HasSuffix(path, "/logs"):
			if config.failLogs {
				w.WriteHeader(http.StatusNotFound)
				writeBody(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>BlobNotFound</Code>`+
					`<Message>The specified blob does not exist.</Message></Error>`)
				return
			}
			writeBody(w, config.log)
		case strings.HasSuffix(path, "/pending_deployments"):
			if config.failApproval {
				w.WriteHeader(http.StatusForbidden)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			writeBody(w, fmt.Sprintf(
				`[{"environment":{"id":7,"name":"production"},"current_user_can_approve":%t}]`,
				config.canApproveDeployment))
		case strings.Contains(path, "/collaborators/") && strings.HasSuffix(path, "/permission"):
			writeBody(w, `{"permission":"`+config.permission+`","user":{"login":"octocat"}}`)
		case strings.HasSuffix(path, "/approve"):
			if config.failApproval {
				w.WriteHeader(http.StatusForbidden)
				writeBody(w, `{"message":"no fixture"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
			writeBody(w, `{"message":"no fixture"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeBody writes a fixture response.
func writeBody(w http.ResponseWriter, body string) {
	if body != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	_, _ = w.Write([]byte(body))
}

// repoFromRunPath reads the repository name out of a run list path.
func repoFromRunPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// forRepo picks the runs that belong to one repository, in the order the fixture
// lists them.
func forRepo(runs []runSpec, repo string) []runSpec {
	out := make([]runSpec, 0, len(runs))
	for _, r := range runs {
		if r.repo == repo {
			out = append(out, r)
		}
	}
	return out
}

// reposBody is the context's repository list, owned by owner.
func reposBody(t *testing.T, owner string) string {
	t.Helper()
	repos := []map[string]any{
		repoJSON(fixtureRepo, 0, owner),
		repoJSON(secondRepo, 1, owner),
	}
	return jsonBody(t, repos)
}

// repoJSON builds one repository.
func repoJSON(name string, index int, owner string) map[string]any {
	return map[string]any{
		"id":        index + 1,
		"name":      name,
		"full_name": owner + "/" + name,
		"owner":     map[string]any{"login": owner},
		"pushed_at": workflowNow.Add(-time.Duration(index) * time.Hour).Format(time.RFC3339),
	}
}

// runsBody builds a run list response. The runs belong to owner, which is not
// always the viewer: whether they own the repository decides how a run is
// approved.
func runsBody(t *testing.T, owner string, runs ...runSpec) string {
	t.Helper()
	items := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		items = append(items, runJSON(r, owner))
	}
	return jsonBody(t, map[string]any{"total_count": len(items), "workflow_runs": items})
}

// runJSON builds one run. The actor is always present: the client-side actor
// re-check drops a run that does not report one.
func runJSON(r runSpec, owner string) map[string]any {
	updated := r.created.Add(2 * time.Minute)
	if r.status == "waiting" || r.status == "in_progress" || r.status == "queued" {
		updated = r.created
	}
	head := owner + "/" + r.repo
	if r.fork {
		// A fork run: the head repository is not the one the run belongs to,
		// which is what makes it approvable through the API.
		head = "contributor/" + r.repo
	}
	attempt := r.attempt
	if attempt < 1 {
		attempt = 1
	}
	return map[string]any{
		"id":            r.id,
		"name":          fmt.Sprintf("CI %d", r.id),
		"display_title": fmt.Sprintf("Build %d", r.id),
		"run_number":    r.id % 100,
		"event":         "push",
		"status":        r.status,
		"conclusion":    r.conclusion,
		"head_branch":   "main",
		"head_sha":      "abcdef1234567890",
		"html_url":      fmt.Sprintf("https://github.com/%s/%s/actions/runs/%d", owner, r.repo, r.id),
		"created_at":    r.created.Format(time.RFC3339),
		"updated_at":    updated.Format(time.RFC3339),
		"run_attempt":   attempt,
		"repository": map[string]any{
			"name":      r.repo,
			"full_name": owner + "/" + r.repo,
			"owner":     map[string]any{"login": owner},
		},
		"head_repository": map[string]any{
			"name":      r.repo,
			"full_name": head,
			"owner":     map[string]any{"login": strings.Split(head, "/")[0]},
		},
		"actor": map[string]any{"login": "octocat"},
	}
}

// jobsBody is the job list of the fixture run: one finished job with steps and
// one that is still running.
func jobsBody(t *testing.T) string {
	t.Helper()
	return jsonBody(t, map[string]any{"total_count": 2, "jobs": []map[string]any{
		{
			"id":           7001,
			"run_id":       900,
			"name":         "build",
			"status":       "completed",
			"conclusion":   "failure",
			"started_at":   workflowNow.Format(time.RFC3339),
			"completed_at": workflowNow.Add(2 * time.Minute).Format(time.RFC3339),
			"html_url":     "https://github.com/octocat/api/actions/runs/900/job/7001",
			"steps": []map[string]any{
				stepJSON("Set up job", "completed", "success", 0, 1*time.Minute),
				stepJSON("Run tests", "completed", "failure", 1*time.Minute, 2*time.Minute),
				stepJSON("Upload coverage", "completed", "skipped", 2*time.Minute, 3*time.Minute),
			},
		},
		{
			"id":         7002,
			"run_id":     900,
			"name":       "lint",
			"status":     "in_progress",
			"conclusion": "",
			"started_at": workflowNow.Add(3 * time.Minute).Format(time.RFC3339),
			"html_url":   "https://github.com/octocat/api/actions/runs/900/job/7002",
			"steps": []map[string]any{
				stepJSON("Set up job", "completed", "success", 3*time.Minute, 4*time.Minute),
				stepJSON("Run linters", "in_progress", "", 4*time.Minute, 0),
			},
		},
	}})
}

// stepJSON builds one job step.
func stepJSON(name, status, conclusion string, start, end time.Duration) map[string]any {
	step := map[string]any{
		"name":       name,
		"status":     status,
		"conclusion": conclusion,
		"started_at": workflowNow.Add(start).Format(time.RFC3339),
	}
	if end > 0 {
		step["completed_at"] = workflowNow.Add(end).Format(time.RFC3339)
	}
	return step
}

// jobLogBody is the runner log of the fixture job: a group per step, each with
// the timestamp window the step reports.
const jobLogBody = `2024-05-01T10:00:00.0000000Z ##[group]Set up job
2024-05-01T10:00:10.0000000Z Preparing the runner
2024-05-01T10:00:20.0000000Z ##[endgroup]
2024-05-01T10:01:00.0000000Z ##[group]Run tests
2024-05-01T10:01:10.0000000Z go test ./...
2024-05-01T10:01:20.0000000Z FAIL ghspector/internal/ui
2024-05-01T10:01:30.0000000Z ##[endgroup]
2024-05-01T10:02:00.0000000Z ##[group]Upload coverage
2024-05-01T10:02:10.0000000Z Nothing to upload
2024-05-01T10:02:20.0000000Z ##[endgroup]
`

// longLog builds a job log of n lines inside one step, for the viewport tests.
// The lines all fall inside the second step's window, so the whole log belongs
// to the step that failed.
func longLog(n int) string {
	var b strings.Builder
	b.WriteString("2024-05-01T10:01:00.0000000Z ##[group]Run tests\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "2024-05-01T10:01:00.0000000Z output line %03d\n", i)
	}
	b.WriteString("2024-05-01T10:01:30.0000000Z ##[endgroup]\n")
	return b.String()
}

// jsonBody marshals a fixture body.
func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return string(b)
}

// drive runs a command tree to completion and returns the model. Commands run
// synchronously, so a test asserts on the state a fetch lands in without a
// runtime and without sleeping.
//
// A status message is fed back but its follow-up command is not run: the model
// answers one with the timer that expires it, which is the runtime's business.
func drive(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range messagesOf(t, cmd) {
		next, follow := send(t, m, msg)
		if _, isStatus := msg.(StatusMsg); isStatus {
			m = next
			continue
		}
		m = drive(t, next, follow)
	}
	return m
}

// messageBudget is how long a command is given to answer before it is treated
// as a timer and left to the runtime.
//
// A data command answers from a server in the same process in microseconds, so
// the budget is generous on purpose; only a command that waits for a clock
// spends it. The alternative — executing timers — would block the test until
// the interval elapsed.
const messageBudget = 200 * time.Millisecond

// messagesOf executes a command tree and collects the messages it produces.
func messagesOf(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case msg := <-done:
		if msg == nil {
			return nil
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, messagesOf(t, c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(messageBudget):
		return nil
	}
}

// pressKey sends one key and returns the model and the command it produced.
func pressKey(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	return send(t, m, press(t, key))
}

// pressAndDrive presses each key and runs the commands it produced, so a test
// reaches a screen the way a person does: one key and one command loop turn at
// a time.
func pressAndDrive(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, key := range keys {
		next, cmd := pressKey(t, m, key)
		m = drive(t, next, cmd)
	}
	return m
}

// waitingFixture is a context whose only run is paused on a protected
// environment, which is the state the approval screens are reached from. One
// run means the cursor rests on it, so no key moves are needed to reach it.
func waitingFixture() []runSpec {
	return []runSpec{{
		id:      900,
		repo:    fixtureRepo,
		status:  "waiting",
		attempt: 1,
		created: workflowNow,
	}}
}

// workflowsModel is the state every workflow test starts from: the model is
// ready, the Workflows tab is active, and its first page of runs has landed.
func workflowsModel(t *testing.T, srv *httptest.Server, w, h int, tweak ...func(*Options)) Model {
	t.Helper()
	m := readyModel(t, srv, w, h, tweak...)
	m, cmd := pressKey(t, m, "tab")
	if tab(t, m) != TabWorkflows {
		t.Fatalf("tab = %v, want Workflows", tab(t, m))
	}
	return drive(t, m, cmd)
}

// tab reads the active tab off the main route.
func tab(t *testing.T, m Model) Tab {
	t.Helper()
	main, ok := m.routes.Top().(Main)
	if !ok {
		t.Fatalf("top route is %T, want ui.Main", m.routes.Top())
	}
	return main.tab
}

// runsTabOf reads the main route's Workflows state.
func runsTabOf(t *testing.T, m Model) runsTab {
	t.Helper()
	main, ok := m.routes.Top().(Main)
	if !ok {
		t.Fatalf("top route is %T, want ui.Main", m.routes.Top())
	}
	return main.runs
}

// jobsRoute returns the top route as the Jobs route.
func jobsRoute(t *testing.T, m Model) *Jobs {
	t.Helper()
	jobs, ok := m.routes.Top().(*Jobs)
	if !ok {
		t.Fatalf("top route is %T, want *ui.Jobs", m.routes.Top())
	}
	return jobs
}

// logsRoute returns the top route as the Logs route.
func logsRoute(t *testing.T, m Model) *Logs {
	t.Helper()
	logs, ok := m.routes.Top().(*Logs)
	if !ok {
		t.Fatalf("top route is %T, want *ui.Logs", m.routes.Top())
	}
	return logs
}

// footerHasAction reports whether the footer offers an action.
//
// The footer drops hints that do not fit, so a screen-level search for a key's
// label is not a reliable answer to whether the key is available.
func footerHasAction(m Model, action keymap.Action) bool {
	for _, b := range m.footerBindings() {
		if b.Action == action {
			return true
		}
	}
	return false
}

// screen renders the model and asserts the frame is exactly the geometry.
func screen(t *testing.T, m Model, w, h int) string {
	t.Helper()
	out := m.View().Content
	assertGeometry(t, "screen", out, w, h)
	return out
}
