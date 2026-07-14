package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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

	// Always handle tickMsg first to keep the loading spinner animating
	if _, ok := msg.(tickMsg); ok {
		m.tickCount++

		// Check if we can recover from rate limit error
		if m.err != nil && strings.Contains(strings.ToLower(m.err.Error()), "rate limit") {
			rl := m.client.GetRateLimit()
			if !rl.Reset.IsZero() && time.Now().After(rl.Reset) {
				m.err = nil
				m.isLoading = true
				m.loadingMsg = "Rate limit reset. Reconnecting..."
				
				m.runPage = 1
				m.hasMoreRuns = true
				m.selectedRunIdx = 0
				m.runStartIndex = 0
				m.runs = nil

				m.pullPage = 1
				m.hasMorePulls = true
				m.selectedPullIdx = 0
				m.pullStartIndex = 0
				m.pulls = nil

				m.issuePage = 1
				m.hasMoreIssues = true
				m.selectedIssueIdx = 0
				m.issueStartIndex = 0
				m.issues = nil

				return m, tea.Batch(m.tick(), m.fetchActiveTabCmd())
			}
		}

		return m, m.tick()
	}

	// If there is a fatal error, only allow quitting
	if m.err != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "q", "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		}
		return m, nil
	}

	// Intercept keys for filter type selection
	if m.state == viewFilterTypeSelect {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "u", "U":
				if m.activeTab == tabWorkflows {
					m.state = viewMain
					m.showFilterInput = true
					m.textInput.SetValue(m.filterActor)
					m.textInput.Focus()
					return m, textinput.Blink
				} else if m.activeTab == tabPRs {
					m.state = viewPRFilterInput
					m.textInput.SetValue("")
					m.textInput.Focus()
					return m, textinput.Blink
				} else if m.activeTab == tabIssues {
					m.state = viewIssueFilterInput
					m.textInput.SetValue("")
					m.textInput.Focus()
					return m, textinput.Blink
				}
			case "r", "R":
				m.state = viewRepoFilterSelect
				m.selectedRepoIdx = 0
				m.repoStartIndex = 0
				if len(m.repos) == 0 {
					m.isLoading = true
					m.loadingMsg = "Loading repositories..."
					return m, m.fetchReposCmd()
				}
				return m, nil
			case "esc":
				m.state = viewMain
				return m, nil
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		}
		return m, nil
	}

	// Intercept keys for repository filter selection
	if m.state == viewRepoFilterSelect {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "j", "down":
				if m.selectedRepoIdx < len(m.repos)-1 {
					m.selectedRepoIdx++
				}
				return m, nil
			case "k", "up":
				if m.selectedRepoIdx > 0 {
					m.selectedRepoIdx--
				}
				return m, nil
			case "enter":
				if len(m.repos) > 0 && m.selectedRepoIdx >= 0 && m.selectedRepoIdx < len(m.repos) {
					m.filterRepo = m.repos[m.selectedRepoIdx].Name
					m.state = viewMain
					
					m.isLoading = true
					m.loadingMsg = fmt.Sprintf("Filtering by repo %s...", m.filterRepo)
					
					if m.activeTab == tabWorkflows {
						m.runPage = 1
						m.hasMoreRuns = true
						m.selectedRunIdx = 0
						m.runStartIndex = 0
						m.runs = nil
						return m, m.fetchRunsCmd()
					} else if m.activeTab == tabPRs {
						m.pullPage = 1
						m.hasMorePulls = true
						m.selectedPullIdx = 0
						m.pullStartIndex = 0
						m.pulls = nil
						return m, m.fetchPullsCmd()
					} else if m.activeTab == tabIssues {
						m.issuePage = 1
						m.hasMoreIssues = true
						m.selectedIssueIdx = 0
						m.issueStartIndex = 0
						m.issues = nil
						return m, m.fetchIssuesCmd()
					}
				}
				return m, nil
			case "esc":
				m.state = viewFilterTypeSelect
				return m, nil
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		}
		return m, nil
	}

	// If filtering input is active, intercept key messages first
	if m.showFilterInput {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				m.filterActor = m.textInput.Value()
				m.showFilterInput = false
				m.isLoading = true
				m.loadingMsg = "Filtering runs"
				m.runPage = 1
				m.hasMoreRuns = true
				m.selectedRunIdx = 0
				m.runStartIndex = 0
				m.runs = nil
				return m, m.fetchRunsCmd()
			case "esc":
				m.showFilterInput = false
				return m, nil
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		}
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	// Intercept keys for PR user filter input
	if m.state == viewPRFilterInput {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				m.prFilterUser = m.textInput.Value()
				m.state = viewPRFilterTypeSelect
				return m, nil
			case "esc":
				m.state = viewMain
				return m, nil
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		}
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	// Intercept keys for PR filter type selection
	if m.state == viewPRFilterTypeSelect {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "a", "A":
				m.filterPRAuthor = m.prFilterUser
				m.filterPRAssignee = ""
				m.filterPRReviewer = ""
				m.state = viewMain
				m.isLoading = true
				m.loadingMsg = "Filtering PRs by author..."
				m.pullPage = 1
				m.hasMorePulls = true
				m.selectedPullIdx = 0
				m.pullStartIndex = 0
				m.pulls = nil
				return m, m.fetchPullsCmd()
			case "i", "I":
				m.filterPRAuthor = ""
				m.filterPRAssignee = m.prFilterUser
				m.filterPRReviewer = ""
				m.state = viewMain
				m.isLoading = true
				m.loadingMsg = "Filtering PRs by assignee..."
				m.pullPage = 1
				m.hasMorePulls = true
				m.selectedPullIdx = 0
				m.pullStartIndex = 0
				m.pulls = nil
				return m, m.fetchPullsCmd()
			case "r", "R":
				m.filterPRAuthor = ""
				m.filterPRAssignee = ""
				m.filterPRReviewer = m.prFilterUser
				m.state = viewMain
				m.isLoading = true
				m.loadingMsg = "Filtering PRs by reviewer..."
				m.pullPage = 1
				m.hasMorePulls = true
				m.selectedPullIdx = 0
				m.pullStartIndex = 0
				m.pulls = nil
				return m, m.fetchPullsCmd()
			case "esc", "c", "C":
				m.state = viewMain
				return m, nil
			}
		}
		return m, nil
	}

	// Intercept keys for Issue user filter input
	if m.state == viewIssueFilterInput {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				m.issueFilterUser = m.textInput.Value()
				m.state = viewIssueFilterTypeSelect
				return m, nil
			case "esc":
				m.state = viewMain
				return m, nil
			case "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		}
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	// Intercept keys for Issue filter type selection
	if m.state == viewIssueFilterTypeSelect {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "a", "A":
				m.filterIssueAuthor = m.issueFilterUser
				m.filterIssueAssignee = ""
				m.state = viewMain
				m.isLoading = true
				m.loadingMsg = "Filtering issues by author..."
				m.issuePage = 1
				m.hasMoreIssues = true
				m.selectedIssueIdx = 0
				m.issueStartIndex = 0
				m.issues = nil
				return m, m.fetchIssuesCmd()
			case "i", "I":
				m.filterIssueAuthor = ""
				m.filterIssueAssignee = m.issueFilterUser
				m.state = viewMain
				m.isLoading = true
				m.loadingMsg = "Filtering issues by assignee..."
				m.issuePage = 1
				m.hasMoreIssues = true
				m.selectedIssueIdx = 0
				m.issueStartIndex = 0
				m.issues = nil
				return m, m.fetchIssuesCmd()
			case "esc", "c", "C":
				m.state = viewMain
				return m, nil
			}
		}
		return m, nil
	}

	// Intercept keys for workflow run approval confirmation
	if m.runApprovalState > 0 {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch m.runApprovalState {
			case 1: // confirm approval
				switch keyMsg.String() {
				case "y", "Y", "enter", "w", "W":
					m.runApprovalState = 0 // reset
					run := m.getRun()
					isForkPR := (run.HeadRepository.FullName != "" && run.HeadRepository.FullName != run.Repository.FullName)
					
					isWPress := (keyMsg.String() == "w" || keyMsg.String() == "W")
					isLocalPR := (run.Conclusion == "action_required" && !isForkPR)
					
					if isWPress || isLocalPR {
						_ = openBrowser(run.HTMLURL)
						m.statusMsg = "Opened approval page in browser."
						return m, nil
					}
					
					m.isLoading = true
					m.loadingMsg = "Approving workflow run..."
					return m, m.approveWorkflowRunCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID, run.Status, run.Conclusion)
				case "n", "N", "esc":
					m.runApprovalState = 0
					return m, nil
				}
			}
		}
		return m, nil
	}

	// Intercept keys for merge confirmation
	if m.state == viewPRDetails && m.mergeState > 0 {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch m.mergeState {
			case 1: // choose method
				switch keyMsg.String() {
				case "s", "S":
					m.mergeMethod = 0
					m.mergeState = 2
					return m, nil
				case "m", "M":
					m.mergeMethod = 1
					m.mergeState = 2
					return m, nil
				case "r", "R":
					m.mergeMethod = 2
					m.mergeState = 2
					return m, nil
				case "d", "D":
					defMethod := "squash"
					if m.config != nil && m.config.DefaultMergeMethod != "" {
						defMethod = strings.ToLower(m.config.DefaultMergeMethod)
					}
					var nextMethod string
					switch defMethod {
					case "squash":
						nextMethod = "merge"
					case "merge":
						nextMethod = "rebase"
					default:
						nextMethod = "squash"
					}
					if m.config != nil {
						m.config.DefaultMergeMethod = nextMethod
						_ = auth.SaveConfig(m.config)
					}
					return m, nil
				case "enter":
					defMethod := "squash"
					if m.config != nil && m.config.DefaultMergeMethod != "" {
						defMethod = strings.ToLower(m.config.DefaultMergeMethod)
					}
					switch defMethod {
					case "merge":
						m.mergeMethod = 1
					case "rebase":
						m.mergeMethod = 2
					default:
						m.mergeMethod = 0
					}
					m.mergeState = 2
					return m, nil
				case "esc", "c", "C":
					m.mergeState = 0
					return m, nil
				}
			case 2: // confirm
				switch keyMsg.String() {
				case "y", "Y":
					m.isLoading = true
					m.loadingMsg = "Merging pull request..."
					methodStr := "squash"
					if m.mergeMethod == 1 {
						methodStr = "merge"
					} else if m.mergeMethod == 2 {
						methodStr = "rebase"
					}
					owner := m.selectedPull.Repository.Owner.Login
					repo := m.selectedPull.Repository.Name
					num := m.selectedPull.Number
					m.mergeState = 0 // reset
					return m, m.mergePRCmd(owner, repo, num, "", "", methodStr)
				case "n", "N", "esc":
					m.mergeState = 0
					return m, nil
				}
			case 4: // confirm close
				switch keyMsg.String() {
				case "y", "Y":
					m.isLoading = true
					m.loadingMsg = "Closing pull request..."
					owner := m.selectedPull.Repository.Owner.Login
					repo := m.selectedPull.Repository.Name
					num := m.selectedPull.Number
					m.mergeState = 0 // reset
					return m, m.closePRCmd(owner, repo, num)
				case "n", "N", "esc":
					m.mergeState = 0
					return m, nil
				}
			}
		}
		return m, nil
	}

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
		case "?":
			if m.state != viewSplash {
				if m.state == viewHelp {
					// close help
					m.state = m.prevState
				} else {
					// open help
					m.prevState = m.state
					m.state = viewHelp
				}
				return m, nil
			}
		}

		// View specific keys
		switch m.state {
		case viewMain:
			switch msg.String() {
			case "tab":
				m.activeTab = (m.activeTab + 1) % 3
				m.selectedRunIdx = 0
				m.selectedPullIdx = 0
				m.selectedIssueIdx = 0
				return m, m.fetchActiveTabCmd()
			case "shift+tab":
				m.activeTab = (m.activeTab - 1 + 3) % 3
				m.selectedRunIdx = 0
				m.selectedPullIdx = 0
				m.selectedIssueIdx = 0
				return m, m.fetchActiveTabCmd()
			case "j", "down":
				if m.activeTab == tabWorkflows {
					maxIdx := len(m.runs)
					if !m.hasMoreRuns {
						maxIdx = len(m.runs) - 1
					}
					if m.selectedRunIdx < maxIdx {
						m.selectedRunIdx++
						m.scrollRuns()
					}
					return m, m.checkApprovalPermissionCmd()
				} else if m.activeTab == tabPRs {
					maxIdx := len(m.pulls)
					if !m.hasMorePulls {
						maxIdx = len(m.pulls) - 1
					}
					if m.selectedPullIdx < maxIdx {
						m.selectedPullIdx++
						m.scrollPulls()
					}
				} else if m.activeTab == tabIssues {
					maxIdx := len(m.issues)
					if !m.hasMoreIssues {
						maxIdx = len(m.issues) - 1
					}
					if m.selectedIssueIdx < maxIdx {
						m.selectedIssueIdx++
						m.scrollIssues()
					}
				}
			case "k", "up":
				if m.activeTab == tabWorkflows {
					if m.selectedRunIdx > 0 {
						m.selectedRunIdx--
						m.scrollRuns()
					}
					return m, m.checkApprovalPermissionCmd()
				} else if m.activeTab == tabPRs {
					if m.selectedPullIdx > 0 {
						m.selectedPullIdx--
						m.scrollPulls()
					}
				} else if m.activeTab == tabIssues {
					if m.selectedIssueIdx > 0 {
						m.selectedIssueIdx--
						m.scrollIssues()
					}
				}
			case "enter":
				if m.activeTab == tabWorkflows {
					// If we selected "Load More..."
					if m.hasMoreRuns && m.selectedRunIdx == len(m.runs) {
						m.runPage++
						m.isLoading = true
						m.statusMsg = "Loading more runs..."
						return m, m.fetchRunsCmd()
					}

					// Otherwise click into Workflow Run
					if len(m.runs) > 0 && m.selectedRunIdx < len(m.runs) {
						run := m.getRun()
						m.state = viewJobs
						m.isLoading = true
						m.loadingMsg = "Fetching jobs for " + run.Name
						m.selectedJobIdx = 0
						m.jobStartIndex = 0
						m.jobs = nil
						m.selectedAttempt = run.RunAttempt
						m.prevState = viewMain
						return m, m.fetchJobsCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID, m.selectedAttempt)
					}
				} else if m.activeTab == tabPRs {
					// Load more PRs
					if m.hasMorePulls && m.selectedPullIdx == len(m.pulls) {
						m.pullPage++
						m.isLoading = true
						m.statusMsg = "Loading more PRs..."
						return m, m.fetchPullsCmd()
					}

					// Go into PR details
					if len(m.pulls) > 0 && m.selectedPullIdx < len(m.pulls) {
						pr := m.pulls[m.selectedPullIdx]
						m.selectedPull = &pr
						m.state = viewPRDetails
						m.isLoading = true
						m.loadingMsg = fmt.Sprintf("Fetching details for PR #%d", pr.Number)
						m.activePRTab = prTabInfo
						m.selectedFileIdx = 0
						m.selectedCommitIdx = 0
						m.selectedCheckIdx = 0
						m.prDescViewport.SetContent("Loading description...")
						m.prChecks = nil
						m.runs = nil
						return m, m.fetchPRDetailsCmd(pr.Repository.Owner.Login, pr.Repository.Name, pr.Number, pr.Head.SHA, pr.Head.Ref)
					}
				} else if m.activeTab == tabIssues {
					// Load more Issues
					if m.hasMoreIssues && m.selectedIssueIdx == len(m.issues) {
						m.issuePage++
						m.isLoading = true
						m.statusMsg = "Loading more Issues..."
						return m, m.fetchIssuesCmd()
					}

					// Go into Issue details
					if len(m.issues) > 0 && m.selectedIssueIdx < len(m.issues) {
						issue := m.issues[m.selectedIssueIdx]
						m.selectedIssue = &issue
						m.state = viewIssueDetails
						m.isLoading = true
						m.loadingMsg = fmt.Sprintf("Fetching details for Issue #%d", issue.Number)
						m.issueDescFocused = true
						return m, m.fetchIssueDetailsCmd(issue.Repository.Owner.Login, issue.Repository.Name, issue.Number)
					}
				}
			case "r", "ctrl+r":
				m.isLoading = true
				if m.activeTab == tabWorkflows {
					m.loadingMsg = "Refreshing workflow runs"
					m.runPage = 1
					m.hasMoreRuns = true
					m.selectedRunIdx = 0
					m.runStartIndex = 0
					m.runs = nil
					return m, m.fetchRunsCmd()
				} else if m.activeTab == tabPRs {
					m.loadingMsg = "Refreshing pull requests"
					m.pullPage = 1
					m.hasMorePulls = true
					m.selectedPullIdx = 0
					m.pullStartIndex = 0
					m.pulls = nil
					return m, m.fetchPullsCmd()
				} else if m.activeTab == tabIssues {
					m.loadingMsg = "Refreshing issues"
					m.issuePage = 1
					m.hasMoreIssues = true
					m.selectedIssueIdx = 0
					m.issueStartIndex = 0
					m.issues = nil
					return m, m.fetchIssuesCmd()
				}
			case "m":
				if m.activeTab == tabWorkflows {
					if m.filterActor == m.currentUser && m.currentUser != "" {
						m.filterActor = ""
					} else {
						m.filterActor = m.currentUser
					}
					m.isLoading = true
					m.loadingMsg = "Filtering runs"
					m.runPage = 1
					m.hasMoreRuns = true
					m.selectedRunIdx = 0
					m.runStartIndex = 0
					m.runs = nil
					return m, m.fetchRunsCmd()
				}
			case "a":
				if m.activeTab == tabWorkflows {
					if m.selectedRunCanApprove() {
						m.runApprovalState = 1
						return m, nil
					}
				} else if m.activeTab == tabPRs {
					m.filterPRAuthor = m.currentUser
					m.filterPRAssignee = ""
					m.filterPRReviewer = ""
					m.isLoading = true
					m.loadingMsg = "Filtering PRs by author..."
					m.pullPage = 1
					m.hasMorePulls = true
					m.selectedPullIdx = 0
					m.pullStartIndex = 0
					m.pulls = nil
					return m, m.fetchPullsCmd()
				} else if m.activeTab == tabIssues {
					m.filterIssueAuthor = m.currentUser
					m.filterIssueAssignee = ""
					m.isLoading = true
					m.loadingMsg = "Filtering issues by author..."
					m.issuePage = 1
					m.hasMoreIssues = true
					m.selectedIssueIdx = 0
					m.issueStartIndex = 0
					m.issues = nil
					return m, m.fetchIssuesCmd()
				}
			case "i":
				if m.activeTab == tabPRs {
					m.filterPRAuthor = ""
					m.filterPRAssignee = m.currentUser
					m.filterPRReviewer = ""
					m.isLoading = true
					m.loadingMsg = "Filtering PRs by assignee..."
					m.pullPage = 1
					m.hasMorePulls = true
					m.selectedPullIdx = 0
					m.pullStartIndex = 0
					m.pulls = nil
					return m, m.fetchPullsCmd()
				} else if m.activeTab == tabIssues {
					m.filterIssueAuthor = ""
					m.filterIssueAssignee = m.currentUser
					m.isLoading = true
					m.loadingMsg = "Filtering issues by assignee..."
					m.issuePage = 1
					m.hasMoreIssues = true
					m.selectedIssueIdx = 0
					m.issueStartIndex = 0
					m.issues = nil
					return m, m.fetchIssuesCmd()
				}
			case "v":
				if m.activeTab == tabPRs {
					m.filterPRAuthor = ""
					m.filterPRAssignee = ""
					m.filterPRReviewer = m.currentUser
					m.isLoading = true
					m.loadingMsg = "Filtering PRs by reviewer..."
					m.pullPage = 1
					m.hasMorePulls = true
					m.selectedPullIdx = 0
					m.pullStartIndex = 0
					m.pulls = nil
					return m, m.fetchPullsCmd()
				}
			case "x":
				if m.activeTab == tabPRs {
					m.filterPRAuthor = ""
					m.filterPRAssignee = ""
					m.filterPRReviewer = ""
					m.filterPRState = "open"
					m.filterRepo = ""
					m.isLoading = true
					m.loadingMsg = "Clearing PR filters..."
					m.pullPage = 1
					m.hasMorePulls = true
					m.selectedPullIdx = 0
					m.pullStartIndex = 0
					m.pulls = nil
					return m, m.fetchPullsCmd()
				} else if m.activeTab == tabWorkflows {
					m.filterActor = ""
					m.filterRepo = ""
					m.isLoading = true
					m.loadingMsg = "Clearing workflow filters..."
					m.runPage = 1
					m.hasMoreRuns = true
					m.selectedRunIdx = 0
					m.runStartIndex = 0
					m.runs = nil
					return m, m.fetchRunsCmd()
				} else if m.activeTab == tabIssues {
					m.filterIssueAuthor = ""
					m.filterIssueAssignee = ""
					m.filterIssueState = "open"
					m.filterRepo = ""
					m.isLoading = true
					m.loadingMsg = "Clearing issue filters..."
					m.issuePage = 1
					m.hasMoreIssues = true
					m.selectedIssueIdx = 0
					m.issueStartIndex = 0
					m.issues = nil
					return m, m.fetchIssuesCmd()
				}
			case "s":
				if m.activeTab == tabPRs {
					if m.filterPRState == "open" {
						m.filterPRState = "closed"
					} else if m.filterPRState == "closed" {
						m.filterPRState = "all"
					} else {
						m.filterPRState = "open"
					}
					m.isLoading = true
					m.loadingMsg = "Toggling PR state filter..."
					m.pullPage = 1
					m.hasMorePulls = true
					m.selectedPullIdx = 0
					m.pullStartIndex = 0
					m.pulls = nil
					return m, m.fetchPullsCmd()
				} else if m.activeTab == tabIssues {
					if m.filterIssueState == "open" {
						m.filterIssueState = "closed"
					} else if m.filterIssueState == "closed" {
						m.filterIssueState = "all"
					} else {
						m.filterIssueState = "open"
					}
					m.isLoading = true
					m.loadingMsg = "Toggling issue state filter..."
					m.issuePage = 1
					m.hasMoreIssues = true
					m.selectedIssueIdx = 0
					m.issueStartIndex = 0
					m.issues = nil
					return m, m.fetchIssuesCmd()
				}
			case "f":
				m.state = viewFilterTypeSelect
				return m, nil
			case "w":
				if m.activeTab == tabWorkflows && len(m.runs) > 0 && m.selectedRunIdx < len(m.runs) {
					run := m.getRun()
					if run.HTMLURL != "" {
						_ = openBrowser(run.HTMLURL)
					}
				} else if m.activeTab == tabPRs && len(m.pulls) > 0 && m.selectedPullIdx < len(m.pulls) {
					pr := m.pulls[m.selectedPullIdx]
					if pr.HTMLURL != "" {
						_ = openBrowser(pr.HTMLURL)
					}
				} else if m.activeTab == tabIssues && len(m.issues) > 0 && m.selectedIssueIdx < len(m.issues) {
					issue := m.issues[m.selectedIssueIdx]
					if issue.HTMLURL != "" {
						_ = openBrowser(issue.HTMLURL)
					}
				}
			}

		case viewSplash:
			switch msg.String() {
			case "esc", "backspace":
				if m.cancel != nil {
					m.cancel()
				}
				m.ctx, m.cancel = context.WithCancel(context.Background())
				if m.prevState != viewSplash && m.prevState != 0 {
					m.state = m.prevState
				} else {
					m.state = viewMain
				}
				m.isLoading = false
				return m, nil
			}

		case viewPRDetails:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewMain
				m.activeTab = tabPRs
			case "tab", "shift+tab":
				m.prDescFocused = !m.prDescFocused
			case "j", "down":
				if m.prDescFocused {
					m.prDescViewport.ScrollDown(1)
				} else {
					if len(m.prChecks) > 0 && m.selectedCheckIdx < len(m.prChecks)-1 {
						m.selectedCheckIdx++
					}
				}
			case "k", "up":
				if m.prDescFocused {
					m.prDescViewport.ScrollUp(1)
				} else {
					if m.selectedCheckIdx > 0 {
						m.selectedCheckIdx--
					}
				}
			case "u":
				if m.prDescFocused {
					m.prDescViewport.ScrollUp(6)
				}
			case "d":
				if m.prDescFocused {
					m.prDescViewport.ScrollDown(6)
				}
			case "enter":
				if !m.prDescFocused && len(m.prChecks) > 0 && m.selectedCheckIdx < len(m.prChecks) {
					check := m.prChecks[m.selectedCheckIdx]
					isActions := false
					if check.App != nil && (check.App.Slug == "github-actions" || strings.Contains(strings.ToLower(check.App.Name), "github actions") || strings.Contains(strings.ToLower(check.App.Slug), "action")) {
						isActions = true
					}
					runID := int64(0)
					if isActions && check.HTMLURL != "" {
						runID = extractRunIDFromURL(check.HTMLURL)
					}

					matchedRunIdx := -1
					if runID > 0 {
						for idx, r := range m.runs {
							if r.ID == runID {
								matchedRunIdx = idx
								break
							}
						}
					}
					if matchedRunIdx == -1 {
						for idx, r := range m.runs {
							if r.Name == check.Name || strings.Contains(strings.ToLower(check.Name), strings.ToLower(r.Name)) || strings.Contains(strings.ToLower(r.Name), strings.ToLower(check.Name)) {
								matchedRunIdx = idx
								break
							}
						}
					}

					if matchedRunIdx != -1 {
						matchedRun := &m.runs[matchedRunIdx]
						m.selectedRunIdx = matchedRunIdx
						m.targetJobName = check.Name
						m.state = viewJobs
						m.isLoading = true
						m.loadingMsg = "Fetching jobs for check workflow " + matchedRun.Name
						m.selectedJobIdx = 0
						m.jobStartIndex = 0
						m.jobs = nil
						m.selectedAttempt = matchedRun.RunAttempt
						m.prevState = viewPRDetails
						return m, m.fetchJobsCmd(matchedRun.Repository.Owner.Login, matchedRun.Repository.Name, matchedRun.ID, m.selectedAttempt)
					} else if runID > 0 {
						owner := m.selectedPull.Repository.Owner.Login
						repo := m.selectedPull.Repository.Name
						m.selectedRunIdx = -1
						m.targetJobName = check.Name
						m.state = viewJobs
						m.isLoading = true
						m.loadingMsg = "Fetching jobs for check run " + check.Name
						m.selectedJobIdx = 0
						m.jobStartIndex = 0
						m.jobs = nil
						m.selectedAttempt = 0
						m.prevState = viewPRDetails
						return m, m.fetchJobsCmd(owner, repo, runID, 0)
					} else {
						if check.HTMLURL != "" {
							_ = openBrowser(check.HTMLURL)
						}
					}
				}
			case "w":
				if m.prDescFocused {
					if m.selectedPull != nil && m.selectedPull.HTMLURL != "" {
						_ = openBrowser(m.selectedPull.HTMLURL)
					}
				} else if len(m.prChecks) > 0 && m.selectedCheckIdx < len(m.prChecks) {
					check := m.prChecks[m.selectedCheckIdx]
					if check.HTMLURL != "" {
						_ = openBrowser(check.HTMLURL)
					}
				}
			case "m":
				if m.viewerCanMerge() {
					m.mergeState = 1
				} else {
					m.statusMsg = "You don't have write scopes (repo) to merge this PR."
				}
			case "c":
				if m.selectedPull != nil {
					m.state = viewPRComments
					m.isLoading = true
					m.commentsViewport.SetContent("Loading comments...")
					owner := m.selectedPull.Repository.Owner.Login
					repo := m.selectedPull.Repository.Name
					num := m.selectedPull.Number
					return m, m.fetchPRCommentsCmd(owner, repo, num)
				}
			case "C":
				if m.viewerCanMerge() {
					m.mergeState = 4
				} else {
					m.statusMsg = "You don't have write scopes (repo) to close this PR."
				}
			case "v":
				if m.selectedPull != nil {
					m.state = viewPRCommits
					m.selectedCommitIdx = 0
				}
			case "D":
				if m.selectedPull != nil {
					m.state = viewPRDiff
					m.selectedFileIdx = 0
					m.prFileStartIndex = 0
					m.updateDiffViewport()
				}
			case "r", "ctrl+r":
				if m.selectedPull != nil {
					pr := m.selectedPull
					m.isLoading = true
					m.loadingMsg = fmt.Sprintf("Refreshing details for PR #%d", pr.Number)
					return m, m.fetchPRDetailsCmd(pr.Repository.Owner.Login, pr.Repository.Name, pr.Number, pr.Head.SHA, pr.Head.Ref)
				}
			}

		case viewPRCommits:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewPRDetails
			case "j", "down":
				if len(m.prCommits) > 0 && m.selectedCommitIdx < len(m.prCommits)-1 {
					m.selectedCommitIdx++
				}
			case "k", "up":
				if m.selectedCommitIdx > 0 {
					m.selectedCommitIdx--
				}
			case "enter":
				if len(m.prCommits) > 0 && m.selectedCommitIdx < len(m.prCommits) {
					c := m.prCommits[m.selectedCommitIdx]
					m.isLoading = true
					shaText := c.SHA
					if len(shaText) > 7 {
						shaText = shaText[:7]
					}
					m.loadingMsg = "Fetching commit details for " + shaText
					owner := m.selectedPull.Repository.Owner.Login
					repo := m.selectedPull.Repository.Name
					return m, m.fetchCommitDetailsCmd(owner, repo, c.SHA)
				}
			case "q":
				return m, tea.Quit
			}

		case viewCommitDetails:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewPRCommits
			case "j", "down":
				if m.selectedCommitFileIdx < len(m.commitFiles)-1 {
					m.selectedCommitFileIdx++
					m.scrollCommitFiles()
					m.updateCommitDiffViewport()
				}
			case "k", "up":
				if m.selectedCommitFileIdx > 0 {
					m.selectedCommitFileIdx--
					m.scrollCommitFiles()
					m.updateCommitDiffViewport()
				}
			case "u":
				m.commitDiffViewport.ScrollUp(3)
			case "d":
				m.commitDiffViewport.ScrollDown(3)
			case "w":
				if m.viewingCommit != nil && m.viewingCommit.HTMLURL != "" {
					_ = openBrowser(m.viewingCommit.HTMLURL)
				}
			}

		case viewPRDiff:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewPRDetails
			case "j", "down":
				if m.selectedFileIdx < len(m.prFiles)-1 {
					m.selectedFileIdx++
					m.scrollPRFiles()
					m.updateDiffViewport()
				}
			case "k", "up":
				if m.selectedFileIdx > 0 {
					m.selectedFileIdx--
					m.scrollPRFiles()
					m.updateDiffViewport()
				}
			case "u":
				m.diffViewport.ScrollUp(3)
			case "d":
				m.diffViewport.ScrollDown(3)
			case "w":
				if m.selectedPull != nil && m.selectedPull.HTMLURL != "" {
					_ = openBrowser(m.selectedPull.HTMLURL + "/files")
				}
			}

		case viewPRComments:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewPRDetails
			case "r", "ctrl+r":
				if m.selectedPull != nil {
					m.isLoading = true
					m.commentsViewport.SetContent("Refreshing comments...")
					owner := m.selectedPull.Repository.Owner.Login
					repo := m.selectedPull.Repository.Name
					num := m.selectedPull.Number
					return m, m.fetchPRCommentsCmd(owner, repo, num)
				}
			default:
				m.commentsViewport, cmd = m.commentsViewport.Update(msg)
				cmds = append(cmds, cmd)
			}

		case viewIssueDetails:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewMain
				m.activeTab = tabIssues
			case "tab", "shift+tab":
				m.issueDescFocused = !m.issueDescFocused
			case "j", "down":
				if m.issueDescFocused {
					m.issueDescViewport.ScrollDown(1)
				}
			case "k", "up":
				if m.issueDescFocused {
					m.issueDescViewport.ScrollUp(1)
				}
			case "u":
				if m.issueDescFocused {
					m.issueDescViewport.ScrollUp(6)
				}
			case "d":
				if m.issueDescFocused {
					m.issueDescViewport.ScrollDown(6)
				}
			case "w":
				if m.selectedIssue != nil && m.selectedIssue.HTMLURL != "" {
					_ = openBrowser(m.selectedIssue.HTMLURL)
				}
			case "c":
				if m.selectedIssue != nil {
					m.state = viewIssueComments
					m.isLoading = true
					m.commentsViewport.SetContent("Loading comments...")
					owner := m.selectedIssue.Repository.Owner.Login
					repo := m.selectedIssue.Repository.Name
					num := m.selectedIssue.Number
					return m, m.fetchIssueCommentsCmd(owner, repo, num)
				}
			case "r", "ctrl+r":
				if m.selectedIssue != nil {
					issue := m.selectedIssue
					m.isLoading = true
					m.loadingMsg = fmt.Sprintf("Refreshing details for Issue #%d", issue.Number)
					return m, m.fetchIssueDetailsCmd(issue.Repository.Owner.Login, issue.Repository.Name, issue.Number)
				}
			}

		case viewIssueComments:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewIssueDetails
			case "r", "ctrl+r":
				if m.selectedIssue != nil {
					m.isLoading = true
					m.commentsViewport.SetContent("Refreshing comments...")
					owner := m.selectedIssue.Repository.Owner.Login
					repo := m.selectedIssue.Repository.Name
					num := m.selectedIssue.Number
					return m, m.fetchIssueCommentsCmd(owner, repo, num)
				}
			default:
				m.commentsViewport, cmd = m.commentsViewport.Update(msg)
				cmds = append(cmds, cmd)
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
					if job.Status == "in_progress" || job.Status == "queued" {
						m.statusMsg = "Logs are not yet available for running jobs. Please wait for completion."
						return m, nil
					}
					run := m.getRun()
					m.state = viewLogs
					m.loadingMsg = "Fetching logs for " + job.Name
					m.logs = ""
					m.logsLoading = true
					return m, m.fetchLogsCmd(run.Repository.Owner.Login, run.Repository.Name, job.ID)
				}
			case "esc", "backspace":
				if m.prevState == viewPRDetails {
					m.state = viewPRDetails
					m.prevState = viewMain
				} else {
					m.state = viewMain
				}
			case "r", "ctrl+r":
				run := m.getRun()
				m.isLoading = true
				m.loadingMsg = "Refreshing jobs"
				m.jobs = nil
				m.selectedJobIdx = 0
				m.jobStartIndex = 0
				return m, m.fetchJobsCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID, m.selectedAttempt)
			case "[":
				run := m.getRun()
				if m.selectedAttempt > 1 {
					m.selectedAttempt--
					m.isLoading = true
					m.loadingMsg = fmt.Sprintf("Fetching jobs for %s (Attempt %d)", run.Name, m.selectedAttempt)
					m.selectedJobIdx = 0
					m.jobStartIndex = 0
					m.jobs = nil
					return m, m.fetchJobsCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID, m.selectedAttempt)
				}
			case "]":
				run := m.getRun()
				if m.selectedAttempt < run.RunAttempt {
					m.selectedAttempt++
					m.isLoading = true
					m.loadingMsg = fmt.Sprintf("Fetching jobs for %s (Attempt %d)", run.Name, m.selectedAttempt)
					m.selectedJobIdx = 0
					m.jobStartIndex = 0
					m.jobs = nil
					return m, m.fetchJobsCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID, m.selectedAttempt)
				}
			case "a":
				if m.selectedRunCanApprove() {
					m.runApprovalState = 1
					return m, nil
				}
			case "w":
				run := m.getRun()
				if run.HTMLURL != "" {
					_ = openBrowser(run.HTMLURL)
				}
			case "v":
				if len(m.jobs) > 0 && m.selectedJobIdx < len(m.jobs) {
					job := m.jobs[m.selectedJobIdx]
					if job.HTMLURL != "" {
						_ = openBrowser(job.HTMLURL)
					}
				}
			}

		case viewLogs:
			switch msg.String() {
			case "esc", "backspace":
				m.state = viewJobs
			case "r", "ctrl+r":
				job := m.jobs[m.selectedJobIdx]
				if job.Status == "in_progress" || job.Status == "queued" {
					m.statusMsg = "Logs are not yet available for running jobs. Please wait for completion."
					m.logsLoading = false
					return m, nil
				}
				run := m.getRun()
				m.logs = ""
				m.logsLoading = true
				m.loadingMsg = "Refreshing logs"
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
				m.state = viewMain
				m.isLoading = true
				targetName := m.targets[m.selectedTargetIdx].Name
				m.loadingMsg = "Loading data for " + targetName
				
				m.runs = nil
				m.runPage = 1
				m.hasMoreRuns = true
				m.selectedRunIdx = 0
				m.runStartIndex = 0
				
				m.pulls = nil
				m.pullPage = 1
				m.hasMorePulls = true
				m.selectedPullIdx = 0
				m.pullStartIndex = 0
				
				m.dashboardPRsCount = 0
				m.dashboardWorkflowsCount = 0
				
				m.repos = nil
				m.filterRepo = ""
				m.selectedRepoIdx = 0
				m.repoStartIndex = 0
				
				return m, m.fetchActiveTabCmd()
			case "esc":
				m.state = m.prevState
			}

		case viewHelp:
			switch msg.String() {
			case "esc", "?":
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
		
		m.diffViewport.Width = msg.Width - 44
		m.diffViewport.Height = msg.Height - 16
		if m.diffViewport.Height < 5 {
			m.diffViewport.Height = 5
		}
		
		m.commentsViewport.Width = msg.Width - 6
		m.commentsViewport.Height = msg.Height - 10
		if m.commentsViewport.Height < 5 {
			m.commentsViewport.Height = 5
		}
		
		m.commitDiffViewport.Width = msg.Width - 44
		m.commitDiffViewport.Height = msg.Height - 16
		if m.commitDiffViewport.Height < 5 {
			m.commitDiffViewport.Height = 5
		}

		sidebarWidth := msg.Width / 5
		if sidebarWidth < 40 {
			sidebarWidth = 40
		}
		m.prDescViewport.Width = msg.Width - sidebarWidth - 4
		if m.prDescViewport.Width < 20 {
			m.prDescViewport.Width = 20
		}
		m.prDescViewport.Height = msg.Height - 10
		if m.prDescViewport.Height < 5 {
			m.prDescViewport.Height = 5
		}
		if m.selectedPull != nil && m.selectedPull.Body != "" {
			if md, err := renderMarkdown(m.selectedPull.Body, m.prDescViewport.Width); err == nil {
				m.prDescViewport.SetContent(md)
			}
		}

		m.issueDescViewport.Width = msg.Width - sidebarWidth - 4
		if m.issueDescViewport.Width < 20 {
			m.issueDescViewport.Width = 20
		}
		m.issueDescViewport.Height = msg.Height - 10
		if m.issueDescViewport.Height < 5 {
			m.issueDescViewport.Height = 5
		}
		if m.selectedIssue != nil && m.selectedIssue.Body != "" {
			if md, err := renderMarkdown(m.selectedIssue.Body, m.issueDescViewport.Width); err == nil {
				m.issueDescViewport.SetContent(md)
			}
		}

	case pollMsg:
		var pollCmd tea.Cmd
		if m.state == viewMain && m.activeTab == tabWorkflows {
			pollCmd = m.pollRunsCmd()
		} else if m.state == viewMain && m.activeTab == tabPRs {
			pollCmd = m.fetchPullsCmd()
		} else if m.state == viewMain && m.activeTab == tabIssues {
			pollCmd = m.fetchIssuesCmd()
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

		m.currentUser = msg.user.Login

		m.targets = append(m.targets, Target{Name: msg.user.Login, IsOrg: false})
		for _, org := range msg.orgs {
			m.targets = append(m.targets, Target{Name: org.Login, IsOrg: true})
		}

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

		m.loadingMsg = "Loading dashboard for " + m.targets[m.selectedTargetIdx].Name
		return m, m.fetchActiveTabCmd()

	case dashboardStatsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading dashboard: " + msg.err.Error()
		} else {
			m.dashboardPRsCount = msg.prsCount
			m.dashboardWorkflowsCount = msg.workflowsCount
			m.statusMsg = "Dashboard stats updated"
		}
		m.state = viewMain

	case pullsLoadedMsg:
		if (m.state != viewMain && m.state != viewSplash) || m.activeTab != tabPRs {
			return m, nil
		}
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading PRs: " + msg.err.Error()
			m.state = viewMain
			return m, nil
		}
		
		if m.pullPage == 1 {
			m.pulls = msg.pulls
		} else {
			m.pulls = append(m.pulls, msg.pulls...)
		}
		
		if len(m.repos) == 0 && len(msg.repos) > 0 {
			m.repos = msg.repos
		}

		if len(msg.pulls) == 0 {
			m.hasMorePulls = false
		}
		m.scrollPulls()
		m.state = viewMain
		m.statusMsg = "Successfully loaded Pull Requests"

	case issuesLoadedMsg:
		if (m.state != viewMain && m.state != viewSplash) || m.activeTab != tabIssues {
			return m, nil
		}
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading issues: " + msg.err.Error()
			m.state = viewMain
			return m, nil
		}

		if m.issuePage == 1 {
			m.issues = msg.issues
		} else {
			m.issues = append(m.issues, msg.issues...)
		}

		if len(m.repos) == 0 && len(msg.repos) > 0 {
			m.repos = msg.repos
		}

		m.hasMoreIssues = msg.hasMore
		m.scrollIssues()
		m.state = viewMain
		m.statusMsg = "Successfully loaded Issues"


	case prDetailsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading PR details: " + msg.err.Error()
			m.state = viewMain
			return m, nil
		}
		
		m.selectedPull = msg.pull
		m.prCommits = msg.commits
		m.prFiles = msg.files
		m.prChecks = msg.checkRuns
		m.prCommitChecks = msg.commitChecks
		m.prComments = msg.comments
		m.runs = msg.actionsRuns
		
		m.activePRTab = prTabInfo
		m.selectedFileIdx = 0
		m.prFileStartIndex = 0
		m.updateDiffViewport()
		m.selectedCommitIdx = 0
		m.selectedCheckIdx = 0
		m.prDescFocused = true
		
		sidebarWidth := m.width / 5
		if sidebarWidth < 40 {
			sidebarWidth = 40
		}
		w := m.width - sidebarWidth - 4
		if w < 20 {
			w = 20
		}
		h := m.height - 10
		if h < 5 {
			h = 15
		}
		m.prDescViewport = viewport.New(w, h)
		m.prDescViewport.SetContent(msg.renderedBody)
		m.prDescViewport.YOffset = 0
		
		m.state = viewPRDetails
		m.statusMsg = ""

	case issueDetailsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading issue details: " + msg.err.Error()
			m.state = viewMain
			return m, nil
		}

		m.selectedIssue = msg.issue
		m.issueComments = msg.comments

		m.issueDescFocused = true

		sidebarWidth := m.width / 5
		if sidebarWidth < 40 {
			sidebarWidth = 40
		}
		w := m.width - sidebarWidth - 4
		if w < 20 {
			w = 20
		}
		h := m.height - 10
		if h < 5 {
			h = 15
		}
		m.issueDescViewport = viewport.New(w, h)
		m.issueDescViewport.SetContent(msg.renderedBody)
		m.issueDescViewport.YOffset = 0

		m.state = viewIssueDetails
		m.statusMsg = ""


	case commitDetailsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading commit details: " + msg.err.Error()
			m.state = viewPRDetails
			return m, nil
		}
		m.viewingCommit = msg.commit
		m.commitFiles = msg.files
		m.selectedCommitFileIdx = 0
		m.commitFileStartIndex = 0
		m.updateCommitDiffViewport()
		m.state = viewCommitDetails

	case prCommentsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading comments: " + msg.err.Error()
			m.state = viewPRDetails
			return m, nil
		}
		m.prComments = msg.comments
		
		// Sort comments by CreatedAt ascending (latest at bottom)
		sort.Slice(m.prComments, func(i, j int) bool {
			return m.prComments[i].CreatedAt.Before(m.prComments[j].CreatedAt)
		})
		
		// Format comments and set viewport content
		var sb strings.Builder
		for idx, c := range m.prComments {
			author := "unknown"
			if c.User != nil {
				author = c.User.Login
			}
			dateStr := c.CreatedAt.Format("2006-01-02 15:04:05")
			sb.WriteString(m.theme.LogoText.Render(fmt.Sprintf("@%s", author)) + " " + m.theme.Subtitle.Render("commented at "+dateStr) + "\n")
			
			body := c.Body
			if md, err := renderMarkdown(body, m.commentsViewport.Width-4); err == nil {
				sb.WriteString(md)
			} else {
				sb.WriteString(body + "\n")
			}
			if idx < len(m.prComments)-1 {
				sb.WriteString(m.theme.Border.Render(strings.Repeat("─", m.commentsViewport.Width-2)) + "\n\n")
			}
		}
		
		if len(m.prComments) == 0 {
			sb.WriteString("No comments found for this Pull Request.")
		}
		
		m.commentsViewport.SetContent(sb.String())
		m.commentsViewport.GotoBottom()
		m.state = viewPRComments
		m.statusMsg = ""

	case issueCommentsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading comments: " + msg.err.Error()
			m.state = viewIssueDetails
			return m, nil
		}
		m.issueComments = msg.comments

		sort.Slice(m.issueComments, func(i, j int) bool {
			return m.issueComments[i].CreatedAt.Before(m.issueComments[j].CreatedAt)
		})

		var sb strings.Builder
		for idx, c := range m.issueComments {
			author := "unknown"
			if c.User != nil {
				author = c.User.Login
			}
			dateStr := c.CreatedAt.Format("2006-01-02 15:04:05")
			sb.WriteString(m.theme.LogoText.Render(fmt.Sprintf("@%s", author)) + " " + m.theme.Subtitle.Render("commented at "+dateStr) + "\n")

			body := c.Body
			if md, err := renderMarkdown(body, m.commentsViewport.Width-4); err == nil {
				sb.WriteString(md)
			} else {
				sb.WriteString(body + "\n")
			}
			if idx < len(m.issueComments)-1 {
				sb.WriteString(m.theme.Border.Render(strings.Repeat("─", m.commentsViewport.Width-2)) + "\n\n")
			}
		}

		if len(m.issueComments) == 0 {
			sb.WriteString("No comments found for this Issue.")
		}

		m.commentsViewport.SetContent(sb.String())
		m.commentsViewport.GotoBottom()
		m.state = viewIssueComments
		m.statusMsg = ""

	case approvalPermissionLoadedMsg:
		if msg.err != nil {
			m.approvalPermissions[msg.runID] = false
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		m.approvalPermissions[msg.runID] = msg.canApprove
		return m, nil

	case workflowRunApprovedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "error: approval failed: " + msg.err.Error()
			return m, nil
		}
		
		m.statusMsg = "Workflow run successfully approved!"
		delete(m.approvalPermissions, msg.runID)
		
		if m.state == viewMain {
			m.isLoading = true
			m.loadingMsg = "Refreshing workflow runs"
			m.runPage = 1
			m.hasMoreRuns = true
			m.selectedRunIdx = 0
			m.runStartIndex = 0
			m.runs = nil
			return m, m.fetchRunsCmd()
		} else if m.state == viewJobs {
			run := m.getRun()
			m.isLoading = true
			m.loadingMsg = "Refreshing jobs"
			m.jobs = nil
			m.selectedJobIdx = 0
			m.jobStartIndex = 0
			return m, m.fetchJobsCmd(run.Repository.Owner.Login, run.Repository.Name, run.ID, m.selectedAttempt)
		}
		return m, nil

	case prMergedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Merge failed: " + msg.err.Error()
			m.state = viewPRDetails
			return m, nil
		}
		
		m.statusMsg = "PR successfully merged!"
		m.state = viewMain
		m.isLoading = true
		m.loadingMsg = "Refreshing pull requests"
		m.pullPage = 1
		m.hasMorePulls = true
		m.selectedPullIdx = 0
		m.pullStartIndex = 0
		m.pulls = nil
		return m, m.fetchPullsCmd()

	case prClosedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Close failed: " + msg.err.Error()
			m.state = viewPRDetails
			return m, nil
		}
		
		m.statusMsg = "PR successfully closed!"
		m.state = viewMain
		m.isLoading = true
		m.loadingMsg = "Refreshing pull requests"
		m.pullPage = 1
		m.hasMorePulls = true
		m.selectedPullIdx = 0
		m.pullStartIndex = 0
		m.pulls = nil
		return m, m.fetchPullsCmd()

	case runsLoadedMsg:
		if (m.state != viewMain && m.state != viewSplash) || m.activeTab != tabWorkflows {
			return m, nil
		}
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading runs: " + msg.err.Error()
			m.state = viewMain
			return m, nil
		}

		filtered := msg.runs
		if m.filterActor != "" {
			filtered = nil
			for _, r := range msg.runs {
				if r.Actor != nil && matchActor(r.Actor.Login, m.filterActor) {
					filtered = append(filtered, r)
				}
			}
		}

		if m.runPage == 1 {
			m.runs = filtered
		} else {
			m.runs = append(m.runs, filtered...)
		}

		if len(m.repos) == 0 && len(msg.repos) > 0 {
			m.repos = msg.repos
		}

		sortRuns(m.runs)

		if len(msg.runs) == 0 {
			m.hasMoreRuns = false
		}

		m.scrollRuns()
		m.state = viewMain
		m.statusMsg = "Successfully loaded runs"
		return m, m.checkApprovalPermissionCmd()

	case runsPolledMsg:
		if m.state != viewMain || m.activeTab != tabWorkflows {
			return m, nil
		}
		if msg.err == nil && len(msg.runs) > 0 {
			filtered := msg.runs
			if m.filterActor != "" {
				filtered = nil
				for _, r := range msg.runs {
					if r.Actor != nil && matchActor(r.Actor.Login, m.filterActor) {
						filtered = append(filtered, r)
					}
				}
			}
			m.runs = mergeRuns(m.runs, filtered)
			m.scrollRuns()
		}
		return m, m.checkApprovalPermissionCmd()

	case jobsLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.statusMsg = "Error loading jobs: " + msg.err.Error()
			if m.prevState == viewPRDetails {
				m.state = viewPRDetails
			} else {
				m.state = viewMain
			}
			return m, nil
		}
		if msg.run != nil {
			m.viewingRun = msg.run
		} else if m.selectedRunIdx >= 0 {
			m.viewingRun = nil
		}
		m.jobs = msg.jobs
		sortJobs(m.jobs)
		
		m.selectedJobIdx = 0
		if m.targetJobName != "" {
			for idx, job := range m.jobs {
				if strings.EqualFold(job.Name, m.targetJobName) {
					m.selectedJobIdx = idx
					break
				}
			}
			m.targetJobName = ""
		}
		
		m.jobStartIndex = 0
		m.scrollJobs()
		m.state = viewJobs
		m.statusMsg = ""
		return m, m.checkApprovalPermissionCmd()

	case logsLoadedMsg:
		m.logsLoading = false
		if msg.err != nil {
			statusMsg := "Error loading logs: " + msg.err.Error()
			errStr := msg.err.Error()
			if strings.Contains(errStr, "404") || strings.Contains(errStr, "BlobNotFound") || strings.Contains(errStr, "The specified blob does not exist") {
				statusMsg = "Logs are not yet available for running jobs. Please wait for the job to complete."
			}
			m.statusMsg = statusMsg
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

		// 1. Resolve repositories list (use cached m.repos or fetch as fallback)
		var repos []gh.Repository
		if len(m.repos) > 0 {
			repos = m.repos
		} else {
			var err error
			if target.IsOrg {
				repos, err = m.client.GetRepos(m.ctx, "org", target.Name, 1, 100)
			} else {
				repos, err = m.client.GetRepos(m.ctx, "user", target.Name, 1, 100)
			}
			if err != nil {
				return runsLoadedMsg{err: err}
			}
		}

		// Apply repository filter if set
		if m.filterRepo != "" {
			var filtered []gh.Repository
			for _, r := range repos {
				if r.Name == m.filterRepo {
					filtered = append(filtered, r)
					break
				}
			}
			repos = filtered
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
				runs, err := m.client.GetWorkflowRuns(m.ctx, r.Owner.Login, r.Name, m.runPage, 8, m.filterActor)
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

		return runsLoadedMsg{runs: allRuns, repos: repos}
	}
}

// fetchJobsCmd fetches jobs inside a workflow run, optionally for a specific attempt.
func (m Model) fetchJobsCmd(owner, repo string, runID int64, attempt int) tea.Cmd {
	return func() tea.Msg {
		var jobs []gh.WorkflowJob
		var err error
		if attempt > 0 {
			jobs, err = m.client.GetWorkflowRunAttemptJobs(m.ctx, owner, repo, runID, attempt)
		} else {
			jobs, err = m.client.GetWorkflowRunJobs(m.ctx, owner, repo, runID)
		}
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
				runs, err := m.client.GetWorkflowRuns(m.ctx, r.Owner.Login, r.Name, 1, 8, m.filterActor)
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
		run := m.getRun()
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
		run := m.getRun()
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

// matchActor matches a user login name against the filter term case-insensitively, handling bot suffixes.
func matchActor(actorLogin, filter string) bool {
	cleanActor := strings.ToLower(actorLogin)
	cleanFilter := strings.ToLower(filter)
	if cleanActor == cleanFilter {
		return true
	}
	if strings.HasPrefix(cleanActor, cleanFilter) && strings.HasSuffix(cleanActor, "[bot]") {
		return true
	}
	return false
}

// scrollPulls adjusts pullStartIndex to keep the selectedPullIdx visible in the viewport.
func (m *Model) scrollPulls() {
	visibleRows := m.height - 12
	if visibleRows < 5 {
		visibleRows = 5
	}
	totalRows := len(m.pulls)
	if m.hasMorePulls {
		totalRows++
	}
	if m.selectedPullIdx < m.pullStartIndex {
		m.pullStartIndex = m.selectedPullIdx
	}
	if m.selectedPullIdx >= m.pullStartIndex+visibleRows {
		m.pullStartIndex = m.selectedPullIdx - visibleRows + 1
	}
	if m.pullStartIndex > totalRows-visibleRows {
		m.pullStartIndex = totalRows - visibleRows
	}
	if m.pullStartIndex < 0 {
		m.pullStartIndex = 0
	}
}

func (m *Model) scrollPRFiles() {
	visibleRows := m.height - 16
	if visibleRows < 5 {
		visibleRows = 5
	}
	totalRows := len(m.prFiles)
	if m.selectedFileIdx < m.prFileStartIndex {
		m.prFileStartIndex = m.selectedFileIdx
	}
	if m.selectedFileIdx >= m.prFileStartIndex+visibleRows {
		m.prFileStartIndex = m.selectedFileIdx - visibleRows + 1
	}
	if m.prFileStartIndex > totalRows-visibleRows {
		m.prFileStartIndex = totalRows - visibleRows
	}
	if m.prFileStartIndex < 0 {
		m.prFileStartIndex = 0
	}
}

func (m *Model) scrollCommitFiles() {
	visibleRows := m.height - 16
	if visibleRows < 5 {
		visibleRows = 5
	}
	totalRows := len(m.commitFiles)
	if m.selectedCommitFileIdx < m.commitFileStartIndex {
		m.commitFileStartIndex = m.selectedCommitFileIdx
	}
	if m.selectedCommitFileIdx >= m.commitFileStartIndex+visibleRows {
		m.commitFileStartIndex = m.selectedCommitFileIdx - visibleRows + 1
	}
	if m.commitFileStartIndex > totalRows-visibleRows {
		m.commitFileStartIndex = totalRows - visibleRows
	}
	if m.commitFileStartIndex < 0 {
		m.commitFileStartIndex = 0
	}
}

func (m *Model) updateCommitDiffViewport() {
	m.commitDiffViewport = viewport.New(m.width-44, m.height-16)
	if m.selectedCommitFileIdx < len(m.commitFiles) {
		m.commitDiffViewport.SetContent(m.formatDiff(m.commitFiles[m.selectedCommitFileIdx].Patch))
	} else {
		m.commitDiffViewport.SetContent("")
	}
}

func (m *Model) updateDiffViewport() {
	m.diffViewport = viewport.New(m.width-44, m.height-16)
	if m.selectedFileIdx < len(m.prFiles) {
		m.diffViewport.SetContent(m.formatDiff(m.prFiles[m.selectedFileIdx].Patch))
	} else {
		m.diffViewport.SetContent("")
	}
}

func (m Model) formatDiff(patch string) string {
	if patch == "" {
		return m.theme.HelpDesc.Render("No diff content available (binary file or empty).")
	}
	lines := strings.Split(patch, "\n")
	var formatted []string
	for _, line := range lines {
		if strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ") {
			formatted = append(formatted, m.theme.LogoText.Render(line))
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			formatted = append(formatted, m.theme.StatusSuccessful.Render(line))
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			formatted = append(formatted, m.theme.StatusFailed.Render(line))
		} else if strings.HasPrefix(line, "@@") {
			formatted = append(formatted, m.theme.HelpKey.Render(line))
		} else {
			formatted = append(formatted, m.theme.TableRow.Render(line))
		}
	}
	return strings.Join(formatted, "\n")
}

func (m Model) viewerCanMerge() bool {
	if m.selectedPull == nil || m.selectedPull.State != "open" {
		return false
	}
	scopes := m.client.GetScopes()
	if len(scopes) == 0 {
		return true // Default to true if scopes header is missing so user can try (with friendly API error fallback)
	}
	for _, s := range scopes {
		if s == "repo" || s == "public_repo" {
			return true
		}
	}
	return false
}

func (m Model) fetchActiveTabCmd() tea.Cmd {
	switch m.activeTab {
	case tabWorkflows:
		if len(m.runs) == 0 {
			m.isLoading = true
			m.loadingMsg = "Loading runs"
			return m.fetchRunsCmd()
		}
	case tabPRs:
		if len(m.pulls) == 0 {
			m.isLoading = true
			m.loadingMsg = "Loading pull requests"
			return m.fetchPullsCmd()
		}
	case tabIssues:
		if len(m.issues) == 0 {
			m.isLoading = true
			m.loadingMsg = "Loading issues"
			return m.fetchIssuesCmd()
		}
	}
	return nil
}

func (m Model) fetchPullsCmd() tea.Cmd {
	return func() tea.Msg {
		if len(m.targets) == 0 {
			return pullsLoadedMsg{err: auth.ErrUnauthenticated}
		}
		target := m.targets[m.selectedTargetIdx]

		var repos []gh.Repository
		if len(m.repos) > 0 {
			repos = m.repos
		} else {
			var err error
			if target.IsOrg {
				repos, err = m.client.GetRepos(m.ctx, "org", target.Name, 1, 100)
			} else {
				repos, err = m.client.GetRepos(m.ctx, "user", target.Name, 1, 100)
			}
			if err != nil {
				return pullsLoadedMsg{err: err}
			}
		}

		if m.filterRepo != "" {
			var filtered []gh.Repository
			for _, r := range repos {
				if r.Name == m.filterRepo {
					filtered = append(filtered, r)
					break
				}
			}
			repos = filtered
		}

		if len(repos) == 0 {
			return pullsLoadedMsg{pulls: nil}
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		var allPulls []gh.PullRequest

		limit := len(repos)
		if limit > 8 {
			limit = 8
		}

		for i := 0; i < limit; i++ {
			repo := repos[i]
			wg.Add(1)
			go func(r gh.Repository) {
				defer wg.Done()
				state := m.filterPRState
				if state == "" {
					state = "open"
				}
				prs, err := m.client.GetPullRequestsWithState(m.ctx, r.Owner.Login, r.Name, state, m.pullPage, 8)
				if err == nil {
					for j := range prs {
						prs[j].Repository = r
					}
					mu.Lock()
					allPulls = append(allPulls, prs...)
					mu.Unlock()
				}
			}(repo)
		}
		wg.Wait()
		var filtered []gh.PullRequest
		for _, pr := range allPulls {
			if m.filterPRAuthor != "" {
				if pr.User == nil || !strings.EqualFold(pr.User.Login, m.filterPRAuthor) {
					continue
				}
			}
			if m.filterPRAssignee != "" {
				assigned := false
				for _, u := range pr.Assignees {
					if strings.EqualFold(u.Login, m.filterPRAssignee) {
						assigned = true
						break
					}
				}
				if !assigned {
					continue
				}
			}
			if m.filterPRReviewer != "" {
				reviewed := false
				for _, u := range pr.RequestedReviewers {
					if strings.EqualFold(u.Login, m.filterPRReviewer) {
						reviewed = true
						break
					}
				}
				if !reviewed {
					continue
				}
			}
			filtered = append(filtered, pr)
		}

		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
		})

		return pullsLoadedMsg{pulls: filtered, repos: repos}
	}
}

func (m Model) fetchPRDetailsCmd(owner, repo string, number int, headSHA, headBranch string) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		var err error

		var pull *gh.PullRequest
		var checks []gh.CheckRun
		var runs []gh.WorkflowRun
		var commits []gh.RepositoryCommit
		var files []gh.CommitFile

		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := m.client.GetPullRequest(m.ctx, owner, repo, number)
			if e == nil {
				pull = p
			} else {
				err = e
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			fList, e := m.client.GetPullRequestFiles(m.ctx, owner, repo, number)
			if e == nil {
				files = fList
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			cList, e := m.client.GetPullRequestCommits(m.ctx, owner, repo, number)
			if e == nil {
				commits = cList
			}
		}()

		if headSHA != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, e := m.client.GetCheckRuns(m.ctx, owner, repo, headSHA)
				if e == nil {
					checks = c
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				r, e := m.client.GetWorkflowRuns(m.ctx, owner, repo, 1, 10, "")
				if e == nil {
					var filtered []gh.WorkflowRun
					for _, run := range r {
						if run.HeadSHA == headSHA {
							run.Repository.Owner = &gh.User{Login: owner}
							run.Repository.Name = repo
							filtered = append(filtered, run)
						}
					}
					runs = filtered
				}
			}()
		}

		wg.Wait()

		if err != nil {
			return prDetailsLoadedMsg{err: err}
		}

		if pull != nil {
			pull.Repository.Name = repo
			pull.Repository.FullName = owner + "/" + repo
			pull.Repository.Owner = &gh.User{Login: owner}
		}

		commitChecks := make(map[string][]gh.CheckRun)
		if len(commits) > 0 {
			var checkWg sync.WaitGroup
			var mu sync.Mutex
			limit := 15
			if len(commits) < limit {
				limit = len(commits)
			}
			for i := 0; i < limit; i++ {
				checkWg.Add(1)
				go func(sha string) {
					defer checkWg.Done()
					runs, err := m.client.GetCheckRuns(m.ctx, owner, repo, sha)
					if err == nil {
						mu.Lock()
						commitChecks[sha] = runs
						mu.Unlock()
					}
				}(commits[i].SHA)
			}
			checkWg.Wait()
		}

		// Collect unique runIDs from all check runs
		runIDsMap := make(map[int64]bool)
		for _, c := range checks {
			rid := extractRunIDFromURL(c.HTMLURL)
			if rid > 0 {
				runIDsMap[rid] = true
			}
		}
		for _, cList := range commitChecks {
			for _, c := range cList {
				rid := extractRunIDFromURL(c.HTMLURL)
				if rid > 0 {
					runIDsMap[rid] = true
				}
			}
		}

		// Filter out runIDs that are already present in runs
		for _, r := range runs {
			delete(runIDsMap, r.ID)
		}

		// Fetch missing runs concurrently
		if len(runIDsMap) > 0 {
			var runsWg sync.WaitGroup
			var runsMu sync.Mutex
			for rid := range runIDsMap {
				runsWg.Add(1)
				go func(id int64) {
					defer runsWg.Done()
					r, err := m.client.GetWorkflowRun(m.ctx, owner, repo, id)
					if err == nil {
						r.Repository.Owner = &gh.User{Login: owner}
						r.Repository.Name = repo
						runsMu.Lock()
						runs = append(runs, *r)
						runsMu.Unlock()
					}
				}(rid)
			}
			runsWg.Wait()
		}

		sidebarWidth := m.width / 5
		if sidebarWidth < 40 {
			sidebarWidth = 40
		}
		w := m.width - sidebarWidth - 4
		if w < 20 {
			w = 20
		}
		renderedDesc := "No description provided."
		if pull.Body != "" {
			if md, err := renderMarkdown(pull.Body, w); err == nil {
				renderedDesc = md
			} else {
				renderedDesc = pull.Body
			}
		}

		return prDetailsLoadedMsg{
			pull:         pull,
			checkRuns:    checks,
			commits:      commits,
			commitChecks: commitChecks,
			actionsRuns:  runs,
			renderedBody: renderedDesc,
			files:        files,
		}
	}
}

func (m Model) fetchCommitDetailsCmd(owner, repo, sha string) tea.Cmd {
	return func() tea.Msg {
		commit, files, err := m.client.GetCommit(m.ctx, owner, repo, sha)
		if err != nil {
			return commitDetailsLoadedMsg{err: err}
		}
		return commitDetailsLoadedMsg{
			commit: commit,
			files:  files,
		}
	}
}

func (m Model) mergePRCmd(owner, repo string, number int, title, message, method string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.MergePullRequest(m.ctx, owner, repo, number, title, message, method)
		return prMergedMsg{err: err}
	}
}

func (m Model) closePRCmd(owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		err := m.client.ClosePullRequest(m.ctx, owner, repo, number)
		return prClosedMsg{err: err}
	}
}

func (m Model) fetchPRCommentsCmd(owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		comments, err := m.client.GetPullRequestComments(m.ctx, owner, repo, number)
		return prCommentsLoadedMsg{comments: comments, err: err}
	}
}

// extractRunIDFromURL parses the workflow run ID from a check run's HTML URL.
func extractRunIDFromURL(url string) int64 {
	idx := strings.Index(url, "/actions/runs/")
	if idx == -1 {
		return 0
	}
	sub := url[idx+len("/actions/runs/"):]
	var runID int64
	for i := 0; i < len(sub); i++ {
		if sub[i] >= '0' && sub[i] <= '9' {
			runID = runID*10 + int64(sub[i]-'0')
		} else {
			break
		}
	}
	return runID
}

// scrollIssues adjusts issueStartIndex to keep the selectedIssueIdx visible in the viewport.
func (m *Model) scrollIssues() {
	visibleRows := m.height - 12
	var filterTexts []string
	if m.filterIssueState != "" && m.filterIssueState != "open" {
		filterTexts = append(filterTexts, m.filterIssueState)
	}
	if m.filterIssueAuthor != "" {
		filterTexts = append(filterTexts, m.filterIssueAuthor)
	}
	if m.filterIssueAssignee != "" {
		filterTexts = append(filterTexts, m.filterIssueAssignee)
	}
	if len(filterTexts) > 0 {
		visibleRows -= 2
	}
	if visibleRows < 5 {
		visibleRows = 5
	}
	totalRows := len(m.issues)
	if m.hasMoreIssues {
		totalRows++
	}
	if m.selectedIssueIdx < m.issueStartIndex {
		m.issueStartIndex = m.selectedIssueIdx
	}
	if m.selectedIssueIdx >= m.issueStartIndex+visibleRows {
		m.issueStartIndex = m.selectedIssueIdx - visibleRows + 1
	}
	if m.issueStartIndex > totalRows-visibleRows {
		m.issueStartIndex = totalRows - visibleRows
	}
	if m.issueStartIndex < 0 {
		m.issueStartIndex = 0
	}
}

func (m Model) fetchIssuesCmd() tea.Cmd {
	return func() tea.Msg {
		if len(m.targets) == 0 {
			return issuesLoadedMsg{err: auth.ErrUnauthenticated}
		}
		target := m.targets[m.selectedTargetIdx]

		var repos []gh.Repository
		if len(m.repos) > 0 {
			repos = m.repos
		} else {
			var err error
			if target.IsOrg {
				repos, err = m.client.GetRepos(m.ctx, "org", target.Name, 1, 100)
			} else {
				repos, err = m.client.GetRepos(m.ctx, "user", target.Name, 1, 100)
			}
			if err != nil {
				return issuesLoadedMsg{err: err}
			}
		}

		if m.filterRepo != "" {
			var filtered []gh.Repository
			for _, r := range repos {
				if r.Name == m.filterRepo {
					filtered = append(filtered, r)
					break
				}
			}
			repos = filtered
		}

		if len(repos) == 0 {
			return issuesLoadedMsg{issues: nil}
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		var allIssues []gh.Issue

		limit := len(repos)
		if limit > 8 {
			limit = 8
		}

		var anyHasMore bool

		for i := 0; i < limit; i++ {
			repo := repos[i]
			wg.Add(1)
			go func(r gh.Repository) {
				defer wg.Done()
				state := m.filterIssueState
				if state == "" {
					state = "open"
				}
				issues, hasMore, err := m.client.GetIssuesWithState(m.ctx, r.Owner.Login, r.Name, state, m.issuePage, 50)
				if err == nil {
					for j := range issues {
						issues[j].Repository = r
					}
					mu.Lock()
					allIssues = append(allIssues, issues...)
					if hasMore {
						anyHasMore = true
					}
					mu.Unlock()
				}
			}(repo)
		}
		wg.Wait()
		var filtered []gh.Issue
		for _, issue := range allIssues {
			if m.filterIssueAuthor != "" {
				if issue.User == nil || !strings.EqualFold(issue.User.Login, m.filterIssueAuthor) {
					continue
				}
			}
			if m.filterIssueAssignee != "" {
				assigned := false
				for _, u := range issue.Assignees {
					if strings.EqualFold(u.Login, m.filterIssueAssignee) {
						assigned = true
						break
					}
				}
				if !assigned {
					continue
				}
			}
			filtered = append(filtered, issue)
		}

		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
		})

		return issuesLoadedMsg{issues: filtered, repos: repos, hasMore: anyHasMore}
	}
}

func (m Model) fetchIssueDetailsCmd(owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		var err error

		var issue *gh.Issue
		var comments []gh.IssueComment

		wg.Add(1)
		go func() {
			defer wg.Done()
			i, e := m.client.GetIssue(m.ctx, owner, repo, number)
			if e == nil {
				issue = i
			} else {
				err = e
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			cList, e := m.client.GetPullRequestComments(m.ctx, owner, repo, number)
			if e == nil {
				comments = cList
			}
		}()

		wg.Wait()

		if err != nil {
			return issueDetailsLoadedMsg{err: err}
		}

		if issue != nil {
			issue.Repository.Name = repo
			issue.Repository.FullName = owner + "/" + repo
			issue.Repository.Owner = &gh.User{Login: owner}
		}

		sidebarWidth := m.width / 5
		if sidebarWidth < 40 {
			sidebarWidth = 40
		}
		w := m.width - sidebarWidth - 4
		if w < 20 {
			w = 20
		}
		renderedDesc := "No description provided."
		if issue != nil && issue.Body != "" {
			if md, err := renderMarkdown(issue.Body, w); err == nil {
				renderedDesc = md
			} else {
				renderedDesc = issue.Body
			}
		}

		return issueDetailsLoadedMsg{
			issue:        issue,
			comments:     comments,
			renderedBody: renderedDesc,
		}
	}
}

func (m Model) fetchIssueCommentsCmd(owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		comments, err := m.client.GetPullRequestComments(m.ctx, owner, repo, number)
		return issueCommentsLoadedMsg{comments: comments, err: err}
	}
}

func (m Model) checkApprovalPermissionCmd() tea.Cmd {
	var run gh.WorkflowRun
	if m.state == viewMain && m.activeTab == tabWorkflows {
		if m.selectedRunIdx >= 0 && m.selectedRunIdx < len(m.runs) {
			run = m.runs[m.selectedRunIdx]
		} else {
			return nil
		}
	} else if m.state == viewJobs {
		run = m.getRun()
	} else {
		return nil
	}

	if run.ID == 0 {
		return nil
	}

	status := run.Status
	conclusion := run.Conclusion
	needsApproval := (status == "waiting" || conclusion == "action_required")
	if !needsApproval {
		return nil
	}

	if _, cached := m.approvalPermissions[run.ID]; cached {
		return nil
	}

	owner := run.Repository.Owner.Login
	repo := run.Repository.Name
	if owner == "" || repo == "" {
		return nil
	}

	return func() tea.Msg {
		if conclusion == "action_required" {
			isForkPR := (run.HeadRepository.FullName != "" && run.HeadRepository.FullName != run.Repository.FullName)
			if !isForkPR {
				// Local PR runs can be approved via browser redirection.
				return approvalPermissionLoadedMsg{runID: run.ID, canApprove: true}
			}

			// Fork PR approval: requires repo & workflow scopes
			if ok, missing := m.client.HasRequiredScopes(); !ok {
				var sourceStr string
				if m.config != nil && m.config.TokenSource != "" {
					sourceStr = fmt.Sprintf(" (Token source: %s)", m.config.TokenSource)
				}
				return approvalPermissionLoadedMsg{
					runID:      run.ID,
					canApprove: false,
					err:        fmt.Errorf("missing scopes: %s%s. Run: gh auth refresh -s %s", strings.Join(missing, ", "), sourceStr, strings.Join(missing, " -s ")),
				}
			}

			// Verify repo write permission
			if m.currentUser != "" && strings.EqualFold(owner, m.currentUser) {
				return approvalPermissionLoadedMsg{runID: run.ID, canApprove: true}
			}
			perm, err := m.client.GetRepoPermission(m.ctx, owner, repo, m.currentUser)
			if err != nil {
				return approvalPermissionLoadedMsg{runID: run.ID, canApprove: false, err: err}
			}
			canApprove := (perm == "admin" || perm == "write" || perm == "maintain")
			return approvalPermissionLoadedMsg{runID: run.ID, canApprove: canApprove}
		}

		if status == "waiting" {
			// Environment deployment approval: requires repo & workflow scopes
			if ok, missing := m.client.HasRequiredScopes(); !ok {
				var sourceStr string
				if m.config != nil && m.config.TokenSource != "" {
					sourceStr = fmt.Sprintf(" (Token source: %s)", m.config.TokenSource)
				}
				return approvalPermissionLoadedMsg{
					runID:      run.ID,
					canApprove: false,
					err:        fmt.Errorf("missing scopes: %s%s. Run: gh auth refresh -s %s", strings.Join(missing, ", "), sourceStr, strings.Join(missing, " -s ")),
				}
			}

			// Check deployment permissions
			deployments, err := m.client.GetPendingDeployments(m.ctx, owner, repo, run.ID)
			if err != nil {
				// Fallback to collaborator permission
				perm, permErr := m.client.GetRepoPermission(m.ctx, owner, repo, m.currentUser)
				if permErr == nil && (perm == "admin" || perm == "write" || perm == "maintain") {
					return approvalPermissionLoadedMsg{runID: run.ID, canApprove: true}
				}
				return approvalPermissionLoadedMsg{runID: run.ID, canApprove: false, err: err}
			}
			canApprove := false
			for _, d := range deployments {
				if d.CurrentUserCanApprove {
					canApprove = true
					break
				}
			}
			return approvalPermissionLoadedMsg{runID: run.ID, canApprove: canApprove}
		}

		return approvalPermissionLoadedMsg{runID: run.ID, canApprove: false}
	}
}

func (m Model) approveWorkflowRunCmd(owner, repo string, runID int64, status, conclusion string) tea.Cmd {
	return func() tea.Msg {
		if conclusion == "action_required" {
			err := m.client.ApproveWorkflowRun(m.ctx, owner, repo, runID)
			return workflowRunApprovedMsg{runID: runID, err: err}
		}
		if status == "waiting" {
			deployments, err := m.client.GetPendingDeployments(m.ctx, owner, repo, runID)
			if err != nil {
				return workflowRunApprovedMsg{runID: runID, err: err}
			}
			var envIDs []int64
			for _, d := range deployments {
				if d.CurrentUserCanApprove {
					envIDs = append(envIDs, d.Environment.ID)
				}
			}
			if len(envIDs) == 0 {
				for _, d := range deployments {
					envIDs = append(envIDs, d.Environment.ID)
				}
			}
			if len(envIDs) == 0 {
				return workflowRunApprovedMsg{runID: runID, err: fmt.Errorf("no pending environments found to approve")}
			}
			err = m.client.ApprovePendingDeployments(m.ctx, owner, repo, runID, envIDs, "Approved via ghspector")
			return workflowRunApprovedMsg{runID: runID, err: err}
		}
		return workflowRunApprovedMsg{runID: runID, err: fmt.Errorf("workflow run does not require approval")}
	}
}

type reposLoadedMsg struct {
	repos []gh.Repository
	err   error
}

func (m Model) fetchReposCmd() tea.Cmd {
	return func() tea.Msg {
		if len(m.targets) == 0 {
			return reposLoadedMsg{err: auth.ErrUnauthenticated}
		}
		target := m.targets[m.selectedTargetIdx]
		var repos []gh.Repository
		var err error
		if target.IsOrg {
			repos, err = m.client.GetRepos(m.ctx, "org", target.Name, 1, 100)
		} else {
			repos, err = m.client.GetRepos(m.ctx, "user", target.Name, 1, 100)
		}
		if err != nil {
			return reposLoadedMsg{repos: nil, err: err}
		}
		return reposLoadedMsg{repos: repos}
	}
}

