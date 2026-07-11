package tui

import (
	"encoding/json"
	"fmt"
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
	if cmd != nil {
		t.Error("expected command to be nil when trying to fetch logs of a running job")
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



