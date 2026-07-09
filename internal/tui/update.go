package tui

import (
	"sort"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"ghspector/internal/auth"
	"ghspector/internal/gh"
)

type batchRunsUpdateMsg []runUpdateMsg
type batchJobsUpdateMsg []jobUpdateMsg

// Update handles state transitions and events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global keys
		switch msg.String() {
		case "ctrl+c", "q":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "o":
			if m.state != viewSplash {
				if m.state == viewSwitcher {
					// close switcher
					m.state = m.prevState
				} else {
					// open switcher
					m.prevState = m.state
					m.state = viewSwitcher
				}
				return m, nil
			}
		}

		// View specific keys
		switch m.state {
		case viewMain:
			switch msg.String() {
			case "j", "down":
				maxIdx := len(m.runs)
				if !m.hasMoreRuns {
					maxIdx = len(m.runs) - 1
				}

				if m.selectedRunIdx < maxIdx {
					m.selectedRunIdx++
					m.scrollRuns()
				}
			case "k", "up":
				if m.selectedRunIdx > 0 {
					m.selectedRunIdx--
					m.scrollRuns()
				}
			case "enter":
				// If we selected "Load More..."
				if m.hasMoreRuns && m.selectedRunIdx == len(m.runs) {
					m.runPage++
					m.isLoading = true
					m.statusMsg = "Loading more runs..."
					return m, m.fetchRunsCmd()
				}

				// Otherwise click into Workflow Run
				if len(m.runs) > 0 && m.selectedRunIdx < len(m.runs) {
					run := m.runs[m.selectedRunIdx]
					m.state = viewSplash
					m.loadingMsg = "Fetching jobs for " + run.Name
					m.selectedJobIdx = 0
					m.jobStartIndex = 0
					m.jobs = nil
					return m, m.fetchJobsCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID)
				}
			case "r", "ctrl+r":
				m.state = viewSplash
				m.loadingMsg = "Refreshing workflow runs"
				m.runPage = 1
				m.hasMoreRuns = true
				m.selectedRunIdx = 0
				m.runStartIndex = 0
				m.runs = nil
				return m, m.fetchRunsCmd()
			}

		case viewJobs:
			switch msg.String() {
			case "j", "down":
				if m.selectedJobIdx < len(m.jobs)-1 {
					m.selectedJobIdx++
					m.scrollJobs()
				}
			case "k", "up":
				if m.selectedJobIdx > 0 {
					m.selectedJobIdx--
					m.scrollJobs()
				}
			case "enter":
				if len(m.jobs) > 0 {
					job := m.jobs[m.selectedJobIdx]
					run := m.runs[m.selectedRunIdx]
					m.state = viewSplash
					m.loadingMsg = "Fetching logs for " + job.Name
					m.logs = ""
					m.logsLoading = true
					return m, m.fetchLogsCmd(run.Repository.Owner.Login, run.Repository.Name, job.ID)
				}
			case "esc", "backspace":
				m.state = viewMain
			case "r", "ctrl+r":
				run := m.runs[m.selectedRunIdx]
				m.state = viewSplash
				m.loadingMsg = "Refreshing jobs"
				m.jobs = nil
				m.selectedJobIdx = 0
				m.jobStartIndex = 0
				return m, m.fetchJobsCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID)
			}

		case viewLogs:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewJobs
			case "r", "ctrl+r":
				job := m.jobs[m.selectedJobIdx]
				run := m.runs[m.selectedRunIdx]
				m.state = viewSplash
				m.loadingMsg = "Refreshing logs"
				m.logs = ""
				m.logsLoading = true
				return m, m.fetchLogsCmd(run.Repository.Owner.Login, run.Repository.Name, job.ID)
			default:
				// Forward movement keys to viewport and handle log follow status
				oldY := m.logsViewport.YOffset
				m.logsViewport, cmd = m.logsViewport.Update(msg)
				cmds = append(cmds, cmd)

				if m.logsViewport.YOffset < oldY {
					m.followLogs = false
				}
				if m.logsViewport.AtBottom() {
					m.followLogs = true
				}
			}

		case viewSwitcher:
			switch msg.String() {
			case "j", "down":
				if m.selectedTargetIdx < len(m.targets)-1 {
					m.selectedTargetIdx++
				}
			case "k", "up":
				if m.selectedTargetIdx > 0 {
					m.selectedTargetIdx--
				}
			case "enter":
				m.state = viewSplash
				m.loadingMsg = "Loading runs for " + m.targets[m.selectedTargetIdx].Name
				m.runs = nil
				m.runPage = 1
				m.hasMoreRuns = true
				m.selectedRunIdx = 0
				m.runStartIndex = 0
				return m, m.fetchRunsCmd()
			case "esc":
				m.state = m.prevState
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logsViewport.Width = msg.Width - 4
		m.logsViewport.Height = msg.Height - 8
		if m.logsViewport.Height < 5 {
			m.logsViewport.Height = 5
		}

	case tickMsg:
		m.tickCount++
		return m, m.tick()

	case pollMsg:
		// Periodically poll active items on screen
		var pollCmd tea.Cmd
		if m.state == viewMain {
			pollCmd = m.pollRunsCmd()
		} else if m.state == viewJobs {
			pollCmd = m.pollActiveJobsCmd()
		} else if m.state == viewLogs {
			pollCmd = m.pollLogsCmd()
		}
		return m, tea.Batch(pollCmd, m.pollTick())

	case initDataMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}

		// Compile target options (User first, then Orgs)
		m.targets = append(m.targets, Target{Name: msg.user.Login, IsOrg: false})
		for _, org := range msg.orgs {
			m.targets = append(m.targets, Target{Name: org.Login, IsOrg: true})
		}

		// Try to match defaults from configuration or fall back to user profile
		m.selectedTargetIdx = 0
		if m.config != nil {
			if m.config.DefaultOrg != "" {
				for idx, target := range m.targets {
					if target.IsOrg && target.Name == m.config.DefaultOrg {
						m.selectedTargetIdx = idx
						break
					}
				}
			} else if m.config.DefaultAccount != "" {
				for idx, target := range m.targets {
					if !target.IsOrg && target.Name == m.config.DefaultAccount {
						m.selectedTargetIdx = idx
						break
					}
				}
			}
		}

		m.loadingMsg = "Loading runs for " + m.targets[m.selectedTargetIdx].Name
		return m, m.fetchRunsCmd()

	case runsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading runs: " + msg.err.Error()
			m.state = viewMain
			return m, nil
		}

		if m.runPage == 1 {
			m.runs = msg.runs
		} else {
			m.runs = append(m.runs, msg.runs...)
		}

		// Sort all runs using stable status priority and date logic
		sortRuns(m.runs)

		// If no runs or small amount returned, mark as no more runs
		if len(msg.runs) == 0 {
			m.hasMoreRuns = false
		}

		m.scrollRuns()
		m.state = viewMain
		m.statusMsg = "Successfully loaded runs"

	case runsPolledMsg:
		if msg.err == nil && len(msg.runs) > 0 {
			m.runs = mergeRuns(m.runs, msg.runs)
			m.scrollRuns()
		}

	case jobsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading jobs: " + msg.err.Error()
			m.state = viewMain
			return m, nil
		}
		m.jobs = msg.jobs
		sortJobs(m.jobs)
		m.selectedJobIdx = 0
		m.jobStartIndex = 0
		m.state = viewJobs
		m.statusMsg = ""

	case logsLoadedMsg:
		m.logsLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading logs: " + msg.err.Error()
			m.state = viewJobs
			return m, nil
		}
		m.logs = msg.logs

		// Initialize viewport only if we are entering logs view for the first time
		if m.state != viewLogs {
			m.followLogs = true
			m.logsViewport = viewport.New(m.width-4, m.height-8)
			if m.logsViewport.Height < 5 {
				m.logsViewport.Height = 5
			}
		}

		m.logsViewport.SetContent(m.logs)
		if m.followLogs {
			m.logsViewport.GotoBottom()
		}
		m.state = viewLogs

	case runUpdateMsg:
		for i, run := range m.runs {
			if run.ID == msg.runID {
				m.runs[i].Status = msg.status
				m.runs[i].Conclusion = msg.conclusion
				m.runs[i].UpdatedAt = time.Now()
				break
			}
		}

	case jobUpdateMsg:
		for i, job := range m.jobs {
			if job.ID == msg.jobID {
				m.jobs[i].Status = msg.status
				m.jobs[i].Conclusion = msg.conclusion
				m.jobs[i].CompletedAt = time.Now()
				break
			}
		}

	case batchRunsUpdateMsg:
		for _, update := range msg {
			for i, run := range m.runs {
				if run.ID == update.runID {
					m.runs[i].Status = update.status
					m.runs[i].Conclusion = update.conclusion
					m.runs[i].UpdatedAt = time.Now()
					break
				}
			}
		}
		sortRuns(m.runs)
		m.scrollRuns()

	case batchJobsUpdateMsg:
		for _, update := range msg {
			for i, job := range m.jobs {
				if job.ID == update.jobID {
					m.jobs[i].Status = update.status
					m.jobs[i].Conclusion = update.conclusion
					m.jobs[i].CompletedAt = time.Now()
					break
				}
			}
		}
		sortJobs(m.jobs)
		m.scrollJobs()
	}

	return m, tea.Batch(cmds...)
}

// fetchRunsCmd fetches the workflows for the active repositories.
func (m Model) fetchRunsCmd() tea.Cmd {
	return func() tea.Msg {
		if len(m.targets) == 0 {
			return runsLoadedMsg{err: auth.ErrUnauthenticated}
		}
		target := m.targets[m.selectedTargetIdx]

		// 1. Fetch repositories sorted by pushes
		var repos []gh.Repository
		var err error
		if target.IsOrg {
			repos, err = m.client.GetRepos(m.ctx, "org", target.Name, 1, 15)
		} else {
			repos, err = m.client.GetRepos(m.ctx, "user", target.Name, 1, 15)
		}
		if err != nil {
			return runsLoadedMsg{err: err}
		}

		if len(repos) == 0 {
			return runsLoadedMsg{runs: nil}
		}

		// 2. Fetch runs concurrently
		var wg sync.WaitGroup
		var mu sync.Mutex
		var allRuns []gh.WorkflowRun

		// Query up to top 8 active repositories to stay under rate limits
		limit := len(repos)
		if limit > 8 {
			limit = 8
		}

		for i := 0; i < limit; i++ {
			repo := repos[i]
			wg.Add(1)
			go func(r gh.Repository) {
				defer wg.Done()
				runs, err := m.client.GetWorkflowRuns(m.ctx, r.Owner.Login, r.Name, m.runPage, 8)
				if err == nil {
					// Embed owner and repo context into runs for displaying
					for j := range runs {
						runs[j].Repository = r
					}
					mu.Lock()
					allRuns = append(allRuns, runs...)
					mu.Unlock()
				}
			}(repo)
		}
		wg.Wait()

		// Sort all runs
		sortRuns(allRuns)

		return runsLoadedMsg{runs: allRuns}
	}
}

// fetchJobsCmd fetches jobs inside a workflow run.
func (m Model) fetchJobsCmd(owner, repo string, runID int64) tea.Cmd {
	return func() tea.Msg {
		jobs, err := m.client.GetWorkflowRunJobs(m.ctx, owner, repo, runID)
		return jobsLoadedMsg{jobs: jobs, err: err}
	}
}

// fetchLogsCmd fetches logs of a job.
func (m Model) fetchLogsCmd(owner, repo string, jobID int64) tea.Cmd {
	return func() tea.Msg {
		logs, err := m.client.GetJobLogs(m.ctx, owner, repo, jobID)
		return logsLoadedMsg{logs: logs, err: err}
	}
}

// mergeRuns merges new workflow runs into the existing slice, updating status/conclusion, removing duplicates, and sorting by CreatedAt descending.
func mergeRuns(existing []gh.WorkflowRun, newRuns []gh.WorkflowRun) []gh.WorkflowRun {
	runMap := make(map[int64]gh.WorkflowRun)
	for _, r := range existing {
		runMap[r.ID] = r
	}
	for _, r := range newRuns {
		runMap[r.ID] = r
	}

	merged := make([]gh.WorkflowRun, 0, len(runMap))
	for _, r := range runMap {
		merged = append(merged, r)
	}

	sortRuns(merged)

	return merged
}

// pollRunsCmd fetches the first page of runs for active repositories to detect new runs and update existing ones.
func (m Model) pollRunsCmd() tea.Cmd {
	return func() tea.Msg {
		if len(m.targets) == 0 {
			return nil
		}
		target := m.targets[m.selectedTargetIdx]

		// 1. Fetch repositories sorted by pushes
		var repos []gh.Repository
		var err error
		if target.IsOrg {
			repos, err = m.client.GetRepos(m.ctx, "org", target.Name, 1, 15)
		} else {
			repos, err = m.client.GetRepos(m.ctx, "user", target.Name, 1, 15)
		}
		if err != nil {
			return runsPolledMsg{err: err}
		}

		if len(repos) == 0 {
			return runsPolledMsg{runs: nil}
		}

		// 2. Fetch page 1 runs concurrently
		var wg sync.WaitGroup
		var mu sync.Mutex
		var allRuns []gh.WorkflowRun

		limit := len(repos)
		if limit > 8 {
			limit = 8
		}

		for i := 0; i < limit; i++ {
			repo := repos[i]
			wg.Add(1)
			go func(r gh.Repository) {
				defer wg.Done()
				runs, err := m.client.GetWorkflowRuns(m.ctx, r.Owner.Login, r.Name, 1, 8)
				if err == nil {
					for j := range runs {
						runs[j].Repository = r
					}
					mu.Lock()
					allRuns = append(allRuns, runs...)
					mu.Unlock()
				}
			}(repo)
		}
		wg.Wait()

		// Sort all runs
		sortRuns(allRuns)

		return runsPolledMsg{runs: allRuns}
	}
}

// pollActiveJobsCmd fetches updates for running/queued jobs.
func (m Model) pollActiveJobsCmd() tea.Cmd {
	return func() tea.Msg {
		if len(m.jobs) == 0 {
			return nil
		}
		run := m.runs[m.selectedRunIdx]
		updatedJobs, err := m.client.GetWorkflowRunJobs(m.ctx, run.Repository.Owner.Login, run.Repository.Name, run.ID)
		if err != nil {
			return nil
		}

		var updates batchJobsUpdateMsg
		for _, job := range updatedJobs {
			updates = append(updates, jobUpdateMsg{
				jobID:      job.ID,
				status:     job.Status,
				conclusion: job.Conclusion,
			})
		}
		return updates
	}
}

// pollLogsCmd updates logs if the viewed job is active.
func (m Model) pollLogsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.state != viewLogs || len(m.jobs) == 0 || m.selectedJobIdx >= len(m.jobs) {
			return nil
		}
		job := m.jobs[m.selectedJobIdx]
		if job.Status == "completed" {
			return nil
		}
		run := m.runs[m.selectedRunIdx]
		logs, err := m.client.GetJobLogs(m.ctx, run.Repository.Owner.Login, run.Repository.Name, job.ID)
		if err == nil {
			return logsLoadedMsg{logs: logs}
		}
		return nil
	}
}

// scrollRuns adjusts runStartIndex to keep the selectedRunIdx visible in the viewport.
func (m *Model) scrollRuns() {
	visibleRows := m.height - 12
	if visibleRows < 5 {
		visibleRows = 5
	}
	totalRows := len(m.runs)
	if m.hasMoreRuns {
		totalRows++
	}
	if m.selectedRunIdx < m.runStartIndex {
		m.runStartIndex = m.selectedRunIdx
	}
	if m.selectedRunIdx >= m.runStartIndex+visibleRows {
		m.runStartIndex = m.selectedRunIdx - visibleRows + 1
	}
	if m.runStartIndex > totalRows-visibleRows {
		m.runStartIndex = totalRows - visibleRows
	}
	if m.runStartIndex < 0 {
		m.runStartIndex = 0
	}
}

// scrollJobs adjusts jobStartIndex to keep the selectedJobIdx visible in the viewport.
func (m *Model) scrollJobs() {
	visibleRows := m.height - 15
	if visibleRows < 5 {
		visibleRows = 5
	}
	totalRows := len(m.jobs)
	if m.selectedJobIdx < m.jobStartIndex {
		m.jobStartIndex = m.selectedJobIdx
	}
	if m.selectedJobIdx >= m.jobStartIndex+visibleRows {
		m.jobStartIndex = m.selectedJobIdx - visibleRows + 1
	}
	if m.jobStartIndex > totalRows-visibleRows {
		m.jobStartIndex = totalRows - visibleRows
	}
	if m.jobStartIndex < 0 {
		m.jobStartIndex = 0
	}
}

func statusPriority(status string) int {
	switch status {
	case "queued":
		return 0
	case "in_progress":
		return 1
	default:
		return 2
	}
}

func sortRuns(runs []gh.WorkflowRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		pI := statusPriority(runs[i].Status)
		pJ := statusPriority(runs[j].Status)
		if pI != pJ {
			return pI < pJ
		}
		if !runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].CreatedAt.After(runs[j].CreatedAt)
		}
		return runs[i].ID > runs[j].ID
	})
}

func sortJobs(jobs []gh.WorkflowJob) {
	sort.SliceStable(jobs, func(i, j int) bool {
		pI := statusPriority(jobs[i].Status)
		pJ := statusPriority(jobs[j].Status)
		if pI != pJ {
			return pI < pJ
		}
		if !jobs[i].StartedAt.Equal(jobs[j].StartedAt) {
			return jobs[i].StartedAt.After(jobs[j].StartedAt)
		}
		return jobs[i].ID > jobs[j].ID
	})
}
