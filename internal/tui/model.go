package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"ghspector/internal/auth"
	"ghspector/internal/gh"
)

type viewState int

const (
	viewSplash viewState = iota
	viewMain
	viewJobs
	viewLogs
	viewSwitcher
	viewHelp
	viewPRDetails
	viewCommitDetails
	viewPRFilterInput
	viewPRFilterTypeSelect
	viewPRComments
	viewPRCommits
	viewPRDiff
	viewIssueDetails
	viewIssueComments
	viewIssueFilterInput
	viewIssueFilterTypeSelect
	viewFilterTypeSelect
	viewRepoFilterSelect
)

type mainTab int

const (
	tabPRs mainTab = iota
	tabWorkflows
	tabIssues
)

type prTab int

const (
	prTabInfo prTab = iota
	prTabCommits
	prTabFiles
	prTabChecks
	prTabComments
)

// Target represents a selected organization or user account to browse.
type Target struct {
	Name  string
	IsOrg bool
}

// Model represents the application state.
type Model struct {
	// API Client & Config
	client *gh.Client
	config *auth.Config
	ctx    context.Context
	cancel context.CancelFunc

	// Theme
	theme *Theme

	// Navigation State
	state       viewState
	prevState   viewState // stored when overlay/switcher opens
	activeTab   mainTab
	tickCount   int
	width, height int

	// Targets (Orgs and User Accounts)
	targets           []Target
	selectedTargetIdx int
	repos             []gh.Repository
	selectedRepoIdx   int
	repoStartIndex    int
	filterRepo        string

	// Data Cache - Workflows
	runs          []gh.WorkflowRun
	selectedRunIdx int
	runStartIndex  int
	runPage       int
	hasMoreRuns   bool
	viewingRun    *gh.WorkflowRun

	jobs          []gh.WorkflowJob
	selectedJobIdx int
	jobStartIndex  int

	// Data Cache - Pull Requests
	pulls             []gh.PullRequest
	selectedPullIdx   int
	pullStartIndex    int
	pullPage          int
	hasMorePulls      bool
	selectedPull      *gh.PullRequest
	activePRTab       prTab
	prCommits         []gh.RepositoryCommit
	selectedCommitIdx int
	prFiles           []gh.CommitFile
	selectedFileIdx   int
	prFileStartIndex  int
	diffViewport      viewport.Model
	prChecks          []gh.CheckRun
	selectedCheckIdx  int
	prCommitChecks    map[string][]gh.CheckRun
	prComments        []gh.IssueComment
	commentsViewport  viewport.Model
	prDescViewport    viewport.Model
	prDescFocused     bool // true: description focused, false: checks sidebar focused
	targetJobName     string

	// Data Cache - Issues
	issues             []gh.Issue
	selectedIssueIdx   int
	issueStartIndex    int
	issuePage          int
	hasMoreIssues      bool
	selectedIssue      *gh.Issue
	issueComments      []gh.IssueComment
	issueDescViewport  viewport.Model
	issueDescFocused   bool // true: description focused, false: metadata sidebar focused

	// Issue filter fields
	filterIssueAuthor   string
	filterIssueAssignee string
	filterIssueState    string
	issueFilterUser     string


	// Data Cache - Commit Viewer
	viewingCommit         *gh.RepositoryCommit
	commitFiles           []gh.CommitFile
	selectedCommitFileIdx int
	commitFileStartIndex  int
	commitDiffViewport    viewport.Model

	// Dashboard state
	dashboardPRsCount       int
	dashboardWorkflowsCount int

	// Merge state
	mergeState      int    // 0: none, 1: method selection, 2: confirmation, 3: completed
	mergeMethod     int    // 0: squash, 1: merge, 2: rebase

	// Logs browser
	logs             string
	logsViewport     viewport.Model
	logsLoading      bool
	followLogs       bool
	selectedStepIdx  int
	logsSegments     map[int]string

	// Status messages & flags
	statusMsg   string
	statusMsgID int
	isLoading   bool
	loadingMsg  string
	
	// Error handling
	err         error

	// Actor filter fields
	filterActor     string
	showFilterInput bool
	textInput       textinput.Model
	currentUser     string

	// PR filter fields
	filterPRAuthor   string
	filterPRAssignee  string
	filterPRReviewer  string
	filterPRState     string
	prFilterUser     string

	// Attempt browsing
	selectedAttempt int

	// Approval confirmation state
	approvalPermissions map[int64]bool // caches runID -> canApprove
	runApprovalState    int            // 0: none, 1: confirm approval
}

// Message types
type tickMsg time.Time
type pollMsg time.Time
type initDataMsg struct {
	user    *gh.User
	orgs    []gh.Org
	err     error
}
type runsLoadedMsg struct {
	runs  []gh.WorkflowRun
	repos []gh.Repository
	err   error
}
type runsPolledMsg struct {
	runs []gh.WorkflowRun
	err  error
}
type jobsLoadedMsg struct {
	jobs []gh.WorkflowJob
	run  *gh.WorkflowRun
	err  error
}
type logsLoadedMsg struct {
	logs string
	err  error
}
type runUpdateMsg struct {
	runID      int64
	status     string
	conclusion string
}
type jobUpdateMsg struct {
	jobID      int64
	status     string
	conclusion string
}

type pullsLoadedMsg struct {
	pulls []gh.PullRequest
	repos []gh.Repository
	err   error
}

type prDetailsLoadedMsg struct {
	pull         *gh.PullRequest
	commits      []gh.RepositoryCommit
	files        []gh.CommitFile
	checkRuns    []gh.CheckRun
	commitChecks map[string][]gh.CheckRun
	comments     []gh.IssueComment
	actionsRuns  []gh.WorkflowRun
	renderedBody string
	err          error
}

type prCommentsLoadedMsg struct {
	comments []gh.IssueComment
	err      error
}

type issuesLoadedMsg struct {
	issues  []gh.Issue
	repos   []gh.Repository
	hasMore bool
	err     error
}

type issueDetailsLoadedMsg struct {
	issue        *gh.Issue
	comments     []gh.IssueComment
	renderedBody string
	err          error
}

type issueCommentsLoadedMsg struct {
	comments []gh.IssueComment
	err      error
}

type commitDetailsLoadedMsg struct {
	commit *gh.RepositoryCommit
	files  []gh.CommitFile
	err    error
}

type prMergedMsg struct {
	err error
}

type prClosedMsg struct {
	err error
}

type dashboardStatsLoadedMsg struct {
	prsCount       int
	workflowsCount int
	err            error
}

type approvalPermissionLoadedMsg struct {
	runID      int64
	canApprove bool
	err        error
}

type workflowRunApprovedMsg struct {
	runID int64
	err   error
}

// InitModel initializes the model.
func InitModel(client *gh.Client, config *auth.Config) Model {
	ctx, cancel := context.WithCancel(context.Background())
	ti := textinput.New()
	ti.Placeholder = "username"
	ti.CharLimit = 39
	ti.Width = 20

	return Model{
		client:       client,
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		theme:        GetTheme(),
		state:        viewSplash,
		activeTab:    tabPRs,
		loadingMsg:   "Initializing ghspector",
		hasMoreRuns:  true,
		runPage:      1,
		commentsViewport: viewport.New(80, 20),
		prCommitChecks:   make(map[string][]gh.CheckRun),
		hasMorePulls:     true,
		pullPage:         1,
		filterPRState:    "open",
		hasMoreIssues:    true,
		issuePage:        1,
		filterIssueState: "open",
		textInput:        ti,
		approvalPermissions: make(map[int64]bool),
		runApprovalState:    0,
	}
}

// Init starts the TUI.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.tick(),
		m.pollTick(),
		m.fetchInitialDataCmd(),
	)
}

// Helper tick command for spinner/splash animations.
func (m Model) tick() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// pollTick schedules the next status poll event.
func (m Model) pollTick() tea.Cmd {
	workflowsInterval := 5
	prsInterval := 10

	if m.config != nil {
		if m.config.PollingIntervalSeconds > 0 {
			workflowsInterval = m.config.PollingIntervalSeconds
			prsInterval = m.config.PollingIntervalSeconds
		}
		if m.config.Polling.WorkflowsIntervalSeconds > 0 {
			workflowsInterval = m.config.Polling.WorkflowsIntervalSeconds
		}
		if m.config.Polling.PRsIntervalSeconds > 0 {
			prsInterval = m.config.Polling.PRsIntervalSeconds
		}
	}

	interval := time.Duration(workflowsInterval) * time.Second
	if m.state == viewMain && m.activeTab == tabPRs {
		interval = time.Duration(prsInterval) * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return pollMsg(t)
	})
}

// Fetch user account and accessible organizations.
func (m Model) fetchInitialDataCmd() tea.Cmd {
	return func() tea.Msg {
		user, err := m.client.GetUser(m.ctx)
		if err != nil {
			return initDataMsg{err: err}
		}

		orgs, err := m.client.GetOrgs(m.ctx)
		if err != nil {
			return initDataMsg{err: err}
		}

		return initDataMsg{user: user, orgs: orgs}
	}
}

// getRun returns the active WorkflowRun, checking m.runs first and falling back to m.viewingRun.
func (m Model) getRun() gh.WorkflowRun {
	var run gh.WorkflowRun
	if m.selectedRunIdx >= 0 && m.selectedRunIdx < len(m.runs) {
		run = m.runs[m.selectedRunIdx]
	} else if m.viewingRun != nil {
		run = *m.viewingRun
	}
	if run.Repository.Owner == nil {
		run.Repository.Owner = &gh.User{}
	}
	if run.Repository.Owner.Login == "" && m.selectedPull != nil && m.selectedPull.Repository.Owner != nil {
		run.Repository.Owner.Login = m.selectedPull.Repository.Owner.Login
	}
	if run.Repository.Name == "" && m.selectedPull != nil {
		run.Repository.Name = m.selectedPull.Repository.Name
	}
	if run.Repository.FullName == "" && m.selectedPull != nil {
		run.Repository.FullName = m.selectedPull.Repository.FullName
	}
	if run.Actor == nil {
		run.Actor = &gh.User{}
	}
	return run
}

// selectedRunCanApprove returns true if the selected workflow run requires approval and the user has permission to do so.
func (m Model) selectedRunCanApprove() bool {
	var run gh.WorkflowRun
	if m.state == viewMain && m.activeTab == tabWorkflows {
		if m.selectedRunIdx >= 0 && m.selectedRunIdx < len(m.runs) {
			run = m.runs[m.selectedRunIdx]
		} else {
			return false
		}
	} else if m.state == viewJobs {
		run = m.getRun()
	} else {
		return false
	}

	if run.ID == 0 {
		return false
	}
	
	needsApproval := (run.Status == "waiting" || run.Conclusion == "action_required")
	return needsApproval && m.approvalPermissions[run.ID]
}

type clearStatusMsg struct {
	id int
}

func (m *Model) setStatusMsg(msg string) tea.Cmd {
	m.statusMsg = msg
	if msg == "" {
		return nil
	}
	m.statusMsgID++
	id := m.statusMsgID
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{id: id}
	})
}
