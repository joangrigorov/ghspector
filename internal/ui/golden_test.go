package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/overlay"
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
	wfSrv := workflowServer(t, fixtureRuns(9))

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
			return issuesModel(t, prServer(t), 80, 24)
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
	srv := workflowServer(t, fixtureRuns(9))
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

// TestGoldenPullRequestScreens pins the screens the pull request phase adds:
// the list with data at three widths, every sub-tab of the details view, the
// commit view, and the three overlays a merge or a close opens.
//
// The fixture server answers the pull request, commit, file, comment and check
// endpoints, and the clock is fixed, so every timestamp and duration on screen
// is a function of the fixtures.
//
// Regenerate with:
//
//	go test ./internal/ui -run TestGoldenPullRequestScreens -update
func TestGoldenPullRequestScreens(t *testing.T) {
	srv := prServer(t)
	opened := func(t *testing.T, w, h int) Model {
		return openPRDetails(t, prsModel(t, srv, w, h))
	}

	cases := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"prs_120x40", func(t *testing.T) Model { return prsModel(t, srv, 120, 40) }},
		{"prs_80x24", func(t *testing.T) Model { return prsModel(t, srv, 80, 24) }},
		// The narrow frame is where the list has to give up its rightmost
		// columns rather than overlap them.
		{"prs_60x16", func(t *testing.T) Model { return prsModel(t, srv, 60, 16) }},
		{"prs_filtered_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, prsModel(t, srv, 120, 40), "a", "s")
		}},
		// A chain of two overlays: the picker above the filter menu.
		{"prs_repo_picker_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, prsModel(t, srv, 120, 40), "f", "r")
		}},
		{"prs_load_more_80x24", func(t *testing.T) Model {
			full := make([]prSpec, 0, 8)
			for i := 0; i < 8; i++ {
				full = append(full, prSpec{
					number: 100 + i, repo: fixtureRepo, title: "Pull request " + fmt.Sprint(i),
					state: "open", author: "octocat",
					updated: workflowNow.Add(-time.Duration(i) * time.Minute),
				})
			}
			return prsModel(t, prServer(t, withPRs(full...)), 80, 24)
		}},
		{"pr_details_120x40", func(t *testing.T) Model { return opened(t, 120, 40) }},
		{"pr_details_80x24", func(t *testing.T) Model { return opened(t, 80, 24) }},
		// Below the width three panes need, the metadata pane is given up.
		{"pr_details_60x16", func(t *testing.T) Model { return opened(t, 60, 16) }},
		{"pr_commits_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "2")
		}},
		{"pr_files_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "3")
		}},
		{"pr_checks_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "4")
		}},
		{"pr_comments_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "5")
		}},
		{"pr_comments_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 80, 24), "5")
		}},
		// The shape that made the sub-tab look broken: nothing in the
		// conversation comments and everything in the reviews. The second review
		// carries no summary, and the reply the standard fixture posts as part
		// of it reads under the thread it answers rather than beneath this
		// review, so the pane has to read well with a review left holding
		// nothing of its own.
		{"pr_comments_reviews_only_120x40", func(t *testing.T) Model {
			heavy := prServer(t,
				withoutConversationComments(),
				withReviews(
					reviewSpec{
						id: reviewFixtureID, author: "carol", state: "CHANGES_REQUESTED",
						body: "The width needs a name before this lands.", at: workflowNow,
					},
					reviewSpec{
						id: replyReviewFixtureID, author: "dave", state: "COMMENTED",
						at: workflowNow.Add(5 * time.Minute),
					},
				),
			)
			return pressAndDrive(t, openPRDetails(t, prsModel(t, heavy, 120, 40)), "5")
		}},
		{"commit_details_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "2", "enter")
		}},
		{"merge_method_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "m")
		}},
		{"merge_confirm_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "m", "s")
		}},
		{"close_confirm_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "C")
		}},
		// A viewer without write permission is told why instead of being shown
		// a merge it cannot perform (finding F1).
		{"pr_details_read_only_80x24", func(t *testing.T) Model {
			readOnly := prServer(t, withPRPermission("read"))
			return openPRDetails(t, prsModel(t, readOnly, 80, 24))
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden.RequireEqual(t, tc.build(t).View().Content)
		})
	}
}

// TestGoldenIssueScreens pins the screens the issue phase adds: the list with
// data at three widths, its filters and overlays, the details screen with the
// description beside the sidebar, and the conversation.
//
// Regenerate with:
//
//	go test ./internal/ui -run TestGoldenIssueScreens -update
func TestGoldenIssueScreens(t *testing.T) {
	srv := prServer(t)
	opened := func(t *testing.T, w, h int) Model {
		return openIssueDetails(t, issuesModel(t, srv, w, h))
	}

	cases := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"issues_120x40", func(t *testing.T) Model { return issuesModel(t, srv, 120, 40) }},
		{"issues_80x24", func(t *testing.T) Model { return issuesModel(t, srv, 80, 24) }},
		// The narrow frame is where the list has to give up its rightmost
		// columns rather than overlap them.
		{"issues_60x16", func(t *testing.T) Model { return issuesModel(t, srv, 60, 16) }},
		{"issues_filtered_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, issuesModel(t, srv, 120, 40), "a")
		}},
		{"issues_state_closed_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, issuesModel(t, srv, 120, 40), "s")
		}},
		{"issues_load_more_80x24", func(t *testing.T) Model {
			m := issuesModel(t, prServer(t, withIssues(manyIssues(issuesPageSize)...)), 80, 24)
			// The sentinel is the last row of a full page, so the cursor walks
			// to the bottom of the list before the frame is taken.
			keys := make([]string, 0, issuesPageSize)
			for range manyIssues(issuesPageSize) {
				keys = append(keys, "down")
			}
			return pressAndDrive(t, m, keys...)
		}},
		{"issues_filter_menu_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, issuesModel(t, srv, 80, 24), "f")
		}},
		{"issues_user_prompt_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, issuesModel(t, srv, 80, 24), "f", "u")
		}},
		// The picker the issue prompt leads to offers two roles, where the pull
		// request one offers three.
		{"issues_filter_type_80x24", func(t *testing.T) Model {
			m := pressAndDrive(t, issuesModel(t, srv, 80, 24), "f", "u")
			m = typeKeys(t, m, "a", "l", "i", "c", "e")
			return pressAndDrive(t, m, "enter")
		}},
		{"issues_repo_picker_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, issuesModel(t, srv, 120, 40), "f", "r")
		}},
		{"issue_details_120x40", func(t *testing.T) Model { return opened(t, 120, 40) }},
		{"issue_details_80x24", func(t *testing.T) Model { return opened(t, 80, 24) }},
		// Below the width two columns need, the sidebar is given up.
		{"issue_details_60x16", func(t *testing.T) Model { return opened(t, 60, 16) }},
		{"issue_details_sidebar_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "tab")
		}},
		{"issue_comments_120x40", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 120, 40), "c")
		}},
		{"issue_comments_80x24", func(t *testing.T) Model {
			return pressAndDrive(t, opened(t, 80, 24), "c")
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
