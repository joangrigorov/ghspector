package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/overlay"
	"github.com/joangrigorov/ghspector/internal/store"
)

// mergeTarget is the pull request a merge or a close applies to.
//
// It is built from the route's own state and captured by the command, so an
// overlay never has to reach back into a route, and a merge that lands after
// the user moved on still applies to the pull request it was confirmed for.
type mergeTarget struct {
	ref    store.RepoRef
	number int
	title  string
	body   string
}

// mergeCmd performs the merge with the chosen method.
func (t mergeTarget) mergeCmd(deps Deps, method string) tea.Cmd {
	ctx := deps.Ctx
	return func() tea.Msg {
		err := deps.Store.Merge(ctx, t.ref, t.number, method, t.title, t.body)
		return mergeDoneMsg{number: t.number, err: err}
	}
}

// closeCmd closes the pull request without merging it.
func (t mergeTarget) closeCmd(deps Deps) tea.Cmd {
	ctx := deps.Ctx
	return func() tea.Msg {
		err := deps.Store.ClosePR(ctx, t.ref, t.number)
		return closeDoneMsg{number: t.number, err: err}
	}
}

// mergeMethodOverlay is the first step of a merge: choose the method. It is a
// Method overlay rather than a Menu because two of its keys act on the overlay
// instead of closing it: `d` changes the persisted default, and Enter accepts
// it (spec §4.19).
func mergeMethodOverlay(deps Deps, target mergeTarget, current string) overlay.Overlay {
	return overlay.Method{
		Scope:   keymap.MergeMethod,
		Heading: "Merge pull request",
		Intro:   []string{"Choose how to merge this pull request."},
		Options: []overlay.Option{
			{Action: keymap.ActionMergeSquash, Value: "squash", Label: "Squash and merge"},
			{Action: keymap.ActionMergeMerge, Value: "merge", Label: "Create a merge commit"},
			{Action: keymap.ActionMergeRebase, Value: "rebase", Label: "Rebase and merge"},
		},
		Default: current,
		OnPick: func(method string) tea.Cmd {
			return func() tea.Msg {
				return overlay.ReplaceMsg{Overlay: mergeConfirmOverlay(deps, target, method)}
			}
		},
		OnSubmit: func() tea.Msg {
			return overlay.ReplaceMsg{Overlay: mergeConfirmOverlay(deps, target, current)}
		},
		OnCycleDefault: func(method string) tea.Cmd {
			return saveMergeMethod(deps, method)
		},
	}
}

// saveMergeMethod persists a new default merge method and says what happened.
// The route is told as well, so reopening the overlay shows the new default
// rather than the one the session started with.
func saveMergeMethod(deps Deps, method string) tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return mergeDefaultMsg{method: method} },
		func() tea.Msg {
			if deps.SaveMergeMethod == nil {
				return StatusMsg{Text: "Default merge method: " + method + " (this session only)."}
			}
			if err := deps.SaveMergeMethod(method); err != nil {
				return StatusMsg{Text: "Default merge method: " + method + " (could not save: " + err.Error() + ")", IsErr: true}
			}
			return StatusMsg{Text: "Default merge method saved: " + method + "."}
		},
	)
}

// mergeConfirmOverlay is the second step: the confirmation for the chosen
// method, with the loading message the spec names.
func mergeConfirmOverlay(deps Deps, target mergeTarget, method string) overlay.Overlay {
	return overlay.Confirm{
		Scope:   keymap.MergeConfirm,
		Heading: "Confirm merge",
		Body: []string{
			"Merge pull request #" + itoa(target.number) + " with " + mergeMethodLabel(method) + "?",
			mergeMethodNote(method),
		},
		OnYes: tea.Batch(status("Merging pull request..."), target.mergeCmd(deps, method)),
	}
}

// closeConfirmOverlay is the confirmation for closing a pull request without
// merging it.
func closeConfirmOverlay(deps Deps, target mergeTarget) overlay.Overlay {
	return overlay.Confirm{
		Scope:   keymap.CloseConfirm,
		Heading: "Confirm close",
		Body: []string{
			"Close pull request #" + itoa(target.number) + " without merging it?",
			"The branch is left as it is, and the pull request can be reopened.",
		},
		OnYes: tea.Batch(status("Closing pull request..."), target.closeCmd(deps)),
	}
}

// mergeMethodLabel names a merge method the way a person reads it.
func mergeMethodLabel(method string) string {
	switch method {
	case "merge":
		return "a merge commit"
	case "rebase":
		return "a rebase"
	default:
		return "a squash commit"
	}
}

// mergeMethodNote explains what the chosen method does to the history, because
// the difference is the reason to choose one.
func mergeMethodNote(method string) string {
	switch method {
	case "merge":
		return "Every commit of the branch is kept, under one merge commit."
	case "rebase":
		return "Every commit of the branch is replayed on the base, without a merge commit."
	default:
		return "The branch is combined into a single commit on the base."
	}
}
