package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"ghspector/internal/auth"
	"ghspector/internal/gh"
	"ghspector/internal/tui"
)

func main() {
	orgFlag := flag.String("org", "", "GitHub organization to view by default")
	userFlag := flag.String("user", "", "GitHub user account to view by default")
	flag.Parse()

	// 1. Resolve token and read/write config
	token, config, err := auth.ResolveToken()
	if err != nil {
		auth.PrintAuthInstructions()
		os.Exit(1)
	}

	// Override defaults with flags if provided
	if *orgFlag != "" {
		config.DefaultOrg = *orgFlag
		config.DefaultAccount = ""
	} else if *userFlag != "" {
		config.DefaultAccount = *userFlag
		config.DefaultOrg = ""
	}

	// 2. Initialize GitHub client
	client := gh.NewClient(token, "")

	// 3. Initialize TUI model
	model := tui.InitModel(client, config)

	// 4. Start Bubble Tea loop
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running ghspector: %v\n", err)
		os.Exit(1)
	}
}
