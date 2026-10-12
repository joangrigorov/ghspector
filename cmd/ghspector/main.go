package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ghspector/internal/auth"
	"ghspector/internal/overlay"
	"ghspector/internal/store"
	"ghspector/internal/ui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run())
}

// run wires the dependencies and starts the program. It returns the process exit
// code so the deferred context cancel still runs.
func run() int {
	versionFlag := flag.Bool("v", false, "Print version information and exit")
	versionLongFlag := flag.Bool("version", false, "Print version information and exit")
	orgFlag := flag.String("org", "", "GitHub organization to view by default")
	userFlag := flag.String("user", "", "GitHub user account to view by default")
	flag.Parse()

	if *versionFlag || *versionLongFlag {
		fmt.Printf("ghspector %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("build date: %s\n", date)
		return 0
	}

	token, config, err := auth.ResolveToken()
	if err != nil {
		auth.PrintAuthInstructions()
		return 1
	}

	// Flags override the configured defaults.
	switch {
	case *orgFlag != "":
		config.DefaultOrg, config.DefaultAccount = *orgFlag, ""
	case *userFlag != "":
		config.DefaultAccount, config.DefaultOrg = *userFlag, ""
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	theme := ui.DarkTheme()
	if !lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		theme = ui.LightTheme()
	}

	polling := config.PollingIntervals()

	model := ui.New(ui.Options{
		Store:         store.NewREST(token, ""),
		Theme:         theme,
		Context:       ctx,
		Version:       version,
		ConfigPath:    configLabel(),
		TokenSource:   config.TokenSource,
		DefaultOwner:  defaultOwner(config),
		PollWorkflows: polling.Workflows,
		PollPRs:       polling.PRs,
		OverlayChrome: overlayChrome(theme),
	})

	if _, err := tea.NewProgram(model, tea.WithContext(ctx)).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running ghspector: %v\n", err)
		return 1
	}
	return 0
}

// defaultOwner turns the configured default into the context the session starts
// in. Nil means "the authenticated user".
func defaultOwner(cfg *auth.Config) *store.OwnerRef {
	switch {
	case cfg.DefaultOrg != "":
		return &store.OwnerRef{Login: cfg.DefaultOrg, IsOrg: true}
	case cfg.DefaultAccount != "":
		return &store.OwnerRef{Login: cfg.DefaultAccount}
	default:
		return nil
	}
}

// configLabel is the config file name shown on the splash, or empty when no
// config file exists. Only the base name is used, so a screen never prints the
// user's home directory.
func configLabel() string {
	path, err := auth.ResolveConfigPath()
	if err != nil {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return filepath.Base(path)
}

// overlayChrome maps the theme onto the modal chrome.
func overlayChrome(theme ui.Theme) overlay.Chrome {
	c := overlay.DefaultChrome()
	c.BorderColor = theme.OverlayBorder
	c.TitleColor = theme.OverlayTitle
	c.BodyColor = theme.OverlayBody
	return c
}
