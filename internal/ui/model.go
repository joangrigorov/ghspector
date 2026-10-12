package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/auth"
	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/overlay"
	"github.com/joangrigorov/ghspector/internal/store"
)

// Options configures the root model. It is built once by main and never mutated.
type Options struct {
	Store store.Store
	Theme Theme
	// Context bounds every request the model starts. main cancels it on exit.
	Context context.Context
	// Version is the build version shown on the splash.
	Version string
	// ConfigPath and TokenSource are display labels, already free of any
	// absolute path.
	ConfigPath  string
	TokenSource string
	// DefaultOwner is the context selected by --org, --user or the config
	// file. Nil means "the authenticated user".
	DefaultOwner *store.OwnerRef
	// PollWorkflows, PollPRs and PollIssues are the refresh intervals resolved
	// from the config file. A non-positive value turns polling off for that tab.
	PollWorkflows time.Duration
	PollPRs       time.Duration
	PollIssues    time.Duration
	// Clock is the time source. Nil means the real clock; a test supplies one
	// it can release by hand instead of sleeping.
	Clock Clock
	// MergeMethod is the default merge method from the config file, resolved to
	// one of squash, merge or rebase.
	MergeMethod string
	// SaveMergeMethod persists a new default merge method. Nil means there is
	// no config file to write to.
	SaveMergeMethod func(method string) error
}

// stage is the coarse state of the root model.
type stage int

const (
	// stageLoading shows the splash while the identity is resolved.
	stageLoading stage = iota
	// stageReady shows the normal chrome.
	stageReady
	// stageError shows a full-screen failure.
	stageError
)

// Model is the root model. It owns the terminal size, the route stack, the
// overlay stack, and the transient status message. Nothing else may.
type Model struct {
	opts Options
	deps Deps

	width, height int
	routes        RouteStack
	overlays      overlay.Stack

	stage stage
	err   error

	tick int

	status           string
	statusErr        bool
	statusGeneration int

	// pollToken identifies the live polling session. A tick carrying any other
	// token belongs to a state that has been left behind and is dropped.
	pollToken int

	// orgs backs the context switcher. Loaded with the identity so opening the
	// switcher never stalls.
	orgs []store.Org
}

// New builds the root model. The main route is on the stack from the start, so
// the stack is never empty and breadcrumbs always have a root.
func New(opts Options) Model {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	m := Model{
		opts:     opts,
		overlays: overlay.NewStack(overlayChrome(opts.Theme)),
		routes:   NewRouteStack(NewMain()),
		stage:    stageLoading,
	}
	m.deps = Deps{
		Store:           opts.Store,
		Theme:           opts.Theme,
		Ctx:             opts.Context,
		Version:         opts.Version,
		ConfigPath:      opts.ConfigPath,
		TokenSource:     opts.TokenSource,
		PollWorkflows:   opts.PollWorkflows,
		PollPRs:         opts.PollPRs,
		PollIssues:      opts.PollIssues,
		Clock:           opts.Clock,
		MergeMethod:     auth.ResolveMergeMethod(opts.MergeMethod),
		SaveMergeMethod: opts.SaveMergeMethod,
		Markdown:        NewMarkdown(),
	}
	// The body renderer is built here, before any frame asks for it, so a view
	// never constructs one.
	m.deps.Markdown.Prepare(m.deps.Theme, m.deps.Size.Width)
	return m
}

// Init starts the splash animation, the identity request, the poll clock, and
// the terminal background query.
//
// The background query is why the palette is not decided by main: the terminal
// answers with the colour it is actually painting, and the reply can arrive
// again later, so the theme follows the terminal instead of being fixed at
// startup. Asking with lipgloss.HasDarkBackground would instead block on a reply
// read before the runtime is listening, which is the same probe the previous
// version called from inside its render path.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinnerTick(), m.loadIdentity(), m.armPoll(), tea.RequestBackgroundColor)
}

// spinnerMsg advances the splash animation.
type spinnerMsg struct{}

// spinnerInterval paces the splash animation.
const spinnerInterval = 300 * time.Millisecond

func (m Model) spinnerTick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerMsg{} })
}

// identityMsg carries the result of resolving who the session acts as.
type identityMsg struct {
	viewer store.User
	orgs   []store.Org
	err    error
}

// identityTimeout bounds the startup requests.
const identityTimeout = 20 * time.Second

// loadIdentity resolves the viewer and their organizations.
func (m Model) loadIdentity() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.opts.Context, identityTimeout)
		defer cancel()

		viewer, err := m.opts.Store.Viewer(ctx)
		if err != nil {
			return identityMsg{err: err}
		}
		// Organizations are optional: a token without read:org still works, it
		// just cannot switch into an organization context.
		orgs, err := m.opts.Store.Organizations(ctx)
		if err != nil {
			orgs = nil
		}
		return identityMsg{viewer: viewer, orgs: orgs}
	}
}

// Update is the single entry point for every message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Quit is the one binding that must work from every screen and from behind
	// every modal, so it is settled before any other routing. A text box is the
	// exception: `q` there is a character somebody is typing, and the way out of
	// a box is Ctrl+C (spec §2.4).
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if action, found := keymap.Lookup(keymap.Global, keymap.KeyString(k)); found && action == keymap.ActionQuit {
			if !m.overlays.TakesText() || k.Mod.Contains(tea.ModCtrl) {
				return m, tea.Quit
			}
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Routes are given the geometry rather than deriving it, so a route
		// that sizes a modal budget knows how much room it has.
		m.deps.Size = Size{Width: msg.Width, Height: msg.Height}
		// The rendered bodies are wrapped to the new width, so the renderer is
		// rebuilt here rather than inside the frame that first needs it.
		m.deps.Markdown.Prepare(m.deps.Theme, msg.Width)
		return m, nil

	case tea.BackgroundColorMsg:
		return m.withTheme(msg.IsDark()), nil

	case tea.FocusMsg:
		// Regaining focus is when a scheme change is most likely to have
		// happened while the program was not looking, so ask again.
		return m, tea.RequestBackgroundColor

	case spinnerMsg:
		m.tick++
		// The animation runs while there is something to animate: the startup
		// screen, or a screen that is waiting for data.
		if m.stage == stageLoading || m.routeLoading() {
			return m, m.spinnerTick()
		}
		return m, nil

	case identityMsg:
		return m.applyIdentity(msg)

	case statusClearMsg:
		if msg.generation == m.statusGeneration {
			m.status = ""
			m.statusErr = false
		}
		return m, nil

	case StatusMsg:
		return m, m.setStatus(msg.Text, msg.IsErr)

	case pollMsg:
		return m.handlePoll(msg)

	case PushMsg:
		m.routes = m.routes.Push(msg.Route)
		return m, msg.Then

	case PopMsg:
		m.popRoute()
		return m, nil

	case ReplaceMsg:
		stopRoute(m.routes.Top())
		m.routes = m.routes.Replace(msg.Route)
		return m, nil

	case PopToRootMsg:
		m.popToRoot()
		return m, nil

	case ContextSelectedMsg:
		return m.applyContext(msg.Owner)
	}

	// Overlay-control messages are delivered by commands, so they reach the
	// stack whether or not it has anything open: a batch that closes an overlay
	// and then pushes another must not lose the push.
	switch msg.(type) {
	case overlay.CloseMsg, overlay.CloseAllMsg, overlay.PushMsg, overlay.ReplaceMsg:
		var cmd tea.Cmd
		m.overlays, cmd = m.overlays.Update(msg)
		return m, cmd
	}

	// Input is routed to the modal stack first: an open modal owns the keyboard,
	// so the route underneath never sees a keystroke meant for it.
	if !m.overlays.Empty() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.MouseMsg, tea.PasteMsg:
			var cmd tea.Cmd
			m.overlays, cmd = m.overlays.Update(msg)
			return m, cmd
		}
	}
	if m.stage != stageReady {
		// The splash and the error screen accept only quit and retry.
		if k, ok := msg.(tea.KeyPressMsg); ok && m.stage == stageError {
			switch keymap.KeyString(k) {
			case "q":
				return m, tea.Quit
			case "r":
				m.stage = stageLoading
				m.err = nil
				return m, tea.Batch(m.spinnerTick(), m.loadIdentity())
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		route, cmd := m.routes.Top().Update(m.deps, msg)
		m.routes = m.routes.Replace(route)
		return m, cmd
	default:
		route, cmd := m.routes.Top().Update(m.deps, msg)
		m.routes = m.routes.Replace(route)
		return m, cmd
	}
}

// handleKey resolves a key press into an action and dispatches it.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := keymap.KeyString(msg)

	action, ok := m.resolve(key)
	if !ok {
		// Unbound keys go to the route, for routes that take text input.
		route, cmd := m.routes.Top().Update(m.deps, msg)
		m.routes = m.routes.Replace(route)
		return m, cmd
	}
	return m.applyAction(action)
}

// resolve looks a key up in the active route's scope, then in the global scope.
// The route wins, so a route can rebind a global key.
func (m Model) resolve(key string) (keymap.Action, bool) {
	if action, ok := keymap.Lookup(m.routes.Top().Scope(), key); ok {
		return action, true
	}
	return keymap.Lookup(keymap.Global, key)
}

// applyAction runs the actions the root owns and offers the rest to the route.
func (m Model) applyAction(action keymap.Action) (tea.Model, tea.Cmd) {
	switch action {
	case keymap.ActionQuit:
		return m, tea.Quit

	case keymap.ActionHelp:
		if _, open := m.routes.Top().(*Help); open {
			m.popToRoot()
			return m, nil
		}
		m.routes = m.routes.Push(NewHelp())
		return m, nil

	case keymap.ActionSwitchCtx:
		if _, open := m.routes.Top().(*Switcher); open {
			m.popToRoot()
			return m, nil
		}
		m.routes = m.routes.Push(NewSwitcher(m.switcherEntries(), m.deps.Account.Owner))
		return m, nil

	case keymap.ActionBack:
		// The route on screen owns what Back means, so a route can step back
		// inside itself — a pull request leaving a sub-tab for its Info tab —
		// before the stack gives up a screen. A route that does not handle it
		// is asking to be popped.
		route, cmd, handled := m.routes.Top().Act(m.deps, action)
		if handled {
			m.routes = m.routes.Replace(route)
			return m, cmd
		}
		if m.routes.Len() > 1 {
			m.popRoute()
		}
		return m, nil
	}

	route, cmd, handled := m.routes.Top().Act(m.deps, action)
	if handled {
		m.routes = m.routes.Replace(route)
	}
	return m, cmd
}

// popRoute closes the topmost route and releases the requests it owns.
func (m *Model) popRoute() {
	next, gone := m.routes.PopRoute()
	m.routes = next
	stopRoute(gone)
}

// popToRoot closes every route above the root, releasing each.
func (m *Model) popToRoot() {
	for _, gone := range m.routes.AboveRoot() {
		stopRoute(gone)
	}
	m.routes = m.routes.PopToRoot()
}

// handlePoll answers a poll tick: a token that is no longer live is dropped, a
// live one re-arms the next tick and hands the tick to the route on screen.
//
// The root owns the cadence because it is the only layer that knows which state
// is on screen, and the route owns what a tick means for its own data.
func (m Model) handlePoll(msg pollMsg) (tea.Model, tea.Cmd) {
	if msg.token == 0 || msg.token != m.pollToken {
		return m, nil
	}
	if m.stage != stageReady {
		return m, nil
	}
	route, cmd := m.routes.Top().Update(m.deps, msg)
	m.routes = m.routes.Replace(route)
	return m, tea.Batch(cmd, m.armPoll())
}

// armPoll starts a new polling session for the state on screen and returns the
// tick that keeps it going.
func (m *Model) armPoll() tea.Cmd {
	interval := m.pollInterval()
	if interval <= 0 {
		m.pollToken = 0
		return nil
	}
	m.pollToken = nextPollToken()
	return pollTick(m.deps.clock(), interval, m.pollToken)
}

// pollInterval is the refresh cadence for the state on screen.
//
// Each list tab polls on its own interval, and every other screen on the
// workflows cadence, which is the rule the previous version applied and spec
// §5.4 records. The Issues tab used to share the workflows cadence because the
// selector only special-cased the Pull Requests tab; it now has an interval of
// its own, which is the v2 fix spec §5.4 asks for.
func (m Model) pollInterval() time.Duration {
	switch m.routes.Top().Scope() {
	case keymap.MainPRs:
		return m.deps.PollPRs
	case keymap.MainIssues:
		return m.deps.PollIssues
	default:
		return m.deps.PollWorkflows
	}
}

// applyIdentity finishes startup.
func (m Model) applyIdentity(msg identityMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.stage = stageError
		m.err = msg.err
		return m, nil
	}
	m.orgs = msg.orgs
	m.stage = stageReady

	owner := store.OwnerRef{Login: msg.viewer.Login}
	if m.opts.DefaultOwner != nil && m.opts.DefaultOwner.Login != "" {
		owner = *m.opts.DefaultOwner
	}
	m.deps.Account = Account{Login: msg.viewer.Login, Owner: owner}

	// The routes were built before the account was known, so they are told now.
	// This is the fetch that fills the first screen.
	var cmd tea.Cmd
	m.routes, cmd = m.routes.Broadcast(m.deps, AccountChangedMsg{})
	return m, cmd
}

// applyContext switches the active owner, drops every cached read, and tells
// the routes so they can discard data that belonged to the previous context.
func (m Model) applyContext(owner store.OwnerRef) (tea.Model, tea.Cmd) {
	m.deps.Account.Owner = owner
	m.popToRoot()
	m.opts.Store.Invalidate()

	var broadcast tea.Cmd
	m.routes, broadcast = m.routes.Broadcast(m.deps, AccountChangedMsg{})
	statusCmd := m.setStatus("Context: "+owner.String(), false)
	return m, tea.Batch(broadcast, statusCmd)
}

// switcherEntries is the list of selectable owners: the viewer, then the
// organizations they belong to.
func (m Model) switcherEntries() []store.OwnerRef {
	entries := []store.OwnerRef{{Login: m.deps.Account.Login}}
	for _, o := range m.orgs {
		entries = append(entries, store.OwnerRef{Login: o.Login, IsOrg: true})
	}
	return entries
}

// setStatus records a transient message and schedules its expiry.
func (m *Model) setStatus(text string, isErr bool) tea.Cmd {
	if text == "" {
		return nil
	}
	m.status = text
	m.statusErr = isErr
	m.statusGeneration++
	gen := m.statusGeneration
	return tea.Tick(statusTTL, func(time.Time) tea.Msg { return statusClearMsg{generation: gen} })
}

// View renders the frame. The chrome is always exactly the frame geometry, so
// the renderer never has to reconcile a ragged screen.
func (m Model) View() tea.View {
	content := m.render()
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "ghspector"
	// Focus reporting is what makes the palette follow a scheme change while the
	// program is in the background.
	v.ReportFocus = true
	return v
}

// withTheme rebuilds the palette for a terminal background. The theme and the
// modal chrome are derived from the same answer, so a change reaches every
// screen and every overlay rather than half of them.
func (m Model) withTheme(dark bool) Model {
	theme := ThemeFor(dark)
	m.opts.Theme = theme
	m.deps.Theme = theme
	m.overlays.SetChrome(overlayChrome(theme))
	// A body is painted in the palette it is drawn on, so the renderer follows
	// the background change.
	m.deps.Markdown.Prepare(theme, m.width)
	return m
}

// render builds the screen for the current stage.
func (m Model) render() string {
	width, height := m.width, m.height
	if width < 1 || height < 1 {
		return ""
	}

	if layout.TooSmall(width, height) {
		return renderTooSmall(m.deps.Theme, width, height)
	}

	switch m.stage {
	case stageLoading:
		return m.renderSplash(width, height)
	case stageError:
		return renderError(m.deps.Theme, m.errorTitle(), m.errorDetail(), width, height)
	}

	frame := layout.Layout(width, height)

	header := renderHeader(m.deps, frame.Header, breadcrumbs(m.routes.Titles()), m.routeLoading(), m.tick)

	// A route renders into the content box: the terminal less the gutter on the
	// right, which the Fit below fills with spaces. The left gutter stays with
	// the surface, because a list draws its selection marker in it.
	content := frame.Body
	content.W = layout.BoxWidth(content.W)
	body := layout.Fit(m.routes.Top().View(m.deps, content), frame.Body.W, frame.Body.H)
	if !m.overlays.Empty() {
		// The modal is composited into the body so the chrome stays visible and
		// the user keeps their bearings.
		body = m.overlays.View(body, frame.Body.W, frame.Body.H)
	}

	footer := renderFooter(m.deps, frame.Footer, m.footerBindings(), m.status, m.statusErr, m.hintMode())

	return header + "\n" + body + "\n" + footer
}

// routeLoading reports whether the screen on top is waiting for data.
func (m Model) routeLoading() bool {
	if l, ok := m.routes.Top().(loadingRoute); ok {
		return l.Loading()
	}
	return false
}

// hintMode reports what owns the keyboard, which is what the footer's pinned
// global keys depend on.
func (m Model) hintMode() hintMode {
	switch {
	case m.overlays.Empty():
		return hintsRoute
	case m.overlays.TakesText():
		return hintsText
	default:
		return hintsModal
	}
}

// footerBindings describes the surface that owns the keyboard: the topmost
// overlay when one is open, otherwise the route on screen, minus the actions
// that are not available right now.
func (m Model) footerBindings() []keymap.Binding {
	if open := m.overlays.Bindings(); len(open) > 0 {
		return open
	}
	top := m.routes.Top()
	gate, gated := top.(actionGate)
	bindings := make([]keymap.Binding, 0, 8)
	for _, b := range keymap.For(top.Scope()) {
		if gated && !gate.Available(b.Action) {
			continue
		}
		bindings = append(bindings, b)
	}
	return bindings
}

// errorTitle summarises a startup failure.
func (m Model) errorTitle() string {
	switch {
	case errors.Is(m.err, store.ErrForbidden):
		return "Access denied"
	case errors.Is(m.err, store.ErrRateLimited):
		return "Rate limited"
	default:
		return "Cannot reach GitHub"
	}
}

// errorDetail explains a startup failure without printing a token or a path.
func (m Model) errorDetail() string {
	detail := m.err.Error()
	switch {
	case errors.Is(m.err, store.ErrForbidden):
		detail += "\n\nThe token was rejected. Check that it has not expired, and that it\n" +
			"carries the repo and workflow scopes."
	case errors.Is(m.err, store.ErrRateLimited):
		detail += "\n\nWait for the limit to reset, then retry."
	}
	if m.deps.TokenSource != "" {
		detail += "\n\nToken source: " + m.deps.TokenSource
	}
	return detail
}
