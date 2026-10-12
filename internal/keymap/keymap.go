// Package keymap is the single source of truth for keyboard bindings.
//
// The previous TUI declared each binding up to three times: once in the update
// switch, once in the footer's hand-written key slice, and once in the help
// text. The three drifted, which is how the Jobs view ended up advertising `w`
// and `v` as the reverse of what the code did.
//
// Here a binding exists exactly once. Routes dispatch by looking up an Action,
// the footer renders from the same table, the help overlay renders from the same
// table, and a test asserts the table matches docs/spec.md.
//
// This package is pure data and lookup. It does no rendering and imports no
// styling, so it stays cheap to test.
package keymap

import (
	tea "charm.land/bubbletea/v2"
)

// Scope identifies the set of bindings active at a given moment. A route
// reports its scope and its overlays shadow it.
type Scope string

// Scopes, in help-overlay order. Overlay scopes are listed separately because
// they shadow a route rather than replacing it.
const (
	Global Scope = "global"

	MainWorkflows Scope = "main.workflows"
	MainPRs       Scope = "main.prs"
	MainIssues    Scope = "main.issues"

	Jobs Scope = "jobs"
	Logs Scope = "logs"

	PRDetails     Scope = "pr.details"
	PRCommits     Scope = "pr.commits"
	CommitDetails Scope = "pr.commit"
	PRDiff        Scope = "pr.diff"
	PRComments    Scope = "pr.comments"

	IssueDetails  Scope = "issue.details"
	IssueComments Scope = "issue.comments"

	Switcher Scope = "switcher"
	Help     Scope = "help"

	FilterType      Scope = "overlay.filterType"
	RepoSelect      Scope = "overlay.repoSelect"
	UserInput       Scope = "overlay.userInput"
	PRFilterType    Scope = "overlay.prFilterType"
	IssueFilterType Scope = "overlay.issueFilterType"
	MergeMethod     Scope = "overlay.mergeMethod"
	MergeConfirm    Scope = "overlay.mergeConfirm"
	CloseConfirm    Scope = "overlay.closeConfirm"
	ApproveRun      Scope = "overlay.approveRun"
)

// Action is a stable identifier for something the user asked for. A route (or
// the root model) maps an Action to behaviour; the keymap never does.
type Action string

const (
	ActionQuit        Action = "quit"
	ActionHelp        Action = "help"
	ActionSwitchCtx   Action = "switch-context"
	ActionNextTab     Action = "next-tab"
	ActionPrevTab     Action = "prev-tab"
	ActionBack        Action = "back"
	ActionUp          Action = "up"
	ActionDown        Action = "down"
	ActionOpen        Action = "open"
	ActionSelect      Action = "select"
	ActionSubmit      Action = "submit"
	ActionConfirm     Action = "confirm"
	ActionRefresh     Action = "refresh"
	ActionScrollUp    Action = "scroll-up"
	ActionScrollDown  Action = "scroll-down"
	ActionNextStep    Action = "next-step"
	ActionPrevStep    Action = "prev-step"
	ActionNextAttempt Action = "next-attempt"
	ActionPrevAttempt Action = "prev-attempt"
	ActionToggleFocus Action = "toggle-focus"
	ActionOpenBrowser Action = "open-browser"
	ActionApprove     Action = "approve"
	ActionMerge       Action = "merge"
	ActionClose       Action = "close-pr"
	ActionComments    Action = "comments"
	ActionCommits     Action = "commits"
	ActionDiff        Action = "diff"

	ActionFilterType     Action = "filter-type"
	ActionFilterMine     Action = "filter-mine"
	ActionFilterAuthor   Action = "filter-author"
	ActionFilterAssignee Action = "filter-assignee"
	ActionFilterReviewer Action = "filter-reviewer"
	ActionFilterUser     Action = "filter-by-user"
	ActionFilterRepo     Action = "filter-by-repo"
	ActionCycleState     Action = "cycle-state"
	ActionClearFilters   Action = "clear-filters"

	ActionMergeSquash  Action = "merge-squash"
	ActionMergeMerge   Action = "merge-merge"
	ActionMergeRebase  Action = "merge-rebase"
	ActionCycleDefault Action = "merge-cycle-default"
)

// Binding is one user intent and the keys that trigger it.
type Binding struct {
	Action Action
	// Keys are the values reported by KeyString, which is not always what
	// tea.KeyPressMsg.String returns; see KeyString.
	Keys []string
	// Help is the footer/help description. Empty means the binding is
	// intentionally undocumented (internal navigation aliases).
	Help string
	// Destructive marks bindings that change remote state, so the help
	// overlay can flag them.
	Destructive bool
}

// table holds every binding, keyed by scope.
var table = map[Scope][]Binding{
	Global: {
		{Action: ActionHelp, Keys: []string{"?"}, Help: "Help"},
		{Action: ActionSwitchCtx, Keys: []string{"o"}, Help: "Switch account/org"},
		{Action: ActionQuit, Keys: []string{"q", "ctrl+c"}, Help: "Quit"},
	},

	MainWorkflows: {
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Move down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Move up"},
		{Action: ActionOpen, Keys: []string{"enter"}, Help: "View jobs"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
		{Action: ActionApprove, Keys: []string{"a"}, Help: "Approve waiting run", Destructive: true},
		{Action: ActionFilterMine, Keys: []string{"m"}, Help: "Filter to my runs"},
		{Action: ActionFilterType, Keys: []string{"f"}, Help: "Filter"},
		{Action: ActionClearFilters, Keys: []string{"x"}, Help: "Clear filters"},
		{Action: ActionOpenBrowser, Keys: []string{"w"}, Help: "Open in browser"},
		{Action: ActionNextTab, Keys: []string{"tab"}, Help: "Next tab"},
		{Action: ActionPrevTab, Keys: []string{"shift+tab"}, Help: "Previous tab"},
	},

	MainPRs: {
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Move down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Move up"},
		{Action: ActionOpen, Keys: []string{"enter"}, Help: "View details"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
		{Action: ActionCycleState, Keys: []string{"s"}, Help: "Cycle state (open/closed/all)"},
		{Action: ActionFilterAuthor, Keys: []string{"a"}, Help: "Filter by author (me)"},
		{Action: ActionFilterAssignee, Keys: []string{"i"}, Help: "Filter by assignee (me)"},
		{Action: ActionFilterReviewer, Keys: []string{"v"}, Help: "Filter by reviewer (me)"},
		{Action: ActionFilterType, Keys: []string{"f"}, Help: "Filter"},
		{Action: ActionClearFilters, Keys: []string{"x"}, Help: "Clear filters"},
		{Action: ActionOpenBrowser, Keys: []string{"w"}, Help: "Open in browser"},
		{Action: ActionNextTab, Keys: []string{"tab"}, Help: "Next tab"},
		{Action: ActionPrevTab, Keys: []string{"shift+tab"}, Help: "Previous tab"},
	},

	MainIssues: {
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Move down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Move up"},
		{Action: ActionOpen, Keys: []string{"enter"}, Help: "View details"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
		{Action: ActionCycleState, Keys: []string{"s"}, Help: "Cycle state (open/closed/all)"},
		{Action: ActionFilterAuthor, Keys: []string{"a"}, Help: "Filter by author (me)"},
		{Action: ActionFilterAssignee, Keys: []string{"i"}, Help: "Filter by assignee (me)"},
		{Action: ActionFilterType, Keys: []string{"f"}, Help: "Filter"},
		{Action: ActionClearFilters, Keys: []string{"x"}, Help: "Clear filters"},
		{Action: ActionOpenBrowser, Keys: []string{"w"}, Help: "Open in browser"},
		{Action: ActionNextTab, Keys: []string{"tab"}, Help: "Next tab"},
		{Action: ActionPrevTab, Keys: []string{"shift+tab"}, Help: "Previous tab"},
	},

	Jobs: {
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Move down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Move up"},
		{Action: ActionOpen, Keys: []string{"enter"}, Help: "View logs"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back"},
		{Action: ActionApprove, Keys: []string{"a"}, Help: "Approve waiting run", Destructive: true},
		// The code's mapping is authoritative here (spec D3): w opens the
		// highlighted job, v opens the parent run.
		{Action: ActionOpenBrowser, Keys: []string{"w", "W"}, Help: "Open job in browser"},
		{Action: ActionOpen, Keys: []string{"v", "V"}, Help: "Open run in browser"},
		{Action: ActionPrevAttempt, Keys: []string{"["}, Help: "Previous attempt"},
		{Action: ActionNextAttempt, Keys: []string{"]"}, Help: "Next attempt"},
	},

	Logs: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
		{Action: ActionNextStep, Keys: []string{"j", "down"}, Help: "Next step"},
		{Action: ActionPrevStep, Keys: []string{"k", "up"}, Help: "Previous step"},
		{Action: ActionScrollUp, Keys: []string{"u", "pgup"}, Help: "Scroll up"},
		{Action: ActionScrollDown, Keys: []string{"d", "pgdown"}, Help: "Scroll down"},
		{Action: ActionOpenBrowser, Keys: []string{"w", "W"}, Help: "Open job in browser"},
	},

	PRDetails: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back to list"},
		{Action: ActionToggleFocus, Keys: []string{"tab", "shift+tab"}, Help: "Switch focus"},
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Up"},
		{Action: ActionScrollUp, Keys: []string{"u"}, Help: "Scroll description up"},
		{Action: ActionScrollDown, Keys: []string{"d"}, Help: "Scroll description down"},
		{Action: ActionOpen, Keys: []string{"enter"}, Help: "Open check"},
		{Action: ActionOpenBrowser, Keys: []string{"w"}, Help: "Open in browser"},
		{Action: ActionMerge, Keys: []string{"m"}, Help: "Merge", Destructive: true},
		{Action: ActionComments, Keys: []string{"c"}, Help: "Comments"},
		{Action: ActionClose, Keys: []string{"C"}, Help: "Close PR", Destructive: true},
		{Action: ActionCommits, Keys: []string{"v"}, Help: "Commits"},
		{Action: ActionDiff, Keys: []string{"D"}, Help: "Diff"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
	},

	PRCommits: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back"},
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Move down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Move up"},
		{Action: ActionOpen, Keys: []string{"enter"}, Help: "View commit"},
	},

	CommitDetails: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back"},
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Next file"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Previous file"},
		{Action: ActionScrollUp, Keys: []string{"u"}, Help: "Scroll diff up"},
		{Action: ActionScrollDown, Keys: []string{"d"}, Help: "Scroll diff down"},
		{Action: ActionOpenBrowser, Keys: []string{"w"}, Help: "Open in browser"},
	},

	PRDiff: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back"},
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Next file"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Previous file"},
		{Action: ActionScrollUp, Keys: []string{"u"}, Help: "Scroll up"},
		{Action: ActionScrollDown, Keys: []string{"d"}, Help: "Scroll down"},
		{Action: ActionOpenBrowser, Keys: []string{"w"}, Help: "Open in browser"},
	},

	PRComments: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
	},

	IssueDetails: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back to list"},
		{Action: ActionToggleFocus, Keys: []string{"tab", "shift+tab"}, Help: "Switch focus"},
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Up"},
		{Action: ActionScrollUp, Keys: []string{"u"}, Help: "Scroll up"},
		{Action: ActionScrollDown, Keys: []string{"d"}, Help: "Scroll down"},
		{Action: ActionOpenBrowser, Keys: []string{"w"}, Help: "Open in browser"},
		{Action: ActionComments, Keys: []string{"c"}, Help: "Comments"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
	},

	IssueComments: {
		{Action: ActionBack, Keys: []string{"esc", "backspace"}, Help: "Back"},
		{Action: ActionRefresh, Keys: []string{"r", "ctrl+r"}, Help: "Refresh"},
	},

	Switcher: {
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Move down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Move up"},
		{Action: ActionSelect, Keys: []string{"enter"}, Help: "Switch context"},
		{Action: ActionBack, Keys: []string{"esc"}, Help: "Close"},
	},

	Help: {
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Scroll down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Scroll up"},
		{Action: ActionBack, Keys: []string{"esc", "?"}, Help: "Close help"},
	},

	FilterType: {
		{Action: ActionFilterUser, Keys: []string{"u", "U"}, Help: "Filter by user"},
		{Action: ActionFilterRepo, Keys: []string{"r", "R"}, Help: "Filter by repository"},
		{Action: ActionBack, Keys: []string{"esc"}, Help: "Cancel"},
	},

	RepoSelect: {
		{Action: ActionDown, Keys: []string{"j", "down"}, Help: "Move down"},
		{Action: ActionUp, Keys: []string{"k", "up"}, Help: "Move up"},
		{Action: ActionSelect, Keys: []string{"enter"}, Help: "Apply repository filter"},
		{Action: ActionBack, Keys: []string{"esc"}, Help: "Back"},
	},

	UserInput: {
		{Action: ActionSubmit, Keys: []string{"enter"}, Help: "Confirm"},
		{Action: ActionBack, Keys: []string{"esc"}, Help: "Cancel"},
	},

	PRFilterType: {
		{Action: ActionFilterAuthor, Keys: []string{"a", "A"}, Help: "As author"},
		{Action: ActionFilterAssignee, Keys: []string{"i", "I"}, Help: "As assignee"},
		{Action: ActionFilterReviewer, Keys: []string{"r", "R"}, Help: "As reviewer"},
		{Action: ActionBack, Keys: []string{"esc", "c", "C"}, Help: "Cancel"},
	},

	IssueFilterType: {
		{Action: ActionFilterAuthor, Keys: []string{"a", "A"}, Help: "As author"},
		{Action: ActionFilterAssignee, Keys: []string{"i", "I"}, Help: "As assignee"},
		{Action: ActionBack, Keys: []string{"esc", "c", "C"}, Help: "Cancel"},
	},

	MergeMethod: {
		{Action: ActionMergeSquash, Keys: []string{"s", "S"}, Help: "Squash merge", Destructive: true},
		{Action: ActionMergeMerge, Keys: []string{"m", "M"}, Help: "Merge commit", Destructive: true},
		{Action: ActionMergeRebase, Keys: []string{"r", "R"}, Help: "Rebase merge", Destructive: true},
		{Action: ActionCycleDefault, Keys: []string{"d", "D"}, Help: "Change default method"},
		{Action: ActionSubmit, Keys: []string{"enter"}, Help: "Use default method"},
		{Action: ActionBack, Keys: []string{"esc", "c", "C"}, Help: "Cancel"},
	},

	MergeConfirm: {
		{Action: ActionConfirm, Keys: []string{"y", "Y"}, Help: "Yes, merge", Destructive: true},
		{Action: ActionBack, Keys: []string{"n", "N", "esc"}, Help: "Cancel"},
	},

	CloseConfirm: {
		{Action: ActionConfirm, Keys: []string{"y", "Y"}, Help: "Yes, close", Destructive: true},
		{Action: ActionBack, Keys: []string{"n", "N", "esc"}, Help: "Cancel"},
	},

	ApproveRun: {
		{Action: ActionConfirm, Keys: []string{"y", "Y", "enter"}, Help: "Yes, approve", Destructive: true},
		{Action: ActionOpenBrowser, Keys: []string{"w", "W"}, Help: "Approve on GitHub"},
		{Action: ActionBack, Keys: []string{"n", "N", "esc"}, Help: "Cancel"},
	},
}

// helpOrder is the order scopes appear in the help overlay. Only scopes with
// documentation-worthy bindings are listed.
var helpOrder = []Scope{
	Global,
	MainWorkflows,
	MainPRs,
	MainIssues,
	PRDetails,
	PRCommits,
	CommitDetails,
	PRDiff,
	PRComments,
	IssueDetails,
	IssueComments,
	Jobs,
	Logs,
	Help,
	Switcher,
	FilterType,
	RepoSelect,
	UserInput,
	PRFilterType,
	IssueFilterType,
	MergeMethod,
	MergeConfirm,
	CloseConfirm,
	ApproveRun,
}

var titles = map[Scope]string{
	Global:          "Global",
	MainWorkflows:   "Workflow runs",
	MainPRs:         "Pull requests",
	MainIssues:      "Issues",
	PRDetails:       "Pull request details",
	PRCommits:       "Pull request commits",
	CommitDetails:   "Commit details",
	PRDiff:          "Pull request diff",
	PRComments:      "Pull request comments",
	IssueDetails:    "Issue details",
	IssueComments:   "Issue comments",
	Jobs:            "Workflow jobs",
	Logs:            "Logs viewer",
	Switcher:        "Context switcher",
	Help:            "Help",
	FilterType:      "Filter options",
	RepoSelect:      "Repository filter",
	UserInput:       "User filter",
	PRFilterType:    "Filter PRs by",
	IssueFilterType: "Filter issues by",
	MergeMethod:     "Merge method",
	MergeConfirm:    "Confirm merge",
	CloseConfirm:    "Confirm close",
	ApproveRun:      "Approve run",
}

// For returns the bindings for a scope, in declaration order.
func For(scope Scope) []Binding {
	return table[scope]
}

// Title returns the human-readable name of a scope.
func Title(scope Scope) string {
	if t, ok := titles[scope]; ok {
		return t
	}
	return string(scope)
}

// Scopes returns the scopes in help-overlay order.
func Scopes() []Scope {
	return helpOrder
}

// Documented returns the bindings in a scope that carry help text.
func Documented(scope Scope) []Binding {
	out := make([]Binding, 0, len(table[scope]))
	for _, b := range table[scope] {
		if b.Help != "" {
			out = append(out, b)
		}
	}
	return out
}

// Lookup resolves a key string to an action within a scope.
func Lookup(scope Scope, key string) (Action, bool) {
	for _, b := range table[scope] {
		for _, k := range b.Keys {
			if k == key {
				return b.Action, true
			}
		}
	}
	return "", false
}

// AllActions returns every action declared in the table. Used by tests to assert
// no binding is unreachable and no action is duplicated within a scope.
func AllActions() map[Action]struct{} {
	out := make(map[Action]struct{})
	for _, bindings := range table {
		for _, b := range bindings {
			out[b.Action] = struct{}{}
		}
	}
	return out
}

// KeysFor renders the keys of a binding for display, e.g. "j/↓".
func KeysFor(b Binding) string {
	if len(b.Keys) == 0 {
		return ""
	}
	out := ""
	for i, k := range b.Keys {
		if i > 0 {
			out += "/"
		}
		out += Display(k)
	}
	return out
}

// display names replace the wire form with something a person reads.
var display = map[string]string{
	"down":      "↓",
	"up":        "↑",
	"left":      "←",
	"right":     "→",
	"esc":       "Esc",
	"enter":     "Enter",
	"tab":       "Tab",
	"shift+tab": "Shift+Tab",
	"backspace": "Backspace",
	"pgup":      "PgUp",
	"pgdown":    "PgDn",
	"ctrl+c":    "Ctrl+C",
	"ctrl+r":    "Ctrl+R",
}

// Display returns the human-readable form of a key string.
func Display(key string) string {
	if d, ok := display[key]; ok {
		return d
	}
	return key
}

// KeyString normalises a key press into the form used by the binding table.
//
// tea.KeyPressMsg.String() is the canonical form ("esc", "shift+tab", "ctrl+r"),
// but it reports an uppercase letter only when the terminal supplies the shifted
// character as Text. Terminals using the Kitty keyboard protocol may instead set
// ShiftedCode and leave Text empty, in which case String() lowercases the letter
// and a "C" binding would never fire. This recovers the uppercase form so
// case-sensitive bindings behave the same everywhere.
func KeyString(msg tea.KeyPressMsg) string {
	k := msg.Key()
	if k.Text == "" && k.ShiftedCode >= 'A' && k.ShiftedCode <= 'Z' {
		return string(k.ShiftedCode)
	}
	return msg.String()
}
