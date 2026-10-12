package ui

import (
	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
	"ghspector/internal/store"
)

// Deps is the shared service set handed to a route on every call. Passing it in
// rather than storing it on the route means a route can never hold a stale
// store or a stale account after a context switch.
type Deps struct {
	Store   store.Store
	Theme   Theme
	Account Account
	Version string
	// ConfigPath is a display label, already reduced to a basename so a screen
	// never prints the user's home directory.
	ConfigPath string
	// TokenSource describes where the token came from, without a path.
	TokenSource string
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
	// PushMsg opens a route above the current one.
	PushMsg struct{ Route Route }
	// PopMsg closes the topmost route.
	PopMsg struct{}
	// ReplaceMsg swaps the topmost route.
	ReplaceMsg struct{ Route Route }
	// PopToRootMsg closes every route above the root.
	PopToRootMsg struct{}
	// StatusMsg shows a transient message in the footer.
	StatusMsg struct{ Text string }
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
