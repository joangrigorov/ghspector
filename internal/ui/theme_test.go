package ui

import (
	"context"
	"image/color"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/layout"
)

// namedColour pairs a palette entry with its name, so a contrast failure says
// which colour is unreadable rather than which hex value it is.
type namedColour struct {
	name   string
	colour color.Color
}

// contentColours lists the colours a theme draws on the terminal's own
// background. The header band is not in the list: its colours are drawn on
// HeaderBackground and are checked against it.
func contentColours(theme Theme) []namedColour {
	return []namedColour{
		{"Title", theme.Title},
		{"Text", theme.Text},
		{"Muted", theme.Muted},
		{"Accent", theme.Accent},
		{"StatusOK", theme.StatusOK},
		{"StatusWarn", theme.StatusWarn},
		{"StatusFail", theme.StatusFail},
		{"KeyHint", theme.KeyHint},
		{"KeyDesc", theme.KeyDesc},
		{"ErrorTitle", theme.ErrorTitle},
		{"ErrorBody", theme.ErrorBody},
		{"SplashLogo", theme.SplashLogo},
		{"SplashText", theme.SplashText},
		{"OverlayTitle", theme.OverlayTitle},
		{"OverlayBody", theme.OverlayBody},
	}
}

// TestThemeColoursHoldTheirContrast guards the class of defect the review found:
// a highlighted row and, after a scheme change, whole panes of the pull request
// screen drawn in a colour that its background swallowed. Every colour is
// measured against the surface it is drawn on — white for the light theme and
// black for the dark one, which is what each assumes its terminal to be.
//
// The threshold is 3:1, the WCAG minimum for a large or non-text element. This is
// a terminal palette rather than a document: a status colour is a one-cell glyph
// and the muted grey is secondary text, and the stricter 4.5:1 would rule out
// amber on white, which is the colour of "waiting" and worth keeping.
func TestThemeColoursHoldTheirContrast(t *testing.T) {
	const minRatio = 3.0
	backgrounds := []struct {
		name  string
		theme Theme
		on    color.Color
	}{
		{"light", LightTheme(), lipgloss.Color("#ffffff")},
		{"dark", DarkTheme(), lipgloss.Color("#000000")},
	}

	for _, bg := range backgrounds {
		for _, c := range contentColours(bg.theme) {
			if got := contrastRatio(c.colour, bg.on); got < minRatio {
				t.Errorf("%s theme: %s is %.2f:1 against the terminal background, want %.1f:1 or more",
					bg.name, c.name, got, minRatio)
			}
		}
		for _, c := range []namedColour{
			{"HeaderTitle", bg.theme.HeaderTitle},
			{"HeaderContext", bg.theme.HeaderContext},
		} {
			if got := contrastRatio(c.colour, bg.theme.HeaderBackground); got < minRatio {
				t.Errorf("%s theme: %s is %.2f:1 against the header band, want %.1f:1 or more",
					bg.name, c.name, got, minRatio)
			}
		}
		// A rule and a modal border are structure rather than text, so they sit
		// below the threshold on purpose. They still have to be visible: a
		// separator that matches its surface is a separator that is not there.
		for _, c := range []namedColour{
			{"HeaderRule", bg.theme.HeaderRule},
			{"OverlayBorder", bg.theme.OverlayBorder},
		} {
			on := bg.on
			if c.name == "HeaderRule" {
				on = bg.theme.HeaderBackground
			}
			if got := contrastRatio(c.colour, on); got <= 1.05 {
				t.Errorf("%s theme: %s is %.2f:1 against its surface, which hides it",
					bg.name, c.name, got)
			}
		}
	}
}

// TestThemeForFollowsTheTerminalBackground pins the one place the choice lives.
func TestThemeForFollowsTheTerminalBackground(t *testing.T) {
	if got := ThemeFor(true); got.Accent != DarkTheme().Accent {
		t.Errorf("ThemeFor(true) is not the dark theme: accent %v", got.Accent)
	}
	if got := ThemeFor(false); got.Accent != LightTheme().Accent {
		t.Errorf("ThemeFor(false) is not the light theme: accent %v", got.Accent)
	}
}

// TestBackgroundColorChangeMatchesAStartupInThatScheme covers the defect the
// review found: the program sampled the terminal background once, at startup, so
// changing the OS scheme while it ran left the dark palette's greys on a light
// screen. The model now takes the answer from the runtime.
//
// The check is an equivalence rather than a list of colours: a screen the change
// produced has to be the screen the same program draws when it starts in that
// scheme, which is the state the review confirmed to be correct.
func TestBackgroundColorChangeMatchesAStartupInThatScheme(t *testing.T) {
	srv := ghServer(t, defaultFixtures())
	light := func(o *Options) { o.Theme = LightTheme() }
	dark := func(o *Options) { o.Theme = DarkTheme() }

	atStart := readyModel(t, srv, 80, 24, dark)
	switched, _ := send(t, readyModel(t, srv, 80, 24, light), darkBackground())

	if switched.deps.Theme.Accent != DarkTheme().Accent {
		t.Fatalf("the palette did not change: accent %v", switched.deps.Theme.Accent)
	}
	lightScreen := readyModel(t, srv, 80, 24, light).View().Content
	if switched.View().Content == lightScreen {
		t.Fatal("the screen did not change with the palette")
	}

	// The overlay stack is built from the theme at construction, so it is the
	// part most likely to be left behind.
	if got, want := modalModel(t, switched).View().Content, modalModel(t, atStart).View().Content; got != want {
		t.Errorf("a modal opened after the change does not match one opened in the dark scheme\n got: %q\nwant: %q",
			firstDiff(got, want), firstDiff(want, got))
	}
	if got, want := switched.View().Content, atStart.View().Content; got != want {
		t.Errorf("the screen after the change does not match a startup in the dark scheme\n got: %q\nwant: %q",
			firstDiff(got, want), firstDiff(want, got))
	}
}

// darkBackground is the message a terminal sends when it answers the background
// colour query with something dark.
func darkBackground() tea.BackgroundColorMsg {
	return tea.BackgroundColorMsg{Color: color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}}
}

// firstDiff returns the first line that differs, so a failing screen comparison
// reports something readable instead of two whole screens.
func firstDiff(got, want string) string {
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			return "line " + strconv.Itoa(i+1) + ": " + g
		}
	}
	return "(identical)"
}

// TestSelectedRowPaintsTheRowInTheAccent pins the single highlight treatment
// every list uses: the shared marker in the gutter and the row's own text in the
// accent colour and bold.
//
// The state marker in front of a run, a job or a check is the one part that keeps
// its own colour, because the glyph it is drawn on is one cell of a shape and the
// colour is what says whether the thing passed.
func TestSelectedRowPaintsTheRowInTheAccent(t *testing.T) {
	theme := DarkTheme()
	plain := tableIndent + statusIndicator(theme, "completed", "failure") + " " + "lint"

	got := selectedRow(theme, plain, 40)

	if !strings.HasPrefix(got, highlightPrefix(theme)+selectMarker) {
		t.Errorf("the row does not open with the marker in the accent colour: %q", got)
	}
	// The marker in front of the row ends with a reset, so the text behind it has
	// to be re-opened in the accent: a reset alone would leave it in the
	// terminal's own colour.
	if !strings.HasSuffix(got, style(theme.Accent, bold).Render(" lint")) {
		t.Errorf("the row's text is not drawn in the accent colour and bold: %q", got)
	}
	if !strings.Contains(got, style(theme.StatusFail, normal).Render("■")) {
		t.Errorf("the state marker lost the colour of its conclusion: %q", got)
	}
	// A highlight that changed a row's width would push every column beside it.
	if layout.Width(got) != layout.Width(plain) {
		t.Errorf("the highlighted row is %d cells and the plain one %d", layout.Width(got), layout.Width(plain))
	}

	// A row wider than the space it is given is cut to fit, without counting the
	// marker's own cells against it.
	long := tableIndent + strings.Repeat("x", 80)
	if width := layout.Width(selectedRow(theme, long, 30)); width != 30 {
		t.Errorf("a truncated row is %d cells wide, want 30", width)
	}
}

// TestSelectedRowLeavesAPlainThemeUncoloured keeps the golden renders and the
// "no styling" contract intact: a theme with no colours adds bold and nothing
// else.
func TestSelectedRowLeavesAPlainThemeUncoloured(t *testing.T) {
	got := selectedRow(PlainTheme(), tableIndent+"octocat", 40)
	if strings.Contains(got, "38;") || strings.Contains(got, "48;") {
		t.Errorf("a plain theme emitted a colour: %q", got)
	}
	if !strings.Contains(got, selectMarker+"octocat") {
		t.Errorf("the marker is missing: %q", got)
	}
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("the highlight is not bold: %q", got)
	}
}

// TestHeaderCarriesNoWordmark pins the header the review asked for: the trail
// names the screen on the left and the account is the last thing on the row,
// and nothing above them names the program, which the splash and the window title
// already do.
func TestHeaderCarriesNoWordmark(t *testing.T) {
	m := readyModel(t, ghServer(t, defaultFixtures()), 80, 24)
	first := strings.Split(m.View().Content, "\n")[0]

	if strings.Contains(strings.ToLower(first), "ghspector") {
		t.Errorf("the header still carries the product name: %q", first)
	}
	if !strings.Contains(first, "Pull Requests") {
		t.Errorf("the header lost the trail: %q", first)
	}
	// The band is painted to the full width, so the account is the last content
	// on the row rather than the last cell.
	content := strings.TrimRight(ansi.Strip(first), " ")
	if !strings.HasSuffix(content, "Account: octocat") {
		t.Errorf("the account is not the right-hand end of the band: %q", first)
	}
}

// TestHeaderPutsTheAccountAtTheRightEdge covers the band with a rate limit on it,
// which no fixture normally provides: the count is written before the account, so
// a count that changes width — a remaining request crossing a power of ten, or
// the reset time appearing — does not move the name it belongs to.
func TestHeaderPutsTheAccountAtTheRightEdge(t *testing.T) {
	fixtures := defaultFixtures()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		body, ok := fixtures[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no fixture"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	band := strings.TrimRight(ansi.Strip(strings.Split(readyModel(t, srv, 100, 30).View().Content, "\n")[0]), " ")

	if !strings.HasPrefix(band, "  Pull Requests") {
		t.Errorf("the trail is not at the left edge: %q", band)
	}
	if !strings.HasSuffix(band, "Account: octocat") {
		t.Errorf("the account is not the right-hand end of the band: %q", band)
	}
	rate := strings.Index(band, "Rate limit: 4999/5000")
	if rate < 0 {
		t.Fatalf("the band is missing the rate limit: %q", band)
	}
	if account := strings.Index(band, "Account:"); rate > account {
		t.Errorf("the rate limit is written after the account, so the count moves it: %q", band)
	}
}

// TestHeaderCountsDownTheRateLimitReset covers the exhausted budget: the header
// says how long is left, measured on the API's clock. The fixtures answer the
// identity requests in order, so the last header of the sequence — the one the
// band is drawn from — carries the exhausted budget.
func TestHeaderCountsDownTheRateLimitReset(t *testing.T) {
	fixtures := defaultFixtures()
	reset := time.Now().Add(12 * time.Minute)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		if r.URL.Path == "/user/orgs" {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			// The server's clock runs a minute ahead of this machine's, and the
			// countdown has to follow the server.
			w.Header().Set("Date", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat))
		}
		body, ok := fixtures[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no fixture"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	band := ansi.Strip(strings.Split(readyModel(t, srv, 120, 30).View().Content, "\n")[0])
	if !strings.Contains(band, "Rate limit: 0/5000") {
		t.Fatalf("the band does not show the exhausted budget: %q", band)
	}
	if !strings.Contains(band, "resets in 11m") {
		t.Errorf("the band does not count down on the server's clock: %q", band)
	}
}

// TestRateLimitBandColours covers the indicator's three bands: plenty is the
// normal colour, low is amber, and exhausted is red.
func TestRateLimitBandColours(t *testing.T) {
	theme := DarkTheme()
	for _, tc := range []struct {
		name      string
		remaining int
		want      color.Color
	}{
		{"plenty", 4999, theme.StatusOK},
		{"at the warning line", rateWarnBelow - 1, theme.StatusWarn},
		{"at the failure line", rateFailBelow - 1, theme.StatusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Limit", "5000")
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(tc.remaining))
				_, _ = w.Write([]byte(`{"login":"octocat","id":1}`))
			}))
			t.Cleanup(srv.Close)

			st := testStore(srv)
			if _, err := st.Viewer(context.Background()); err != nil {
				t.Fatalf("the fixture server did not answer: %v", err)
			}

			text := "Rate limit: " + strconv.Itoa(tc.remaining) + "/5000"
			want := style(tc.want, normal).Render(text)
			if got := renderRateLimit(Deps{Store: st, Theme: theme}); got != want {
				t.Errorf("renderRateLimit = %q, want %q", got, want)
			}
		})
	}
}

// contrastRatio is the WCAG contrast ratio between two colours.
func contrastRatio(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// luminance is the WCAG relative luminance of a colour.
func luminance(c color.Color) float64 {
	if c == nil {
		return 0
	}
	r, g, b, _ := c.RGBA()
	return 0.2126*linearise(float64(r)/65535) +
		0.7152*linearise(float64(g)/65535) +
		0.0722*linearise(float64(b)/65535)
}

// linearise converts a channel to linear light, the step the WCAG formula takes
// before weighting the channels.
func linearise(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}
