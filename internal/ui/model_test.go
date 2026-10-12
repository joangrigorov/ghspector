package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/keymap"
	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/overlay"
	"github.com/joangrigorov/ghspector/internal/store"
	"github.com/joangrigorov/ghspector/internal/ui/widgets"
)

// ghServer serves canned JSON per path, so the tests exercise the real store
// and the real transport rather than a stub.
func ghServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no fixture"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// defaultFixtures covers the startup requests: the viewer, their orgs, and an
// empty repository list. The lists are aggregated across the context's
// repositories, so an empty context is the state that leaves every list empty
// without a request per repository.
func defaultFixtures() map[string]string {
	return map[string]string{
		"/user":                `{"login":"octocat","id":1}`,
		"/user/orgs":           `[{"login":"acme","id":2},{"login":"beta","id":3}]`,
		"/users/octocat/repos": `[]`,
		"/users/acme/repos":    `[]`,
		"/users/beta/repos":    `[]`,
	}
}

// testStore is the store a UI test runs against. The outgoing rate limit paces
// the requests a person makes one at a time, and a test drives a whole screen's
// fetch at once, so it is lifted here; the limiter has its own tests in the
// store package. Extra options adjust the cache a test needs to see through.
func testStore(srv *httptest.Server, opts ...store.Option) *store.Client {
	return store.New("test-token", srv.URL, srv.URL+graphQLPath,
		append([]store.Option{store.WithRateLimit(1000, 1000)}, opts...)...)
}

// newModel builds a model at a size, on the splash.
func newModel(t *testing.T, srv *httptest.Server, w, h int, tweak ...func(*Options)) Model {
	t.Helper()
	opts := Options{
		Store:       testStore(srv),
		Theme:       PlainTheme(),
		Context:     context.Background(),
		Version:     "test",
		ConfigPath:  "config.yaml",
		TokenSource: "config file: config.yaml",
		// A stopped clock keeps every rendered duration a function of the
		// fixtures rather than of when the test ran.
		Clock: newFixedClock(workflowNow),
	}
	for _, fn := range tweak {
		fn(&opts)
	}
	m := New(opts)
	next, _ := send(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	return next
}

// readyModel drives startup to completion by running the identity command and
// feeding its result back, which is exactly what the runtime does. The command
// that answer produces — the active tab's first fetch — is returned, because
// what it fetches depends on the fixtures the test serves.
func readyModel(t *testing.T, srv *httptest.Server, w, h int, tweak ...func(*Options)) Model {
	t.Helper()
	m, _ := startup(t, newModel(t, srv, w, h, tweak...))
	return m
}

// startup resolves the identity and returns the ready model with the command
// that fills the first screen. Tests that serve the active tab's data drive it;
// the rest leave it, which is the state a real user sees while it is in flight.
func startup(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := send(t, m, m.loadIdentity()())
	if next.stage != stageReady {
		t.Fatalf("startup failed: stage = %v, err = %v", next.stage, next.err)
	}
	return next, cmd
}

// send feeds one message and returns the resulting model.
func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want ui.Model", next)
	}
	return got, cmd
}

// press builds the key message a terminal sends for a bound key string.
func press(t *testing.T, key string) tea.KeyPressMsg {
	t.Helper()
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	if len(key) != 1 {
		t.Fatalf("press: cannot build a key message for %q", key)
	}
	r := rune(key[0])
	if r >= 'A' && r <= 'Z' {
		return tea.KeyPressMsg{Code: r + ('a' - 'A'), ShiftedCode: r}
	}
	return tea.KeyPressMsg{Text: key, Code: r}
}

// typeKeys sends each key string in order.
func typeKeys(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		m, _ = send(t, m, press(t, k))
	}
	return m
}

// lineWidths returns the display width of each rendered line.
func lineWidths(s string) []int {
	lines := strings.Split(s, "\n")
	out := make([]int, len(lines))
	for i, line := range lines {
		out[i] = layout.Width(line)
	}
	return out
}

// assertGeometry is the invariant every screen must satisfy: exactly height
// lines, each exactly width cells.
func assertGeometry(t *testing.T, label, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%s: %d lines, want %d\n%s", label, len(lines), h, out)
	}
	for i, got := range lineWidths(out) {
		if got != w {
			t.Fatalf("%s: line %d width %d, want %d\n%s", label, i, got, w, out)
		}
	}
}

// TestEveryScreenHasExactGeometry is the core layout invariant, swept over the
// supported size range and every stage.
func TestEveryScreenHasExactGeometry(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	sizes := [][2]int{{80, 24}, {120, 40}, {200, 60}, {60, 16}, {50, 12}, {51, 13}}

	for _, size := range sizes {
		w, h := size[0], size[1]

		splash := newModel(t, srv, w, h)
		assertGeometry(t, "splash", splash.View().Content, w, h)

		m := readyModel(t, srv, w, h)
		assertGeometry(t, "main", m.View().Content, w, h)

		for _, tab := range []Tab{TabPRs, TabWorkflows, TabIssues} {
			tabbed, _ := send(t, m, ReplaceMsg{Route: mainTab(tab)})
			assertGeometry(t, "tab "+tabMeta[tab].Name, tabbed.View().Content, w, h)
		}

		help := typeKeys(t, m, "?")
		assertGeometry(t, "help", help.View().Content, w, h)

		switcher := typeKeys(t, m, "o")
		if _, ok := switcher.routes.Top().(*Switcher); !ok {
			t.Fatalf("size %dx%d: o did not open the switcher", w, h)
		}
		assertGeometry(t, "switcher", switcher.View().Content, w, h)
	}
}

// TestTooSmallReplacesTheScreen checks that an unsupported terminal gets a
// message rather than a malformed frame.
func TestTooSmallReplacesTheScreen(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	// The message needs a few cells to be legible; below that, exact geometry is
	// all that can be asked for.
	const legibleWidth, legibleHeight = 25, 3

	for _, size := range [][2]int{{40, 15}, {49, 24}, {80, 11}, {20, 8}, {1, 1}} {
		w, h := size[0], size[1]
		m := readyModel(t, srv, w, h)
		out := m.View().Content
		if w >= legibleWidth && h >= legibleHeight && !strings.Contains(out, "Terminal too small") {
			t.Errorf("size %dx%d did not show the too-small message:\n%s", w, h, out)
		}
		if w > 0 && h > 0 {
			assertGeometry(t, "too small", out, w, h)
		}
	}
}

func TestSplashUntilIdentityResolves(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := newModel(t, srv, 80, 24)

	if m.stage != stageLoading {
		t.Fatalf("stage = %v, want loading", m.stage)
	}
	splash := m.View().Content
	if !strings.Contains(splash, "g h s p e c t o r") {
		t.Errorf("splash missing the name:\n%s", splash)
	}
	if !strings.Contains(splash, "config.yaml") {
		t.Errorf("splash missing the config label:\n%s", splash)
	}

	next, _ := send(t, m, m.loadIdentity()())
	got := next
	if got.stage != stageReady {
		t.Fatalf("stage = %v, want ready", got.stage)
	}
	out := got.View().Content
	if strings.Contains(out, "g h s p e c t o r") {
		t.Error("the splash is still on screen after startup")
	}
	if !strings.Contains(out, "Account: octocat") {
		t.Errorf("header missing the account:\n%s", out)
	}
}

// TestStartupUsesTheConfiguredContext pins the top of the config's reach: the
// context the file or a flag names is where the session starts, and the header
// says which kind of context it is.
func TestStartupUsesTheConfiguredContext(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24, func(o *Options) {
		o.DefaultOwner = &store.OwnerRef{Login: "acme", IsOrg: true}
	})

	if got := m.deps.Account.Owner; got.Login != "acme" || !got.IsOrg {
		t.Fatalf("owner = %+v, want acme as an organization", got)
	}
	if out := m.View().Content; !strings.Contains(out, "Account: acme (org)") {
		t.Errorf("the header does not name the configured context:\n%s", out)
	}
}

// TestStartupAsksForTheViewerThenTheirOrgs pins the startup sequence: the
// account first, then the organizations it may switch into. The order matters,
// because the organizations answer decides which contexts the switcher offers
// and the account is what the header names.
func TestStartupAsksForTheViewerThenTheirOrgs(t *testing.T) {
	log := newRequestLog()
	m := newModel(t, prServerWithLog(t, log), 80, 24)

	m, _ = send(t, m, m.loadIdentity()())
	if m.stage != stageReady {
		t.Fatalf("stage = %v, want ready", m.stage)
	}

	log.mu.Lock()
	entries := append([]string(nil), log.entries...)
	log.mu.Unlock()
	if len(entries) < 2 {
		t.Fatalf("startup made %d requests, want at least the two identity reads: %v", len(entries), entries)
	}
	for i, want := range []string{"GET /user", "GET /user/orgs"} {
		if entries[i] != want {
			t.Errorf("request %d = %q, want %q", i, entries[i], want)
		}
	}
}

func TestForbiddenShowsTheErrorScreen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
	}))
	t.Cleanup(srv.Close)

	m := newModel(t, srv, 80, 24)
	next, _ := send(t, m, m.loadIdentity()())
	got := next
	if got.stage != stageError {
		t.Fatalf("stage = %v, want error", got.stage)
	}
	out := got.View().Content
	if !strings.Contains(out, "Access denied") {
		t.Errorf("error screen missing the title:\n%s", out)
	}
	if !strings.Contains(out, "repo and workflow scopes") {
		t.Errorf("error screen missing the scope hint:\n%s", out)
	}
	// No absolute path may appear on screen.
	if strings.Contains(out, "/home") || strings.Contains(out, "/Users") {
		t.Errorf("error screen leaked a path:\n%s", out)
	}
}

func TestRetryFromTheErrorScreen(t *testing.T) {
	var fail bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/orgs" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if fail {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
			return
		}
		_, _ = w.Write([]byte(`{"login":"octocat"}`))
	}))
	t.Cleanup(srv.Close)

	m := newModel(t, srv, 80, 24)
	fail = true
	next, _ := send(t, m, m.loadIdentity()())
	m = next
	if m.stage != stageError {
		t.Fatalf("stage = %v, want error", m.stage)
	}

	fail = false
	next, cmd := send(t, m, press(t, "r"))
	m = next
	if m.stage != stageLoading {
		t.Fatalf("stage after retry = %v, want loading", m.stage)
	}
	if cmd == nil {
		t.Fatal("retry returned no command")
	}

	// Drive the identity request the retry scheduled.
	next, _ = send(t, m, m.loadIdentity()())
	m = next
	if m.stage != stageReady {
		t.Fatalf("stage after a successful retry = %v, err = %v", m.stage, m.err)
	}
}

// TestRateLimitedStartupIsRecoverable covers a budget that is already spent when
// the program starts. A window longer than the client is willing to sleep
// through becomes a full-screen error that names the failure and the way out; a
// window that is about to roll over is waited out inside the request, so the
// next screen arrives without a detour.
func TestRateLimitedStartupIsRecoverable(t *testing.T) {
	fixtures := defaultFixtures()
	exhausted := func(w http.ResponseWriter, reset time.Time) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}
	serveFixtures := func(w http.ResponseWriter, r *http.Request) {
		body, ok := fixtures[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no fixture"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}

	t.Run("a long window is reported rather than slept through", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			exhausted(w, time.Now().Add(10*time.Minute))
		}))
		t.Cleanup(srv.Close)

		m := newModel(t, srv, 80, 24)
		next, _ := send(t, m, m.loadIdentity()())
		m = next
		if m.stage != stageError {
			t.Fatalf("stage = %v, want error", m.stage)
		}
		if !errors.Is(m.err, store.ErrRateLimited) {
			t.Fatalf("error = %v, want a rate-limited error", m.err)
		}
		out := m.View().Content
		for _, want := range []string{"Rate limited", "Wait for the limit to reset, then retry."} {
			if !strings.Contains(out, want) {
				t.Errorf("error screen is missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("a window that is about to roll over is waited out", func(t *testing.T) {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if requests == 1 {
				exhausted(w, time.Now().Add(time.Second))
				return
			}
			serveFixtures(w, r)
		}))
		t.Cleanup(srv.Close)

		m := newModel(t, srv, 80, 24)
		next, _ := send(t, m, m.loadIdentity()())
		m = next
		if m.stage != stageError {
			t.Fatalf("stage = %v, want error", m.stage)
		}

		next, cmd := send(t, m, press(t, "r"))
		m = next
		if cmd == nil {
			t.Fatal("retry returned no command")
		}
		next, _ = send(t, m, m.loadIdentity()())
		m = next
		if m.stage != stageReady {
			t.Fatalf("stage after the limit resets = %v, err = %v", m.stage, m.err)
		}
	})
}

// TestTabCyclingOrder pins the tab order from spec D2: PRs, Workflows, Issues.
func TestTabCyclingOrder(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)

	want := []string{"Pull Requests", "Workflows", "Issues", "Pull Requests"}
	for i, w := range want {
		if got := m.routes.Top().Title(); got != w {
			t.Fatalf("step %d: title = %q, want %q", i, got, w)
		}
		m = typeKeys(t, m, "tab")
	}

	m = typeKeys(t, m, "shift+tab")
	// The loop above leaves the active tab on Workflows, so stepping back lands
	// on the first tab.
	if got := m.routes.Top().Title(); got != "Pull Requests" {
		t.Fatalf("shift+tab from Workflows gave %q, want Pull Requests", got)
	}

	m = typeKeys(t, m, "shift+tab")
	if got := m.routes.Top().Title(); got != "Issues" {
		t.Fatalf("shift+tab wraps to the last tab; got %q", got)
	}
}

// TestEveryTabScopeIsReachable keeps the footer and the tab strip in step.
func TestEveryTabScopeIsReachable(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)

	seen := map[keymap.Scope]bool{}
	for i := 0; i < tabCount; i++ {
		scope := m.routes.Top().Scope()
		if seen[scope] {
			t.Fatalf("scope %q reached twice", scope)
		}
		seen[scope] = true
		m = typeKeys(t, m, "tab")
	}
	for _, want := range []keymap.Scope{keymap.MainPRs, keymap.MainWorkflows, keymap.MainIssues} {
		if !seen[want] {
			t.Errorf("scope %q is not reachable by cycling tabs", want)
		}
	}
}

// TestFooterRendersTheActiveScope checks the single-source-of-truth property
// from the outside: every documented key of the active scope appears in the
// footer when there is room for it.
func TestFooterRendersTheActiveScope(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	// Wide enough that no hint is truncated; truncation is covered by
	// TestFooterTruncatesWhenNarrow.
	m := readyModel(t, srv, 400, 40)

	for _, scope := range []keymap.Scope{keymap.MainPRs, keymap.MainWorkflows, keymap.MainIssues} {
		tabbed, _ := send(t, m, ReplaceMsg{Route: mainTab(tabForScope(t, scope))})
		footer := lastLines(tabbed.View().Content, layout.FooterHeight)
		for _, b := range keymap.Documented(scope) {
			keys := keymap.KeysFor(b)
			if keys == "" {
				continue
			}
			if !strings.Contains(footer, keys) {
				t.Errorf("scope %q: footer is missing key %q (%s)\n%s", scope, keys, b.Help, footer)
			}
		}
	}
}

// TestFooterTruncatesWhenNarrow checks the other half of the contract: a narrow
// footer keeps whole hints and drops the tail rather than cutting one in half.
func TestFooterTruncatesWhenNarrow(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 50, 12)

	for _, w := range []int{50, 51, 60, 72, 90} {
		next, _ := send(t, m, tea.WindowSizeMsg{Width: w, Height: 24})
		m = next
		out := m.View().Content
		assertGeometry(t, "narrow footer", out, w, 24)

		footer := lastLines(out, layout.FooterHeight)
		if !strings.Contains(footer, "Help") {
			t.Errorf("width %d: the footer dropped the pinned global keys:\n%s", w, footer)
		}
	}
}

// TestEveryRouteFooterPinsTheGlobalKeys walks the screens a session reaches and
// checks the footer carries the help hint on each one. The footer pins the
// global keys ahead of the surface's own, so `?`, `o` and quit are always on
// hand wherever a key works (spec §2.3).
func TestEveryRouteFooterPinsTheGlobalKeys(t *testing.T) {
	srv := prServer(t)
	const w, h = 120, 40

	for _, tc := range []struct {
		name string
		keys []string
	}{
		{"pull requests", nil},
		{"workflow runs", []string{"tab"}},
		{"issues", []string{"tab", "tab"}},
		{"help", []string{"?"}},
		{"context switcher", []string{"o"}},
		{"pull request details", []string{"enter", "1"}},
		{"pull request commits", []string{"enter", "2"}},
		{"pull request files", []string{"enter", "3"}},
		{"pull request checks", []string{"enter", "4"}},
		{"pull request comments", []string{"enter", "5"}},
		{"commit details", []string{"enter", "2", "enter"}},
		{"jobs from a check", []string{"enter", "tab", "enter"}},
		{"logs", []string{"enter", "tab", "enter", "j", "enter"}},
		{"issue details", []string{"tab", "tab", "enter"}},
		{"issue conversation", []string{"tab", "tab", "enter", "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pressAndDrive(t, prsModel(t, srv, w, h), tc.keys...)
			footer := ansi.Strip(lastLines(m.View().Content, layout.FooterHeight))
			// Only the first pinned hint is asserted here: the footer drops the
			// tail of the global keys when a surface's own hints and its status
			// message take the room, which is the truncation policy and has its
			// own test.
			if !strings.Contains(footer, "?:Help") {
				t.Errorf("footer is missing the help hint:\n%s", footer)
			}
		})
	}
}

// TestFooterAdvertisesOnlyTheKeysThatWork covers the three surfaces that differ.
// A route answers every global key; a modal eats the keys the route would act
// on, so only quit is left; a text box takes characters, so `q`, `o` and `?` are
// what somebody is typing and Ctrl+C is the way out (spec §2.4).
func TestFooterAdvertisesOnlyTheKeysThatWork(t *testing.T) {
	srv := prServer(t)
	const w, h = 120, 30

	footerOf := func(m Model) string { return ansi.Strip(lastLines(m.View().Content, layout.FooterHeight)) }
	base := func() Model { return prsModel(t, srv, w, h) }

	t.Run("a route pins every global key", func(t *testing.T) {
		footer := footerOf(base())
		for _, want := range []string{"?:Help", "o:Switch account/org", "q/Ctrl+C:Quit"} {
			if !strings.Contains(footer, want) {
				t.Errorf("footer is missing %q:\n%s", want, footer)
			}
		}
	})

	t.Run("a modal keeps quit alone", func(t *testing.T) {
		m := base()
		m.overlays = m.overlays.Push(overlay.Confirm{
			Scope:   keymap.MergeConfirm,
			Heading: "Confirm merge",
			Body:    []string{"This will squash-merge pull request #16."},
			OnYes:   func() tea.Msg { return nil },
		})
		footer := footerOf(m)
		if !strings.Contains(footer, "q/Ctrl+C:Quit") {
			t.Errorf("a modal footer dropped quit:\n%s", footer)
		}
		for _, unwanted := range []string{"?:Help", "o:Switch account/org"} {
			if strings.Contains(footer, unwanted) {
				t.Errorf("a modal footer advertises %q, which the modal eats:\n%s", unwanted, footer)
			}
		}
	})

	t.Run("a text box keeps Ctrl+C alone", func(t *testing.T) {
		m := pressAndDrive(t, base(), "f", "u")
		if !m.overlays.TakesText() {
			t.Fatalf("f then u did not open a text box: %d overlays", m.overlays.Len())
		}
		footer := footerOf(m)
		if !strings.Contains(footer, "Ctrl+C:Quit") {
			t.Errorf("a text box footer dropped the way out:\n%s", footer)
		}
		for _, unwanted := range []string{"?:Help", "o:Switch account/org", "q/Ctrl+C:Quit"} {
			if strings.Contains(footer, unwanted) {
				t.Errorf("a text box footer advertises %q, which the box types:\n%s", unwanted, footer)
			}
		}
	})
}

// TestHelpOpensClosesAndScrolls opens the reference and drives it.
func TestHelpOpensClosesAndScrolls(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	// Wide enough for the help scope's own hints to survive alongside the
	// pinned global keys.
	const w, h = 120, 30
	m := readyModel(t, srv, w, h)

	if _, ok := m.routes.Top().(*Help); ok {
		t.Fatal("help is open before it was requested")
	}

	m = typeKeys(t, m, "?")
	if _, ok := m.routes.Top().(*Help); !ok {
		t.Fatalf("? did not open help; top = %T", m.routes.Top())
	}
	opened := m.View().Content
	if !strings.Contains(opened, "Global") {
		t.Errorf("help body missing a section:\n%s", opened)
	}
	// The footer must come from the help scope while help is on top.
	footer := lastLines(opened, layout.FooterHeight)
	for _, want := range []string{"Close help", "Scroll down"} {
		if !strings.Contains(footer, want) {
			t.Errorf("help footer missing %q:\n%s", want, footer)
		}
	}

	// Scrolling moves the window without changing the geometry.
	before := opened
	m = typeKeys(t, m, "j", "j", "j")
	after := m.View().Content
	if before == after {
		t.Error("scrolling changed nothing")
	}
	assertGeometry(t, "help scrolled", after, w, h)

	// Scrolling past the end clamps instead of running away.
	m = typeKeys(t, m, "k", "k", "k", "k", "k", "k", "k", "k", "k", "k")
	top := m.View().Content
	if !strings.Contains(top, "Global") {
		t.Errorf("scrolling back up did not reach the top:\n%s", top)
	}

	m = typeKeys(t, m, "?")
	if _, ok := m.routes.Top().(*Help); ok {
		t.Error("? did not close help")
	}

	m = typeKeys(t, m, "?")
	m = typeKeys(t, m, "esc")
	if _, ok := m.routes.Top().(*Help); ok {
		t.Error("esc did not close help")
	}
}

// TestHelpRendersTheStatusLegend pins the legend to the markers the tables
// draw: every glyph below comes from statusIndicator, which is what puts the
// same marker in front of a run, job, check or log row.
func TestHelpRendersTheStatusLegend(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 100, 40)
	m = typeKeys(t, m, "?")

	body := m.View().Content
	for _, want := range []string{"Status", "◆ waiting", "□ queued", "■ running", "■ success", "■ failed"} {
		if !strings.Contains(body, want) {
			t.Errorf("help body missing %q:\n%s", want, body)
		}
	}
	// The legend leads the body, so it is on screen without scrolling.
	lines := splitLines(body)
	legend, global := -1, -1
	for i, line := range lines {
		if legend < 0 && strings.Contains(line, "waiting") {
			legend = i
		}
		if global < 0 && strings.Contains(line, "Global") {
			global = i
		}
	}
	if legend < 0 || legend > global {
		t.Errorf("the legend (line %d) does not lead the keymap sections (line %d):\n%s", legend, global, body)
	}
}

func TestSwitcherSelectsAndCloses(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)

	if got := m.deps.Account.Owner.Login; got != "octocat" {
		t.Fatalf("initial owner = %q, want octocat", got)
	}

	m = typeKeys(t, m, "o")
	if _, ok := m.routes.Top().(*Switcher); !ok {
		t.Fatalf("o did not open the switcher; top = %T", m.routes.Top())
	}
	body := m.View().Content
	for _, want := range []string{"octocat", "acme", "beta", "organization", "← active"} {
		if !strings.Contains(body, want) {
			t.Errorf("switcher is missing %q:\n%s", want, body)
		}
	}

	// The cursor starts on the active entry; one step down lands on acme.
	m = typeKeys(t, m, "j")
	next, cmd := send(t, m, press(t, "enter"))
	m = next
	if cmd == nil {
		t.Fatal("selecting a context returned no command")
	}
	// The select command is a plain function, so running it is immediate.
	next, _ = send(t, m, cmd())
	m = next

	if _, ok := m.routes.Top().(*Switcher); ok {
		t.Error("the switcher stayed open after selecting")
	}
	if got := m.deps.Account.Owner; got.Login != "acme" || !got.IsOrg {
		t.Fatalf("owner = %+v, want acme as an organization", got)
	}
	if !strings.Contains(m.View().Content, "acme") {
		t.Errorf("header does not show the new context:\n%s", m.View().Content)
	}

	m = typeKeys(t, m, "o")
	// The switcher asks the stack to pop, so the command is driven the way the
	// runtime would.
	m, cmd = pressKey(t, m, "esc")
	m = drive(t, m, cmd)
	if _, ok := m.routes.Top().(*Switcher); ok {
		t.Error("esc did not close the switcher")
	}
}

// TestModalOwnsTheKeyboard is the input-isolation rule: a modal consumes keys
// that the route would otherwise act on.
func TestModalOwnsTheKeyboard(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)

	// Without a modal, tab cycles to the next tab.
	if got := typeKeys(t, m, "tab").routes.Top().Title(); got != "Workflows" {
		t.Fatalf("tab without a modal gave %q", got)
	}

	var yes bool
	m.overlays = m.overlays.Push(overlay.Confirm{
		Scope:   keymap.MergeConfirm,
		Heading: "Confirm merge",
		Body:    []string{"This will squash-merge pull request #16."},
		OnYes:   func() tea.Msg { yes = true; return nil },
	})

	if got := typeKeys(t, m, "tab").routes.Top().Title(); got != "Pull Requests" {
		t.Fatalf("tab reached the route behind the modal: %q", got)
	}

	next, cmd := send(t, m, press(t, "y"))
	m = next
	if m.overlays.Len() != 0 {
		t.Fatalf("confirm stayed open: %d overlays", m.overlays.Len())
	}
	if cmd == nil {
		t.Fatal("confirm returned no command")
	}
	cmd()
	if !yes {
		t.Error("confirming did not run the action")
	}
}

func TestStatusMessageAndExpiry(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 100, 24)

	if strings.Contains(m.View().Content, "Refreshing pull requests...") {
		t.Fatal("a status message appeared before one was set")
	}

	next, cmd := send(t, m, press(t, "r"))
	m = next
	if cmd == nil {
		t.Fatal("refresh returned no command")
	}
	// A refresh answers with a batch: the status message, and the fetch it
	// describes. Running it yields exactly the messages the runtime delivers.
	var expiry tea.Cmd
	for _, msg := range messagesOf(t, cmd) {
		stepped, follow := send(t, m, msg)
		m = stepped
		if _, ok := msg.(StatusMsg); ok {
			expiry = follow
		}
	}
	if !strings.Contains(m.View().Content, "Refreshing pull requests...") {
		t.Fatalf("status message missing from the footer:\n%s", m.View().Content)
	}
	if expiry == nil {
		t.Fatal("setting a status message did not schedule its expiry")
	}
	generation := m.statusGeneration

	next, _ = send(t, m, statusClearMsg{generation: generation})
	m = next
	if strings.Contains(m.View().Content, "Refreshing pull requests...") {
		t.Error("the status message did not expire")
	}
}

// TestStaleExpiryDoesNotClearANewerMessage guards the generation counter.
func TestStaleExpiryDoesNotClearANewerMessage(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 100, 24)

	next, _ := send(t, m, StatusMsg{Text: "first"})
	m = next
	stale := m.statusGeneration

	next, _ = send(t, m, StatusMsg{Text: "second"})
	m = next

	next, _ = send(t, m, statusClearMsg{generation: stale})
	m = next
	if !strings.Contains(m.View().Content, "second") {
		t.Error("an old expiry timer cleared a newer message")
	}

	next, _ = send(t, m, statusClearMsg{generation: m.statusGeneration})
	m = next
	if strings.Contains(m.View().Content, "second") {
		t.Error("the current expiry timer did not clear the message")
	}
}

// TestQuitWorksFromEveryStage checks that no screen traps the user.
func TestQuitWorksFromEveryStage(t *testing.T) {
	srv := ghServer(t, defaultFixtures())

	cases := map[string]Model{
		"splash": newModel(t, srv, 80, 24),
		"main":   readyModel(t, srv, 80, 24),
		"help":   typeKeys(t, readyModel(t, srv, 80, 24), "?"),
		"switch": typeKeys(t, readyModel(t, srv, 80, 24), "o"),
		"small":  readyModel(t, srv, 40, 10),
	}
	for name, m := range cases {
		_, cmd := send(t, m, press(t, "q"))
		if cmd == nil {
			t.Errorf("%s: q returned no command", name)
			continue
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s: q did not quit", name)
		}
	}
}

// TestCtrlCQuitsFromBehindAModal covers the one global key that must punch
// through a modal.
func TestCtrlCQuitsFromBehindAModal(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)
	m.overlays = m.overlays.Push(overlay.Confirm{Scope: keymap.MergeConfirm, Heading: "h"})

	_, cmd := send(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c did not quit")
	}

	// An input route takes raw keys, so quit has to be settled before the
	// overlay is offered the press (spec §2.4). A modal that is not a text box
	// gives up `q` as well.
	_, cmd = send(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q returned no command from behind a modal")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q did not quit from behind a modal")
	}
}

// TestQuitGivesWayToATextBox covers the exception to the global quit binding: a
// character somebody is typing is not a command, and Ctrl+C is the way out of a
// box (spec §2.4).
func TestQuitGivesWayToATextBox(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)
	m.overlays = m.overlays.Push(overlay.Prompt{
		Scope:   keymap.UserInput,
		Heading: "Filter by user",
		Input:   newPromptInput(m.deps.Theme),
	})

	next, cmd := send(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q quit from inside a text box")
		}
	}
	prompt, ok := next.overlays.Top().(overlay.Prompt)
	if !ok {
		t.Fatalf("the prompt was replaced by %T", next.overlays.Top())
	}
	if got := prompt.Input.Value(); got != "q" {
		t.Errorf("the box holds %q, want the character that was typed", got)
	}

	if _, cmd := send(t, next, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+c returned no command from inside a text box")
	} else if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+c did not quit from inside a text box")
	}
}

// TestUnboundKeyReachesTheRoute checks the fall-through path that routes taking
// text input rely on.
func TestUnboundKeyReachesTheRoute(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)
	m.routes = m.routes.Push(&recordingRoute{scope: keymap.Help})

	typed := typeKeys(t, m, "z")
	top := typed.routes.Top().(*recordingRoute)
	if len(top.raw) != 1 || top.raw[0] != "z" {
		t.Fatalf("route received %v, want [z]", top.raw)
	}
}

// TestBreadcrumbsJoinTheStack checks that the trail is derived, never written
// by hand.
func TestBreadcrumbsJoinTheStack(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 120, 40)

	if got := breadcrumbs(m.routes.Titles()); got != "Pull Requests" {
		t.Fatalf("root trail = %q", got)
	}

	m = typeKeys(t, m, "o")
	trail := breadcrumbs(m.routes.Titles())
	if trail != "Pull Requests › Switch context" {
		t.Fatalf("trail = %q", trail)
	}
	if !strings.Contains(m.View().Content, "Switch context") {
		t.Error("the header does not show the trail")
	}
}

func TestBackPopsOnlyAboveTheRoot(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	m := readyModel(t, srv, 80, 24)

	// Esc on the root is not bound, so nothing happens and nothing is lost.
	m = typeKeys(t, m, "esc")
	if m.routes.Len() != 1 {
		t.Fatalf("stack depth = %d, want 1", m.routes.Len())
	}

	m = typeKeys(t, m, "?")
	if m.routes.Len() != 2 {
		t.Fatalf("stack depth after help = %d, want 2", m.routes.Len())
	}
	m = typeKeys(t, m, "esc")
	if m.routes.Len() != 1 {
		t.Fatalf("stack depth after esc = %d, want 1", m.routes.Len())
	}
}

// recordingRoute captures the raw messages it receives, for the fall-through
// test.
type recordingRoute struct {
	scope keymap.Scope
	raw   []string
}

func (r *recordingRoute) Title() string       { return "Recording" }
func (r *recordingRoute) Scope() keymap.Scope { return r.scope }
func (r *recordingRoute) Act(Deps, keymap.Action) (Route, tea.Cmd, bool) {
	return r, nil, false
}
func (r *recordingRoute) Update(_ Deps, msg tea.Msg) (Route, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		r.raw = append(r.raw, keymap.KeyString(k))
	}
	return r, nil
}
func (r *recordingRoute) View(Deps, layout.Rect) string { return "" }

// mainTab returns a main route on a specific tab.
func mainTab(tab Tab) Main { return Main{tab: tab} }

// tabForScope maps a scope back to its tab.
func tabForScope(t *testing.T, scope keymap.Scope) Tab {
	t.Helper()
	for i := 0; i < tabCount; i++ {
		if tabMeta[Tab(i)].Scope == scope {
			return Tab(i)
		}
	}
	t.Fatalf("no tab uses scope %q", scope)
	return 0
}

// lastLines returns the final n lines of a rendered screen.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// headerBand is the header row of a frame with its styling removed.
func headerBand(t *testing.T, m Model) string {
	t.Helper()
	return ansi.Strip(strings.Split(m.View().Content, "\n")[0])
}

// TestHeaderShowsTheSpinnerWhileAScreenFetches covers the chrome side of §2.2:
// the header says that something is in flight, and the pane says what is being
// waited for.
func TestHeaderShowsTheSpinnerWhileAScreenFetches(t *testing.T) {
	srv := prServer(t)
	m := prsModel(t, srv, 120, 40)

	if band := headerBand(t, m); strings.Contains(band, widgets.Dots(m.tick)+" ") {
		t.Errorf("a settled screen shows a spinner: %q", band)
	}
	if _, spin := send(t, m, spinnerMsg{}); spin != nil {
		t.Error("the spinner keeps ticking on a settled screen")
	}

	details, cmd := newPRDetails(m.deps, store.PullRequest{Number: 1, Title: "A pull request"})
	if !details.Loading() {
		t.Fatal("a fresh details route does not report a fetch in flight")
	}
	next, _ := send(t, m, PushMsg{Route: details, Then: cmd})

	if band := headerBand(t, next); !strings.Contains(band, widgets.Dots(next.tick)+" ") {
		t.Errorf("the header does not show the spinner while the screen fetches: %q", band)
	}
	if _, spin := send(t, next, spinnerMsg{}); spin == nil {
		t.Error("the spinner stopped while a fetch is in flight")
	}
}

// TestAlternateScreenIsOn pins the terminal mode: the program owns the screen
// while it runs (spec §2.1).
func TestAlternateScreenIsOn(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	if v := readyModel(t, srv, 80, 24).View(); !v.AltScreen {
		t.Error("the program does not start in the alternate screen")
	}
}
