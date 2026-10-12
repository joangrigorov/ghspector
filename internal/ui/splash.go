package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"ghspector/internal/layout"
	"ghspector/internal/ui/widgets"
)

// catLogo is the mascot shown while the session starts. It is kept as literal
// art rather than box-drawing so it renders on every terminal.
const catLogo = `
      /\___/\
     / ⌐■_■ \
    (  = ~ = )
     \______/
`

// renderSplash draws the startup screen: the mascot, the name, the loading
// message, and where the credentials came from.
func (m Model) renderSplash(width, height int) string {
	theme := m.deps.Theme

	var sb strings.Builder
	for _, line := range strings.Split(strings.Trim(catLogo, "\n"), "\n") {
		sb.WriteString(style(theme.SplashLogo, normal).Render(line))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(style(theme.SplashLogo, bold).Render("g h s p e c t o r"))
	sb.WriteString("\n\n")
	sb.WriteString(style(theme.SplashText, normal).Render("Resolving account" + widgets.Dots(m.tick)))
	sb.WriteString("\n")

	facts := m.splashFacts()
	if facts != "" {
		sb.WriteString("\n")
		sb.WriteString(style(theme.Muted, normal).Render(facts))
		sb.WriteString("\n")
	}

	placed := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, sb.String())
	return layout.Fit(placed, width, height)
}

// splashFacts describes the build and the credential source. It never includes
// an absolute path, which is why the config path arrives as a basename.
func (m Model) splashFacts() string {
	var parts []string
	if m.deps.Version != "" {
		parts = append(parts, "v"+m.deps.Version)
	}
	if m.deps.TokenSource != "" {
		parts = append(parts, "token: "+m.deps.TokenSource)
	}
	if m.deps.ConfigPath != "" {
		parts = append(parts, "config: "+m.deps.ConfigPath)
	}
	return strings.Join(parts, "  ·  ")
}
