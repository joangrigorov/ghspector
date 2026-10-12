package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"ghspector/internal/keymap"
	"ghspector/internal/overlay"
)

// modalModel opens a merge confirmation over m, so the composited frame has a
// golden of its own.
func modalModel(t *testing.T, m Model) Model {
	t.Helper()
	m.overlays = m.overlays.Push(overlay.Confirm{
		Scope:   keymap.MergeConfirm,
		Heading: "Confirm merge",
		Body:    []string{"This will squash-merge pull request #16 and delete the branch."},
		OnYes:   func() tea.Msg { return nil },
	})
	return m
}

// TestGoldenScreens pins the rendered output of every screen at the canonical
// sizes.
//
// 40x15 is below the supported minimum (50x12), so its golden is the too-small
// message. 60x16 stands in for a narrow but supported terminal.
//
// Regenerate with:
//
//	go test ./internal/ui -run TestGoldenScreens -update
func TestGoldenScreens(t *testing.T) {
	srv := ghServer(t, defaultFixtures())

	// The Workflows tab is the one shell screen that needs run data: its
	// fixture server answers the startup requests as well as the run lists.
	wfSrv := workflowServer(t, fixtureRuns(8))

	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
	}))
	t.Cleanup(forbidden.Close)

	cases := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"splash_80x24", func(t *testing.T) Model { return newModel(t, srv, 80, 24) }},
		{"splash_120x40", func(t *testing.T) Model { return newModel(t, srv, 120, 40) }},
		{"main_prs_80x24", func(t *testing.T) Model { return readyModel(t, srv, 80, 24) }},
		{"main_prs_120x40", func(t *testing.T) Model { return readyModel(t, srv, 120, 40) }},
		{"main_prs_60x16", func(t *testing.T) Model { return readyModel(t, srv, 60, 16) }},
		{"main_workflows_80x24", func(t *testing.T) Model {
			return workflowsModel(t, wfSrv, 80, 24)
		}},
		{"main_issues_80x24", func(t *testing.T) Model {
			return typeKeys(t, readyModel(t, srv, 80, 24), "tab", "tab")
		}},
		{"help_80x24", func(t *testing.T) Model {
			return typeKeys(t, readyModel(t, srv, 80, 24), "?")
		}},
		{"help_120x40", func(t *testing.T) Model {
			return typeKeys(t, readyModel(t, srv, 120, 40), "?")
		}},
		{"switcher_80x24", func(t *testing.T) Model {
			return typeKeys(t, readyModel(t, srv, 80, 24), "o")
		}},
		{"switcher_80x24_second_entry", func(t *testing.T) Model {
			return typeKeys(t, readyModel(t, srv, 80, 24), "o", "j")
		}},
		{"too_small_40x15", func(t *testing.T) Model { return readyModel(t, srv, 40, 15) }},
		{"error_80x24", func(t *testing.T) Model {
			m := newModel(t, forbidden, 80, 24)
			next, _ := send(t, m, m.loadIdentity()())
			return next
		}},
		{"modal_80x24", func(t *testing.T) Model {
			m := readyModel(t, srv, 80, 24)
			return modalModel(t, m)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden.RequireEqual(t, tc.build(t).View().Content)
		})
	}
}

// TestGoldenWorkflowScreens pins the screens the workflow phase adds: the run
// list with data, the job list, the log viewer, and the overlays they open.
//
// The fixture server answers the run, job, log and approval endpoints, and the
// clock is fixed, so every duration on screen is a function of the fixtures.
//
// Regenerate with:
//
//	go test ./internal/ui -run TestGoldenWorkflowScreens -update
func TestGoldenWorkflowScreens(t *testing.T) {
	srv := workflowServer(t, fixtureRuns(8))
	waiting := workflowServer(t, waitingFixture())

	cases := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"workflows_120x40", func(t *testing.T) Model {
			return workflowsModel(t, srv, 120, 40)
		}},
		{"workflows_filter_mine_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, workflowsModel(t, srv, 80, 24), "m")
		}},
		{"jobs_80x24", func(t *testing.T) Model {
			return openJobs(t, workflowsModel(t, srv, 80, 24))
		}},
		{"jobs_120x40", func(t *testing.T) Model {
			return openJobs(t, workflowsModel(t, srv, 120, 40))
		}},
		// A run paused on an environment draws the approval banner in place of
		// the job table.
		{"jobs_approval_80x24", func(t *testing.T) Model {
			return openJobs(t, workflowsModel(t, waiting, 80, 24))
		}},
		{"approve_run_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, openJobs(t, workflowsModel(t, waiting, 80, 24)), "a")
		}},
		// The viewer opens on the step that failed rather than on the first one.
		{"logs_100x30", func(t *testing.T) Model {
			return openLogs(t, 100, 30, srv)
		}},
		{"filter_menu_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, workflowsModel(t, srv, 80, 24), "f")
		}},
		{"actor_prompt_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, workflowsModel(t, srv, 80, 24), "f", "u")
		}},
		{"repo_picker_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, workflowsModel(t, srv, 80, 24), "f", "r")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden.RequireEqual(t, tc.build(t).View().Content)
		})
	}
}

// TestGoldenResizeDoesNotLeaveArtifacts checks that a resize redraws cleanly:
// the frame at the new size must equal a frame built at that size directly.
func TestGoldenResizeDoesNotLeaveArtifacts(t *testing.T) {
	srv := ghServer(t, defaultFixtures())

	reshaped := readyModel(t, srv, 120, 40)
	for _, size := range [][2]int{{80, 24}, {60, 16}, {100, 30}, {120, 40}} {
		next, _ := send(t, reshaped, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		reshaped = next
		fresh := readyModel(t, srv, size[0], size[1])
		if reshaped.View().Content != fresh.View().Content {
			t.Fatalf("a resized frame at %dx%d differs from a frame built at that size", size[0], size[1])
		}
	}
}
