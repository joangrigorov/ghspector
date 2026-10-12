package ui

import (
	"image/color"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/layout"
	"github.com/joangrigorov/ghspector/internal/store"
)

// markdownDoc is a body that exercises the structures a real description has.
const markdownDoc = "# Heading\n\nSome **bold** text and a list:\n\n- one\n- two\n\n> quoted\n"

func TestMarkdownPaintsWithThePalette(t *testing.T) {
	plain := strings.Join(NewMarkdown().Lines(PlainTheme(), markdownDoc, 40), "\n")
	if strings.Contains(plain, "\x1b") {
		t.Errorf("a plain palette rendered escape sequences:\n%q", plain)
	}
	if !strings.Contains(plain, "# Heading") {
		t.Errorf("the plain render lost the heading marker:\n%q", plain)
	}

	styled := strings.Join(NewMarkdown().Lines(DarkTheme(), markdownDoc, 40), "\n")
	if !strings.Contains(styled, sgrColour(darkAccent)) {
		t.Errorf("the dark render does not use the accent:\n%q", styled)
	}
	if strings.Contains(styled, "**bold**") {
		t.Errorf("bold kept its markers with a colour available:\n%q", styled)
	}
	if !strings.Contains(ansi.Strip(styled), "bold") {
		t.Errorf("bold text went missing:\n%q", styled)
	}
}

// TestMarkdownUsesOnlyPaletteColours is the rule the palette review asked for:
// a body is drawn in colours the rest of the screen already uses. The standard
// dark style, which this is not, paints its top-level headings on a purple bar.
func TestMarkdownUsesOnlyPaletteColours(t *testing.T) {
	out := strings.Join(NewMarkdown().Lines(DarkTheme(), markdownDoc, 40), "\n")
	colours := sgrColours(out)
	if len(colours) == 0 {
		t.Fatal("no colour reached the render")
	}
	allowed := paletteRGBAs(DarkTheme())
	for _, c := range colours {
		if !allowed[c] {
			t.Errorf("the render used %s, which is not in the palette", c)
		}
	}
}

func TestMarkdownRebuildsOnlyWhenThePaletteOrWidthChanges(t *testing.T) {
	m := NewMarkdown()
	m.Prepare(DarkTheme(), 80)
	if m.builds != 1 {
		t.Fatalf("Prepare built the renderer %d times, want 1", m.builds)
	}

	for i := 0; i < 5; i++ {
		m.Lines(DarkTheme(), markdownDoc, 80)
	}
	if m.builds != 1 {
		t.Errorf("five frames built %d renderers, want the one that was prepared", m.builds)
	}

	m.Lines(DarkTheme(), markdownDoc, 60)
	if m.builds != 2 {
		t.Errorf("a new width built %d renderers in total, want 2", m.builds)
	}
	m.Lines(LightTheme(), markdownDoc, 60)
	if m.builds != 3 {
		t.Errorf("a new palette built %d renderers in total, want 3", m.builds)
	}
	for i := 0; i < 3; i++ {
		m.Lines(LightTheme(), markdownDoc, 60)
	}
	if m.builds != 3 {
		t.Errorf("the same pair built again: %d renderers in total", m.builds)
	}
}

func TestMarkdownKeepsTerminalInstructionsOut(t *testing.T) {
	body := "before \x1b]52;c;cGF5bG9hZA==\x07 and \x1b[2Jafter"
	out := strings.Join(NewMarkdown().Lines(DarkTheme(), body, 60), "\n")
	if strings.Contains(out, "52;c;") || strings.Contains(out, "\x1b[2J") {
		t.Errorf("a terminal instruction survived rendering:\n%q", out)
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "before") || !strings.Contains(plain, "and") {
		t.Errorf("the text around an instruction went missing:\n%q", plain)
	}
}

// TestMarkdownLinesCarryNoPad pins the shape of a rendered line: a renderer pads
// every line to its wrap width, and the pad is cells the layout draws itself.
func TestMarkdownLinesCarryNoPad(t *testing.T) {
	lines := NewMarkdown().Lines(DarkTheme(), "one short line\n", 60)
	if len(lines) == 0 {
		t.Fatal("no lines rendered")
	}
	for _, line := range lines {
		cells := ansi.StringWidth(line)
		if cells == 0 {
			continue
		}
		// The escapes a line keeps are the ones its own runs need. A padded
		// line carries two sequences per blank cell, which is what this bound
		// separates.
		if len(line) > 8*cells+16 {
			t.Errorf("%d bytes for %d cells, which is mostly pad: %q", len(line), cells, line)
		}
	}
}

func TestMarkdownWithoutAHyperlinkCapabilityStripsTheTokens(t *testing.T) {
	body := "See [the docs](https://example.com/docs)."
	theme := DarkTheme()
	theme.Hyperlinks = false

	plainOut := strings.Join(NewMarkdown().Lines(theme, body, 60), "\n")
	if strings.Contains(plainOut, "\x1b]8;") {
		t.Errorf("a theme without hyperlinks rendered an OSC 8 token:\n%q", plainOut)
	}
	if !strings.Contains(ansi.Strip(plainOut), "https://example.com/docs") {
		t.Errorf("the URL went missing with the token:\n%q", ansi.Strip(plainOut))
	}

	linkedOut := strings.Join(NewMarkdown().Lines(DarkTheme(), body, 60), "\n")
	if !strings.Contains(linkedOut, "\x1b]8;") {
		t.Errorf("a theme with hyperlinks lost the OSC 8 token:\n%q", linkedOut)
	}
}

func TestMarkdownWithoutARendererFallsBackToWrapping(t *testing.T) {
	var m *Markdown
	lines := m.Lines(DarkTheme(), "a body that wraps", 8)
	if len(lines) < 2 {
		t.Fatalf("the fallback did not wrap: %q", lines)
	}
	for _, line := range lines {
		if layout.Width(line) > 8 {
			t.Errorf("the fallback line %q is wider than 8 cells", line)
		}
	}
}

// TestPRDescriptionIsRenderedAsMarkdown proves the wiring: the description pane
// draws a rendered body, not the wrapped source.
func TestPRDescriptionIsRenderedAsMarkdown(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	details := prDetailsRoute(t, m)
	details.pr.Body = "# Summary\n\n- first point\n- second point\n"

	plain := strings.Join(details.descriptionPane(m.deps, 60, 20), "\n")
	for _, want := range []string{"# Summary", "• first point", "• second point"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the description pane is missing %q:\n%s", want, plain)
		}
	}

	deps := m.deps
	deps.Theme = DarkTheme()
	deps.Markdown.Prepare(deps.Theme, 60)
	styled := strings.Join(details.descriptionPane(deps, 60, 20), "\n")
	if !strings.Contains(styled, sgrColour(darkAccent)) {
		t.Errorf("a coloured description is not drawn in the palette:\n%q", styled)
	}
}

// TestPRDescriptionScrollsOverItsRenderedBody keeps the pane and the scroll
// bound on the same lines: the bound is what the pane draws, at the width the
// pane has.
func TestPRDescriptionScrollsOverItsRenderedBody(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	details := prDetailsRoute(t, m)
	details.pr.Body = strings.Repeat("- a bullet point in a list\n", 40)

	details.descScroll = 1 << 20
	details.clamp(m.deps)

	lines := details.descriptionLines(m.deps, details.bodyWidth(m.deps))
	if details.descScroll != len(lines)-1 {
		t.Errorf("the scroll bound is %d, want the last of the %d rendered lines", details.descScroll, len(lines))
	}
	pane := details.descriptionPane(m.deps, details.bodyWidth(m.deps), 20)
	if last := lines[len(lines)-1]; !strings.Contains(strings.Join(pane, "\n"), last) {
		t.Errorf("the last rendered line %q is not in the pane:\n%s", last, strings.Join(pane, "\n"))
	}
}

// TestCommitDetailsShowsTheMessageBody covers the third body the app renders,
// and the geometry the extra block must not break.
func TestCommitDetailsShowsTheMessageBody(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	deps := m.deps

	commit := store.RepositoryCommit{SHA: "abcdef1234567890"}
	commit.Commit.Message = "fix(tui): the thing\n\nThe body explains **why** it was wrong,\nand what changed."
	commit.Commit.Author.Name = "octocat"
	commit.Commit.Author.Date = workflowNow
	route := &CommitDetails{
		ref:    store.RepoRef{Owner: "octocat", Repo: "hello"},
		commit: commit,
		files:  []store.CommitFile{{Filename: "main.go", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new\n"}},
		sess:   newSession(deps.Ctx),
	}

	for _, r := range []layout.Rect{{W: 80, H: 24}, {W: 120, H: 40}, {W: 52, H: 13}, {W: 200, H: 60}} {
		out := route.View(deps, r)
		lines := strings.Split(out, "\n")
		if len(lines) != r.H {
			t.Fatalf("%dx%d: %d lines", r.W, r.H, len(lines))
		}
		for i, line := range lines {
			if got := layout.Width(line); got != r.W {
				t.Fatalf("%dx%d: line %d is %d cells: %q", r.W, r.H, i, got, line)
			}
		}
	}

	view := route.View(deps, layout.Rect{W: 120, H: 40})
	if !strings.Contains(view, "The body explains") {
		t.Errorf("the commit message body is not on screen:\n%s", view)
	}
	if !strings.Contains(view, "main.go") {
		t.Errorf("the message took the file list's rows:\n%s", view)
	}
}

// TestCommitDetailsWithoutABodyKeepsTheRegion pins the common case: a one-line
// commit message leaves every row below the heading to the files.
func TestCommitDetailsWithoutABodyKeepsTheRegion(t *testing.T) {
	srv := prServer(t)
	m := openPRDetails(t, prsModel(t, srv, 120, 40))
	deps := m.deps

	commit := store.RepositoryCommit{SHA: "abcdef1234567890"}
	commit.Commit.Message = "fix(tui): one line only"
	route := &CommitDetails{
		ref:    store.RepoRef{Owner: "octocat", Repo: "hello"},
		commit: commit,
		files:  []store.CommitFile{{Filename: "main.go", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new\n"}},
		sess:   newSession(deps.Ctx),
	}
	if got := route.messageLines(deps, 100); got != nil {
		t.Errorf("a one-line message produced %q", got)
	}
	body := layout.Rect{W: 120, H: 40}.Inner(0, 0)
	if view := route.View(deps, body); !strings.Contains(view, "FILES & DIFF") && !strings.Contains(view, "main.go") {
		t.Errorf("the file list is missing:\n%s", view)
	}
}

// sgrColour is the sequence that sets a true-colour foreground.
func sgrColour(hex string) string {
	r, g, b, _ := lipgloss.Color(hex).RGBA()
	return "\x1b[38;2;" + rgbString(r, g, b)
}

// sgrColours returns every foreground triple an SGR sequence in s sets.
func sgrColours(s string) []string {
	var out []string
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '\x1b' || s[i+1] != '[' {
			continue
		}
		end := strings.IndexByte(s[i:], 'm')
		if end < 0 {
			break
		}
		params := strings.Split(s[i+2:i+end], ";")
		if len(params) >= 5 && params[0] == "38" && params[1] == "2" {
			out = append(out, strings.Join(params[2:5], ";"))
		}
		i += end
	}
	return out
}

// paletteRGBAs is every colour a theme draws with, as the triple an SGR
// sequence would write.
func paletteRGBAs(theme Theme) map[string]bool {
	out := map[string]bool{}
	v := reflect.ValueOf(theme)
	for i := 0; i < v.NumField(); i++ {
		c, ok := v.Field(i).Interface().(color.Color)
		if !ok || c == nil {
			continue
		}
		r, g, b, _ := c.RGBA()
		out[rgbString(r, g, b)] = true
	}
	return out
}

// rgbString is a premultiplied 16-bit colour as the "r;g;b" an SGR sequence
// carries.
func rgbString(r, g, b uint32) string {
	return strings.Join([]string{
		strconv.Itoa(int(r >> 8)),
		strconv.Itoa(int(g >> 8)),
		strconv.Itoa(int(b >> 8)),
	}, ";")
}
