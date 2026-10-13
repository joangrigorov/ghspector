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
