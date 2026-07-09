package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
}
