package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/overlay"
	"github.com/joangrigorov/ghspector/internal/store"
)

// FilterKind names the filter an overlay set.
type FilterKind int

const (
	// FilterActor narrows the run list to one login.
	FilterActor FilterKind = iota
	// FilterRepo narrows every list to one repository by exact name.
	FilterRepo
	// FilterAuthor, FilterAssignee and FilterReviewer are the pull-request and
	// issue filters, which arrive with those tabs.
	FilterAuthor
	FilterAssignee
	FilterReviewer
)

// String names a filter for a status message.
func (k FilterKind) String() string {
	switch k {
	case FilterActor:
		return "actor"
	case FilterRepo:
		return "repository"
	case FilterAuthor:
		return "author"
	case FilterAssignee:
		return "assignee"
	case FilterReviewer:
		return "reviewer"
	}
	return "filter"
}

// FilterMsg reports a filter chosen in an overlay.
//
// Overlays cannot reach a route directly — the stack hands them a width and
// nothing else — so a choice that outlives the overlay is delivered as a
// message. The root forwards it to the route on top, which owns the filter.
type FilterMsg struct {
	Kind  FilterKind
	Value string
}

// applyFilter reports the chosen filter and closes every open overlay, which is
// what picking a value in a nested picker means.
func applyFilter(kind FilterKind, value string) tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return overlay.CloseAllMsg{} },
		func() tea.Msg { return FilterMsg{Kind: kind, Value: value} },
	)
}

// filterMenu is the filter-type overlay: which filters this tab offers, and
// what picking one opens.
//
// The actor prompt is pre-filled with the filter already in force, so opening
// it shows the current value instead of an empty box that looks like the filter
// was lost.
func filterMenu(deps Deps, actorFilter string) overlay.Overlay {
	choices := []overlay.Choice{
		{Action: keymap.ActionFilterUser, Label: "Filter by user (actor)"},
		{Action: keymap.ActionFilterRepo, Label: "Filter by repository"},
	}
	return overlay.Menu{
		Scope:   keymap.FilterType,
		Heading: "Filter",
		Intro:   []string{"Narrow the workflow runs on this screen."},
		Choices: choices,
		OnPick: func(action keymap.Action) tea.Cmd {
			switch action {
			case keymap.ActionFilterUser:
				return func() tea.Msg { return overlay.PushMsg{Overlay: actorPrompt(deps, actorFilter)} }
			case keymap.ActionFilterRepo:
				return tea.Batch(status("Loading repositories..."), loadRepos(deps))
			}
			return nil
		},
	}
}

// loadRepos fetches the context's repositories and opens the picker with them.
//
// The list is fetched when the picker is opened rather than with the tab: the
// picker is opened rarely and the run list always, so the repositories are not
// worth a request on every startup.
func loadRepos(deps Deps) tea.Cmd {
	owner := deps.Account.Owner
	ctx := deps.Ctx
	return func() tea.Msg {
		list, err := deps.Store.Repositories(ctx, owner, store.Page{Number: 1, Size: repoPageSize})
		if err != nil {
			return StatusMsg{Text: "Could not load repositories: " + err.Error(), IsErr: true}
		}
		return overlay.PushMsg{Overlay: repoPicker(deps, list.Items)}
	}
}

// repoPageSize is how many repositories the picker lists, matching the previous
// version's repository list.
const repoPageSize = 100

// actorPrompt is the text box for the actor filter.
func actorPrompt(deps Deps, current string) overlay.Overlay {
	input := newPromptInput(deps.Theme)
	input.SetValue(current)

	return overlay.Prompt{
		Scope:   keymap.UserInput,
		Heading: "Filter by user",
		Intro: []string{
			"Show only runs started by this GitHub login.",
			"Current actor filter: " + displayFilter(current) + ".",
		},
		Input: input,
		OnSubmit: func(value string) tea.Cmd {
			return applyFilter(FilterActor, value)
		},
		// Esc leaves the whole filter flow, matching the overlay chain the user
		// opened from the list.
		OnCancel: func() tea.Msg { return overlay.CloseAllMsg{} },
	}
}

// repoPicker is the repository list overlay. When the repositories are not
// loaded yet the caller passes nil and the picker loads them first, so the user
// never sees an empty chooser for a repository list that is merely unfetched.
func repoPicker(deps Deps, repos []store.Repository) overlay.Overlay {
	rows := make([]string, 0, len(repos))
	for _, r := range repos {
		rows = append(rows, r.Name)
	}

	// The overlay stack gives a modal a width but no height, so the row budget
	// comes from the terminal size the route was built with.
	maxRows := deps.Size.Height - 10
	if maxRows < 3 {
		maxRows = 3
	}

	l := overlay.List{
		Scope:    keymap.RepoSelect,
		Heading:  "Filter by repository",
		Rows:     rows,
		MaxRows:  maxRows,
		OnSelect: func(i int) tea.Cmd { return applyFilter(FilterRepo, repos[i].Name) },
		OnCancel: nil,
	}
	if len(rows) == 0 {
		l.Empty = "No repositories loaded yet."
		l.Intro = []string{"The repository list has not loaded."}
	}
	return l
}

// displayFilter renders a filter value for a message.
func displayFilter(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
