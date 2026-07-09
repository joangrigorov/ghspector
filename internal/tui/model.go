package tui

import (
	"context"
	"time"

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
)

// Target represents a selected organization or user account to browse.
type Target struct {
	Name string
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
	tickCount   int
	width, height int

	// Targets (Orgs and User Accounts)
	targets       []Target
	selectedTargetIdx int

	// Data Cache
	repos         []gh.Repository
	runs          []gh.WorkflowRun
	selectedRunIdx int
	runPage       int
	hasMoreRuns   bool

	jobs          []gh.WorkflowJob
	selectedJobIdx int

	// Logs browser
	logs          string
	logsViewport  viewport.Model
	logsLoading   bool

	// Status messages & flags
	statusMsg   string
	isLoading   bool
	loadingMsg  string
	
	// Error handling
	err         error
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
	runs []gh.WorkflowRun
	err  error
}
type jobsLoadedMsg struct {
	jobs []gh.WorkflowJob
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

// InitModel initializes the model.
func InitModel(client *gh.Client, config *auth.Config) Model {
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		client:       client,
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		theme:        GetTheme(),
		state:        viewSplash,
		loadingMsg:   "Initializing ghspector",
		hasMoreRuns:  true,
		runPage:      1,
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
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
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
