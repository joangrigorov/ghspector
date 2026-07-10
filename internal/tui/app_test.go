package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"

	"ghspector/internal/auth"
	"ghspector/internal/gh"
)

func TestTUI_Integration(t *testing.T) {
	// 1. Mock GitHub API using httptest.Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/user":
			json.NewEncoder(w).Encode(gh.User{Login: "test-user", ID: 1})
		case "/user/orgs":
			json.NewEncoder(w).Encode([]gh.Org{{Login: "test-org", ID: 2}})
		case "/users/test-user/repos":
			json.NewEncoder(w).Encode([]gh.Repository{
				{
					ID:    10,
					Name:  "repo-1",
					Owner: &gh.User{Login: "test-user"},
				},
			})
		case "/repos/test-user/repo-1/actions/runs":
			json.NewEncoder(w).Encode(gh.WorkflowRunsResponse{
				TotalCount: 1,
				WorkflowRuns: []gh.WorkflowRun{
					{
						ID:         1001,
						Name:       "CI Build",
						Status:     "in_progress",
						Conclusion: "",
						CreatedAt:  time.Now().Add(-10 * time.Minute),
					},
				},
			})
		case "/repos/test-user/repo-1/actions/runs/1001":
			json.NewEncoder(w).Encode(gh.WorkflowRun{
				ID:         1001,
				Name:       "CI Build",
				Status:     "completed",
				Conclusion: "success",
			})
		case "/repos/test-user/repo-1/actions/runs/1001/jobs":
			json.NewEncoder(w).Encode(gh.WorkflowJobsResponse{
				TotalCount: 1,
				Jobs: []gh.WorkflowJob{
					{
						ID:         2001,
						RunID:      1001,
						Name:       "build-job",
						Status:     "completed",
						Conclusion: "success",
					},
				},
			})
		case "/repos/test-user/repo-1/actions/jobs/2001/logs":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("Testing logs contents"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// 2. Setup client and model
	client := gh.NewClient("test-token", server.URL)
	cfg := &auth.Config{}

	m := InitModel(client, cfg)

	// Test Init() command
	initCmd := m.Init()
	if initCmd == nil {
		t.Fatal("expected Init() to return a command")
	}

	// 3. Test handling initDataMsg
	// Simulate user and orgs loading successfully
	var rawModel tea.Model
	var cmd tea.Cmd

	rawModel, cmd = m.Update(initDataMsg{
		user: &gh.User{Login: "test-user"},
		orgs: []gh.Org{{Login: "test-org"}},
	})

	model := rawModel.(Model)
	if model.state != viewSplash {
		t.Errorf("expected viewSplash while loading runs, got state: %d", model.state)
	}
	if len(model.targets) != 2 {
		t.Errorf("expected 2 targets (user, org), got %d", len(model.targets))
	}

	// Execute the fetchRunsCmd returned as cmd
	if cmd == nil {
		t.Fatal("expected fetchRunsCmd, got nil")
	}
	runsMsg := cmd()

	// Feed runsLoadedMsg into update
	rawModel, cmd = model.Update(runsMsg)
	model = rawModel.(Model)

	if model.state != viewMain {
		t.Errorf("expected viewMain state after runs load, got: %d", model.state)
	}
	if len(model.runs) != 1 || model.runs[0].ID != 1001 {
		t.Errorf("expected run 1001 loaded, got: %+v", model.runs)
	}

	// 4. Test status navigation: move cursor and press Enter to load jobs
	// Pressing Enter on the selected run
	rawModel, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	model = rawModel.(Model)
	if model.state != viewSplash {
		t.Errorf("expected viewSplash while loading jobs, got state: %d", model.state)
	}
	if cmd == nil {
		t.Fatal("expected fetchJobsCmd, got nil")
	}

	// Execute fetchJobsCmd
	jobsMsg := cmd()
	rawModel, cmd = model.Update(jobsMsg)
	model = rawModel.(Model)
	if model.state != viewJobs {
		t.Errorf("expected viewJobs state, got %d", model.state)
	}
	if len(model.jobs) != 1 || model.jobs[0].ID != 2001 {
		t.Errorf("expected job 2001 loaded, got: %+v", model.jobs)
	}

	// 5. Test navigation to logs
	rawModel, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	model = rawModel.(Model)
	if model.state != viewSplash {
		t.Errorf("expected viewSplash while loading logs, got state: %d", model.state)
	}
	if cmd == nil {
		t.Fatal("expected fetchLogsCmd, got nil")
	}

	// Execute fetchLogsCmd
	logsMsg := cmd()
	rawModel, cmd = model.Update(logsMsg)
	model = rawModel.(Model)
	if model.state != viewLogs {
		t.Errorf("expected viewLogs state, got %d", model.state)
	}
	if model.logs != "Testing logs contents" {
		t.Errorf("expected logs contents, got: %s", model.logs)
	}

	// Test Esc key to navigate back
	rawModel, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	model = rawModel.(Model)
	if model.state != viewJobs {
		t.Errorf("expected Esc to return to viewJobs, got %d", model.state)
	}

	rawModel, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	model = rawModel.(Model)
	if model.state != viewMain {
		t.Errorf("expected Esc to return to viewMain, got %d", model.state)
	}

	// 6. Test scrolling window functionality
	model.height = 10 // restrict height
	model.runs = make([]gh.WorkflowRun, 15) // mock 15 runs
	model.selectedRunIdx = 0
	model.runStartIndex = 0
	model.hasMoreRuns = false

	// Scroll down 9 times to row 9
	for i := 0; i < 9; i++ {
		model.selectedRunIdx++
		model.scrollRuns()
	}

	// With height 10, visibleRows is m.height - 8 = 2.
	// We started at index 0. If selectedRunIdx = 9, runStartIndex should have moved.
	if model.runStartIndex == 0 {
		t.Errorf("expected runStartIndex to scroll and be > 0, got %d (selectedRunIdx: %d)", model.runStartIndex, model.selectedRunIdx)
	}

	// 7. Test background polling merge and sorting order
	now := time.Now()
	runOld := gh.WorkflowRun{ID: 5001, Name: "Old Run", CreatedAt: now.Add(-1 * time.Hour)}
	runNew := gh.WorkflowRun{ID: 5002, Name: "New Run", CreatedAt: now}
	
	// Start with only old run
	model.runs = []gh.WorkflowRun{runOld}
	
	// Simulate poll returns a new run and updates status of old run
	runOldUpdated := runOld
	runOldUpdated.Status = "completed"
	runOldUpdated.Conclusion = "success"
	
	polledMsg := runsPolledMsg{
		runs: []gh.WorkflowRun{runOldUpdated, runNew},
	}
	
	rawModel, _ = model.Update(polledMsg)
	model = rawModel.(Model)
	
	if len(model.runs) != 2 {
		t.Errorf("expected 2 runs after poll merge, got %d", len(model.runs))
	}
	// Check sorting (New run should be first because it is newer)
	if model.runs[0].ID != 5002 {
		t.Errorf("expected newest run to be first in list, got ID: %d", model.runs[0].ID)
	}
	// Check updated status of old run
	if model.runs[1].Status != "completed" {
		t.Errorf("expected old run status to be updated, got: %s", model.runs[1].Status)
	}

	// 8. Test status-based priority sorting (queued must float to the top)
	runQueuedOld := gh.WorkflowRun{ID: 5003, Name: "Queued Old Run", Status: "queued", CreatedAt: now.Add(-2 * time.Hour)}
	
	polledMsgPriority := runsPolledMsg{
		runs: []gh.WorkflowRun{runOldUpdated, runNew, runQueuedOld},
	}
	
	rawModel, _ = model.Update(polledMsgPriority)
	model = rawModel.(Model)
	
	if len(model.runs) != 3 {
		t.Errorf("expected 3 runs, got %d", len(model.runs))
	}
	// The queued old run should be first (index 0) because queued priority is higher than completed
	if model.runs[0].ID != 5003 {
		t.Errorf("expected queued run to float to top (index 0), got ID: %d", model.runs[0].ID)
	}

	// 9. Test tail/follow logs behavior
	model.state = viewLogs
	model.followLogs = true
	model.logsViewport = viewport.New(80, 5)
	model.logsViewport.SetContent("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7")

	if !model.followLogs {
		t.Error("expected followLogs to be true initially")
	}

	// Move scroll up: YOffset should become less than oldY (which was at bottom/max height)
	// We can manually decrease YOffset to simulate scrolling up
	model.logsViewport.YOffset = 1
	rawModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("up")})
	model = rawModel.(Model)

	if model.followLogs {
		t.Error("expected followLogs to be false after scrolling up")
	}

	// 10. Test waiting status and Actor rendering in main view
	model.state = viewMain
	model.runs = []gh.WorkflowRun{
		{
			ID:     6001,
			Name:   "Waiting Run",
			Status: "waiting",
			Actor:  &gh.User{Login: "octocat"},
			Repository: gh.Repository{
				Name: "test-repo",
			},
		},
	}
	rendered := model.View()
	// Should show actor "octocat"
	if !strings.Contains(rendered, "octocat") {
		t.Error("expected main view to render actor 'octocat'")
	}
	// Should render the waiting status indicator "◆"
	if !strings.Contains(rendered, "◆") {
		t.Error("expected main view to render waiting status indicator '◆'")
	}

	// 11. Test legend rendering on Help page
	model.state = viewHelp
	model.width = 80
	renderedWithLegend := model.View()
	if !strings.Contains(renderedWithLegend, "Legend:") {
		t.Error("expected legend to be rendered for screen width 80 on Help view")
	}
}

func TestTUI_HelpScreen(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.state = viewMain

	// Pressing ? should enter Help View
	rawModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = rawModel.(Model)
	if m.state != viewHelp {
		t.Errorf("expected viewHelp, got state: %d", m.state)
	}
	if cmd != nil {
		t.Error("expected no command for Help toggle")
	}

	viewStr := m.View()
	if !strings.Contains(viewStr, "Keyboard Shortcuts & Help") {
		t.Error("expected Help view to display shortcuts header")
	}
	if !strings.Contains(viewStr, "Legend:") {
		t.Error("expected Help view to display status legend")
	}

	// Pressing esc should close Help View and restore viewMain
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = rawModel.(Model)
	if m.state != viewMain {
		t.Errorf("expected restore to viewMain, got state: %d", m.state)
	}
}

func TestTUI_ActorFilter(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.currentUser = "octocat"
	m.state = viewMain
	m.targets = []Target{{Name: "octocat", IsOrg: false}}

	// Test quick filter own toggle (m)
	rawModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = rawModel.(Model)
	if m.filterActor != "octocat" {
		t.Errorf("expected filterActor to be 'octocat', got '%s'", m.filterActor)
	}
	if cmd == nil {
		t.Fatal("expected fetchRunsCmd to be returned")
	}

	// Pressing m again should clear it
	m.state = viewMain
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = rawModel.(Model)
	if m.filterActor != "" {
		t.Errorf("expected filterActor to be cleared, got '%s'", m.filterActor)
	}

	// Test custom filter prompt input (f)
	m.state = viewMain
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = rawModel.(Model)
	if !m.showFilterInput {
		t.Error("expected showFilterInput to be true")
	}

	// Input keys while filter input is active
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = rawModel.(Model)
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = rawModel.(Model)

	// Press enter to apply
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = rawModel.(Model)
	if m.showFilterInput {
		t.Error("expected filter input to be closed")
	}
	if m.filterActor != "ab" {
		t.Errorf("expected filterActor 'ab', got '%s'", m.filterActor)
	}
	if cmd == nil {
		t.Error("expected fetchRunsCmd after filter apply")
	}
}

func TestTUI_AttemptNavigation(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.state = viewJobs
	m.runs = []gh.WorkflowRun{
		{ID: 101, Name: "CI", RunAttempt: 3, Repository: gh.Repository{Name: "repo", Owner: &gh.User{Login: "owner"}}},
	}
	m.selectedRunIdx = 0
	m.selectedAttempt = 3

	// Pressing [ should navigate to attempt 2
	rawModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = rawModel.(Model)
	if m.selectedAttempt != 2 {
		t.Errorf("expected selectedAttempt to be 2, got %d", m.selectedAttempt)
	}
	if cmd == nil {
		t.Fatal("expected fetchJobsCmd to be triggered")
	}

	// Pressing [ again should navigate to attempt 1
	m.state = viewJobs
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = rawModel.(Model)
	if m.selectedAttempt != 1 {
		t.Errorf("expected selectedAttempt to be 1, got %d", m.selectedAttempt)
	}

	// Pressing [ at attempt 1 should do nothing
	m.state = viewJobs
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = rawModel.(Model)
	if m.selectedAttempt != 1 {
		t.Errorf("expected attempt to stay 1, got %d", m.selectedAttempt)
	}

	// Pressing ] should navigate to attempt 2
	m.state = viewJobs
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = rawModel.(Model)
	if m.selectedAttempt != 2 {
		t.Errorf("expected attempt to increase to 2, got %d", m.selectedAttempt)
	}
}

func Test_MatchActor(t *testing.T) {
	tests := []struct {
		actor  string
		filter string
		want   bool
	}{
		{"yoan", "yoan", true},
		{"Yoan", "yoan", true},
		{"yoan", "Yoan", true},
		{"dependabot[bot]", "dependabot", true},
		{"dependabot[bot]", "dependabot[bot]", true},
		{"dependabot[bot]", "dep", true},
		{"some-other-bot[bot]", "some-other-bot", true},
		{"yoan", "dependabot", false},
	}

	for _, tt := range tests {
		got := matchActor(tt.actor, tt.filter)
		if got != tt.want {
			t.Errorf("matchActor(%q, %q) = %v; want %v", tt.actor, tt.filter, got, tt.want)
		}
	}
}
