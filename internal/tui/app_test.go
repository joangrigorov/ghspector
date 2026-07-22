package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	encodeJSON := func(w http.ResponseWriter, val interface{}) {
		_ = json.NewEncoder(w).Encode(val)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/user":
			encodeJSON(w, gh.User{Login: "test-user", ID: 1})
		case "/user/orgs":
			encodeJSON(w, []gh.Org{{Login: "test-org", ID: 2}})
		case "/users/test-user/repos":
			encodeJSON(w, []gh.Repository{
				{
					ID:    10,
					Name:  "repo-1",
					Owner: &gh.User{Login: "test-user"},
				},
			})
		case "/repos/test-user/repo-1/actions/runs":
			encodeJSON(w, gh.WorkflowRunsResponse{
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
			encodeJSON(w, gh.WorkflowRun{
				ID:         1001,
				Name:       "CI Build",
				Status:     "completed",
				Conclusion: "success",
			})
		case "/repos/test-user/repo-1/actions/runs/1001/jobs":
			encodeJSON(w, gh.WorkflowJobsResponse{
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
			_, _ = w.Write([]byte("Testing logs contents"))
		case "/repos/test-user/repo-1/pulls":
			encodeJSON(w, []gh.PullRequest{
				{
					ID:         101,
					Number:     101,
					Title:      "Add new feature",
					State:      "open",
					HTMLURL:    "https://github.com/test-user/repo-1/pull/101",
					CreatedAt:  time.Now().Add(-2 * time.Hour),
					UpdatedAt:  time.Now(),
					User:       &gh.User{Login: "test-user"},
					Repository: gh.Repository{Name: "repo-1", Owner: &gh.User{Login: "test-user"}},
					Head:       gh.PullRequestRef{SHA: "test-sha-123", Ref: "feature-branch"},
				},
			})
		case "/repos/test-user/repo-1/pulls/101":
			encodeJSON(w, gh.PullRequest{
				ID:         101,
				Number:     101,
				Title:      "Add new feature",
				Body:       "This is a description",
				State:      "open",
				HTMLURL:    "https://github.com/test-user/repo-1/pull/101",
				User:       &gh.User{Login: "test-user"},
				Repository: gh.Repository{Name: "repo-1", Owner: &gh.User{Login: "test-user"}},
				Head:       gh.PullRequestRef{SHA: "test-sha-123", Ref: "feature-branch"},
			})
		case "/repos/test-user/repo-1/pulls/101/commits":
			var commits []gh.RepositoryCommit
			c := gh.RepositoryCommit{
				SHA:     "test-sha-123",
				HTMLURL: "https://github.com/test-user/repo-1/commit/test-sha-123",
			}
			c.Commit.Message = "My commit message"
			c.Commit.Author.Date = time.Now()
			commits = append(commits, c)
			encodeJSON(w, commits)
		case "/repos/test-user/repo-1/pulls/101/files":
			encodeJSON(w, []gh.CommitFile{
				{
					Filename:  "main.go",
					Status:    "modified",
					Additions: 5,
					Deletions: 2,
					Patch:     "+added line\n-removed line",
				},
			})
		case "/repos/test-user/repo-1/commits/test-sha-123/check-runs":
			encodeJSON(w, gh.CheckRunsResponse{
				TotalCount: 1,
				CheckRuns: []gh.CheckRun{
					{
						ID:         901,
						Name:       "lint check",
						Status:     "completed",
						Conclusion: "success",
					},
				},
			})
		case "/repos/test-user/repo-1/issues/101/comments":
			encodeJSON(w, []gh.IssueComment{
				{
					ID:        401,
					Body:      "Looks good to me!",
					User:      &gh.User{Login: "reviewer-user"},
					CreatedAt: time.Now(),
				},
			})
		case "/repos/test-user/repo-1/pulls/101/merge":
			w.WriteHeader(http.StatusOK)
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
		t.Errorf("expected viewSplash while loading, got state: %d", model.state)
	}
	if len(model.targets) != 2 {
		t.Errorf("expected 2 targets (user, org), got %d", len(model.targets))
	}

	// Execute dashboard stats fetch returned as cmd
	if cmd == nil {
		t.Fatal("expected fetchDashboardStatsCmd, got nil")
	}
	dbStatsMsg := cmd()
	rawModel, _ = model.Update(dbStatsMsg)
	model = rawModel.(Model)

	if model.state != viewMain {
		t.Errorf("expected viewMain state after dashboard load, got: %d", model.state)
	}

	// Press Tab to switch to workflows tab
	rawModel, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tab")})
	model = rawModel.(Model)
	if model.activeTab != tabWorkflows {
		t.Errorf("expected activeTab tabWorkflows, got %d", model.activeTab)
	}

	if cmd == nil {
		t.Fatal("expected fetchRunsCmd, got nil")
	}
	runsMsg := cmd()

	// Feed runsLoadedMsg into update
	rawModel, _ = model.Update(runsMsg)
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
	if model.state != viewJobs || !model.isLoading {
		t.Errorf("expected viewJobs and isLoading=true while loading jobs, got state: %d, isLoading: %t", model.state, model.isLoading)
	}
	if cmd == nil {
		t.Fatal("expected fetchJobsCmd, got nil")
	}

	// Execute fetchJobsCmd
	jobsMsg := cmd()
	rawModel, _ = model.Update(jobsMsg)
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
	if model.state != viewLogs || !model.logsLoading {
		t.Errorf("expected viewLogs and logsLoading=true while loading logs, got state: %d, logsLoading: %t", model.state, model.logsLoading)
	}
	if cmd == nil {
		t.Fatal("expected fetchLogsCmd, got nil")
	}

	// Execute fetchLogsCmd
	logsMsg := cmd()
	rawModel, _ = model.Update(logsMsg)
	model = rawModel.(Model)
	if model.state != viewLogs {
		t.Errorf("expected viewLogs state, got %d", model.state)
	}
	if model.logs != "Testing logs contents" {
		t.Errorf("expected logs contents, got: %s", model.logs)
	}

	// Test Esc key to navigate back
	rawModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	model = rawModel.(Model)
	if model.state != viewJobs {
		t.Errorf("expected Esc to return to viewJobs, got %d", model.state)
	}

	rawModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
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
	rawModel, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
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
	if !strings.Contains(renderedWithLegend, "LEGENDS") {
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
	if !strings.Contains(viewStr, "LEGENDS") {
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
	m.activeTab = tabWorkflows

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
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = rawModel.(Model)
	if m.filterActor != "" {
		t.Errorf("expected filterActor to be cleared, got '%s'", m.filterActor)
	}

	// Test custom filter prompt input (f)
	m.state = viewMain
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = rawModel.(Model)
	if m.state != viewFilterTypeSelect {
		t.Error("expected state viewFilterTypeSelect after pressing f")
	}
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = rawModel.(Model)
	if !m.showFilterInput {
		t.Error("expected showFilterInput to be true")
	}

	// Input keys while filter input is active
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = rawModel.(Model)
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
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
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = rawModel.(Model)
	if m.selectedAttempt != 1 {
		t.Errorf("expected selectedAttempt to be 1, got %d", m.selectedAttempt)
	}

	// Pressing [ at attempt 1 should do nothing
	m.state = viewJobs
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = rawModel.(Model)
	if m.selectedAttempt != 1 {
		t.Errorf("expected attempt to stay 1, got %d", m.selectedAttempt)
	}

	// Pressing ] should navigate to attempt 2
	m.state = viewJobs
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
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
		{"testuser", "testuser", true},
		{"Testuser", "testuser", true},
		{"testuser", "Testuser", true},
		{"dependabot[bot]", "dependabot", true},
		{"dependabot[bot]", "dependabot[bot]", true},
		{"dependabot[bot]", "dep", true},
		{"some-other-bot[bot]", "some-other-bot", true},
		{"testuser", "dependabot", false},
	}

	for _, tt := range tests {
		got := matchActor(tt.actor, tt.filter)
		if got != tt.want {
			t.Errorf("matchActor(%q, %q) = %v; want %v", tt.actor, tt.filter, got, tt.want)
		}
	}
}

func TestTUI_PRViewer(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.state = viewMain
	m.activeTab = tabPRs

	// Test pullsLoadedMsg updates pulls list
	pr := gh.PullRequest{
		ID:         101,
		Number:     101,
		Title:      "My PR Title",
		State:      "open",
		User:       &gh.User{Login: "coder"},
		Repository: gh.Repository{Name: "repo-name", Owner: &gh.User{Login: "repo-owner"}},
	}
	
	rawModel, _ := m.Update(pullsLoadedMsg{
		pulls: []gh.PullRequest{pr},
	})
	m = rawModel.(Model)

	if len(m.pulls) != 1 {
		t.Fatalf("expected 1 pull request, got %d", len(m.pulls))
	}
	if m.pulls[0].Number != 101 {
		t.Errorf("expected PR number 101, got %d", m.pulls[0].Number)
	}

	// Test prDetailsLoadedMsg transitions to viewPRDetails
	prDetailsMsg := prDetailsLoadedMsg{
		pull: &pr,
		commits: []gh.RepositoryCommit{
			{
				SHA: "sha1",
			},
		},
		files: []gh.CommitFile{
			{
				Filename: "README.md",
				Status:   "modified",
				Patch:    "+hello",
			},
		},
		checkRuns: []gh.CheckRun{
			{
				ID:   99,
				Name: "check-1",
			},
		},
		comments: []gh.IssueComment{
			{
				ID:   1,
				Body: "nice",
			},
		},
	}

	rawModel, _ = m.Update(prDetailsMsg)
	m = rawModel.(Model)

	if m.state != viewPRDetails {
		t.Errorf("expected viewPRDetails state, got %d", m.state)
	}
	if m.activePRTab != prTabInfo {
		t.Errorf("expected activePRTab to be prTabInfo, got %d", m.activePRTab)
	}

	// Test scrolling description viewport
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = rawModel.(Model)
	// We just verify it doesn't panic and keeps state
	if m.state != viewPRDetails {
		t.Errorf("expected state to remain viewPRDetails, got %d", m.state)
	}
}

func TestPRStateFiltering(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.state = viewMain
	m.activeTab = tabPRs
	
	if m.filterPRState != "open" {
		t.Errorf("expected default state open, got: %s", m.filterPRState)
	}

	// Press 's' to cycle to "closed"
	raw, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = raw.(Model)
	if m.filterPRState != "closed" {
		t.Errorf("expected state to cycle to closed, got: %s", m.filterPRState)
	}
	if cmd == nil {
		t.Error("expected fetchPullsCmd after state toggle")
	}

	// Press 's' to cycle to "all"
	raw, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = raw.(Model)
	if m.filterPRState != "all" {
		t.Errorf("expected state to cycle to all, got: %s", m.filterPRState)
	}

	// Press 's' to cycle back to "open"
	raw, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = raw.(Model)
	if m.filterPRState != "open" {
		t.Errorf("expected state to cycle back to open, got: %s", m.filterPRState)
	}
}

func TestPRCommentsView(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.state = viewPRDetails
	m.selectedPull = &gh.PullRequest{
		Number: 42,
		Repository: gh.Repository{
			Name: "test-repo",
			Owner: &gh.User{Login: "test-org"},
		},
	}
	m.commentsViewport.Width = 80
	m.commentsViewport.Height = 20

	// 1. Press 'c' to enter comments view
	raw, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = raw.(Model)
	if m.state != viewPRComments {
		t.Errorf("expected viewPRComments state, got: %d", m.state)
	}
	if cmd == nil {
		t.Error("expected fetchPRCommentsCmd to be returned")
	}

	// 2. Load mock comments
	now := time.Now()
	comments := []gh.IssueComment{
		{
			User:      &gh.User{Login: "commenter1"},
			Body:      "This is comment 1",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			User:      &gh.User{Login: "commenter2"},
			Body:      "This is comment 2",
			CreatedAt: now,
		},
	}
	raw, _ = m.Update(prCommentsLoadedMsg{comments: comments})
	m = raw.(Model)
	if m.isLoading {
		t.Error("expected isLoading to be false after comments loaded")
	}
	if len(m.prComments) != 2 {
		t.Errorf("expected 2 comments, got: %d", len(m.prComments))
	}
	// Verify sorting (latest at bottom)
	if m.prComments[0].User.Login != "commenter1" || m.prComments[1].User.Login != "commenter2" {
		t.Error("expected comments to be sorted chronologically")
	}

	// 3. View check
	viewStr := m.View()
	plainView := stripANSI(viewStr)
	if !strings.Contains(plainView, "Pull Request #42 Comments") {
		t.Error("expected view to render pull request comments header")
	}
	if !strings.Contains(plainView, "@commenter1") || !strings.Contains(plainView, "This is comment 2") {
		t.Error("expected view to contain comments content")
	}

	// 4. Press 'esc' to go back to details
	raw, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = raw.(Model)
	if m.state != viewPRDetails {
		t.Errorf("expected back to viewPRDetails, got: %d", m.state)
	}
}

func TestPrintFilterTypeSelectModal(t *testing.T) {
	m := Model{
		prFilterUser: "testuser",
	}
	m.theme = GetTheme()
	modalStr := m.renderPRFilterTypeSelectModal()
	lines := strings.Split(modalStr, "\n")
	t.Logf("Modal has %d lines:", len(lines))
	for idx, line := range lines {
		t.Logf("[%d]: %s (len=%d)", idx, line, len(line))
	}
}

func TestOverlayFilterTypeSelectModal(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.prFilterUser = "testuser"
	m.width = 80
	m.height = 24
	bg := m.renderPullsView()
	modalStr := m.renderPRFilterTypeSelectModal()
	res := overlayModal(bg, modalStr, m.width, m.height, 48)
	lines := strings.Split(res, "\n")
	t.Logf("Result has %d lines:", len(lines))
	for idx, line := range lines {
		t.Logf("[%d]: %s", idx, line)
	}
}

func TestPRCommitsListScreen(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.width = 120
	m.height = 24
	m.state = viewPRDetails
	m.selectedPull = &gh.PullRequest{
		Number: 42,
		Repository: gh.Repository{
			Name: "test-repo",
			Owner: &gh.User{Login: "test-org"},
		},
	}
	m.prCommits = []gh.RepositoryCommit{
		{
			SHA: "sha1111111111111111111111111111111111111",
			Commit: struct {
				Message string    `json:"message"`
				Author  struct {
					Name  string    `json:"name"`
					Email string    `json:"email"`
					Date  time.Time `json:"date"`
				} `json:"author"`
			}{
				Message: "First commit msg",
			},
		},
		{
			SHA: "sha2222222222222222222222222222222222222",
			Commit: struct {
				Message string    `json:"message"`
				Author  struct {
					Name  string    `json:"name"`
					Email string    `json:"email"`
					Date  time.Time `json:"date"`
				} `json:"author"`
			}{
				Message: "Second commit msg",
			},
		},
	}
	m.prCommitChecks = map[string][]gh.CheckRun{
		"sha1111111111111111111111111111111111111": {
			{Status: "completed", Conclusion: "success", Name: "Check 1"},
		},
	}
	m.prDescViewport.Height = 15

	// 1. Press 'l' to go to commits view state
	raw, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = raw.(Model)
	if m.state != viewPRCommits {
		t.Errorf("expected state viewPRCommits, got: %d", m.state)
	}

	// 2. View check
	viewStr := m.View()
	plainView := stripANSI(viewStr)
	if !strings.Contains(plainView, "First commit msg") || !strings.Contains(plainView, "Second commit msg") {
		t.Error("expected view to render commit list")
	}
	if !strings.Contains(plainView, "1/1") { // Check status rollup
		t.Error("expected view to contain commit checks status rollup")
	}

	// 3. Navigate down 'j'
	raw, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = raw.(Model)
	if m.selectedCommitIdx != 1 {
		t.Errorf("expected selectedCommitIdx to be 1, got: %d", m.selectedCommitIdx)
	}

	// 4. Press 'Enter' to view commit details
	raw, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	m = raw.(Model)
	if cmd == nil {
		t.Error("expected fetchCommitDetailsCmd to be returned on Enter")
	}

	// 5. Press 'esc' to go back to commits view
	m.state = viewCommitDetails
	raw, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = raw.(Model)
	if m.state != viewPRCommits {
		t.Errorf("expected state to return to viewPRCommits, got: %d", m.state)
	}

	// 6. Press 'esc' inside viewPRCommits to go back to details
	raw, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = raw.(Model)
	if m.state != viewPRDetails {
		t.Errorf("expected state to return to viewPRDetails, got: %d", m.state)
	}
}

func TestPRDetailsCheckEnterHTMLURLFallback(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.state = viewPRDetails
	m.prDescFocused = false // focus checks sidebar
	m.selectedPull = &gh.PullRequest{
		Number: 42,
		Repository: gh.Repository{
			Name: "test-repo",
			Owner: &gh.User{Login: "test-org"},
		},
	}
	m.prChecks = []gh.CheckRun{
		{
			ID:      999,
			Name:    "unmatched-run-name",
			HTMLURL: "https://github.com/test-org/test-repo/actions/runs/888777666/job/123",
			App:     &gh.CheckApp{Slug: "github-actions"},
		},
	}
	m.selectedCheckIdx = 0

	// Press 'Enter'
	raw, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	m = raw.(Model)
	if cmd == nil {
		t.Fatal("expected fetchJobsCmd to be returned via HTMLURL fallback")
	}
}

func TestPRDetailsFormatCheckName(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.viewingRun = &gh.WorkflowRun{
		ID:   888777666,
		Name: "Semantic Pull Request",
	}

	check1 := gh.CheckRun{
		Name:    "semantic-pr",
		HTMLURL: "https://github.com/test-org/test-repo/actions/runs/888777666/job/123",
		App:     &gh.CheckApp{Slug: "github-actions"},
	}
	name1 := m.formatCheckName(check1)
	if name1 != "Semantic Pull Request / semantic-pr" {
		t.Errorf("expected format 'Semantic Pull Request / semantic-pr', got: %q", name1)
	}

	check2 := gh.CheckRun{
		Name:    "Semantic Pull Request / semantic-pr",
		HTMLURL: "https://github.com/test-org/test-repo/actions/runs/888777666/job/123",
		App:     &gh.CheckApp{Slug: "github-actions"},
	}
	name2 := m.formatCheckName(check2)
	if name2 != "Semantic Pull Request / semantic-pr" {
		t.Errorf("expected keep original when already contains slash, got: %q", name2)
	}
}

func TestExtractRunIDFromURL(t *testing.T) {
	url1 := "https://github.com/getpup/backend/actions/runs/888777666/job/123"
	id1 := extractRunIDFromURL(url1)
	if id1 != 888777666 {
		t.Errorf("expected 888777666, got: %d", id1)
	}

	url2 := "https://github.com/getpup/backend/actions/runs/12345"
	id2 := extractRunIDFromURL(url2)
	if id2 != 12345 {
		t.Errorf("expected 12345, got: %d", id2)
	}

	url3 := "https://github.com/getpup/backend/actions/runs/"
	id3 := extractRunIDFromURL(url3)
	if id3 != 0 {
		t.Errorf("expected 0, got: %d", id3)
	}
}

func TestRenderJobsViewEmptySHA(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.state = viewJobs
	m.viewingRun = &gh.WorkflowRun{
		ID:      123,
		Name:    "Workflow without SHA",
		HeadSHA: "", // empty SHA should not panic renderJobsView
	}
	m.jobs = []gh.WorkflowJob{
		{ID: 1, Name: "job-1", Status: "success"},
	}

	// Try rendering view
	viewStr := m.View()
	if !strings.Contains(viewStr, "SHA: unknown") {
		t.Errorf("expected view to contain 'SHA: unknown', got: %q", viewStr)
	}
}

func TestModelPollTick(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	
	t.Run("Default intervals", func(t *testing.T) {
		m := InitModel(client, &auth.Config{})
		cmd := m.pollTick()
		if cmd == nil {
			t.Error("expected cmd to be non-nil")
		}
	})

	t.Run("Configured intervals", func(t *testing.T) {
		cfg := &auth.Config{
			Polling: auth.PollingConfig{
				WorkflowsIntervalSeconds: 15,
				PRsIntervalSeconds:       25,
			},
		}
		m := InitModel(client, cfg)
		cmd := m.pollTick()
		if cmd == nil {
			t.Error("expected cmd to be non-nil")
		}
	})
}

func TestTUI_PRDiffViewAndMergePermissions(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.width = 100
	m.height = 30
	m.state = viewMain
	m.activeTab = tabPRs

	// 1. Setup an open PR details
	prOpen := gh.PullRequest{
		ID:         101,
		Number:     101,
		Title:      "Open PR Title",
		State:      "open",
		User:       &gh.User{Login: "coder"},
		Repository: gh.Repository{Name: "repo-name", Owner: &gh.User{Login: "repo-owner"}},
	}
	
	prDetailsMsg := prDetailsLoadedMsg{
		pull: &prOpen,
		commits: []gh.RepositoryCommit{
			{SHA: "sha1"},
		},
		files: []gh.CommitFile{
			{
				Filename: "README.md",
				Status:   "modified",
				Patch:    "+hello",
			},
			{
				Filename: "main.go",
				Status:   "added",
				Patch:    "+func main()",
			},
		},
	}

	rawModel, _ := m.Update(prDetailsMsg)
	m = rawModel.(Model)

	if m.state != viewPRDetails {
		t.Fatalf("expected viewPRDetails state, got %d", m.state)
	}

	// Verify that viewerCanMerge is true (since PR is open)
	if !m.viewerCanMerge() {
		t.Error("expected viewerCanMerge to be true for open PR")
	}

	// Pressing D transitions to viewPRDiff
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m = rawModel.(Model)

	if m.state != viewPRDiff {
		t.Errorf("expected viewPRDiff state, got %d", m.state)
	}
	if m.selectedFileIdx != 0 {
		t.Errorf("expected selectedFileIdx to be 0, got %d", m.selectedFileIdx)
	}

	// Pressing j navigates to the next file
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = rawModel.(Model)
	if m.selectedFileIdx != 1 {
		t.Errorf("expected selectedFileIdx to be 1, got %d", m.selectedFileIdx)
	}

	// Pressing esc goes back to viewPRDetails
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = rawModel.(Model)
	if m.state != viewPRDetails {
		t.Errorf("expected state to return to viewPRDetails, got %d", m.state)
	}

	// 2. Setup a closed PR details
	prClosed := gh.PullRequest{
		ID:         102,
		Number:     102,
		Title:      "Closed PR Title",
		State:      "closed",
		User:       &gh.User{Login: "coder"},
		Repository: gh.Repository{Name: "repo-name", Owner: &gh.User{Login: "repo-owner"}},
	}
	
	prDetailsMsgClosed := prDetailsLoadedMsg{
		pull: &prClosed,
		commits: []gh.RepositoryCommit{
			{SHA: "sha1"},
		},
		files: []gh.CommitFile{
			{
				Filename: "README.md",
				Status:   "modified",
				Patch:    "+hello",
			},
		},
	}

	rawModel, _ = m.Update(prDetailsMsgClosed)
	m = rawModel.(Model)

	// Verify that viewerCanMerge is false (since PR is closed/merged)
	if m.viewerCanMerge() {
		t.Error("expected viewerCanMerge to be false for closed PR")
	}

	// Pressing m should not open merge method selection (mergeState should remain 0)
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = rawModel.(Model)
	if m.mergeState != 0 {
		t.Errorf("expected mergeState to remain 0, got %d", m.mergeState)
	}

	// Pressing C should not change mergeState (which represents close PR modal state)
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	m = rawModel.(Model)
	if m.mergeState != 0 {
		t.Errorf("expected mergeState to remain 0, got %d", m.mergeState)
	}

	// 3. Test scrolling behavior with multiple files
	m.state = viewPRDiff
	m.height = 20 // visibleRowsFiles = 20 - 16 = 4 files, clamped to 5 min
	m.prFiles = make([]gh.CommitFile, 15)
	for i := 0; i < 15; i++ {
		m.prFiles[i] = gh.CommitFile{Filename: fmt.Sprintf("file%d.go", i)}
	}
	m.selectedFileIdx = 0
	m.prFileStartIndex = 0

	// Scroll down 8 times (selectedFileIdx goes to 8, which is >= 0 + 5)
	for i := 0; i < 8; i++ {
		rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = rawModel.(Model)
	}

	if m.selectedFileIdx != 8 {
		t.Errorf("expected selectedFileIdx to be 8, got %d", m.selectedFileIdx)
	}
	if m.prFileStartIndex != 4 {
		t.Errorf("expected prFileStartIndex to be 4 (8 - 5 + 1), got %d", m.prFileStartIndex)
	}

	// Scroll back up 3 times (index goes to 5, which is > prFileStartIndex (4), so start index shouldn't change)
	for i := 0; i < 3; i++ {
		rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		m = rawModel.(Model)
	}
	if m.selectedFileIdx != 5 {
		t.Errorf("expected selectedFileIdx to be 5, got %d", m.selectedFileIdx)
	}
	if m.prFileStartIndex != 4 {
		t.Errorf("expected prFileStartIndex to remain 4, got %d", m.prFileStartIndex)
	}

	// Scroll back up to index 1 (which is < prFileStartIndex (4), so start index should become 1)
	for i := 0; i < 4; i++ {
		rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		m = rawModel.(Model)
	}
	if m.selectedFileIdx != 1 {
		t.Errorf("expected selectedFileIdx to be 1, got %d", m.selectedFileIdx)
	}
	if m.prFileStartIndex != 1 {
		t.Errorf("expected prFileStartIndex to become 1, got %d", m.prFileStartIndex)
	}
}

func TestTUI_RunningJobLogsAndPRDetailsRefresh(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.state = viewPRDetails
	m.selectedPull = &gh.PullRequest{
		Number:     101,
		Repository: gh.Repository{Name: "repo-name", Owner: &gh.User{Login: "repo-owner"}},
		Head:       gh.PullRequestRef{SHA: "sha1", Ref: "branch1"},
	}

	// 1. Verify PR details refresh
	rawModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = rawModel.(Model)
	if cmd == nil {
		t.Error("expected refresh command to be non-nil")
	}

	// 2. Verify running job logs check
	m.state = viewJobs
	m.jobs = []gh.WorkflowJob{
		{
			ID:     201,
			Name:   "running-job",
			Status: "in_progress",
		},
	}
	m.selectedJobIdx = 0
	
	// Pressing Enter on running job should show warning and not fetch logs
	rawModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = rawModel.(Model)
	if cmd == nil {
		t.Error("expected auto-dismiss timer command when trying to fetch logs of a running job")
	}
	if !strings.Contains(m.statusMsg, "not yet available") {
		t.Errorf("expected statusMsg to contain warning about running job logs, got %q", m.statusMsg)
	}

	// 3. Verify logsLoadedMsg error handling for 404/BlobNotFound
	m.state = viewJobs
	m.statusMsg = ""
	rawModel, _ = m.Update(logsLoadedMsg{
		err: fmt.Errorf("github api logs error (status 404): BlobNotFound: The specified blob does not exist"),
	})
	m = rawModel.(Model)
	if !strings.Contains(m.statusMsg, "not yet available") {
		t.Errorf("expected statusMsg to contain friendly warning on 404 BlobNotFound, got %q", m.statusMsg)
	}
}

func TestTUI_IssueViewer(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.state = viewMain
	m.activeTab = tabIssues

	// Test issuesLoadedMsg updates issues list
	issue := gh.Issue{
		ID:         202,
		Number:     202,
		Title:      "My Issue Title",
		State:      "open",
		User:       &gh.User{Login: "coder"},
		Repository: gh.Repository{Name: "repo-name", Owner: &gh.User{Login: "repo-owner"}},
	}
	
	rawModel, _ := m.Update(issuesLoadedMsg{
		issues: []gh.Issue{issue},
	})
	m = rawModel.(Model)

	if len(m.issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(m.issues))
	}
	if m.issues[0].Number != 202 {
		t.Errorf("expected issue number 202, got %d", m.issues[0].Number)
	}

	// Test issueDetailsLoadedMsg transitions to viewIssueDetails
	issueDetailsMsg := issueDetailsLoadedMsg{
		issue: &issue,
		comments: []gh.IssueComment{
			{
				ID:   1,
				Body: "nice comment",
			},
		},
		renderedBody: "markdown issue body",
	}

	rawModel, _ = m.Update(issueDetailsMsg)
	m = rawModel.(Model)

	if m.state != viewIssueDetails {
		t.Errorf("expected viewIssueDetails state, got %d", m.state)
	}

	// Test scrolling description viewport
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = rawModel.(Model)
	if m.state != viewIssueDetails {
		t.Errorf("expected state to remain viewIssueDetails, got %d", m.state)
	}
}

func TestIssueStateFiltering(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.state = viewMain
	m.activeTab = tabIssues

	// Initial filter state should be open
	if m.filterIssueState != "open" {
		t.Errorf("expected default filterIssueState to be open, got %q", m.filterIssueState)
	}

	// Toggle state once -> closed
	rawModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = rawModel.(Model)
	if m.filterIssueState != "closed" {
		t.Errorf("expected filterIssueState to be closed after one toggle, got %q", m.filterIssueState)
	}

	// Toggle state twice -> all
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = rawModel.(Model)
	if m.filterIssueState != "all" {
		t.Errorf("expected filterIssueState to be all after two toggles, got %q", m.filterIssueState)
	}

	// Toggle state thrice -> open
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = rawModel.(Model)
	if m.filterIssueState != "open" {
		t.Errorf("expected filterIssueState to be open after three toggles, got %q", m.filterIssueState)
	}
}

func TestWorkflowApprovalFlow(t *testing.T) {
	client := gh.NewClient("test-token", "")
	m := InitModel(client, nil)
	m.state = viewMain
	m.activeTab = tabWorkflows
	m.currentUser = "test-owner"

	// Mock runs list with one run needing approval
	m.runs = []gh.WorkflowRun{
		{
			ID:         123,
			Name:       "Awaiting approval",
			Status:     "waiting",
			Conclusion: "",
		},
	}
	m.runs[0].Repository.Owner = &gh.User{Login: "test-owner"}
	m.runs[0].Repository.Name = "test-repo"

	m.selectedRunIdx = 0

	// Before approval permissions loaded, canApprove should be false
	if m.selectedRunCanApprove() {
		t.Error("expected selectedRunCanApprove to be false before loading permission")
	}

	// Simulating checkApprovalPermissionCmd return message: user can approve
	rawModel, _ := m.Update(approvalPermissionLoadedMsg{runID: 123, canApprove: true})
	m = rawModel.(Model)

	if !m.selectedRunCanApprove() {
		t.Error("expected selectedRunCanApprove to be true after loading permission")
	}

	// Pressing a should open the approval confirmation modal (state = 1)
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = rawModel.(Model)

	if m.runApprovalState != 1 {
		t.Errorf("expected runApprovalState to be 1, got %d", m.runApprovalState)
	}

	// Pressing 'n' or cancel key should reset the approval modal state to 0
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = rawModel.(Model)

	if m.runApprovalState != 0 {
		t.Errorf("expected runApprovalState to revert to 0 on cancel, got %d", m.runApprovalState)
	}

	// Re-open approval modal
	m.runApprovalState = 1

	// Pressing 'y' should confirm approval, start loading, trigger cmd, and reset runApprovalState
	rawModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = rawModel.(Model)

	if m.runApprovalState != 0 {
		t.Errorf("expected runApprovalState to reset to 0 after confirming, got %d", m.runApprovalState)
	}
	if !m.isLoading {
		t.Error("expected isLoading to be true during approval execution")
	}
	if cmd == nil {
		t.Error("expected a command to be returned to dispatch API approval")
	}

	// Triggering workflowRunApprovedMsg should reset loading and set status
	rawModel, _ = m.Update(workflowRunApprovedMsg{runID: 123, err: nil})
	m = rawModel.(Model)

	if m.isLoading {
		if m.loadingMsg != "Refreshing workflow runs" {
			t.Errorf("expected loadingMsg to be 'Refreshing workflow runs', got %q", m.loadingMsg)
		}
	}
	if m.statusMsg != "Workflow run successfully approved!" {
		t.Errorf("expected success statusMsg, got %q", m.statusMsg)
	}
}

func TestTUI_DefaultMergeMethod(t *testing.T) {
	client := gh.NewClient("test-token", "")
	cfg := &auth.Config{
		DefaultMergeMethod: "squash",
	}
	m := InitModel(client, cfg)
	m.state = viewPRDetails
	m.mergeState = 1 // choose merge method modal active

	// 1. Pressing 'd' cycles DefaultMergeMethod: squash -> merge
	rawModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = rawModel.(Model)
	if m.config.DefaultMergeMethod != "merge" {
		t.Errorf("expected default merge method to cycle to 'merge', got %q", m.config.DefaultMergeMethod)
	}

	// 2. Pressing 'd' cycles DefaultMergeMethod: merge -> rebase
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = rawModel.(Model)
	if m.config.DefaultMergeMethod != "rebase" {
		t.Errorf("expected default merge method to cycle to 'rebase', got %q", m.config.DefaultMergeMethod)
	}

	// 3. Pressing 'd' cycles DefaultMergeMethod: rebase -> squash
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = rawModel.(Model)
	if m.config.DefaultMergeMethod != "squash" {
		t.Errorf("expected default merge method to cycle to 'squash', got %q", m.config.DefaultMergeMethod)
	}

	// 4. Pressing Enter confirms the default (squash) and transitions to confirm screen (mergeState = 2)
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = rawModel.(Model)
	if m.mergeState != 2 {
		t.Errorf("expected mergeState to be 2 (confirm screen), got %d", m.mergeState)
	}
	if m.mergeMethod != 0 { // 0 is Squash
		t.Errorf("expected mergeMethod to be 0 (squash), got %d", m.mergeMethod)
	}
}

func TestTUI_RepoFilterSelection(t *testing.T) {
	client := gh.NewClient("test-token", "")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.state = viewMain
	m.activeTab = tabPRs
	m.repos = []gh.Repository{
		{Name: "repo-a"},
		{Name: "repo-b"},
		{Name: "repo-c"},
	}

	// 1. Pressing 'f' enters filter type selection modal
	rawModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = rawModel.(Model)
	if m.state != viewFilterTypeSelect {
		t.Errorf("expected state to be viewFilterTypeSelect, got %d", m.state)
	}

	// 2. Pressing 'r' enters repository list select modal
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = rawModel.(Model)
	if m.state != viewRepoFilterSelect {
		t.Errorf("expected state to be viewRepoFilterSelect, got %d", m.state)
	}
	if m.selectedRepoIdx != 0 {
		t.Errorf("expected default selectedRepoIdx to be 0, got %d", m.selectedRepoIdx)
	}

	// 3. Pressing 'j' (down) selects the next repository
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = rawModel.(Model)
	if m.selectedRepoIdx != 1 {
		t.Errorf("expected selectedRepoIdx to be 1 after down key, got %d", m.selectedRepoIdx)
	}

	// 4. Pressing Enter selects "repo-b", sets it as active repository filter, returns to main view and triggers pull request fetching
	rawModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = rawModel.(Model)
	if m.state != viewMain {
		t.Errorf("expected state to return to viewMain, got %d", m.state)
	}
	if m.filterRepo != "repo-b" {
		t.Errorf("expected filterRepo to be 'repo-b', got %q", m.filterRepo)
	}
	if cmd == nil {
		t.Error("expected fetchPullsCmd to be returned")
	}

	// 5. Pressing 'x' clears all active filters (including repo filter)
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = rawModel.(Model)
	if m.filterRepo != "" {
		t.Errorf("expected filterRepo to be cleared, got %q", m.filterRepo)
	}
}

func TestTUI_IssuesPaginationPRFiltering(t *testing.T) {
	client := gh.NewClient("test-token", "")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.state = viewMain
	m.activeTab = tabIssues

	// 1. Receive issuesLoadedMsg with 0 issues but hasMore = true
	rawModel, _ := m.Update(issuesLoadedMsg{
		issues:  []gh.Issue{},
		hasMore: true,
	})
	m = rawModel.(Model)

	// hasMoreIssues should be true because the message indicated there are more issues on subsequent pages
	if !m.hasMoreIssues {
		t.Error("expected hasMoreIssues to be true when msg.hasMore is true")
	}

	// 2. Receive issuesLoadedMsg with 0 issues and hasMore = false
	rawModel, _ = m.Update(issuesLoadedMsg{
		issues:  []gh.Issue{},
		hasMore: false,
	})
	m = rawModel.(Model)

	// hasMoreIssues should be false because the message indicated there are no more pages
	if m.hasMoreIssues {
		t.Error("expected hasMoreIssues to be false when msg.hasMore is false")
	}
}

func TestTUI_RateLimitErrorRecovery(t *testing.T) {
	client := gh.NewClient("test-token", "")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.width = 80

	// 1. Simulate rate limit error
	m.err = fmt.Errorf("github api access forbidden: API rate limit exceeded")

	// Verify error view renders and wraps the text
	viewStr := m.View()
	if !strings.Contains(viewStr, "FATAL ERROR") || !strings.Contains(viewStr, "rate limit exceeded") {
		t.Error("expected error view to render rate limit error message")
	}

	// 2. Set rate limit reset time to 5 seconds in the future
	resetTime := time.Now().Add(5 * time.Second)
	client.SetRateLimit(gh.RateLimitInfo{
		Limit:     5000,
		Remaining: 0,
		Reset:     resetTime,
	})

	// Run tickMsg, should NOT recover yet because resetTime is in the future
	rawModel, _ := m.Update(tickMsg(time.Now()))
	m = rawModel.(Model)
	if m.err == nil {
		t.Error("expected error to persist while reset time is in the future")
	}

	// 3. Set rate limit reset time to 1 second in the past
	client.SetRateLimit(gh.RateLimitInfo{
		Limit:     5000,
		Remaining: 1000,
		Reset:     time.Now().Add(-1 * time.Second),
	})

	// Run tickMsg, should recover, clear error, set loading, and return fetchActiveTabCmd
	rawModel, cmd := m.Update(tickMsg(time.Now()))
	m = rawModel.(Model)
	if m.err != nil {
		t.Errorf("expected error to be cleared after reset time passed, got: %v", m.err)
	}
	if !m.isLoading {
		t.Error("expected isLoading to be true during recovery reconnect")
	}
	if cmd == nil {
		t.Error("expected fetchActiveTabCmd to be returned on recovery")
	}
}

func TestTUI_LogsSplitPaneAndSegmentation(t *testing.T) {
	client := gh.NewClient("test-token", "")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.width = 80
	m.height = 20

	t1 := time.Date(2026, 7, 14, 14, 32, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 14, 14, 32, 1, 0, time.UTC)
	t3 := time.Date(2026, 7, 14, 14, 32, 2, 0, time.UTC)

	steps := []gh.JobStep{
		{Name: "Set up job", Number: 1, Status: "completed", Conclusion: "success", StartedAt: t1},
		{Name: "Run actions/checkout@v4", Number: 2, Status: "completed", Conclusion: "success", StartedAt: t2},
		{Name: "Run unit tests", Number: 3, Status: "completed", Conclusion: "failure", StartedAt: t3},
	}
	m.jobs = []gh.WorkflowJob{
		{
			ID:    7001,
			Name:  "Build and Test",
			Steps: steps,
		},
	}
	m.selectedJobIdx = 0

	rawLogs := `2026-07-14T14:32:00.000Z ##[group]Set up job
Setting up github runner...
##[endgroup]
2026-07-14T14:32:01.000Z ##[group]Run actions/checkout@v4
Checking out code repository...
##[endgroup]
2026-07-14T14:32:02.000Z ##[group]Run unit tests
go test ./...
Error: Test failed!
`

	// 1. Simulate loading logs
	rawModel, _ := m.Update(logsLoadedMsg{
		logs: rawLogs,
	})
	m = rawModel.(Model)

	// selectedStepIdx should default to the failed step (index 2: "Run unit tests")
	if m.selectedStepIdx != 2 {
		t.Errorf("expected selectedStepIdx to default to failed step 2, got %d", m.selectedStepIdx)
	}

	// Logs content should contain the failed test run log output
	content := m.logsViewport.View()
	if !strings.Contains(content, "Test failed!") {
		t.Error("expected logs viewport to display logs for the failed step")
	}

	// 2. Navigate step list upwards (to step 1: "Run actions/checkout@v4")
	rawModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = rawModel.(Model)

	if m.selectedStepIdx != 1 {
		t.Errorf("expected selectedStepIdx to update to 1, got %d", m.selectedStepIdx)
	}

	content = m.logsViewport.View()
	if !strings.Contains(content, "Checking out code repository...") {
		t.Error("expected logs viewport to display logs for actions/checkout step")
	}

	// 3. Verify side-by-side view renders steps list and logs
	viewStr := m.View()
	if !strings.Contains(viewStr, "STEPS") || !strings.Contains(viewStr, "Run unit tests") {
		t.Error("expected logs view to render steps list sidebar")
	}
}

func TestTUI_LogsSegmentRealWorldData(t *testing.T) {
	// Timestamps from user report
	tSetup := time.Date(2026, 7, 14, 12, 29, 2, 967000000, time.UTC)
	tGovulncheck := time.Date(2026, 7, 14, 12, 29, 2, 979000000, time.UTC)

	steps := []gh.JobStep{
		{Name: "Set up job", Number: 1, Status: "completed", Conclusion: "success", StartedAt: tSetup},
		{Name: "Run govulncheck", Number: 2, Status: "completed", Conclusion: "success", StartedAt: tGovulncheck},
	}

	rawLogs := `2026-07-14T12:29:02.9670914Z GOROOT='/opt/hostedtoolcache/go/1.26.5/x64'
2026-07-14T12:29:02.9671454Z GOSUMDB='sum.golang.org'
2026-07-14T12:29:02.9676988Z ##[endgroup]
2026-07-14T12:29:02.9794750Z ##[group]Run go install golang.org/x/vuln/cmd/govulncheck@latest
2026-07-14T12:29:02.9795335Z go install golang.org/x/vuln/cmd/govulncheck@latest
`

	segments := segmentLogs(rawLogs, steps)

	// Step 0 ("Set up job") logs should contain GOROOT and GOSUMDB
	setupLogs := segments[0]
	if !strings.Contains(setupLogs, "GOROOT") || !strings.Contains(setupLogs, "GOSUMDB") {
		t.Error("expected Set up job logs to contain setup step variables")
	}
	if strings.Contains(setupLogs, "go install") {
		t.Error("expected Set up job logs NOT to leak govulncheck step details")
	}

	// Step 1 ("Run govulncheck") logs should contain the go install details
	govulncheckLogs := segments[1]
	if !strings.Contains(govulncheckLogs, "go install") {
		t.Error("expected Run govulncheck logs to contain command details")
	}
	if strings.Contains(govulncheckLogs, "GOROOT") {
		t.Error("expected Run govulncheck logs NOT to contain setup step variables")
	}
}

func TestTUI_LogsSegmentRealWorldOverlap(t *testing.T) {
	tSetupGo := time.Date(2026, 7, 14, 12, 36, 31, 0, time.UTC)
	tGovulncheck := time.Date(2026, 7, 14, 12, 36, 41, 0, time.UTC)
	tRunTests := time.Date(2026, 7, 14, 12, 36, 44, 0, time.UTC)

	steps := []gh.JobStep{
		{Name: "Set up Go", Number: 4, Status: "completed", Conclusion: "success", StartedAt: tSetupGo},
		{Name: "Run govulncheck", Number: 5, Status: "completed", Conclusion: "success", StartedAt: tGovulncheck},
		{Name: "Run Tests", Number: 6, Status: "completed", Conclusion: "success", StartedAt: tRunTests},
	}

	rawLogs := `2026-07-14T12:36:31.1184254Z ##[group]Run actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16
2026-07-14T12:36:42.1107776Z GOROOT='/opt/hostedtoolcache/go/1.26.5/x64'
2026-07-14T12:36:42.1108127Z GOSUMDB='sum.golang.org'
2026-07-14T12:36:42.1110541Z GOWORK=''
2026-07-14T12:36:42.1110736Z PKG_CONFIG='pkg-config'
2026-07-14T12:36:42.1110885Z 
2026-07-14T12:36:42.1111185Z ##[endgroup]
2026-07-14T12:36:42.1213529Z ##[group]Run go install golang.org/x/vuln/cmd/govulncheck@latest
2026-07-14T12:36:42.1214030Z go install golang.org/x/vuln/cmd/govulncheck@latest
2026-07-14T12:36:42.1248973Z shell: /usr/bin/bash --noprofile --norc -e -o pipefail {0}
2026-07-14T12:36:42.1250075Z ##[endgroup]
2026-07-14T12:36:42.7494409Z ##[group]Run govulncheck -C "$WORK_DIR" -format "$OUTPUT_FORMAT" "$GO_PACKAGE"
2026-07-14T12:36:42.7494945Z govulncheck -C "$WORK_DIR" -format "$OUTPUT_FORMAT" "$GO_PACKAGE"
`

	segments := segmentLogs(rawLogs, steps)

	// Step 0 ("Set up Go") should contain GOWORK='' and PKG_CONFIG='pkg-config'
	setupGoLogs := segments[0]
	if !strings.Contains(setupGoLogs, "GOWORK=''") || !strings.Contains(setupGoLogs, "PKG_CONFIG='pkg-config'") {
		t.Error("expected Set up Go logs to contain GOWORK and PKG_CONFIG")
	}
	if strings.Contains(setupGoLogs, "go install") || strings.Contains(setupGoLogs, "govulncheck") {
		t.Error("expected Set up Go logs NOT to contain govulncheck command logs")
	}

	// Step 1 ("Run govulncheck") should contain the go install command and the govulncheck execution
	govulncheckLogs := segments[1]
	if !strings.Contains(govulncheckLogs, "go install") || !strings.Contains(govulncheckLogs, "govulncheck -C") {
		t.Error("expected Run govulncheck logs to contain command logs and execution logs")
	}
	if strings.Contains(govulncheckLogs, "GOWORK=''") {
		t.Error("expected Run govulncheck logs NOT to contain setup step variables")
	}
}

func TestTUI_LogsSegmentTestsOverlap(t *testing.T) {
	steps := []gh.JobStep{
		{Name: "Run govulncheck", Number: 5, Status: "completed", Conclusion: "success"},
		{Name: "Run Tests", Number: 6, Status: "completed", Conclusion: "success"},
	}

	rawLogs := `2026-07-14T12:40:12.7528311Z ##[endgroup]
2026-07-14T12:40:12.7919385Z No vulnerabilities found.
2026-07-14T12:40:12.8043267Z ##[group]Run go test -v -race ./...
2026-07-14T12:40:12.8043612Z go test -v -race ./...
2026-07-14T12:40:12.8077252Z shell: /usr/bin/bash -e {0}
2026-07-14T12:40:12.8077528Z env:
2026-07-14T12:40:12.8077732Z   GOTOOLCHAIN: local
2026-07-14T12:40:12.8077956Z ##[endgroup]
2026-07-14T12:40:14.5238213Z ?       ghspector/cmd/ghspector    [no test files]
2026-07-14T12:40:15.5375145Z === RUN   TestResolveTokenWithCliGetter
`

	segments := segmentLogs(rawLogs, steps)

	// Step 0 ("Run govulncheck") should contain the "No vulnerabilities found." line
	govulncheckLogs := segments[0]
	if !strings.Contains(govulncheckLogs, "No vulnerabilities found.") {
		t.Error("expected Run govulncheck logs to contain 'No vulnerabilities found.' line")
	}
	if strings.Contains(govulncheckLogs, "go test -v") {
		t.Error("expected Run govulncheck logs NOT to contain go test command logs")
	}

	// Step 1 ("Run Tests") should contain the go test command and outputs
	testsLogs := segments[1]
	if !strings.Contains(testsLogs, "go test -v") || !strings.Contains(testsLogs, "TestResolveTokenWithCliGetter") {
		t.Error("expected Run Tests logs to contain command logs and run outputs")
	}
	if strings.Contains(testsLogs, "No vulnerabilities found.") {
		t.Error("expected Run Tests logs NOT to contain 'No vulnerabilities found.'")
	}
}

func TestTUI_LogsSegmentRealWorldComplete(t *testing.T) {
	// Read the full real-world log file
	logData, err := os.ReadFile("testdata/raw_logs.txt")
	if err != nil {
		t.Fatalf("failed to read test log file: %v", err)
	}

	parseTime := func(s string) time.Time {
		tVal, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("failed to parse time %q: %v", s, err)
		}
		return tVal
	}

	steps := []gh.JobStep{
		{Name: "Set up job", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:26Z"), CompletedAt: parseTime("2026-07-14T12:36:27Z")},
		{Name: "Pull ghcr.io/reviewdog/action-actionlint:v1.72.0", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:27Z"), CompletedAt: parseTime("2026-07-14T12:36:31Z")},
		{Name: "Checkout code", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:31Z"), CompletedAt: parseTime("2026-07-14T12:36:31Z")},
		{Name: "Set up Go", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:31Z"), CompletedAt: parseTime("2026-07-14T12:36:41Z")},
		{Name: "Run govulncheck", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:41Z"), CompletedAt: parseTime("2026-07-14T12:36:44Z")},
		{Name: "Run Tests", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:44Z"), CompletedAt: parseTime("2026-07-14T12:36:49Z")},
		{Name: "Run golangci-lint", Conclusion: "failure", StartedAt: parseTime("2026-07-14T12:36:49Z"), CompletedAt: parseTime("2026-07-14T12:36:52Z")},
		{Name: "Run actionlint", Conclusion: "skipped", StartedAt: parseTime("2026-07-14T12:36:52Z"), CompletedAt: parseTime("2026-07-14T12:36:52Z")},
		{Name: "Post Run golangci-lint", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:52Z"), CompletedAt: parseTime("2026-07-14T12:36:53Z")},
		{Name: "Post Run govulncheck", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:53Z"), CompletedAt: parseTime("2026-07-14T12:36:53Z")},
		{Name: "Post Set up Go", Conclusion: "skipped", StartedAt: parseTime("2026-07-14T12:36:53Z"), CompletedAt: parseTime("2026-07-14T12:36:53Z")},
		{Name: "Post Checkout code", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:53Z"), CompletedAt: parseTime("2026-07-14T12:36:53Z")},
		{Name: "Complete job", Conclusion: "success", StartedAt: parseTime("2026-07-14T12:36:53Z"), CompletedAt: parseTime("2026-07-14T12:36:53Z")},
	}

	segments := segmentLogs(string(logData), steps)

	// Assert that all executed steps have non-empty logs and skipped steps are empty
	for i, step := range steps {
		logLen := len(segments[i])
		if step.Conclusion == "skipped" {
			if logLen > 0 {
				t.Errorf("expected skipped step %d (%q) to have 0 log length, got %d", i, step.Name, logLen)
			}
		} else {
			if logLen == 0 {
				t.Errorf("expected executed step %d (%q) to have non-empty logs", i, step.Name)
			}
		}
	}

	// Step 4 ("Run govulncheck") should have logs and contain "No vulnerabilities found."
	govulncheckLogs := segments[4]
	if !strings.Contains(govulncheckLogs, "No vulnerabilities found.") {
		t.Error("expected Run govulncheck logs to contain 'No vulnerabilities found.'")
	}

	// Step 5 ("Run Tests") should have logs and contain "ghspector/internal/tui"
	testsLogs := segments[5]
	if !strings.Contains(testsLogs, "ghspector/internal/tui") {
		t.Error("expected Run Tests logs to contain test suite execution output")
	}

	// Step 6 ("Run golangci-lint") should have logs and contain the linter findings
	linterLogs := segments[6]
	if !strings.Contains(linterLogs, "SA1019") || !strings.Contains(linterLogs, "LineUp is deprecated") {
		t.Error("expected Run golangci-lint logs to contain linter deprecation issues")
	}

	// Step 12 ("Complete job") should have logs containing post cleanup lines
	completeJobLogs := segments[12]
	if !strings.Contains(completeJobLogs, "Cleaning up orphan processes") {
		t.Error("expected Complete job logs to contain post cleanup orphan processes message")
	}
}

func TestTUI_LogsSegmentSyntheticFastSteps(t *testing.T) {
	parseTime := func(s string) time.Time {
		tVal, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("failed to parse time %q: %v", s, err)
		}
		return tVal
	}

	steps := []gh.JobStep{
		{Name: "Set up job", Conclusion: "success", StartedAt: parseTime("2026-07-22T12:30:47Z"), CompletedAt: parseTime("2026-07-22T12:30:48Z")},
		{Name: "Checkout Code", Conclusion: "success", StartedAt: parseTime("2026-07-22T12:30:48Z"), CompletedAt: parseTime("2026-07-22T12:30:50Z")},
		{Name: "Set up Go", Conclusion: "success", StartedAt: parseTime("2026-07-22T12:30:50Z"), CompletedAt: parseTime("2026-07-22T12:30:50Z")},
		{Name: "Log in to GHCR", Conclusion: "success", StartedAt: parseTime("2026-07-22T12:30:50Z"), CompletedAt: parseTime("2026-07-22T12:30:50Z")},
		{Name: "Install Development Tools", Conclusion: "success", StartedAt: parseTime("2026-07-22T12:30:50Z"), CompletedAt: parseTime("2026-07-22T12:31:04Z")},
		{Name: "Post Set up Go", Conclusion: "success", StartedAt: parseTime("2026-07-22T12:32:39Z"), CompletedAt: parseTime("2026-07-22T12:32:39Z")},
		{Name: "Complete job", Conclusion: "success", StartedAt: parseTime("2026-07-22T12:32:40Z"), CompletedAt: parseTime("2026-07-22T12:32:40Z")},
	}

	syntheticLogs := `2026-07-22T12:30:47.0000000Z ##[group]Operating System
2026-07-22T12:30:47.5000000Z Linux 6.8.0
2026-07-22T12:30:48.1000000Z ##[group]Run actions/checkout@v4
2026-07-22T12:30:49.0000000Z Syncing repository
2026-07-22T12:30:50.1000000Z ##[group]Run actions/setup-go@v5
2026-07-22T12:30:50.2000000Z Setup go version spec 1.26.5
2026-07-22T12:30:50.3000000Z Found in cache
2026-07-22T12:30:50.4000000Z Successfully set up Go version 1.26.5
2026-07-22T12:30:50.5000000Z go version go1.26.5 linux/amd64
2026-07-22T12:30:51.1000000Z go env
2026-07-22T12:30:51.2000000Z   GOOS='linux'
2026-07-22T12:30:51.3000000Z   GOARCH='amd64'
2026-07-22T12:30:51.4000000Z   GOROOT='/opt/tool/go'
2026-07-22T12:30:52.1000000Z ##[group]Install Development Tools
2026-07-22T12:30:53.0000000Z Installing tools...
2026-07-22T12:32:39.1000000Z Post job cleanup
2026-07-22T12:32:40.1000000Z Cleaning up orphan processes`

	segments := segmentLogs(syntheticLogs, steps)

	// Step 2 ("Set up Go") should have logs and contain GOROOT output
	goLogs := segments[2]
	if len(goLogs) == 0 {
		t.Error("expected Set up Go logs to not be empty")
	}
	if !strings.Contains(goLogs, "GOROOT='/opt/tool/go'") {
		t.Error("expected Set up Go logs to contain go env GOROOT output")
	}

	// Step 6 ("Complete job") should contain orphan process cleanup
	completeLogs := segments[6]
	if len(completeLogs) == 0 {
		t.Error("expected Complete job logs to not be empty")
	}
	if !strings.Contains(completeLogs, "Cleaning up orphan processes") {
		t.Error("expected Complete job logs to contain orphan cleanup output")
	}
}

func TestCleanLogForDisplay(t *testing.T) {
	rawInput := "2026-07-22T14:00:00Z ##[group]Run actions/checkout@v4\n" +
		"2026-07-22T14:00:01Z [command]/usr/bin/git version\n" +
		"2026-07-22T14:00:02Z ##[error]something went wrong\n" +
		"2026-07-22T14:00:03Z ##[endgroup]\n"

	expected := "2026-07-22T14:00:00Z Run actions/checkout@v4\n" +
		"2026-07-22T14:00:01Z /usr/bin/git version\n" +
		"2026-07-22T14:00:02Z something went wrong"

	actual := cleanLogForDisplay(rawInput)
	if actual != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, actual)
	}
}

func TestPRDraftIndicators(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.width = 120
	m.height = 30

	draftPR := gh.PullRequest{
		Number: 42,
		Title:  "Draft Feature Implementation",
		Draft:  true,
		State:  "open",
		User:   &gh.User{Login: "testuser"},
		Repository: gh.Repository{
			Name:     "myrepo",
			FullName: "myorg/myrepo",
		},
	}

	m.pulls = []gh.PullRequest{draftPR}
	m.selectedPull = &draftPR

	// 1. Verify PR List view output
	listOutput := m.renderPullsView()
	if !strings.Contains(listOutput, "[Draft]") {
		t.Errorf("expected PR list view to contain '[Draft]', got:\n%s", listOutput)
	}

	// 2. Verify PR Details view output
	detailsOutput := m.renderPRDetailsView()
	if !strings.Contains(detailsOutput, "[DRAFT]") {
		t.Errorf("expected PR details view header to contain '[DRAFT]', got:\n%s", detailsOutput)
	}

	sidebarOutput := m.renderPRRightSidebar(40, 20)
	if !strings.Contains(sidebarOutput, "DRAFT") {
		t.Errorf("expected PR sidebar to contain 'State: DRAFT', got:\n%s", sidebarOutput)
	}
}

func TestStatusBannerAndAutoDismiss(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	cfg := &auth.Config{}
	m := InitModel(client, cfg)
	m.width = 100
	m.height = 30

	cmd := m.setStatusMsg("Logs are not yet available for running jobs. Please wait for completion.")
	if cmd == nil {
		t.Fatal("expected setStatusMsg to return auto-dismiss timer command")
	}

	// 1. Verify status banner renders above footer
	footerOutput := m.renderFooter([]string{"q:Quit"})
	if !strings.Contains(footerOutput, "Logs are not yet available") {
		t.Errorf("expected footer to contain status banner text, got:\n%s", footerOutput)
	}
	if !strings.Contains(footerOutput, "✖") {
		t.Errorf("expected status banner to contain error icon '✖', got:\n%s", footerOutput)
	}

	// 2. Verify clearStatusMsg clears statusMsg
	id := m.statusMsgID
	rawModel, _ := m.Update(clearStatusMsg{id: id})
	m = rawModel.(Model)
	if m.statusMsg != "" {
		t.Errorf("expected statusMsg to be cleared after clearStatusMsg, got %q", m.statusMsg)
	}
}

func TestJobViewBrowserShortcut(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	m := InitModel(client, nil)
	m.state = viewJobs
	m.jobs = []gh.WorkflowJob{
		{
			ID:      101,
			Name:    "build-job",
			HTMLURL: "https://github.com/myorg/myrepo/actions/runs/1/job/101",
		},
	}
	m.selectedJobIdx = 0

	// Pressing 'w' in viewJobs should not crash or error out
	rawModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	_ = rawModel.(Model)
	if cmd != nil {
		t.Errorf("expected nil cmd for browser open key press, got %v", cmd)
	}
}

func TestRenderFooterDeterministicNoWrap(t *testing.T) {
	client := gh.NewClient("test-token", "https://api.github.com")
	m := InitModel(client, nil)
	m.width = 120
	m.height = 30

	testCases := []struct {
		state        viewState
		keys         []string
		expectedLeft string
	}{
		{
			state:        viewMain,
			keys:         []string{"Tab:Tabs", "j/k:Navigate", "Enter:Jobs", "w:Browser", "f:Filter", "m:My Runs", "x:Clear", "r:Refresh"},
			expectedLeft: "?:Help  Esc:Exit",
		},
		{
			state:        viewMain,
			keys:         []string{"Tab:Tabs", "j/k:Navigate", "Enter:View PR", "w:Browser", "f:Filter", "s:State", "a:My PRs", "i:Assigned", "v:Reviewed", "x:Clear", "r:Refresh"},
			expectedLeft: "?:Help  Esc:Exit",
		},
		{
			state:        viewJobs,
			keys:         []string{"j/k:Navigate", "Enter:Logs", "w:Job Browser", "v:Run Browser", "[/]:Attempts", "r:Refresh"},
			expectedLeft: "?:Help  Esc:Back",
		},
		{
			state:        viewLogs,
			keys:         []string{"j/k:Steps", "u/d:Scroll Logs", "w:Browser", "r:Refresh"},
			expectedLeft: "?:Help  Esc:Back",
		},
		{
			state:        viewPRDetails,
			keys:         []string{"Tab:Focus", "j/k:Navigate", "Enter:Run/Browser", "Shift+D:Diff", "r:Refresh", "m:Merge", "c:Comments", "v:Commits", "Shift+C:Close"},
			expectedLeft: "?:Help  Esc:Back",
		},
	}

	for _, tc := range testCases {
		m.state = tc.state
		footer := m.renderFooter(tc.keys)

		// Border (line 1) + 1 single bottom bar text line (line 2) = 2 lines total
		lines := strings.Split(footer, "\n")
		if len(lines) != 2 {
			t.Errorf("state %v: expected top border + single content line without wrapping (2 lines total), got %d lines:\n%s", tc.state, len(lines), footer)
		}

		if !strings.Contains(footer, tc.expectedLeft) {
			t.Errorf("state %v: expected left side to contain %q, got:\n%s", tc.state, tc.expectedLeft, footer)
		}
	}
}




