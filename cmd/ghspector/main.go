package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run wires the dependencies and starts the program. It returns the process exit
// code so the deferred context cancel still runs.
//
// The arguments and the version writer are parameters rather than os.Args and
// stdio so the flag handling can be tested without a terminal: the version path
// returns before anything touches the network or the screen.
func run(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("ghspector", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	versionFlag := flags.Bool("v", false, "Print version information and exit")
	versionLongFlag := flags.Bool("version", false, "Print version information and exit")
	orgFlag := flags.String("org", "", "GitHub organization to view by default")
	userFlag := flags.String("user", "", "GitHub user account to view by default")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *versionFlag || *versionLongFlag {
		printVersion(stdout)
		return 0
	}

	token, config, err := auth.ResolveToken()
	if err != nil {
		auth.PrintAuthInstructions()
		return 1
	}

	applyFlagOverrides(config, *orgFlag, *userFlag)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	theme := ui.DarkTheme()
	if !lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		theme = ui.LightTheme()
	}

	polling := config.PollingIntervals()

	model := ui.New(ui.Options{
		Store:           store.NewREST(token, ""),
		Theme:           theme,
		Context:         ctx,
		Version:         version,
		ConfigPath:      configLabel(),
		TokenSource:     config.TokenSource,
		DefaultOwner:    defaultOwner(config),
		PollWorkflows:   polling.Workflows,
		PollPRs:         polling.PRs,
		MergeMethod:     config.DefaultMergeMethod,
		SaveMergeMethod: auth.SetDefaultMergeMethod,
		OverlayChrome:   overlayChrome(theme),
	})

	if _, err := tea.NewProgram(model, tea.WithContext(ctx)).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running ghspector: %v\n", err)
		return 1
	}
	return 0
}

// printVersion writes the three build stamps the release pipeline injects
// through ldflags. A banner that cannot be written is not actionable, so the
// write error is dropped deliberately.
func printVersion(w io.Writer) {
	_, _ = fmt.Fprintf(w, "ghspector %s\ncommit: %s\nbuild date: %s\n", version, commit, date)
}

// applyFlagOverrides applies -org and -user. At most one wins: choosing an
// organization and choosing a user account are the same choice, so setting one
// clears the other.
func applyFlagOverrides(cfg *auth.Config, org, user string) {
	switch {
	case org != "":
		cfg.DefaultOrg, cfg.DefaultAccount = org, ""
	case user != "":
		cfg.DefaultAccount, cfg.DefaultOrg = user, ""
	}
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
