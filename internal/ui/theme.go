// Package ui is the presentation layer: the root model, the navigation stack,
// the routes, and the chrome.
//
// Two rules keep it predictable, and both are enforced by tests rather than
// convention:
//
//   - No layout arithmetic. A route receives a layout.Rect and renders inside
//     it. Nothing computes height minus twelve.
//   - No key literals. A route receives a keymap.Action, resolved once by the
//     root from the active scope. Nothing switches on msg.String().
package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme holds every style the UI uses. A theme with nil colours renders
// identically on any terminal, which is what golden tests rely on.
//
// Two rules keep a screen readable on either terminal. Every colour is chosen
// against the background its theme is built for, so a palette swap has to
// accompany a terminal that changed its background (the model does that on
// tea.BackgroundColorMsg). And no colour is the only signal for a state: the
// highlighted row carries a marker as well as a colour, and a status carries a
// glyph as well as green, amber or red.
type Theme struct {
	// Chrome.
	HeaderBackground color.Color
	HeaderTitle      color.Color
	HeaderContext    color.Color
	HeaderRule       color.Color
	StatusOK         color.Color
	StatusWarn       color.Color
	StatusFail       color.Color

	// Content.
	Title  color.Color
	Text   color.Color
	Muted  color.Color
	Accent color.Color

	// AuthorTints are the washes the conversation paints behind a contribution,
	// one shade per commenter: this palette's accent mixed into this palette's
	// background, in even steps, so the shades are one family and stay on the
	// side of the background this palette was built for. An empty slice paints
	// nothing, which is what keeps a plain render free of escape sequences.
	AuthorTints []color.Color

	// Footer.
	KeyHint color.Color
	KeyDesc color.Color

	// Errors.
	ErrorTitle color.Color
	ErrorBody  color.Color

	// Splash.
	SplashLogo color.Color
	SplashText color.Color

	// Modal chrome, passed through to the overlay stack.
	OverlayBorder color.Color
	OverlayTitle  color.Color
	OverlayBody   color.Color

	// Hyperlinks enables OSC 8 links on the names that point at a GitHub page.
	// It is a capability, not a colour: a plain theme emits no escape sequences
	// at all, so golden renders and the "no styling" contract hold. A terminal
	// without OSC 8 support shows the text and ignores the sequence, which is
	// why this is on by default rather than opt-in.
	Hyperlinks bool

	// Dark reports which background the palette was built for. It is not a
	// colour and no rule uses it for one: the markdown renderer needs to know
	// which side of the accent contrast it is on, and that question has no
	// answer in a set of colours.
	Dark bool
}

// Colored reports whether the theme paints at all. A plain theme renders
// identically on every terminal, which is what golden tests and the markdown
// renderer's plain style both rely on.
func (t Theme) Colored() bool { return t.Text != nil }

// ThemeFor returns the theme for a terminal background. It is the one place the
// decision lives, so a background change and the first draw cannot disagree.
func ThemeFor(dark bool) Theme {
	if dark {
		return DarkTheme()
	}
	return LightTheme()
}

// The three colours a rendered body draws with, named once because two readers
// need them: the palette, and the markdown style, which glamour takes as strings
// rather than as parsed colours. A literal repeated in two places is a hue that
// can drift between a pane title and a heading in a body.
const (
	darkAccent = "#87d7ff"
	darkText   = "#e4e4e4"
	darkMuted  = "#9e9e9e"

	lightAccent = "#005f87"
	lightText   = "#262626"
	lightMuted  = "#6c6c6c"
)

// DarkTheme returns the theme for a dark terminal background.
//
// The accent is a sky blue: it carries the header trail, the pane that owns the
// keyboard, key hints and the highlighted row, and it holds its contrast on the
// dark chrome without competing with the green, amber and red status markers.
func DarkTheme() Theme {
	return Theme{
		HeaderBackground: lipgloss.Color("#303030"),
		HeaderTitle:      lipgloss.Color("#5fd7ff"),
		HeaderContext:    lipgloss.Color(darkAccent),
		HeaderRule:       lipgloss.Color("#3a3a3a"),
		StatusOK:         lipgloss.Color("#5fd75f"),
		StatusWarn:       lipgloss.Color("#d7af00"),
		StatusFail:       lipgloss.Color("#df5f5f"),

		Title:  lipgloss.Color("#5fd7ff"),
		Text:   lipgloss.Color(darkText),
		Muted:  lipgloss.Color(darkMuted),
		Accent: lipgloss.Color(darkAccent),

		AuthorTints: authorTints(true, lipgloss.Color("#000000"), lipgloss.Color(darkAccent)),

		KeyHint: lipgloss.Color("#5fd7ff"),
		KeyDesc: lipgloss.Color(darkMuted),

		ErrorTitle: lipgloss.Color("#df5f5f"),
		ErrorBody:  lipgloss.Color(darkText),

		SplashLogo: lipgloss.Color("#5fd7ff"),
		SplashText: lipgloss.Color(darkMuted),

		OverlayBorder: lipgloss.Color("#3a3a3a"),
		OverlayTitle:  lipgloss.Color("#5fd7ff"),
		OverlayBody:   lipgloss.Color(darkText),

		Hyperlinks: true,
		Dark:       true,
	}
}

// LightTheme returns the theme for a light terminal background. The accent is
// the same hue as the dark theme's, deep enough to hold its contrast on white.
func LightTheme() Theme {
	return Theme{
		HeaderBackground: lipgloss.Color("#eaeaea"),
		HeaderTitle:      lipgloss.Color(lightAccent),
		HeaderContext:    lipgloss.Color(lightAccent),
		HeaderRule:       lipgloss.Color("#d7d7d7"),
		StatusOK:         lipgloss.Color("#008700"),
		StatusWarn:       lipgloss.Color("#af8700"),
		StatusFail:       lipgloss.Color("#af0000"),

		Title:  lipgloss.Color(lightAccent),
		Text:   lipgloss.Color(lightText),
		Muted:  lipgloss.Color(lightMuted),
		Accent: lipgloss.Color(lightAccent),

		AuthorTints: authorTints(false, lipgloss.Color("#ffffff"), lipgloss.Color(lightAccent)),

		KeyHint: lipgloss.Color(lightAccent),
		KeyDesc: lipgloss.Color(lightMuted),

		ErrorTitle: lipgloss.Color("#af0000"),
		ErrorBody:  lipgloss.Color(lightText),

		SplashLogo: lipgloss.Color(lightAccent),
		SplashText: lipgloss.Color(lightMuted),

		OverlayBorder: lipgloss.Color("#d7d7d7"),
		OverlayTitle:  lipgloss.Color(lightAccent),
		OverlayBody:   lipgloss.Color(lightText),

		Hyperlinks: true,
	}
}

// PlainTheme returns a theme with no colours, for deterministic rendering.
func PlainTheme() Theme { return Theme{} }

// style builds a style and applies a colour only when it is set, so a plain
// theme emits no escape sequences at all.
func style(c color.Color, opts ...func(lipgloss.Style) lipgloss.Style) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c != nil {
		s = s.Foreground(c)
	}
	for _, opt := range opts {
		s = opt(s)
	}
	return s
}
