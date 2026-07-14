package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme contains all the styles used in the TUI.
type Theme struct {
	Title         lipgloss.Style
	Subtitle      lipgloss.Style
	TableHeader   lipgloss.Style
	TableRow      lipgloss.Style
	TableSelected lipgloss.Style
	Border        lipgloss.Style
	HelpKey       lipgloss.Style
	HelpDesc      lipgloss.Style
	BottomBar     lipgloss.Style
	Header         lipgloss.Style
	HeaderTitle    lipgloss.Style
	HeaderSubtitle lipgloss.Style
	HeaderBg       lipgloss.TerminalColor

	// Status Colors
	StatusRunning    lipgloss.Style
	StatusSuccessful lipgloss.Style
	StatusFailed     lipgloss.Style
	StatusQueued     lipgloss.Style
	StatusNeutral    lipgloss.Style
	StatusWaiting    lipgloss.Style

	// Custom UI styles
	CatGlasses lipgloss.Style
	LogoText   lipgloss.Style
}

// GetTheme returns the adaptive theme.
func GetTheme() *Theme {
	// Adaptive colors for light/dark terminal backgrounds
	primaryColor := lipgloss.AdaptiveColor{Light: "#5f00af", Dark: "#d787ff"}   // Purple
	secondaryColor := lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#87d7ff"} // Blue
	borderColor := lipgloss.AdaptiveColor{Light: "#d7d7d7", Dark: "#3a3a3a"}
	textColor := lipgloss.AdaptiveColor{Light: "#262626", Dark: "#e4e4e4"}
	subduedColor := lipgloss.AdaptiveColor{Light: "#8a8a8a", Dark: "#767676"}

	// Status colors
	runningColor := lipgloss.AdaptiveColor{Light: "#d75f00", Dark: "#ff8700"}    // Orange
	successColor := lipgloss.AdaptiveColor{Light: "#008700", Dark: "#00af00"}    // Green
	failedColor := lipgloss.AdaptiveColor{Light: "#af0000", Dark: "#df0000"}     // Red
	queuedColor := lipgloss.AdaptiveColor{Light: "#af8700", Dark: "#d7af00"}     // Yellow
	neutralColor := lipgloss.AdaptiveColor{Light: "#626262", Dark: "#bcbcbc"}    // Gray
	headerBg := lipgloss.AdaptiveColor{Light: "#eaeaea", Dark: "#262626"}

	return &Theme{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1),

		Subtitle: lipgloss.NewStyle().
			Foreground(subduedColor).
			Italic(true),

		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(secondaryColor).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(borderColor),

		TableRow: lipgloss.NewStyle().
			Foreground(textColor),

		TableSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(primaryColor),

		Border: lipgloss.NewStyle().
			BorderForeground(borderColor),

		HelpKey: lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true),

		HelpDesc: lipgloss.NewStyle().
			Foreground(subduedColor),

		BottomBar: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(borderColor).
			Padding(0, 1),

		// Status Styles
		StatusRunning: lipgloss.NewStyle().
			Foreground(runningColor).
			Bold(true),

		StatusSuccessful: lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true),

		StatusFailed: lipgloss.NewStyle().
			Foreground(failedColor).
			Bold(true),

		StatusQueued: lipgloss.NewStyle().
			Foreground(queuedColor).
			Bold(true),

		StatusNeutral: lipgloss.NewStyle().
			Foreground(neutralColor),

		StatusWaiting: lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true),

		// Logo Styles
		CatGlasses: lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true),

		LogoText: lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true),

		Header: lipgloss.NewStyle().
			Background(headerBg),

		HeaderTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Background(headerBg).
			Padding(0, 1),

		HeaderSubtitle: lipgloss.NewStyle().
			Foreground(subduedColor).
			Background(headerBg).
			Italic(true),

		HeaderBg: headerBg,
	}
}
