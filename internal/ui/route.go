package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
	"ghspector/internal/overlay"
	"ghspector/internal/store"
)

// Deps is the shared service set handed to a route on every call. Passing it in
// rather than storing it on the route means a route can never hold a stale
// store or a stale account after a context switch.
type Deps struct {
	Store store.Store
	Theme Theme
	// Ctx bounds every request a route starts. It is the program's context, so
	// a route that is popped stops waiting on a reply nobody will read.
	Ctx     context.Context
	Account Account
	Version string
	// ConfigPath is a display label, already reduced to a basename so a screen
	// never prints the user's home directory.
	ConfigPath string
	// TokenSource describes where the token came from, without a path.
	TokenSource string
	// Clock paces polling. Nil means the real clock.
	Clock Clock
	// PollWorkflows and PollPRs are the refresh intervals from the config file.
	// A non-positive interval turns polling off for that tab.
	PollWorkflows time.Duration
	PollPRs       time.Duration
	// Size is the terminal geometry. A route that opens a modal needs it,
	// because an overlay is given a width by the stack but never a height.
	Size Size
	// MergeMethod is the configured default merge method, already resolved to
	// one of squash, merge or rebase.
	MergeMethod string
	// SaveMergeMethod persists a new default merge method. Nil means there is
	// no config file to write, and the choice then lasts for the session.
	SaveMergeMethod func(method string) error
}

// Size is the terminal geometry in cells.
type Size struct {
	Width  int
	Height int
}

// clock returns the time source to use, substituting the real clock so callers
// never have to check.
func (d Deps) clock() Clock {
	if d.Clock == nil {
		return RealClock{}
	}
	return d.Clock
}

// Account is the identity the session acts as.
type Account struct {
	// Login is the authenticated user.
	Login string
	// Owner is the account whose repositories the lists cover. It is the user
	// themselves until the switcher picks an organization.
	Owner store.OwnerRef
}

// Route is one screen in the navigation stack.
type Route interface {
	// Title is the breadcrumb label.
	Title() string
	// Scope is the keymap scope active while this route is on top. The root
	// resolves key presses against it, falling back to the global scope.
	Scope() keymap.Scope
	// Update handles a message the action dispatch did not consume: data load
	// results, and raw key presses for routes that take text input.
	Update(deps Deps, msg tea.Msg) (Route, tea.Cmd)
	// Act handles a keymap action. It reports whether the action was consumed;
	// an unconsumed action falls through to the root's global handling, which
	// is how `?`, `o` and `q` keep working on every screen.
	Act(deps Deps, action keymap.Action) (Route, tea.Cmd, bool)
	// View renders the route into r. The rect is authoritative: a route draws
	// within it and never derives its own size.
	View(deps Deps, r layout.Rect) string
}

// RouteStack is the navigation stack. The last element is on screen.
type RouteStack struct {
	routes []Route
}

// Stoppable is implemented by a route that owns requests in flight. The root
// stops a route as it leaves the stack, so a fetch for a screen nobody is
// looking at is abandoned rather than allowed to finish and land in state that
// has been discarded.
type Stoppable interface {
	Stop()
}

// actionGate narrows the footer to the actions that are available right now.
// A route implements it when one of its keys depends on state — approving a run
// that is not waiting, for example — so the footer never advertises a key that
// does nothing.
type actionGate interface {
	Available(action keymap.Action) bool
}

// pushRouteThen asks the root to open a route above the current one. A route
// returns one of these from a command, because the stack belongs to the root.
//
// A route that fetches as it appears uses this rather than batching a push with
// its request: batched commands run concurrently, so the reply could arrive
// before the route that asked for it.
func pushRouteThen(r Route, cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg { return PushMsg{Route: r, Then: cmd} }
}

// popCmd asks the root to close the topmost route.
func popCmd() tea.Cmd {
	return func() tea.Msg { return PopMsg{} }
}

// pushOverlay asks the root to open a modal above the current one.
func pushOverlay(o overlay.Overlay) tea.Cmd {
	return func() tea.Msg { return overlay.PushMsg{Overlay: o} }
}

// session is the cancellable request scope of one route instance. A route that
// fetches holds one; the root cancels it on pop.
type session struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// newSession derives a cancellable scope from the program's context.
func newSession(parent context.Context) *session {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &session{ctx: ctx, cancel: cancel}
}

// Ctx is the context to hand to store calls.
func (s *session) Ctx() context.Context {
	if s == nil || s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// Stop cancels the session's requests. It is safe to call more than once.
func (s *session) Stop() {
	if s == nil || s.cancel == nil {
		return
	}
	s.cancel()
}

// stopRoute releases a route that has left the stack.
func stopRoute(r Route) {
	if s, ok := r.(Stoppable); ok {
		s.Stop()
	}
}

// NewRouteStack returns a stack containing root, which is never popped.
func NewRouteStack(root Route) RouteStack {
	return RouteStack{routes: []Route{root}}
}

// Len is the number of routes on the stack.
func (s RouteStack) Len() int { return len(s.routes) }

// Top returns the route on screen. A stack always has at least one route.
func (s RouteStack) Top() Route { return s.routes[len(s.routes)-1] }

// Push returns a stack with r on top.
func (s RouteStack) Push(r Route) RouteStack {
	if r == nil {
		return s
	}
	next := make([]Route, len(s.routes), len(s.routes)+1)
	copy(next, s.routes)
	return RouteStack{routes: append(next, r)}
}

// Pop returns a stack without its topmost route. The root route is retained, so
// the stack is never empty.
func (s RouteStack) Pop() RouteStack {
	if len(s.routes) <= 1 {
		return s
	}
	next := make([]Route, len(s.routes)-1)
	copy(next, s.routes)
	return RouteStack{routes: next}
}

// PopRoute returns the stack without its topmost route, and the route that was
// removed so the caller can release whatever it holds.
func (s RouteStack) PopRoute() (RouteStack, Route) {
	if len(s.routes) <= 1 {
		return s, nil
	}
	return s.Pop(), s.routes[len(s.routes)-1]
}

// AboveRoot returns the routes that PopToRoot would remove, outermost last.
func (s RouteStack) AboveRoot() []Route {
	if len(s.routes) <= 1 {
		return nil
	}
	out := make([]Route, len(s.routes)-1)
	copy(out, s.routes[1:])
	return out
}

// Replace swaps the topmost route, which is how a route returns its new state.
func (s RouteStack) Replace(r Route) RouteStack {
	if r == nil {
		return s
	}
	next := make([]Route, len(s.routes))
	copy(next, s.routes)
	next[len(next)-1] = r
	return RouteStack{routes: next}
}

// PopToRoot returns a stack containing only the root route. The help and
// switcher routes use it so leaving them lands on the main screen regardless of
// how deep the user had drilled.
func (s RouteStack) PopToRoot() RouteStack {
	return RouteStack{routes: s.routes[:1]}
}

// Titles returns the breadcrumb labels, root first.
func (s RouteStack) Titles() []string {
	out := make([]string, 0, len(s.routes))
	for _, r := range s.routes {
		out = append(out, r.Title())
	}
	return out
}

// Broadcast delivers a message to every route, root first. It returns a new
// stack so a shared backing array is never mutated in place.
func (s RouteStack) Broadcast(deps Deps, msg tea.Msg) (RouteStack, tea.Cmd) {
	next := make([]Route, len(s.routes))
	var cmds []tea.Cmd
	for i, r := range s.routes {
		updated, cmd := r.Update(deps, msg)
		if updated == nil {
			updated = r
		}
		next[i] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return RouteStack{routes: next}, tea.Batch(cmds...)
}

// Navigation messages. A route asks for a stack change by returning one of
// these from a command, which keeps the Route interface free of stack
// bookkeeping.
type (
	// PushMsg opens a route above the current one. Then, when set, is run once
	// the route is on the stack.
	PushMsg struct {
		Route Route
		Then  tea.Cmd
	}
	// PopMsg closes the topmost route.
	PopMsg struct{}
	// ReplaceMsg swaps the topmost route.
	ReplaceMsg struct{ Route Route }
	// PopToRootMsg closes every route above the root.
	PopToRootMsg struct{}
	// StatusMsg shows a transient message in the footer. IsErr marks a failure,
	// which the footer renders in the error colour.
	StatusMsg struct {
		Text  string
		IsErr bool
	}
	// ContextSelectedMsg asks the root to switch the active owner.
	ContextSelectedMsg struct{ Owner store.OwnerRef }
	// AccountChangedMsg tells every route that the active context changed, so
	// it can drop data that belonged to the previous one.
	AccountChangedMsg struct{}
)

// joinLines joins lines with newlines. Kept here so routes do not each import
// strings for the same one-liner.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	for _, line := range lines[1:] {
		out += "\n" + line
	}
	return out
}
