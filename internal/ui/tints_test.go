package ui

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestAuthorWashesStayInOneFamily covers the wash behind a contribution: every
// shade is the palette's own background with a little of its own accent in it,
// the shades walk in one direction, each further shade carries more, and none
// carries so much that the text on top loses its contrast.
func TestAuthorWashesStayInOneFamily(t *testing.T) {
	if got := len(PlainTheme().AuthorTints); got != 0 {
		t.Errorf("a palette with no colours carries %d washes, want none", got)
	}

	for _, tc := range []struct {
		name       string
		theme      Theme
		background color.Color
		accent     color.Color
	}{
		{"dark", DarkTheme(), lipgloss.Color("#000000"), lipgloss.Color(darkAccent)},
		{"light", LightTheme(), lipgloss.Color("#ffffff"), lipgloss.Color(lightAccent)},
	} {
		ramp := tc.theme.AuthorTints
		if len(ramp) != tintCount {
			t.Fatalf("%s: %d washes, want %d", tc.name, len(ramp), tintCount)
		}
		base := channels(t, tc.background)
		accent := channels(t, tc.accent)

		seen := make(map[color.Color]bool, len(ramp))
		previous := 0.0
		for i, c := range ramp {
			rgba, ok := c.(color.RGBA)
			if !ok {
				t.Fatalf("%s: wash %d is %T, want a colour the paint knows", tc.name, i, c)
			}
			gain := [3]uint8{rgba.R, rgba.G, rgba.B}
			if seen[c] {
				t.Errorf("%s: wash %d repeats an earlier shade", tc.name, i)
			}
			seen[c] = true

			// The share of the accent is what a shade is: it has to be the same
			// in every channel, or the shades stop being one hue and start being
			// a rainbow.
			share := -1.0
			for ch, name := range []string{"red", "green", "blue"} {
				travel := float64(accent[ch]) - float64(base[ch])
				if travel == 0 {
					continue
				}
				got := (float64(gain[ch]) - float64(base[ch])) / travel
				switch {
				case share < 0:
					share = got
				case math.Abs(got-share) > 0.02:
					t.Errorf("%s: wash %d carries %.3f of the accent in the %s but %.3f in the red, want one hue",
						tc.name, i, share, name, got)
				}
			}
			if share <= 0 || share > 0.3 {
				t.Errorf("%s: wash %d carries %.3f of the accent, want a share above zero and quiet enough to stay subtle",
					tc.name, i, share)
			}
			if i > 0 && share <= previous {
				t.Errorf("%s: wash %d carries %.3f of the accent, want more than its neighbour's %.3f", tc.name, i, share, previous)
			}
			previous = share

			// The wash is a background, so the text on it has to survive it.
			if got := contrastRatio(tc.theme.Text, c); got < 7 {
				t.Errorf("%s: wash %d leaves the body colour a contrast of %.1f:1, want at least 7:1", tc.name, i, got)
			}
			if got := contrastRatio(tc.theme.Muted, c); got < 4 {
				t.Errorf("%s: wash %d leaves the muted colour a contrast of %.1f:1, want at least 4:1", tc.name, i, got)
			}
		}

		// The first shade sits nearest the background, so a palette's washes
		// never reach past the other end of its own surface.
		first := channels(t, ramp[0])
		last := channels(t, ramp[len(ramp)-1])
		if near(first, base) >= near(last, base) {
			t.Errorf("%s: the ramp does not start nearest the background: %v then %v", tc.name, first, last)
		}
	}
}

// TestWashesFollowTheLogin covers what makes a wash useful: it belongs to the
// person, so it survives a refresh, a different conversation and a different
// order of the same remarks — nobody's shade depends on who else is talking.
func TestWashesFollowTheLogin(t *testing.T) {
	theme := DarkTheme()
	carol := authorTint(theme, "carol")
	if carol == nil {
		t.Fatal("a coloured palette returned no wash")
	}
	for _, login := range []string{"dave", "octocat", "carol"} {
		_ = authorTint(theme, login)
	}
	if got := authorTint(theme, "carol"); got != carol {
		t.Errorf("carol's wash moved after other people spoke")
	}
	if got := authorTint(theme, "CAROL"); got != carol {
		t.Errorf("the login's case moved carol's wash")
	}

	// A sample of logins spreads over several of the shades rather than piling
	// onto one, which is what a reader of a long thread relies on.
	shades := make(map[color.Color]bool)
	for _, login := range []string{
		"alice", "bob", "carol", "dave", "erin", "frank", "grace", "heidi",
		"ivan", "judy", "mallory", "niaj", "olivia", "peggy", "rupert", "sybil",
	} {
		shades[authorTint(theme, login)] = true
	}
	if len(shades) < 4 {
		t.Errorf("sixteen logins land on %d shades, want them spread over the ramp", len(shades))
	}
}

// TestTintLinePaintsUnderTheText covers the paint itself. A styled run inside the
// line ends with a reset, and a reset clears the background as well, so the wash
// has to be re-armed after every one of them: without that it would cover the
// padding and the gaps and leave the words on the terminal's own background.
func TestTintLinePaintsUnderTheText(t *testing.T) {
	theme := DarkTheme()
	tint := authorTint(theme, "carol")
	code := paintCode(tint)
	if code == "" {
		t.Fatal("a coloured palette produced no paint")
	}

	inner := style(theme.Title, bold).Render("carol") + style(theme.Muted, normal).Render("  10:03")
	line := paintLine(tint, 40, "  "+inner)

	if !strings.HasPrefix(line, code) {
		t.Errorf("the line does not open with the wash: %q", line)
	}
	resets := strings.Count(line, "\x1b[m") + strings.Count(line, "\x1b[0m")
	armed := strings.Count(line, "\x1b[m"+code) + strings.Count(line, "\x1b[0m"+code)
	if armed != resets-1 {
		t.Errorf("%d of %d resets are followed by the wash, want all but the closing one: %q", armed, resets, line)
	}
	if got := ansi.StringWidth(line); got != 40 {
		t.Errorf("the painted line is %d cells wide, want 40", got)
	}
	if got := ansi.Strip(line); got != strings.Repeat(" ", 2)+"carol  10:03"+strings.Repeat(" ", 40-14) {
		t.Errorf("the paint changed the text: %q", got)
	}

	// A palette with no washes hands the line back untouched.
	if got := paintLine(nil, 40, "short"); got != "short" {
		t.Errorf("a palette with no washes painted %q, want the line as it was", got)
	}
}

// TestSpanPaintsTheBoxAndNothingElse covers the body wash: it is a background
// behind the cells left to right of a line and nothing else, so the indent before
// the box keeps the terminal's own colour. The box is the same on every line of a
// block, which is why a line shorter than it is padded out to it — and that
// padding sits behind a reset, so the wash has to be armed again after the reset
// for the box to be one colour rather than a colour with a hole at its end.
func TestSpanPaintsTheBoxAndNothingElse(t *testing.T) {
	theme := DarkTheme()
	tint := authorTint(theme, "carol")
	code := paintCode(tint)
	if code == "" {
		t.Fatal("a coloured palette produced no paint")
	}

	// A body line at the card's indent, carried out to a box that ends six cells
	// past its text.
	line := "  " + style(theme.Title, bold).Render("carol  10:03")
	got := paintSpan(tint, 2, 19, line)

	// Inside the box, every run that ends by clearing the background is followed by
	// the wash again; only the closing reset ends it.
	opened := got[strings.Index(got, code):]
	resets := strings.Count(opened, "\x1b[m") + strings.Count(opened, "\x1b[0m")
	armed := strings.Count(opened, "\x1b[m"+code) + strings.Count(opened, "\x1b[0m"+code)
	if armed != resets-1 {
		t.Errorf("%d of %d resets inside the box are followed by the wash, want all but the closing one: %q", armed, resets, got)
	}
	if want := "  carol  10:03" + strings.Repeat(" ", 6); ansi.Strip(got) != want {
		t.Errorf("the painted line reads %q, want the text carried out to the box %q", ansi.Strip(got), want)
	}
	// The two cells of indent before the box are not in it.
	if start := ansi.StringWidth(ansi.Strip(got[:strings.Index(got, code)])); start != 2 {
		t.Errorf("the wash opens at cell %d, want the box's own 2: %q", start, got)
	}
	// The padding is inside the wash: the line ends with the closing reset, and the
	// wash is armed right up against the padding.
	pad := strings.Repeat(" ", 6)
	if !strings.HasSuffix(got, pad+ansi.ResetStyle) {
		t.Fatalf("the box is not closed after the padding: %q", got)
	}
	if body := strings.TrimSuffix(got, pad+ansi.ResetStyle); !strings.HasSuffix(body, code) {
		t.Errorf("the padding is not behind the wash: %q", got)
	}

	// A line that reaches past the box is cut at it, so the wash never covers a
	// cell the box does not.
	if got := paintSpan(tint, 2, 6, line); ansi.Strip(got) != "  carol" {
		t.Errorf("a line past the box reads %q, want it cut at the box", ansi.Strip(got))
	}

	// A palette with no washes, and a box with no cells, both hand the line back.
	if got := paintSpan(nil, 0, 4, "abc"); got != "abc" {
		t.Errorf("a palette with no washes painted %q, want the line as it was", got)
	}
	if got := paintSpan(tint, 5, 4, "abc"); got != "abc" {
		t.Errorf("a box with no cells painted %q, want the line as it was", got)
	}
}

// channels is a colour's three channels, for a test that reasons about how far
// one colour is from another.
func channels(t *testing.T, c color.Color) [3]uint8 {
	t.Helper()
	r, g, b := channels8(c)
	return [3]uint8{r, g, b}
}

// near is how far a colour sits from a base, channel by channel.
func near(c [3]uint8, base [3]uint8) int {
	total := 0
	for i := range c {
		delta := int(c[i]) - int(base[i])
		if delta < 0 {
			delta = -delta
		}
		total += delta
	}
	return total
}
