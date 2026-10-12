package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/store"
)

// TestURLBuildersStayOffTheNetworkAndOutOfEmptyState covers the builders: a URL
// is put together from what the API returned, and a missing piece answers with
// nothing rather than a link to somewhere made up.
func TestURLBuildersStayOffTheNetworkAndOutOfEmptyState(t *testing.T) {
	if got, want := repoPageURL("octocat/api"), "https://github.com/octocat/api"; got != want {
		t.Errorf("repository URL = %q, want %q", got, want)
	}
	if got := repoPageURL(""); got != "" {
		t.Errorf("repository URL with no name = %q, want nothing", got)
	}

	for name, tc := range map[string]struct {
		repo, base, head, want string
	}{
		"a whole pair":  {"octocat/api", "main", "feat/prs", "https://github.com/octocat/api/compare/main...feat/prs"},
		"no head":       {"octocat/api", "main", "", ""},
		"no base":       {"octocat/api", "", "feat/prs", ""},
		"no repository": {"", "main", "feat/prs", ""},
	} {
		if got := compareURL(tc.repo, tc.base, tc.head); got != tc.want {
			t.Errorf("%s: compare URL = %q, want %q", name, got, tc.want)
		}
	}

	thread := reviewThread{comments: []store.ReviewComment{
		{ID: 2, Body: "no permalink"},
		{ID: 1, HTMLURL: "https://github.com/octocat/api/pull/7#discussion_r1"},
	}}
	if got, want := threadURL(thread), "https://github.com/octocat/api/pull/7#discussion_r1"; got != want {
		t.Errorf("thread URL = %q, want the first permalink %q", got, want)
	}
	if got := threadURL(reviewThread{comments: []store.ReviewComment{{ID: 2}}}); got != "" {
		t.Errorf("thread URL with no permalink = %q, want nothing", got)
	}

	if got, want := issueCommentsURL(store.RepoRef{Owner: "octocat", Repo: "api"}, 3), "https://github.com/octocat/api/issues/3"; got != want {
		t.Errorf("issue URL = %q, want %q", got, want)
	}
	if got := issueCommentsURL(store.RepoRef{Owner: "octocat"}, 3); got != "" {
		t.Errorf("issue URL for a half-filled reference = %q, want nothing", got)
	}

	// The name a screen shows and links comes from the payload when the payload
	// carried one, and from the reference the route was opened with otherwise,
	// which is the case a live detail response is.
	ref := store.RepoRef{Owner: "octocat", Repo: "api"}
	if got := repoFullName("acme/other", ref); got != "acme/other" {
		t.Errorf("repository name = %q, want the payload's %q", got, "acme/other")
	}
	if got := repoFullName("", ref); got != "octocat/api" {
		t.Errorf("repository name with no payload field = %q, want the reference", got)
	}
}

// TestConversationLinksTheChromeAndTheThread covers what the Comments screen
// points at: the repository and the branch pair in the heading, the location of
// an inline thread, and the contribution's own permalink twice on its heading —
// on the name and on the time, so the whole of what names it opens it.
func TestConversationLinksTheChromeAndTheThread(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	details := prDetailsRoute(t, m)

	deps := m.deps
	deps.Theme = DarkTheme()

	heading := strings.Join(details.heading(deps, details.bodyWidth(deps)), "\n")
	for _, want := range []string{
		ansi.SetHyperlink("https://github.com/octocat/api"),
		ansi.SetHyperlink("https://github.com/octocat/api/compare/main...feat/prs"),
	} {
		if !strings.Contains(heading, want) {
			t.Errorf("the heading does not carry the link %q:\n%q", want, heading)
		}
	}

	// The plain palette the goldens render carries no link at all, which is what
	// keeps a golden readable and a terminal without OSC 8 unchanged.
	if plain := strings.Join(details.heading(m.deps, details.bodyWidth(m.deps)), "\n"); strings.Contains(plain, osc8) {
		t.Errorf("a plain palette rendered a link:\n%q", plain)
	}

	m = pressAndDrive(t, m, "5")
	lines := prDetailsRoute(t, m).commentLines(deps)

	var place string
	for _, line := range lines {
		if strings.Contains(ansi.Strip(line), "internal/ui/table.go:12") {
			place = line
			break
		}
	}
	if place == "" {
		t.Fatalf("the thread's location is not on screen:\n%s", strings.Join(lines, "\n"))
	}
	if got := ansi.SetHyperlink("https://github.com/octocat/api/pull/7#discussion_r4001"); !strings.Contains(place, got) {
		t.Errorf("the thread's location is not a link to the thread %q:\n%q", got, place)
	}

	// The first conversation comment's heading links the comment from both the
	// name and the time.
	comment := ansi.SetHyperlink("https://github.com/octocat/api/pull/7#issuecomment-1")
	if got := strings.Count(lines[0], comment); got != 2 {
		t.Errorf("the author line carries %d links to the comment, want the name and the time:\n%q", got, lines[0])
	}
}

// TestIssueScreensLinkTheIssue covers the issue side of the same contract: the
// repository in the heading, and the issue itself from both the heading of its
// description and the heading of its conversation.
func TestIssueScreensLinkTheIssue(t *testing.T) {
	srv := prServer(t)
	m := openIssueDetails(t, issuesModel(t, srv, 120, 40))
	details := issueDetailsRoute(t, m)

	deps := m.deps
	deps.Theme = DarkTheme()

	heading := strings.Join(details.heading(deps, details.bodyWidth(deps)), "\n")
	if got := ansi.SetHyperlink("https://github.com/octocat/api"); !strings.Contains(heading, got) {
		t.Errorf("the issue heading does not link the repository %q:\n%q", got, heading)
	}

	m = pressAndDrive(t, m, "c")
	comments := issueCommentsRoute(t, m)
	head := strings.Join(comments.heading(deps, comments.bodyWidth(deps)), "\n")
	if got := ansi.SetHyperlink("https://github.com/octocat/api/issues/3"); !strings.Contains(head, got) {
		t.Errorf("the conversation heading does not link the issue %q:\n%q", got, head)
	}
}
