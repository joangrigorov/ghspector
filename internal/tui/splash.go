package tui

import (
	"strings"
)

const catLogo = `
      /\___/\
     / ⌐■_■ \
    (  = ~ = )
     \______/
`

// RenderSplash returns a string representation of the loading splash screen.
func RenderSplash(theme *Theme, loadingMsg string, tickCount int) string {
	var sb strings.Builder
	sb.WriteString("\n\n\n")

	// Center-aligned cat logo
	catLines := strings.Split(strings.Trim(catLogo, "\n"), "\n")
	for _, line := range catLines {
		sb.WriteString("       " + theme.CatGlasses.Render(line) + "\n")
	}
	sb.WriteString("\n")

	// Title logo in smallcaps
	logo := "    g h s p e c t o r"
	sb.WriteString("    " + theme.LogoText.Render(logo) + "\n\n")

	// Animated dots for the loading message
	dots := strings.Repeat(".", (tickCount%4)+1)
	sb.WriteString("      " + theme.HelpDesc.Render(loadingMsg+dots) + "\n")

	return sb.String()
}
