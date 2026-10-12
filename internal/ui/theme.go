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
	Title              color.Color
	Text               color.Color
	Muted              color.Color
	Accent             color.Color
	Selected           color.Color
	SelectedBackground color.Color

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
}

// DarkTheme returns the theme for a dark terminal background.
func DarkTheme() Theme {
	return Theme{
		HeaderBackground: lipgloss.Color("#303030"),
		HeaderTitle:      lipgloss.Color("#d787ff"),
		HeaderContext:    lipgloss.Color("#87d7ff"),
		HeaderRule:       lipgloss.Color("#3a3a3a"),
		StatusOK:         lipgloss.Color("#00af00"),
		StatusWarn:       lipgloss.Color("#d7af00"),
		StatusFail:       lipgloss.Color("#df0000"),

		Title:              lipgloss.Color("#d787ff"),
		Text:               lipgloss.Color("#e4e4e4"),
		Muted:              lipgloss.Color("#767676"),
		Accent:             lipgloss.Color("#87d7ff"),
		Selected:           lipgloss.Color("#ffffff"),
		SelectedBackground: lipgloss.Color("#5f00af"),

		KeyHint: lipgloss.Color("#d787ff"),
		KeyDesc: lipgloss.Color("#767676"),

		ErrorTitle: lipgloss.Color("#df0000"),
		ErrorBody:  lipgloss.Color("#e4e4e4"),

		SplashLogo: lipgloss.Color("#d787ff"),
		SplashText: lipgloss.Color("#767676"),

		OverlayBorder: lipgloss.Color("#3a3a3a"),
		OverlayTitle:  lipgloss.Color("#d787ff"),
		OverlayBody:   lipgloss.Color("#e4e4e4"),
	}
}

// LightTheme returns the theme for a light terminal background.
func LightTheme() Theme {
	return Theme{
		HeaderBackground: lipgloss.Color("#eaeaea"),
		HeaderTitle:      lipgloss.Color("#5f00af"),
		HeaderContext:    lipgloss.Color("#005f87"),
		HeaderRule:       lipgloss.Color("#d7d7d7"),
		StatusOK:         lipgloss.Color("#008700"),
		StatusWarn:       lipgloss.Color("#af8700"),
		StatusFail:       lipgloss.Color("#af0000"),

		Title:              lipgloss.Color("#5f00af"),
		Text:               lipgloss.Color("#262626"),
		Muted:              lipgloss.Color("#8a8a8a"),
		Accent:             lipgloss.Color("#005f87"),
		Selected:           lipgloss.Color("#ffffff"),
		SelectedBackground: lipgloss.Color("#5f00af"),

		KeyHint: lipgloss.Color("#5f00af"),
		KeyDesc: lipgloss.Color("#8a8a8a"),

		ErrorTitle: lipgloss.Color("#af0000"),
		ErrorBody:  lipgloss.Color("#262626"),

		SplashLogo: lipgloss.Color("#5f00af"),
		SplashText: lipgloss.Color("#8a8a8a"),

		OverlayBorder: lipgloss.Color("#d7d7d7"),
		OverlayTitle:  lipgloss.Color("#5f00af"),
		OverlayBody:   lipgloss.Color("#262626"),
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
